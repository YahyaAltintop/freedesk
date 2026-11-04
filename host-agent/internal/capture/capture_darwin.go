//go:build darwin && cgo

package capture

import (
	"errors"
	"fmt"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/macos"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/vpx"
)

// The macOS hooks of the pipeline (pipeline.go). ScreenCaptureKit captures
// the main display (internal/macos): scaled on the GPU to the size the
// encoder takes, the pointer already drawn in, as BGRA. Its pixel buffers are
// read where they are, with no copy, in the tick that grabbed them.

const (
	// displayCheckInterval spaces the checks for another main display or a
	// new resolution; either one reopens the source, at the new size.
	displayCheckInterval = time.Second
	// primeWidth is the width of the one short capture CheckAccess runs at
	// start-up (primeScreenCapture): the smallest picture worth asking for.
	primeWidth = 320
)

// errScreenRecordingDenied: macOS has not let the agent record the screen.
// It only asks once (CheckAccess), and a permission given afterwards takes
// effect when the agent next starts.
var errScreenRecordingDenied = errors.New("macOS does not let FreeDesk record the screen: allow it in System Settings > Privacy & Security > Screen & System Audio Recording (Screen Recording before macOS 15), then start FreeDesk again")

// unavailableWhy says, in the log, what keeps the screen from being read.
const unavailableWhy = "locked, asleep, or without a picture yet"

// CheckAccess reports whether macOS lets the agent record the screen. With
// prompt, macOS asks the operator if it never has; and, when it is allowed,
// one short capture is run in the background, so that the reminder macOS
// shows now and then comes up now too (primeScreenCapture).
func CheckAccess(prompt bool) bool {
	ok := macos.ScreenRecordingAllowed(prompt)
	if ok && prompt {
		go primeScreenCapture()
	}
	return ok
}

// primeScreenCapture starts one small stream of the main display and stops
// it again. macOS 15 and newer remind the operator every month or so that
// the program records the screen, and the reminder comes up the first time a
// start of the program actually captures: that would be the first
// connection, behind the consent prompt. Capturing once now puts it at
// start-up, beside the other questions. A still screen sends its first
// picture at once, so this takes a moment.
func primeScreenCapture() {
	id, pw, ph, ok := mainDisplay()
	if !ok {
		return
	}
	w, h := vpx.OutputSize(pw, ph, primeWidth)
	if stream, err := macos.OpenScreenStream(id, w, h); err == nil {
		stream.Close()
	}
}

// mainDisplay is the main display, the one at the top-left of the desktop and
// the one input is mapped to, with its size in pixels.
func mainDisplay() (id uint32, w, h int, ok bool) { return macos.MainDisplay() }

// openSource starts ScreenCaptureKit on the main display, at the size the
// encoder takes (vpx.OutputSize): a Retina panel is scaled down on the GPU,
// not by the converter. It never asks for the permission itself; that is
// asked once at start-up (CheckAccess).
func openSource(maxWidth int) (source, error) {
	if !CheckAccess(false) {
		return nil, errScreenRecordingDenied
	}
	id, pw, ph, ok := mainDisplay()
	if !ok {
		return nil, errors.New("no display to capture")
	}
	w, h := vpx.OutputSize(pw, ph, maxWidth)
	stream, err := macos.OpenScreenStream(id, w, h)
	if err != nil {
		return nil, fmt.Errorf("ScreenCaptureKit: %w", err)
	}
	return &sckSource{stream: stream, display: id, pw: pw, ph: ph}, nil
}

// sckSource is one ScreenCaptureKit stream of the main display. All its
// methods run on the capture goroutine.
type sckSource struct {
	stream  *macos.ScreenStream
	display uint32
	pw, ph  int     // the display's size in pixels when the stream started
	frame   picture // the picture the last grab handed out

	probe            desktopProbe // spaces the screen-lock checks
	nextDisplayCheck time.Time
}

func (s *sckSource) name() string { return "ScreenCaptureKit" }

func (s *sckSource) current() *picture { return &s.frame }

func (s *sckSource) origin() (x, y int) { return 0, 0 }

// grab takes the newest picture the stream has delivered. changed is false
// while the screen is still: the stream sends nothing new then.
func (s *sckSource) grab() (changed bool, err error) {
	now := time.Now()
	if !s.probe.check(now) {
		return false, errDesktopUnavailable
	}
	if !now.Before(s.nextDisplayCheck) {
		s.nextDisplayCheck = now.Add(displayCheckInterval)
		id, pw, ph, ok := mainDisplay()
		if !ok {
			// Nothing to read off the displays while they are being
			// rearranged (a lid closing, a monitor unplugged): a pause,
			// until one is back, not a reopen.
			return false, errDesktopUnavailable
		}
		if id != s.display || pw != s.pw || ph != s.ph {
			return false, errors.New("the main display changed")
		}
	}
	p, changed, err := s.stream.Grab()
	switch {
	case errors.Is(err, macos.ErrNoPicture):
		return false, errDesktopUnavailable
	case err != nil:
		return false, err
	}
	if changed {
		s.frame = picture{w: p.W, h: p.H, stride: p.Stride, bits: p.Pixels}
	}
	return changed, nil
}

func (s *sckSource) close() {
	s.stream.Close()
	s.frame = picture{}
}

// cursorPainter has nothing to do: ScreenCaptureKit draws the pointer into
// every picture, and a pointer that moves makes a new one.
type cursorPainter struct{}

func (*cursorPainter) paint(*picture, int, int, bool, time.Time) bool { return false }

// setupCaptureThread: the capture thread needs nothing set; ScreenCaptureKit
// delivers on a queue of its own.
func setupCaptureThread() (restore func()) { return func() {} }

// flushFrame: nothing draws into the pictures after ScreenCaptureKit.
func flushFrame() {}

// inputDesktopReadable reports whether the screen can be shown now: not
// locked, not asleep, and this user's session at the console.
func inputDesktopReadable() bool { return macos.ScreenShowable() }
