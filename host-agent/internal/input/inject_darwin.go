//go:build darwin && cgo

package input

/*
// -mmacosx-version-min with the availability check as an error: every macOS
// API this package reaches must exist on macOS 13, the oldest the agent runs
// on, whatever macOS builds it. Plain C: the Objective-C lives in
// internal/macos.
#cgo CFLAGS: -mmacosx-version-min=13.0 -Werror=unguarded-availability-new
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
#include "cgevent_darwin.h"
*/
import "C"

import (
	"log"
	"math"
	"sync"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/macos"
)

// macOS takes input as CoreGraphics events (cgevent_darwin.c), and unlike
// Windows' SendInput it wants each event to say more than the protocol does:
// which modifiers are held (an "a" posted while a Shift is down types "a"
// unless the event itself says Shift), how many clicks a click is (a double
// click is a second click that says it is the second), and which button a
// move drags with (a move with the button down must say so, or nothing gets
// selected or dragged). cgInput keeps that state and writes it into every
// event.

// eventType is a CGEventType.
type eventType uint32

const (
	evMoved        eventType = C.kCGEventMouseMoved
	evLeftDown     eventType = C.kCGEventLeftMouseDown
	evLeftUp       eventType = C.kCGEventLeftMouseUp
	evLeftDragged  eventType = C.kCGEventLeftMouseDragged
	evRightDown    eventType = C.kCGEventRightMouseDown
	evRightUp      eventType = C.kCGEventRightMouseUp
	evRightDragged eventType = C.kCGEventRightMouseDragged
	evOtherDown    eventType = C.kCGEventOtherMouseDown
	evOtherUp      eventType = C.kCGEventOtherMouseUp
	evOtherDragged eventType = C.kCGEventOtherMouseDragged
)

// The events of each button, by protocol number: 0 left, 1 right, 2 middle,
// which are also CoreGraphics' own numbers for them.
var (
	downEvent    = [3]eventType{evLeftDown, evRightDown, evOtherDown}
	upEvent      = [3]eventType{evLeftUp, evRightUp, evOtherUp}
	draggedEvent = [3]eventType{evLeftDragged, evRightDragged, evOtherDragged}
)

// The modifier flags (CGEventFlags): the masks every application reads, and
// the device bits that tell the left key from the right one.
const (
	flagAlphaShift  = uint64(C.kCGEventFlagMaskAlphaShift)
	flagShift       = uint64(C.kCGEventFlagMaskShift)
	flagControl     = uint64(C.kCGEventFlagMaskControl)
	flagOption      = uint64(C.kCGEventFlagMaskAlternate)
	flagCommand     = uint64(C.kCGEventFlagMaskCommand)
	flagSecondaryFn = uint64(C.kCGEventFlagMaskSecondaryFn)

	deviceLeftControl  = uint64(C.NX_DEVICELCTLKEYMASK)
	deviceLeftShift    = uint64(C.NX_DEVICELSHIFTKEYMASK)
	deviceRightShift   = uint64(C.NX_DEVICERSHIFTKEYMASK)
	deviceLeftCommand  = uint64(C.NX_DEVICELCMDKEYMASK)
	deviceRightCommand = uint64(C.NX_DEVICERCMDKEYMASK)
	deviceLeftOption   = uint64(C.NX_DEVICELALTKEYMASK)
	deviceRightOption  = uint64(C.NX_DEVICERALTKEYMASK)
	deviceRightControl = uint64(C.NX_DEVICERCTLKEYMASK)
)

// modifierFlags is what holding each modifier key adds to the flags of every
// event, as the hardware's own events state it.
var modifierFlags = map[uint16]uint64{
	kvkShift:        flagShift | deviceLeftShift,
	kvkRightShift:   flagShift | deviceRightShift,
	kvkControl:      flagControl | deviceLeftControl,
	kvkRightControl: flagControl | deviceRightControl,
	kvkOption:       flagOption | deviceLeftOption,
	kvkRightOption:  flagOption | deviceRightOption,
	kvkCommand:      flagCommand | deviceLeftCommand,
	kvkRightCommand: flagCommand | deviceRightCommand,
	kvkFunction:     flagSecondaryFn,
}

const (
	// clickSlop is how far, in points, a click may land from the one before
	// it and still count towards a double click.
	clickSlop = 4.0
	// accessRecheck spaces the questions to macOS while it does not let the
	// agent post input, so a permission given meanwhile is noticed.
	accessRecheck = 2 * time.Second
	// accessReconfirm spaces the checks, once macOS has said yes, that it
	// still does: a permission taken away drops every event without a word.
	accessReconfirm = time.Minute
	// accessWarnEvery spaces the reminders, in the activity log, of why the
	// viewer's mouse and keyboard do nothing.
	accessWarnEvery = time.Minute
)

// sink is where cgInput's events go: CoreGraphics, or a test's record.
type sink interface {
	mouse(t eventType, x, y float64, button, clicks int, flags uint64)
	key(keycode uint16, down, repeat bool, flags uint64)
	flagsChanged(keycode uint16, flags uint64)
	scroll(vertical, horizontal int32, flags uint64)
}

// cgInput turns the protocol's input into CoreGraphics events. One serves
// every session, as there is one mouse and one keyboard.
type cgInput struct {
	mu sync.Mutex

	out         sink
	screen      func() (w, h float64, ok bool) // the main display, in points
	doubleClick func() time.Duration           // the operator's double-click speed
	now         func() time.Time
	allowed     func() bool // whether macOS lets the agent post input

	x, y     float64 // where the pointer was put last, in points
	buttons  [3]bool // held, by protocol number
	click    lastClick
	mods     map[uint16]bool // modifier keys held
	capsLock bool
	keys     map[uint16]bool // other keys held

	trusted bool      // what macOS said when last asked
	nextAsk time.Time // when to ask it again
	warned  time.Time // when the operator was last told why nothing happens
}

// lastClick is the press a next one may make a double click of.
type lastClick struct {
	button int
	at     time.Time
	x, y   float64
	count  int
}

func newCGInput(out sink) *cgInput {
	return &cgInput{
		out:         out,
		screen:      mainScreen,
		doubleClick: doubleClickInterval,
		now:         time.Now,
		allowed:     func() bool { return CheckAccess(false) },
		mods:        make(map[uint16]bool),
		keys:        make(map[uint16]bool),
	}
}

// system is the injector the protocol's input goes to (handler.go).
var system = newCGInput(cgSink{})

func moveMouse(x, y float64)                         { system.moveMouse(x, y) }
func clickMouse(button int, down bool, x, y float64) { system.clickMouse(button, down, x, y) }
func scrollWheel(dx, dy int)                         { system.scrollWheel(dx, dy) }
func pressKey(code string, down bool)                { system.pressKey(code, down) }

// CheckAccess reports whether macOS lets the agent control this computer:
// post mouse and keyboard input. With prompt, macOS asks the operator if it
// never has.
func CheckAccess(prompt bool) bool {
	p := C.int(0)
	if prompt {
		p = 1
	}
	return C.fd_cg_access(p) != 0
}

func (in *cgInput) moveMouse(x, y float64) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.x, in.y = in.point(x, y)
	for b, held := range in.buttons {
		if held {
			in.emitMouse(draggedEvent[b], b, in.clicksOf(b))
			return
		}
	}
	in.emitMouse(evMoved, 0, 0)
}

func (in *cgInput) clickMouse(button int, down bool, x, y float64) {
	if button < 0 || button >= len(in.buttons) {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	in.x, in.y = in.point(x, y)
	in.buttons[button] = down
	if !down {
		in.emitMouse(upEvent[button], button, in.clicksOf(button))
		return
	}
	// A press of the same button soon after the last one, close to where it
	// was, is the next click of a double (or triple) click, as macOS counts
	// a local mouse's.
	now := in.now()
	c := &in.click
	if c.count > 0 && c.button == button && now.Sub(c.at) <= in.doubleClick() &&
		math.Hypot(in.x-c.x, in.y-c.y) <= clickSlop {
		c.count++
	} else {
		c.count = 1
	}
	c.button, c.at, c.x, c.y = button, now, in.x, in.y
	in.emitMouse(downEvent[button], button, c.count)
}

// clicksOf is the click count a button's events carry: the count of its last
// press, so the release and the drags of a double click are a double click's.
func (in *cgInput) clicksOf(button int) int {
	if in.click.button == button && in.click.count > 0 {
		return in.click.count
	}
	return 1
}

func (in *cgInput) scrollWheel(dx, dy int) {
	vertical, horizontal := scrollAxes(dx, dy)
	if vertical == 0 && horizontal == 0 {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.mayPost() {
		in.out.scroll(vertical, horizontal, in.flags())
	}
}

// scrollAxes turns the protocol's wheel deltas into CoreGraphics' two axes,
// in pixels. A positive vertical delta scrolls up there, as the protocol's dy
// does; a positive horizontal one scrolls left, where the protocol's dx, as
// the browser counts it, is positive for right.
func scrollAxes(dx, dy int) (vertical, horizontal int32) {
	return clampInt32(dy), clampInt32(-dx)
}

func clampInt32(v int) int32 {
	return int32(max(min(v, math.MaxInt32), math.MinInt32))
}

func (in *cgInput) pressKey(code string, down bool) {
	kc, ok := codeToKeycode(code)
	if !ok {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	switch {
	case kc == kvkCapsLock:
		// Caps Lock turns on or off where it goes down, as it does on
		// Windows, and its coming up changes nothing. The state lives in the
		// flags of the events posted from here; the Mac's own Caps Lock is
		// left as it is.
		if down {
			in.capsLock = !in.capsLock
			in.emitFlags(kc)
		}
	case modifierFlags[kc] != 0:
		if in.mods[kc] == down {
			return // a browser repeating a held modifier, or a release never pressed
		}
		if down {
			in.mods[kc] = true
		} else {
			if kc == kvkCommand && !in.mods[kvkRightCommand] || kc == kvkRightCommand && !in.mods[kvkCommand] {
				in.releaseHeldKeys() // the last Command key coming up
			}
			delete(in.mods, kc)
		}
		in.emitFlags(kc)
	default:
		repeat := down && in.keys[kc]
		if down {
			in.keys[kc] = true
		} else {
			delete(in.keys, kc)
		}
		if in.mayPost() {
			in.out.key(kc, down, repeat, in.flags())
		}
	}
}

// releaseHeldKeys lets go of every key held other than the modifiers, with
// the modifiers still in the flags. A browser on a Mac delivers no key-up
// for a key pressed while Command is held (the C of Command-C never comes
// up), so when Command itself comes up, those keys come up here, as the
// hardware's would have.
func (in *cgInput) releaseHeldKeys() {
	for kc := range in.keys {
		delete(in.keys, kc)
		if in.mayPost() {
			in.out.key(kc, false, false, in.flags())
		}
	}
}

// flags is what every event says is held: the modifier keys down, and Caps
// Lock while it is on.
func (in *cgInput) flags() uint64 {
	var f uint64
	for kc := range in.mods {
		f |= modifierFlags[kc]
	}
	if in.capsLock {
		f |= flagAlphaShift
	}
	return f
}

// point maps a normalised position onto the main display, in points: the
// display the capture shows, whose top-left corner is the origin CoreGraphics
// counts from. The right and bottom edges stay a point inside it, where
// another display may begin. Without a display the pointer stays where it is.
func (in *cgInput) point(x, y float64) (float64, float64) {
	w, h, ok := in.screen()
	if !ok {
		return in.x, in.y
	}
	return min(clamp01(x)*w, w-1), min(clamp01(y)*h, h-1)
}

func (in *cgInput) emitMouse(t eventType, button, clicks int) {
	if in.mayPost() {
		in.out.mouse(t, in.x, in.y, button, clicks, in.flags())
	}
}

func (in *cgInput) emitFlags(keycode uint16) {
	if in.mayPost() {
		in.out.flagsChanged(keycode, in.flags())
	}
}

// mayPost reports whether macOS lets the agent post input, asking it now and
// then: every few seconds while it says no, since it then drops every event
// without a word and the operator is told instead; and once a minute after
// it has said yes, since a permission taken away drops them as silently.
// The held state above is kept either way.
func (in *cgInput) mayPost() bool {
	now := in.now()
	if now.Before(in.nextAsk) {
		return in.trusted
	}
	if in.allowed() {
		in.trusted = true
		in.nextAsk = now.Add(accessReconfirm)
		return true
	}
	if in.trusted {
		in.trusted = false
		in.warned = time.Time{} // taken away: told at once
	}
	in.nextAsk = now.Add(accessRecheck)
	if in.warned.IsZero() || now.Sub(in.warned) >= accessWarnEvery {
		in.warned = now
		log.Printf("[input] macOS does not let FreeDesk control this computer, so the viewer's mouse and keyboard do nothing: allow FreeDesk in System Settings > Privacy & Security > Accessibility")
	}
	return false
}

// cgSink posts into CoreGraphics.
type cgSink struct{}

func (cgSink) mouse(t eventType, x, y float64, button, clicks int, flags uint64) {
	C.fd_cg_mouse(C.uint32_t(t), C.double(x), C.double(y), C.uint32_t(button), C.int64_t(clicks), C.uint64_t(flags))
}

func (cgSink) key(keycode uint16, down, repeat bool, flags uint64) {
	C.fd_cg_key(C.uint16_t(keycode), cBool(down), cBool(repeat), C.uint64_t(flags))
}

func (cgSink) flagsChanged(keycode uint16, flags uint64) {
	C.fd_cg_flags_changed(C.uint16_t(keycode), C.uint64_t(flags))
}

func (cgSink) scroll(vertical, horizontal int32, flags uint64) {
	C.fd_cg_scroll(C.int32_t(vertical), C.int32_t(horizontal), C.uint64_t(flags))
}

func cBool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// mainScreen is the main display's size in points.
func mainScreen() (w, h float64, ok bool) {
	var cw, ch C.double
	if C.fd_cg_screen(&cw, &ch) == 0 {
		return 0, 0, false
	}
	return float64(cw), float64(ch), true
}

// doubleClickInterval is the operator's double-click speed setting, which
// only AppKit says.
func doubleClickInterval() time.Duration { return macos.DoubleClickInterval() }

// cursorPosition is where the pointer is, in global points.
func cursorPosition() (x, y float64) {
	var cx, cy C.double
	C.fd_cg_cursor(&cx, &cy)
	return float64(cx), float64(cy)
}
