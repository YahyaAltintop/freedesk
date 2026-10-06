package capture

import (
	"testing"
	"time"
)

var procLoadCursor = user32.NewProc("LoadCursorW")

const (
	idcArrow = 32512 // IDC_ARROW
	idcWait  = 32514 // IDC_WAIT, animated in Windows' default cursor scheme
)

// TestCursorPainterForgetsTheOldFrame replays a mode change: the source hands
// the painter a new, smaller bitmap while the painter still holds the
// rectangle it covered in the old one. It must start afresh in the new bitmap
// rather than put the old rectangle back into it, which ran off the end of
// the new bitmap and crashed the agent.
func TestCursorPainterForgetsTheOldFrame(t *testing.T) {
	old, err := newDIB(400, 400)
	if err != nil {
		t.Fatal(err)
	}
	defer old.close()
	small, err := newDIB(100, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer small.close()
	arrow, _, _ := procLoadCursor.Call(0, idcArrow)
	if arrow == 0 {
		t.Fatal("LoadCursorW(IDC_ARROW) failed")
	}

	// What a paint into old leaves behind with the pointer at its bottom
	// right corner.
	var p cursorPainter
	p.draw(old, arrow, 0, 390, 390)
	p.frame, p.last, p.valid = old, pointerState{visible: true, handle: arrow, x: 390, y: 390}, true
	if !p.drawn || (p.rx+p.rw <= small.w && p.ry+p.rh <= small.h) {
		t.Fatalf("test setup: the pointer's rectangle (%d,%d %dx%d) must lie outside the small bitmap", p.rx, p.ry, p.rw, p.rh)
	}

	// The pointer moved (a pointer-only frame, so not fresh) and the frame
	// is another bitmap. Whether the real pointer lands in the small bitmap
	// depends on where it is on the screen; what matters is that the old
	// rectangle is never written into it.
	p.paint(small, 0, 0, false, time.Now())
	if p.frame != small {
		t.Fatal("the painter must now describe the new bitmap")
	}
	if p.drawn && (p.rx+p.rw > small.w || p.ry+p.rh > small.h) {
		t.Fatalf("the pointer's rectangle (%d,%d %dx%d) lies outside the new bitmap", p.rx, p.ry, p.rw, p.rh)
	}
}

// TestCursorPainterReportsOnlyRealChanges: a pointer that is not in the frame
// (on another monitor) must not count as a change however it moves, or a
// still screen would be converted and encoded on every tick for nothing.
func TestCursorPainterReportsOnlyRealChanges(t *testing.T) {
	frame, err := newDIB(100, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer frame.close()
	// An origin far from any monitor keeps the real pointer out of the frame.
	const far = 1 << 20
	var p cursorPainter
	now := time.Now()
	for i := range 5 {
		if p.paint(frame, far, far, i == 0, now.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("paint %d reported a change with the pointer outside the frame", i)
		}
	}
	if p.drawn {
		t.Fatal("nothing can have been drawn")
	}
}

// TestAnimatedCursorSteps: a still cursor stays at frame 0; an animated one
// advances one frame per its rate, from the moment it appeared, and wraps.
func TestAnimatedCursorSteps(t *testing.T) {
	arrow, _, _ := procLoadCursor.Call(0, idcArrow)
	wait, _, _ := procLoadCursor.Call(0, idcWait)
	if arrow == 0 || wait == 0 {
		t.Fatal("LoadCursorW failed")
	}
	t0 := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	var a animatedCursor
	if s := a.step(arrow, t0); s != 0 {
		t.Fatalf("arrow at t0: step %d, want 0", s)
	}
	if s := a.step(arrow, t0.Add(time.Hour)); s != 0 {
		t.Fatalf("a still cursor must stay at frame 0, got %d", s)
	}

	a.step(wait, t0)
	t.Logf("IDC_WAIT: %d frames of %v", a.steps, a.rate)
	if a.steps <= 1 {
		t.Skip("the wait cursor is not animated on this machine (cursor scheme), or GetCursorFrameInfo is missing")
	}
	if a.rate <= 0 {
		t.Fatalf("an animated cursor must have a frame time, got %v", a.rate)
	}
	if s := a.step(wait, t0); s != 0 {
		t.Fatalf("at t0: step %d, want 0", s)
	}
	if s := a.step(wait, t0.Add(a.rate)); s != 1 {
		t.Fatalf("one frame time later: step %d, want 1", s)
	}
	if s := a.step(wait, t0.Add(a.rate*time.Duration(a.steps))); s != 0 {
		t.Fatalf("a full cycle later: step %d, want 0 (wrapped)", s)
	}
	// Another cursor starts its own clock.
	if s := a.step(arrow, t0.Add(time.Hour)); s != 0 {
		t.Fatalf("back to the arrow: step %d, want 0", s)
	}
}
