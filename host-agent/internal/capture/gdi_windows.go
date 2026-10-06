package capture

import "time"

// The GDI copy: BitBlt from the screen (what ffmpeg's gdigrab did when the
// agent used it), of the primary monitor only. Used when desktop duplication
// is not available (some virtual machines and remote sessions, a rotated
// monitor, too many other programs duplicating the same screen) and to seed a
// still desktop.

// gdiRetryInterval spaces the attempts while the desktop is unreadable.
const gdiRetryInterval = 500 * time.Millisecond

// copyScreenGDI copies the primary monitor into frame, reporting success.
// CAPTUREBLT includes layered windows (tooltips, menus with shadows).
func copyScreenGDI(frame *dib) bool {
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return false
	}
	defer func() { _, _, _ = procReleaseDC.Call(0, screen) }()
	ok, _, _ := procBitBlt.Call(frame.dc, 0, 0, uintptr(frame.w), uintptr(frame.h), screen, 0, 0, srccopy|captureblt)
	return ok != 0
}

type gdiSource struct {
	frame   *dib
	nextTry time.Time
	lost    bool
	probe   desktopProbe // spaces the secure-desktop checks
}

func openGDI() (*gdiSource, error) {
	w, h := primaryScreenSize()
	frame, err := newDIB(w, h)
	if err != nil {
		return nil, err
	}
	return &gdiSource{frame: frame}, nil
}

func (s *gdiSource) name() string { return "GDI screen copy" }

func (s *gdiSource) current() *dib { return s.frame }

func (s *gdiSource) origin() (x, y int) { return 0, 0 }

// grab copies the whole monitor on every tick: GDI cannot tell what changed,
// so every frame counts as changed.
func (s *gdiSource) grab() (changed bool, err error) {
	now := time.Now()
	if s.lost {
		if now.Before(s.nextTry) {
			return false, errDesktopUnavailable
		}
		s.nextTry = now.Add(gdiRetryInterval)
	}
	if !s.probe.check(now) {
		s.lost = true
		return false, errDesktopUnavailable
	}
	if w, h := primaryScreenSize(); w != s.frame.w || h != s.frame.h {
		frame, err := newDIB(w, h)
		if err != nil {
			return false, err
		}
		s.frame.close()
		s.frame = frame
	}
	if !copyScreenGDI(s.frame) {
		s.lost = true
		return false, errDesktopUnavailable
	}
	s.lost = false
	return true, nil
}

func (s *gdiSource) close() {
	s.frame.close()
	s.frame = nil
}
