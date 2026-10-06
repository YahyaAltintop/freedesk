package capture

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/diag"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/vpx"
)

// source is where frames come from: desktop duplication, or the GDI copy.
type source interface {
	name() string
	// grab brings current() up to date; changed reports a new desktop
	// image. errDesktopUnavailable means the desktop cannot be read now.
	grab() (changed bool, err error)
	current() *dib
	// origin is the captured monitor's top-left corner on the virtual
	// screen, to place the pointer.
	origin() (x, y int)
	close()
}

// skipDuplication makes openSource go straight to the GDI copy, so tests can
// exercise the fallback on a machine where duplication works; newTestSource,
// when set, replaces both with a source of the test's own.
var (
	skipDuplication bool
	newTestSource   func() (source, error)
)

// openSource prefers desktop duplication and falls back to the GDI copy.
func openSource() (source, error) {
	if newTestSource != nil {
		return newTestSource()
	}
	err := errors.New("skipped")
	if !skipDuplication {
		var d *dxgiSource
		if d, err = openDXGI(); err == nil {
			return d, nil
		}
	}
	diag.Printf("[capture] desktop duplication is not available (%v); using the GDI screen copy", err)
	g, gerr := openGDI()
	if gerr != nil {
		return nil, fmt.Errorf("no screen capture method works: duplication: %v; GDI: %w", err, gerr)
	}
	return g, nil
}

// ScreenCapture captures the primary monitor and encodes it to VP8 inside
// the agent: desktop duplication (or GDI), the mouse pointer drawn in, and
// libvpx (see internal/vpx for how it is configured).
//
// Two goroutines share the work: one grabs the screen and converts it to
// I420 on every tick of a 30 fps clock, the other encodes. Done one after the
// other, a slow GDI copy plus the encoder would not fit in a tick. While the
// encoder is busy, a newer tick replaces the one waiting for it, so a slow
// moment costs frames, never latency. While nothing on the screen changes,
// the last picture is encoded again only every idleInterval, so a still
// screen costs next to nothing.
type ScreenCapture struct {
	maxWidth  int
	framerate int
	keyframe  *KeyframeRequest // nil: no on-demand keyframes

	// sample, when set (tests), is told about every encoded frame.
	sample func(frameSample)
}

// frameSample describes one encoded frame, for measurements.
type frameSample struct {
	size     int
	keyframe bool
	changed  bool          // a new picture, not the previous one again
	work     time.Duration // from the tick to the encoded frame
}

// job is one tick, handed from the capture goroutine to the encoder.
type job struct {
	at       time.Time  // when the screen was grabbed
	img      *vpx.Image // a new picture, or nil: the previous one again
	restart  bool       // start a new encoder (a new size, or back from a pause)
	keyframe bool       // encode this one as a keyframe (a viewer's request)
}

// NewScreenCapture returns a capture with the agent's settings.
func NewScreenCapture(opts Options) *ScreenCapture {
	width := opts.MaxWidth
	if width <= 0 {
		width = DefaultMaxWidth
	}
	return &ScreenCapture{maxWidth: width, framerate: defaultFramerate, keyframe: opts.Keyframe}
}

// Start begins capturing and returns the encoded frames. The channel closes
// when ctx ends or the capture fails for good, which includes a goroutine of
// its own crashing: that is logged, not fatal, and the session starts a new
// capture. An error means capture could not start at all: a build without
// the encoder, or no way to read the screen.
func (c *ScreenCapture) Start(ctx context.Context) (<-chan Frame, error) {
	if !vpx.Available() {
		return nil, vpx.ErrUnavailable
	}
	ctx, cancel := context.WithCancel(ctx)
	frames := make(chan Frame)
	jobs := make(chan job, 1)
	free := make(chan *vpx.Image, 4)
	started := make(chan error, 1)
	go guarded("capture", started, func() { c.capture(ctx, jobs, free, started) })
	go func() {
		defer cancel() // an encoder that gives up stops the capture too
		guarded("encoder", nil, func() { c.encode(ctx, jobs, free, frames) })
	}()
	if err := <-started; err != nil {
		cancel()
		return nil, err
	}
	return frames, nil
}

// guarded runs one of the capture's goroutines. A panic in it ends the
// capture (its deferred clean-up runs, the frame channel closes, the session
// starts a new capture) instead of ending the program: a remote session is
// worth more than a crash report. The panic is logged with its stack, and
// started, when given, is told, so Start does not wait for a goroutine that
// is gone.
func guarded(what string, started chan<- error, run func()) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		diag.Printf("[capture] the %s goroutine crashed: %v\n%s", what, r, debug.Stack())
		select {
		case started <- fmt.Errorf("the capture's %s goroutine crashed: %v", what, r):
		default: // already answered, or nobody waiting
		}
	}()
	run()
}

// capture grabs the screen on every tick and hands the work to the encoder.
// It owns an OS thread for its whole life: Direct3D's immediate context and
// the thread's DPI setting belong to that thread. The thread is not handed
// back to the scheduler; it ends with the goroutine.
func (c *ScreenCapture) capture(ctx context.Context, jobs chan job, free chan *vpx.Image, started chan<- error) {
	runtime.LockOSThread()
	defer close(jobs)
	restoreDPI := setThreadPhysicalPixels()
	defer restoreDPI()

	src, err := openSource()
	if err != nil {
		started <- err
		return
	}
	defer func() { src.close() }()
	diag.Printf("[capture] capturing the primary monitor with %s", src.name())
	started <- nil

	tick := time.NewTicker(time.Second / time.Duration(c.framerate))
	defer tick.Stop()
	var (
		conv       *vpx.Converter
		cursor     cursorPainter
		paused     bool
		restart    = true
		lastJob    time.Time // the previous tick handed to the encoder
		lastForced time.Time // the previous keyframe forced by a viewer's request
	)
	defer func() {
		if conv != nil {
			conv.Close()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		now := time.Now()

		changed, err := src.grab()
		if errors.Is(err, errDesktopUnavailable) {
			if !paused {
				paused = true
				diag.Printf("[capture] the desktop cannot be read (locked, or a UAC prompt); paused")
			}
			continue
		}
		if err != nil {
			diag.Printf("[capture] %s failed: %v; reopening", src.name(), err)
			src.close()
			if src, err = openSource(); err != nil {
				diag.Printf("[capture] cannot capture the screen any more: %v", err)
				src = nopSource{}
				return
			}
			diag.Printf("[capture] capturing the primary monitor with %s", src.name())
			continue
		}
		frame := src.current()
		if paused {
			// Back from the secure desktop: a new encoder, which opens on a
			// keyframe, so the viewer recovers at once.
			paused = false
			restart = true
			diag.Printf("[capture] the desktop is readable again; resumed")
		}
		if conv != nil {
			if w, h := conv.SourceSize(); w != frame.w || h != frame.h {
				conv.Close()
				conv = nil
			}
		}
		if conv == nil {
			if conv, err = vpx.NewConverter(frame.w, frame.h, c.maxWidth); err != nil {
				diag.Printf("[capture] cannot convert a %dx%d screen: %v", frame.w, frame.h, err)
				return
			}
			w, h := conv.Size()
			diag.Printf("[capture] encoding %dx%d as %dx%d VP8", frame.w, frame.h, w, h)
			restart = true
		}

		// A new picture is needed when the desktop changed, the pointer
		// moved, or a new encoder starts; otherwise the encoder repeats the
		// last one.
		load := changed || restart
		ox, oy := src.origin()
		if cursor.paint(frame, ox, oy, changed, now) {
			load = true
		}
		// A viewer's keyframe request is honoured on the current picture,
		// unchanged or not, but no more often than keyframeMinInterval; one
		// that arrives within that waits rather than being dropped (it stays
		// pending until take succeeds). A restart makes a fresh encoder, whose
		// first frame is a keyframe already, so it clears any pending request
		// and starts the rate-limit window rather than forcing a second one
		// right behind it.
		force := false
		switch {
		case restart:
			lastForced = now
			if c.keyframe != nil {
				c.keyframe.take()
			}
		case keyframeDue(c.keyframe, now.Sub(lastForced)):
			force = true
			lastForced = now
		}
		if !load && !force && now.Sub(lastJob) < idleInterval {
			// Nothing new and no keyframe due: the encoder repeats the last
			// picture every idleInterval, not on every tick.
			continue
		}
		lastJob = now
		j := job{at: now, restart: restart, keyframe: force}
		restart = false
		if load {
			w, h := conv.Size()
			j.img = takeImage(free, w, h)
			gdiFlush()
			conv.Convert(j.img, frame.bits, frame.stride())
		}
		send(ctx, jobs, free, j)
	}
}

// send hands j to the encoder. If the encoder has not yet taken the previous
// tick, j replaces it, keeping its picture when j has none of its own.
func send(ctx context.Context, jobs chan job, free chan *vpx.Image, j job) {
	select {
	case jobs <- j:
		return
	default:
	}
	select {
	case old := <-jobs:
		j.restart = j.restart || old.restart
		j.keyframe = j.keyframe || old.keyframe
		if j.img == nil {
			j.img = old.img
		} else if old.img != nil {
			recycle(free, old.img)
		}
	default: // the encoder took it meanwhile
	}
	select {
	case jobs <- j:
	case <-ctx.Done():
	}
}

// takeImage reuses an image the encoder is done with, or allocates one.
func takeImage(free chan *vpx.Image, w, h int) *vpx.Image {
	for {
		select {
		case img := <-free:
			if img.Width == w && img.Height == h {
				return img
			}
		default:
			return vpx.NewImage(w, h)
		}
	}
}

func recycle(free chan *vpx.Image, img *vpx.Image) {
	select {
	case free <- img:
	default:
	}
}

// encode turns jobs into frames until the capture goroutine closes jobs.
//
// The encoder is fed a nominal 30 fps: each picture it gets stands for one
// tick or a few (frameDuration), and the ticks it was too slow for, or on
// which nothing changed, are simply not fed. Its rate control therefore
// spends the same budget per encoded frame whatever the real gaps; real
// time travels separately, in Frame.Elapsed.
func (c *ScreenCapture) encode(ctx context.Context, jobs <-chan job, free chan *vpx.Image, out chan<- Frame) {
	defer close(out)
	interval := time.Second / time.Duration(c.framerate)
	var (
		enc      *vpx.Encoder
		cur      *vpx.Image
		pts      time.Duration // the encoder's clock: the durations of the frames it encoded
		lastEnc  time.Time     // the previous encode, for the real gap
		lastSent time.Time     // the previous frame handed out, for Elapsed
	)
	defer func() {
		if enc != nil {
			enc.Close()
		}
	}()
	for j := range jobs {
		if j.img != nil {
			if cur != nil {
				recycle(free, cur)
			}
			cur = j.img
		}
		if cur == nil {
			continue
		}
		if enc != nil {
			if w, h := enc.Size(); j.restart || w != cur.Width || h != cur.Height {
				enc.Close()
				enc = nil
			}
		}
		if enc == nil {
			var err error
			if enc, err = vpx.NewEncoder(vpx.Settings{Width: cur.Width, Height: cur.Height, Framerate: c.framerate}); err != nil {
				diag.Printf("[capture] cannot start the encoder for %dx%d: %v", cur.Width, cur.Height, err)
				return
			}
			pts, lastEnc = 0, time.Time{}
		}
		var gap time.Duration
		if !lastEnc.IsZero() {
			gap = j.at.Sub(lastEnc)
		}
		lastEnc = j.at
		duration := frameDuration(j.img == nil, gap, interval)
		// A fresh encoder's first frame is a keyframe anyway; j.keyframe
		// forces one on an encoder that is already running (a viewer's
		// request).
		pkt, err := enc.Encode(cur, pts, duration, j.keyframe)
		pts += duration
		if err != nil {
			diag.Printf("[capture] encoding failed: %v", err)
			return
		}
		if len(pkt.Data) == 0 {
			continue
		}
		if c.sample != nil {
			c.sample(frameSample{size: len(pkt.Data), keyframe: pkt.Keyframe, changed: j.img != nil, work: time.Since(j.at)})
		}
		var elapsed time.Duration
		if !lastSent.IsZero() {
			elapsed = min(j.at.Sub(lastSent), maxElapsed)
		}
		lastSent = j.at
		select {
		case out <- Frame{Data: pkt.Data, Elapsed: elapsed}:
		case <-ctx.Done():
			return
		}
	}
}

// frameDuration is the time a frame is encoded as standing for. A repeat of
// the previous picture stands for one tick whatever the real gap: nothing
// happened in between that rate control should budget for, so a quiet spell
// does not inflate the frames that end it. A new picture stands for the real
// time since the previous encode, at least one tick and at most a few, so an
// encoder that is falling behind is budgeted its real frame rate rather than
// starved.
func frameDuration(repeat bool, gap, interval time.Duration) time.Duration {
	if repeat {
		return interval
	}
	return min(max(gap, interval), maxTicksPerFrame*interval)
}

// nopSource stands in after the capture has given up, so the deferred close
// has something to close.
type nopSource struct{}

func (nopSource) name() string        { return "nothing" }
func (nopSource) grab() (bool, error) { return false, errDesktopUnavailable }
func (nopSource) current() *dib       { return nil }
func (nopSource) origin() (int, int)  { return 0, 0 }
func (nopSource) close()              {}
