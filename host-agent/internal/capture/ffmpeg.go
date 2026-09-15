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
)

const (
	defaultFFmpegBinary = "ffmpeg"
	defaultFramerate    = 30
	defaultBitrate      = "2M"

	ivfFrameHeaderSize = 12
	// maxElapsed caps the gap attributed to a single frame so a corrupt pts can
	// never jump the RTP clock by minutes.
	maxElapsed = 5 * time.Second
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

// ScreenCapture runs ffmpeg to capture and encode the desktop.
type ScreenCapture struct {
	binary    string
	framerate int
	bitrate   string
}

// NewScreenCapture returns a capture with sensible real-time defaults. binary is
// the ffmpeg executable to run; an empty value picks the ffmpeg shipped next
// to the agent, or "ffmpeg" from PATH.
func NewScreenCapture(binary string) *ScreenCapture {
	return &ScreenCapture{binary: resolveBinary(binary), framerate: defaultFramerate, bitrate: defaultBitrate}
}

// Binary reports the ffmpeg executable this capture will run.
func (s *ScreenCapture) Binary() string { return s.binary }

// Start launches ffmpeg and returns a channel of encoded VP8 frames. Capture
// runs until ctx is cancelled or ffmpeg exits, at which point the channel is
// closed. A missing ffmpeg binary is reported as a Start error.
func (s *ScreenCapture) Start(ctx context.Context) (<-chan Frame, error) {
	cmd := exec.CommandContext(ctx, s.binary, s.args()...)

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

	go logStderr(stderr)

	frames := make(chan Frame)
	go func() {
		defer close(frames)
		readIVF(ctx, stdout, frames)
		_ = cmd.Wait()
	}()
	return frames, nil
}

func (s *ScreenCapture) args() []string {
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "gdigrab",
		"-framerate", strconv.Itoa(s.framerate),
		"-i", "desktop",
		"-an",
		"-c:v", "libvpx",
		"-b:v", s.bitrate,
		"-deadline", "realtime",
		"-cpu-used", "5",
		"-error-resilient", "1",
		"-g", strconv.Itoa(s.framerate * 2),
		"-pix_fmt", "yuv420p",
		// Keep the real capture pts instead of duplicating/dropping frames to a
		// constant rate: a slow grab then appears as a wider pts gap, which the
		// reader turns into Frame.Elapsed. Requires ffmpeg >= 5.1.
		"-fps_mode", "passthrough",
		"-f", "ivf",
		"pipe:1",
	}
}

// readIVF parses the IVF stream and emits one Frame per VP8 frame. The file
// header is validated by Pion's reader; the 12-byte frame headers are parsed
// here because Pion rescales the pts into a unit that is neither raw pts nor
// seconds.
func readIVF(ctx context.Context, r io.Reader, out chan<- Frame) {
	_, header, err := ivfreader.NewWith(r)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("[capture] could not read IVF header: %v", err)
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
				log.Printf("[capture] could not read frame header: %v", err)
			}
			return
		}
		size := binary.LittleEndian.Uint32(frameHeader[0:4])
		pts := binary.LittleEndian.Uint64(frameHeader[4:12])

		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			if ctx.Err() == nil {
				log.Printf("[capture] could not read frame: %v", err)
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

func logStderr(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		log.Printf("[ffmpeg] %s", scanner.Text())
	}
	// The pipe closes when ffmpeg exits; any read error is not actionable here.
	_ = scanner.Err()
}
