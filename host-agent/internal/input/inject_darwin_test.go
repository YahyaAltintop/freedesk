//go:build cgo

package input

import (
	"bytes"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// posted is one event cgInput handed its sink, written out for comparison.
type posted string

// eventLog is a sink that writes every event down instead of posting it.
type eventLog struct{ events []posted }

func (l *eventLog) mouse(t eventType, x, y float64, button, clicks int, flags uint64) {
	l.events = append(l.events, posted(fmt.Sprintf("mouse %s %g,%g b%d c%d %s", eventName[t], x, y, button, clicks, flagNames(flags))))
}

func (l *eventLog) key(keycode uint16, down, repeat bool, flags uint64) {
	what := "up"
	if down {
		what = "down"
	}
	if repeat {
		what += " repeat"
	}
	l.events = append(l.events, posted(fmt.Sprintf("key 0x%02X %s %s", keycode, what, flagNames(flags))))
}

func (l *eventLog) flagsChanged(keycode uint16, flags uint64) {
	l.events = append(l.events, posted(fmt.Sprintf("flags 0x%02X %s", keycode, flagNames(flags))))
}

func (l *eventLog) scroll(vertical, horizontal int32, flags uint64) {
	l.events = append(l.events, posted(fmt.Sprintf("scroll %d,%d %s", vertical, horizontal, flagNames(flags))))
}

// take returns the events written down so far and starts a new list.
func (l *eventLog) take() []posted {
	e := l.events
	l.events = nil
	return e
}

var eventName = map[eventType]string{
	evMoved: "moved", evLeftDown: "left-down", evLeftUp: "left-up", evLeftDragged: "left-dragged",
	evRightDown: "right-down", evRightUp: "right-up", evRightDragged: "right-dragged",
	evOtherDown: "other-down", evOtherUp: "other-up", evOtherDragged: "other-dragged",
}

// flagNames spells out a flags word, "-" for none.
func flagNames(f uint64) string {
	var names []string
	for _, n := range []struct {
		bit  uint64
		name string
	}{
		{flagAlphaShift, "caps"}, {flagShift, "shift"}, {flagControl, "ctrl"}, {flagOption, "opt"},
		{flagCommand, "cmd"}, {flagSecondaryFn, "fn"},
		{deviceLeftShift, "L-shift"}, {deviceRightShift, "R-shift"},
		{deviceLeftControl, "L-ctrl"}, {deviceRightControl, "R-ctrl"},
		{deviceLeftOption, "L-opt"}, {deviceRightOption, "R-opt"},
		{deviceLeftCommand, "L-cmd"}, {deviceRightCommand, "R-cmd"},
	} {
		if f&n.bit != 0 {
			names = append(names, n.name)
			f &^= n.bit
		}
	}
	if f != 0 {
		names = append(names, fmt.Sprintf("0x%X", f))
	}
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, "+")
}

// testInput is a cgInput on a 1000 x 800 point display with a half-second
// double click, writing its events down, with a clock the test moves.
func testInput() (*cgInput, *eventLog, *time.Time) {
	events := &eventLog{}
	in := newCGInput(events)
	clock := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	in.screen = func() (float64, float64, bool) { return 1000, 800, true }
	in.doubleClick = func() time.Duration { return 500 * time.Millisecond }
	in.now = func() time.Time { return clock }
	in.allowed = func() bool { return true }
	return in, events, &clock
}

func expect(t *testing.T, got []posted, want ...posted) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d:\n%s", len(got), len(want), dump(got, want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("event %d differs:\n%s", i, dump(got, want))
		}
	}
}

func dump(got, want []posted) string {
	var b strings.Builder
	fmt.Fprintln(&b, "got:")
	for _, e := range got {
		fmt.Fprintln(&b, "  ", e)
	}
	fmt.Fprintln(&b, "want:")
	for _, e := range want {
		fmt.Fprintln(&b, "  ", e)
	}
	return b.String()
}

// TestDoubleAndTripleClicks: presses of the same button close together in
// time and place count up, down and up alike, as a local mouse's do; a press
// after the double-click time starts again from one.
func TestDoubleAndTripleClicks(t *testing.T) {
	in, events, clock := testInput()
	click := func() {
		in.clickMouse(0, true, 0.5, 0.5)
		in.clickMouse(0, false, 0.5, 0.5)
	}
	click()
	*clock = clock.Add(200 * time.Millisecond)
	click()
	*clock = clock.Add(200 * time.Millisecond)
	click()
	*clock = clock.Add(time.Second)
	click()
	expect(t, events.take(),
		"mouse left-down 500,400 b0 c1 -", "mouse left-up 500,400 b0 c1 -",
		"mouse left-down 500,400 b0 c2 -", "mouse left-up 500,400 b0 c2 -",
		"mouse left-down 500,400 b0 c3 -", "mouse left-up 500,400 b0 c3 -",
		"mouse left-down 500,400 b0 c1 -", "mouse left-up 500,400 b0 c1 -",
	)
}

// TestClickCountStartsAgain: another button, or a press too far from the
// last, is a first click.
func TestClickCountStartsAgain(t *testing.T) {
	in, events, clock := testInput()
	in.clickMouse(0, true, 0.5, 0.5)
	in.clickMouse(0, false, 0.5, 0.5)
	*clock = clock.Add(100 * time.Millisecond)
	in.clickMouse(1, true, 0.5, 0.5) // another button
	in.clickMouse(1, false, 0.5, 0.5)
	*clock = clock.Add(100 * time.Millisecond)
	in.clickMouse(1, true, 0.6, 0.5) // 100 points away
	in.clickMouse(1, false, 0.6, 0.5)
	expect(t, events.take(),
		"mouse left-down 500,400 b0 c1 -", "mouse left-up 500,400 b0 c1 -",
		"mouse right-down 500,400 b1 c1 -", "mouse right-up 500,400 b1 c1 -",
		"mouse right-down 600,400 b1 c1 -", "mouse right-up 600,400 b1 c1 -",
	)
}

// TestMovesWithAButtonDownDrag: a move with a button held is that button's
// drag, carrying its click count (a double-click drag selects word by word);
// with none held it is a plain move.
func TestMovesWithAButtonDownDrag(t *testing.T) {
	in, events, clock := testInput()
	in.moveMouse(0.1, 0.1)
	in.clickMouse(0, true, 0.1, 0.1)
	in.clickMouse(0, false, 0.1, 0.1)
	*clock = clock.Add(100 * time.Millisecond)
	in.clickMouse(0, true, 0.1, 0.1)
	in.moveMouse(0.2, 0.1)
	in.clickMouse(0, false, 0.2, 0.1)
	in.moveMouse(0.3, 0.1)
	in.clickMouse(1, true, 0.3, 0.1)
	in.moveMouse(0.3, 0.2)
	in.clickMouse(1, false, 0.3, 0.2)
	in.clickMouse(2, true, 0.3, 0.2)
	in.moveMouse(0.4, 0.2)
	in.clickMouse(2, false, 0.4, 0.2)
	expect(t, events.take(),
		"mouse moved 100,80 b0 c0 -",
		"mouse left-down 100,80 b0 c1 -", "mouse left-up 100,80 b0 c1 -",
		"mouse left-down 100,80 b0 c2 -",
		"mouse left-dragged 200,80 b0 c2 -",
		"mouse left-up 200,80 b0 c2 -",
		"mouse moved 300,80 b0 c0 -",
		"mouse right-down 300,80 b1 c1 -",
		"mouse right-dragged 300,160 b1 c1 -",
		"mouse right-up 300,160 b1 c1 -",
		"mouse other-down 300,160 b2 c1 -",
		"mouse other-dragged 400,160 b2 c1 -",
		"mouse other-up 400,160 b2 c1 -",
	)
}

// TestModifiersRideOnEveryEvent: a modifier key is a change of flags, and
// every event after it says it is held, so Shift-A types "A", Shift-click
// extends a selection and Command-C copies.
func TestModifiersRideOnEveryEvent(t *testing.T) {
	in, events, _ := testInput()
	in.pressKey("ShiftLeft", true)
	in.pressKey("KeyA", true)
	in.pressKey("KeyA", false)
	in.clickMouse(0, true, 0.5, 0.5)
	in.clickMouse(0, false, 0.5, 0.5)
	in.pressKey("ShiftLeft", false)
	in.pressKey("KeyA", true)
	in.pressKey("KeyA", false)
	in.pressKey("MetaLeft", true)
	in.pressKey("KeyC", true)
	in.pressKey("KeyC", false)
	in.pressKey("MetaLeft", false)
	expect(t, events.take(),
		"flags 0x38 shift+L-shift",
		"key 0x00 down shift+L-shift", "key 0x00 up shift+L-shift",
		"mouse left-down 500,400 b0 c1 shift+L-shift", "mouse left-up 500,400 b0 c1 shift+L-shift",
		"flags 0x38 -",
		"key 0x00 down -", "key 0x00 up -",
		"flags 0x37 cmd+L-cmd",
		"key 0x08 down cmd+L-cmd", "key 0x08 up cmd+L-cmd",
		"flags 0x37 -",
	)
}

// TestBothShiftKeys: letting go of one Shift while the other is held leaves
// Shift held.
func TestBothShiftKeys(t *testing.T) {
	in, events, _ := testInput()
	in.pressKey("ShiftLeft", true)
	in.pressKey("ShiftRight", true)
	in.pressKey("ShiftLeft", false)
	in.pressKey("KeyB", true)
	expect(t, events.take(),
		"flags 0x38 shift+L-shift",
		"flags 0x3C shift+L-shift+R-shift",
		"flags 0x38 shift+R-shift",
		"key 0x0B down shift+R-shift",
	)
}

// TestRepeats: the browser repeats a held key's key-down; macOS is told it is
// a repeat. A repeated modifier changes nothing, and is not posted again.
func TestRepeats(t *testing.T) {
	in, events, _ := testInput()
	in.pressKey("KeyA", true)
	in.pressKey("KeyA", true)
	in.pressKey("KeyA", false)
	in.pressKey("KeyA", true)
	in.pressKey("ControlLeft", true)
	in.pressKey("ControlLeft", true)
	in.pressKey("ControlLeft", false)
	in.pressKey("ControlLeft", false) // a release never pressed
	expect(t, events.take(),
		"key 0x00 down -", "key 0x00 down repeat -", "key 0x00 up -", "key 0x00 down -",
		"flags 0x3B ctrl+L-ctrl", "flags 0x3B -",
	)
}

// TestCapsLockToggles: Caps Lock turns on where it goes down and off where it
// goes down again, as it does on Windows; its coming up changes nothing.
func TestCapsLockToggles(t *testing.T) {
	in, events, _ := testInput()
	in.pressKey("CapsLock", true)
	in.pressKey("CapsLock", false)
	in.pressKey("KeyA", true)
	in.pressKey("CapsLock", true)
	in.pressKey("KeyA", true)
	expect(t, events.take(),
		"flags 0x39 caps",
		"key 0x00 down caps",
		"flags 0x39 -",
		"key 0x00 down repeat -",
	)
}

// TestUnknownKeysAreDropped: a code with no key on a Mac posts nothing.
func TestUnknownKeysAreDropped(t *testing.T) {
	in, events, _ := testInput()
	in.pressKey("PrintScreen", true)
	in.pressKey("Unknown", true)
	expect(t, events.take())
}

// TestPointsOnTheMainDisplay: a position is the same fraction of the main
// display, in points; the far edges stay inside it, and a position off the
// picture is held at its edge.
func TestPointsOnTheMainDisplay(t *testing.T) {
	in, events, _ := testInput()
	in.moveMouse(0.5, 0.25)
	in.moveMouse(1, 1)
	in.moveMouse(-0.5, 2)
	expect(t, events.take(),
		"mouse moved 500,200 b0 c0 -",
		"mouse moved 999,799 b0 c0 -",
		"mouse moved 0,799 b0 c0 -",
	)
}

// TestScroll: the protocol's dy is positive for up, as CoreGraphics' vertical
// axis is; its dx is positive for right, which CoreGraphics counts negative.
// Scrolls carry the modifiers held (Shift turns a wheel sideways, Command
// zooms in some applications).
func TestScroll(t *testing.T) {
	if v, h := scrollAxes(0, 120); v != 120 || h != 0 {
		t.Errorf("up 120: got %d,%d", v, h)
	}
	if v, h := scrollAxes(30, -40); v != -40 || h != -30 {
		t.Errorf("right 30, down 40: got %d,%d", v, h)
	}
	if v, h := scrollAxes(-1<<40, 1<<40); v != math.MaxInt32 || h != math.MaxInt32 {
		t.Errorf("huge deltas: got %d,%d, want them held at the int32 range", v, h)
	}

	in, events, _ := testInput()
	in.scrollWheel(0, 0)
	in.pressKey("ShiftLeft", true)
	in.scrollWheel(5, 100)
	expect(t, events.take(), "flags 0x38 shift+L-shift", "scroll 100,-5 shift+L-shift")
}

// TestNothingIsPostedUntilAllowed: without the permission macOS would drop
// every event silently, so none is posted and the operator is told why, once
// a minute at most; macOS is asked again every few seconds, and once it says
// yes events flow, with the state held meanwhile.
func TestNothingIsPostedUntilAllowed(t *testing.T) {
	in, events, clock := testInput()
	allowed, asked := false, 0
	in.allowed = func() bool { asked++; return allowed }
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	in.pressKey("ShiftLeft", true)
	in.moveMouse(0.5, 0.5)
	if len(events.take()) != 0 || asked != 1 {
		t.Fatalf("events were posted, or macOS asked %d times, before it allowed it", asked)
	}
	if strings.Count(logged.String(), "Accessibility") != 1 {
		t.Fatalf("the operator should be told once why nothing happens:\n%s", logged.String())
	}

	*clock = clock.Add(accessRecheck)
	allowed = true
	in.pressKey("KeyA", true)
	expect(t, events.take(), "key 0x00 down shift+L-shift")
	if asked != 2 {
		t.Errorf("macOS was asked %d times, want 2", asked)
	}
	in.pressKey("KeyA", false)
	if asked != 2 {
		t.Errorf("macOS was asked again after it allowed it (%d times)", asked)
	}
}

// TestSystemSettingsAreRead: what the injector asks macOS, the display's size,
// the double-click speed and where the pointer is, comes back sane. None of it
// needs a permission, and nothing is posted.
func TestSystemSettingsAreRead(t *testing.T) {
	w, h, ok := mainScreen()
	if !ok {
		t.Skip("no display: a machine without a screen")
	}
	interval := doubleClickInterval()
	if interval <= 0 || interval > 5*time.Second {
		t.Errorf("double-click interval %v", interval)
	}
	x, y := cursorPosition()
	t.Logf("main display %gx%g points, double click within %v, pointer at %.0f,%.0f, may post input: %v",
		w, h, interval, x, y, CheckAccess(false))
}

// TestPostedInputMovesThePointer posts a real move, to the middle of the main
// display, and reads where the pointer went. It moves the real pointer and
// needs the Accessibility permission for the program it runs from, so it
// runs only when asked:
//
//	RC_INPUT_INTEGRATION=1 go test -run PostedInput -v ./internal/input/
func TestPostedInputMovesThePointer(t *testing.T) {
	if os.Getenv("RC_INPUT_INTEGRATION") == "" {
		t.Skip("moves the real pointer; run with RC_INPUT_INTEGRATION=1")
	}
	if !CheckAccess(false) {
		t.Fatal("macOS does not let this program post input: allow the terminal in System Settings > Privacy & Security > Accessibility")
	}
	w, h, ok := mainScreen()
	if !ok {
		t.Skip("no display")
	}
	x0, y0 := cursorPosition()
	defer func() {
		in := newCGInput(cgSink{})
		in.moveMouse(x0/w, y0/h)
	}()

	in := newCGInput(cgSink{})
	in.moveMouse(0.5, 0.5)
	time.Sleep(80 * time.Millisecond)
	x, y := cursorPosition()
	if math.Abs(x-w/2) > 2 || math.Abs(y-h/2) > 2 {
		t.Fatalf("the pointer is at %.0f,%.0f, want the middle of a %.0fx%.0f display", x, y, w, h)
	}
}

// TestAccessTakenAwayIsNoticed: once macOS has allowed it, it is asked again
// now and then; a permission taken away stops the posting and tells the
// operator again.
func TestAccessTakenAwayIsNoticed(t *testing.T) {
	in, events, clock := testInput()
	allowed, asked := true, 0
	in.allowed = func() bool { asked++; return allowed }
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	in.pressKey("KeyA", true)
	in.pressKey("KeyA", false)
	if len(events.take()) != 2 || asked != 1 {
		t.Fatalf("macOS was asked %d times for two events, want once", asked)
	}
	allowed = false
	*clock = clock.Add(accessReconfirm)
	in.pressKey("KeyA", true)
	if len(events.take()) != 0 || asked != 2 {
		t.Fatalf("after the permission was taken away: events were posted, or macOS was asked %d times", asked)
	}
	if strings.Count(logged.String(), "Accessibility") != 1 {
		t.Fatalf("the operator should be told once:\n%s", logged.String())
	}
}

// TestCommandUpLetsTheKeysGo: a browser on a Mac sends no key-up for a key
// pressed while Command is held, so the host lets such keys go when Command
// comes up, with Command still in their flags; the next press of the key is
// a fresh press.
func TestCommandUpLetsTheKeysGo(t *testing.T) {
	in, events, _ := testInput()
	in.pressKey("MetaLeft", true)
	in.pressKey("KeyC", true)
	in.pressKey("MetaLeft", false)
	in.pressKey("KeyC", true)
	in.pressKey("KeyC", false)
	expect(t, events.take(),
		"flags 0x37 cmd+L-cmd",
		"key 0x08 down cmd+L-cmd",
		"key 0x08 up cmd+L-cmd",
		"flags 0x37 -",
		"key 0x08 down -", "key 0x08 up -",
	)
}
