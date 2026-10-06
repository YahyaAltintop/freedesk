// Package vpx turns desktop frames into VP8 with libvpx, which is linked
// statically into the agent (see scripts/build-libvpx.sh). It also converts
// the captured BGRA pixels into the I420 layout libvpx encodes, scaling them
// down when the desktop is wider than the configured cap.
//
// The encoder is configured as ffmpeg configured libvpx when the agent still
// ran ffmpeg (`-c:v libvpx` with the rate-control arguments it passed): same
// rate control, same buffer, same keyframe interval, same speed, at a target
// raised from 2 to 3 Mbit/s and on four threads rather than one per CPU. See
// rateControlFor.
//
// Without cgo, or off Windows, NewEncoder reports ErrUnavailable: the rest of
// the agent still builds and runs, only without a picture. Release builds are
// checked by cmd/importcheck to have been built with cgo.
package vpx

import (
	"errors"
	"runtime"
	"time"
)

// ErrUnavailable is returned by NewEncoder in a build without the encoder
// (CGO_ENABLED=0, or a platform other than Windows).
var ErrUnavailable = errors.New("vpx: this build of the agent has no video encoder")

// Available reports whether this build has the encoder.
func Available() bool { return available }

const (
	// DefaultBitrate is the constant rate the encoder is held to, in bits per
	// second. The ffmpeg-based agent was set to 2 Mbit/s but, fed gdigrab's
	// timestamps, actually sent about 3.5 Mbit/s whenever the screen moved;
	// 3 Mbit/s keeps most of that sharpness in motion while still being a
	// rate the encoder holds, which a slow link depends on.
	DefaultBitrate = 3_000_000
	// DefaultFramerate is the rate the capture clock runs at; the keyframe
	// interval is two seconds' worth of it.
	DefaultFramerate = 30

	// defaultThreads is how many threads libvpx gets. ffmpeg handed it one per
	// logical CPU, but VP8 cuts a frame into macroblock rows that each wait
	// for the row above, so beyond a few threads the extra ones mostly wait:
	// on a 20-thread machine at 1080p, four cost a third less CPU than twenty
	// for the same frame rate, latency and bitrate (TestThreadsCost measures
	// it). A machine with fewer CPUs gets one per CPU.
	defaultThreads = 4
	// maxThreads mirrors ffmpeg's MAX_VPX_THREADS, the most it would hand
	// libvpx; an explicit Settings.Threads is capped at it.
	maxThreads = 64

	// timebaseDen is the pts unit the encoder is given: microseconds.
	timebaseDen = 1_000_000
)

// Settings describes one encoder. Zero values pick the defaults.
type Settings struct {
	// Width and Height are the encoded frame size, even numbers; OutputSize
	// gives it for a desktop and a width cap.
	Width, Height int
	// Bitrate is the constant rate in bits per second; zero means
	// DefaultBitrate.
	Bitrate int
	// Framerate is the capture clock's rate; zero means DefaultFramerate.
	Framerate int
	// Threads is how many threads libvpx may use; zero means defaultThreads,
	// or one per logical CPU on a machine with fewer.
	Threads int
}

// Image is one frame in the I420 layout the encoder takes: Y at full size,
// U and V at half size in both directions, each row exactly as wide as the
// plane. It is ordinary Go memory; the C code only reads or writes it during
// a call.
type Image struct {
	Width, Height int
	Y, U, V       []byte
}

// NewImage allocates an image of w x h pixels (both even).
func NewImage(w, h int) *Image {
	return &Image{Width: w, Height: h, Y: make([]byte, w*h), U: make([]byte, w*h/4), V: make([]byte, w*h/4)}
}

// OutputSize is the encoded size for a desktop of srcW x srcH pixels with the
// width capped at maxWidth (0: no cap). It follows the scale filter the agent
// gave ffmpeg, `scale=w='min(iw,maxWidth)':h=-2`: a desktop that fits keeps
// its size, a wider one is scaled to maxWidth with the height that keeps the
// aspect, rounded to an even number. Both dimensions come out even, as I420
// wants them; a desktop that fits but has an odd dimension loses that last
// row or column rather than being rescaled by a fraction of a pixel.
func OutputSize(srcW, srcH, maxWidth int) (w, h int) {
	if srcW <= 0 || srcH <= 0 {
		return 0, 0
	}
	if maxWidth <= 0 || srcW <= maxWidth {
		return srcW &^ 1, srcH &^ 1
	}
	w = maxWidth &^ 1
	// av_rescale rounding (half away from zero) of w*srcH/srcW to a multiple
	// of two, as the scale filter's -2 does.
	den := int64(srcW) * 2
	h = int((int64(w)*int64(srcH)+den/2)/den) * 2
	return w, max(h, 2)
}

// rateControl is the libvpx configuration, in libvpx's own units.
type rateControl struct {
	targetKbps     int // rc_target_bitrate
	bufMs          int // rc_buf_sz
	bufInitialMs   int // rc_buf_initial_sz
	bufOptimalMs   int // rc_buf_optimal_sz
	undershootPct  int // rc_undershoot_pct
	overshootPct   int // rc_overshoot_pct
	maxIntraPct    int // VP8E_SET_MAX_INTRA_BITRATE_PCT
	kfMaxDist      int // kf_max_dist
	cpuUsed        int // VP8E_SET_CPUUSED
	threads        int // g_threads
	errorResilient int // g_error_resilient
}

// rateControlFor derives the libvpx configuration from s the way ffmpeg 9.0
// (libavcodec/libvpxenc.c) derived it from the arguments the agent used to
// pass, R being the bitrate:
//
//	-b:v R -minrate R -maxrate R    → VPX_CBR, rc_target_bitrate = R/1000
//	-bufsize R                      → rc_buf_sz = bufsize*1000/R ms (one second)
//	-rc_init_occupancy R/2          → rc_buf_initial_sz = half of that
//	(always)                        → rc_buf_optimal_sz = rc_buf_sz*5/6
//	-undershoot-pct 100 -overshoot-pct 15 -max-intra-rate 300
//	-g 2*fps                        → kf_max_dist (kf_min_dist left at its default)
//	-cpu-used 5 -error-resilient 1 -lag-in-frames 0 -deadline realtime
//	threads auto                    → g_threads = logical CPUs, at most 64
//
// The one departure is the thread count: defaultThreads instead of one per
// CPU (see there). Everything else ffmpeg set explicitly matches libvpx's
// defaults (static threshold 0, noise sensitivity 0, one token partition, no
// frame dropping).
func rateControlFor(s Settings) rateControl {
	bitrate := s.Bitrate
	if bitrate <= 0 {
		bitrate = DefaultBitrate
	}
	framerate := s.Framerate
	if framerate <= 0 {
		framerate = DefaultFramerate
	}
	threads := s.Threads
	if threads <= 0 {
		threads = min(runtime.NumCPU(), defaultThreads)
	}
	threads = min(threads, maxThreads)

	bufBits := int64(bitrate)  // -bufsize: one second
	initialBits := bufBits / 2 // -rc_init_occupancy: half of it
	bufMs := int(bufBits * 1000 / int64(bitrate))
	return rateControl{
		targetKbps:     int((int64(bitrate) + 500) / 1000),
		bufMs:          bufMs,
		bufInitialMs:   int(initialBits * 1000 / int64(bitrate)),
		bufOptimalMs:   bufMs * 5 / 6,
		undershootPct:  100,
		overshootPct:   15,
		maxIntraPct:    300,
		kfMaxDist:      framerate * 2,
		cpuUsed:        5,
		threads:        threads,
		errorResilient: 1,
	}
}

// Packet is one encoded VP8 frame.
type Packet struct {
	Data     []byte
	Keyframe bool
}

// ticks converts a duration into the encoder's pts unit.
func ticks(d time.Duration) int64 {
	return int64(d / time.Microsecond)
}
