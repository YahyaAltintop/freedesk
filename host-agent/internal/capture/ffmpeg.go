// Package capture grabs the Windows desktop and encodes it to VP8 using ffmpeg
// (gdigrab + libvpx), exposing the encoded frames as a channel. This keeps the
// Go agent free of CGO/native codec dependencies — only the ffmpeg binary is
// required at runtime.
package capture

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"time"

	"github.com/pion/webrtc/v4/pkg/media/ivfreader"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/diag"
)

const (
	defaultFFmpegBinary = "ffmpeg"
	defaultFramerate    = 30
	// defaultBitrate is the constant rate the encoder is held to, in bits per
	// second. Constant rather than a target: see the rate-control arguments.
	defaultBitrate = 2_000_000

	// DefaultMaxWidth caps the encoded frame's width. gdigrab hands over the
	// whole virtual desktop, so a second monitor or a 4K screen would
	// otherwise be squeezed into the same bitrate at two to four times the
	// pixels: the encoder falls behind 30 fps, and what does arrive is mush.
	// 1080p at 2 Mbit/s stays legible; anything wider is scaled down to it,
	// aspect kept.
	DefaultMaxWidth = 1920

	ivfFrameHeaderSize = 12
	// maxElapsed caps the gap attributed to a single frame so a corrupt pts can
	// never jump the RTP clock by minutes.
	maxElapsed = 5 * time.Second

	// warmTimeout bounds the start-up dry run of ffmpeg (see Warm).
	warmTimeout = 30 * time.Second
)

// Frame is one encoded VP8 frame. Elapsed is the capture-time gap since the
// previous frame (zero for the first frame), derived from the IVF pts, so a
// late or dropped capture shows up as a wider gap instead of being hidden
// behind a constant frame rate. The writer advances the media clock by Elapsed
// BEFORE sending the frame, which keeps RTP timestamps equal to real capture
// times.
type Frame struct {
	Data    []byte
	Elapsed time.Duration
}

// Options selects the ffmpeg to run and how the desktop is encoded.
type Options struct {
	// Binary is the ffmpeg executable to run; empty picks the ffmpeg shipped
	// next to the agent, or "ffmpeg" from PATH.
	Binary string
	// MaxWidth caps the encoded width in pixels; 0 means DefaultMaxWidth.
	MaxWidth int
	// Quiet routes ffmpeg's stderr and the IVF read errors to the diagnostic
	// channel instead of the operator's activity pane. A capture that is being
	// relaunched every second — because the desktop it grabs keeps going away,
	// as with a UAC prompt on the secure desktop — would otherwise fill the
	// status window with technical lines the operator cannot act on.
	Quiet bool
}

// ScreenCapture runs ffmpeg to capture and encode the desktop.
type ScreenCapture struct {
	binary    string
	framerate int
	bitrate   int
	maxWidth  int
	quiet     bool
}

// NewScreenCapture returns a capture with sensible real-time defaults.
func NewScreenCapture(opts Options) *ScreenCapture {
	width := opts.MaxWidth
	if width <= 0 {
		width = DefaultMaxWidth
	}
	return &ScreenCapture{
		binary:    resolveBinary(opts.Binary),
		framerate: defaultFramerate,
		bitrate:   defaultBitrate,
		maxWidth:  width,
		quiet:     opts.Quiet,
	}
}

// Binary reports the ffmpeg executable this capture will run.
func (s *ScreenCapture) Binary() string { return s.binary }

// Warm runs ffmpeg once, doing nothing, and discards the result. The first
// run of a freshly downloaded ffmpeg pays for paging a 100 MB executable off
// the disk and for Windows Defender looking it over — seconds, on some
// machines — and without this that bill lands on the first viewer's first
// frame. A missing or broken ffmpeg is not reported here; the session that
// needs it says so, as before.
func (s *ScreenCapture) Warm(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, warmTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.binary, "-hide_banner", "-version")
	hideConsole(cmd)
	_ = cmd.Run()
}

// Start launches ffmpeg and returns a channel of encoded VP8 frames. Capture
// runs until ctx is cancelled or ffmpeg exits, at which point the channel is
// closed. A missing ffmpeg binary is reported as a Start error.
func (s *ScreenCapture) Start(ctx context.Context) (<-chan Frame, error) {
	cmd := exec.CommandContext(ctx, s.binary, s.args()...)
	hideConsole(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start ffmpeg (is it installed?): %w", err)
	}

	logf := log.Printf
	if s.quiet {
		logf = diag.Printf
	}
	go logStderr(stderr, logf)

	frames := make(chan Frame)
	go func() {
		defer close(frames)
		readIVF(ctx, stdout, frames, logf)
		_ = cmd.Wait()
	}()
	return frames, nil
}

func (s *ScreenCapture) args() []string {
	bitrate := strconv.Itoa(s.bitrate)
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "gdigrab",
		"-framerate", strconv.Itoa(s.framerate),
		"-i", "desktop",
		"-an",
		// Never wider than maxWidth, aspect kept, even dimensions (VP8 wants
		// them). A desktop that already fits passes through untouched.
		"-vf", fmt.Sprintf("scale=w='min(iw,%d)':h=-2", s.maxWidth),
		"-c:v", "libvpx",
		// Rate control for a live stream rather than a file: the same rate as
		// floor and ceiling (CBR), a one-second buffer instead of libvpx's six,
		// and a keyframe capped at three frames' worth. Left to its defaults
		// the encoder saves up and spends it on keyframes of 100 KB and more —
		// close to half a second of a 2 Mbit/s link every two seconds, which
		// the viewer feels as a periodic stall; these bring one down to about
		// 30 KB at a cost in keyframe quality that a screen recovers from in
		// the next few frames.
		"-b:v", bitrate, "-minrate", bitrate, "-maxrate", bitrate,
		"-bufsize", bitrate, "-rc_init_occupancy", strconv.Itoa(s.bitrate / 2),
		"-undershoot-pct", "100", "-overshoot-pct", "15",
		"-max-intra-rate", "300",
		// No look-ahead: a frame goes out the moment it is encoded. Realtime VP8
		// already defaults to this, but that is a codec default a later ffmpeg
		// is free to change, and here it decides the latency, so it is said.
		"-lag-in-frames", "0",
		"-deadline", "realtime",
		"-cpu-used", "5",
		"-error-resilient", "1",
		"-g", strconv.Itoa(s.framerate * 2),
		"-pix_fmt", "yuv420p",
		// Keep the real capture pts instead of duplicating/dropping frames to a
		// constant rate: a slow grab then appears as a wider pts gap, which the
		// reader turns into Frame.Elapsed. Requires ffmpeg >= 5.1.
		"-fps_mode", "passthrough",
		// One write down the pipe per encoded frame. ffmpeg already flushes a
		// pipe per packet; "already" is a default, not a promise.
		"-flush_packets", "1",
		"-f", "ivf",
		"pipe:1",
	}
}

// readIVF parses the IVF stream and emits one Frame per VP8 frame. The file
// header is validated by Pion's reader; the 12-byte frame headers are parsed
// here because Pion rescales the pts into a unit that is neither raw pts nor
// seconds.
func readIVF(ctx context.Context, r io.Reader, out chan<- Frame, logf func(string, ...any)) {
	_, header, err := ivfreader.NewWith(r)
	if err != nil {
		if ctx.Err() == nil {
			logf("[capture] could not read IVF header: %v", err)
		}
		return
	}
	// Timebase is num/den seconds per pts tick (ffmpeg writes 1/framerate here).
	tickSeconds := float64(header.TimebaseNumerator) / float64(header.TimebaseDenominator)

	frameHeader := make([]byte, ivfFrameHeaderSize)
	var (
		prevPTS uint64
		first   = true
	)
	for {
		if _, err := io.ReadFull(r, frameHeader); err != nil {
			if ctx.Err() == nil && !errors.Is(err, io.EOF) {
				logf("[capture] could not read frame header: %v", err)
			}
			return
		}
		size := binary.LittleEndian.Uint32(frameHeader[0:4])
		pts := binary.LittleEndian.Uint64(frameHeader[4:12])

		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			if ctx.Err() == nil {
				logf("[capture] could not read frame: %v", err)
			}
			return
		}

		var elapsed time.Duration
		if !first {
			elapsed = elapsedBetween(prevPTS, pts, tickSeconds)
		}
		first = false
		prevPTS = pts

		select {
		case out <- Frame{Data: data, Elapsed: elapsed}:
		case <-ctx.Done():
			return
		}
	}
}

// elapsedBetween converts the pts gap between two consecutive frames into a
// duration. Non-monotonic pts (which ffmpeg never emits for passthrough) yield
// zero rather than a negative or wrapped value.
func elapsedBetween(prev, cur uint64, tickSeconds float64) time.Duration {
	if cur <= prev {
		return 0
	}
	elapsed := time.Duration(float64(cur-prev) * tickSeconds * float64(time.Second))
	if elapsed > maxElapsed {
		return maxElapsed
	}
	return elapsed
}

func logStderr(r io.Reader, logf func(string, ...any)) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		logf("[ffmpeg] %s", scanner.Text())
	}
	// The pipe closes when ffmpeg exits; any read error is not actionable here.
	_ = scanner.Err()
}
