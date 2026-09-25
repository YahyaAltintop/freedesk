//go:build windows

package consent

import (
	"context"
	"log"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	mbYesNo        = 0x00000004
	mbIconQuestion = 0x00000020
	mbDefButton2   = 0x00000100 // "No" is the default, so Enter never approves by accident
	mbTopmost      = 0x00040000

	idYes       = 6
	idNo        = 7
	mbTimedOut  = 32000
	wmCommand   = 0x0111
	dialogClass = "#32770"

	// SetWindowPos flags used to raise the box. It is put at the top of the
	// always-on-top band WITHOUT being activated: not activating is deliberate,
	// because the box must never take the keyboard focus on its own. If it did,
	// a key the operator was already typing into another window could land on
	// the box and, on a Yes/No question, approve the connection by accident.
	// The operator brings it to the front by clicking it, which is a choice.
	hwndTopmost   = ^uintptr(0) // (HWND)-1
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040

	// FlashWindowEx flags: flash the caption and taskbar button until the box
	// is brought to the foreground, so a request that opened behind other
	// windows is still noticed.
	flashwAll       = 0x00000003
	flashwTimerNoFG = 0x0000000C

	// raise() timing: how often to look for the box before it is up, how often
	// to re-assert its place at the top while it is up, and how long past the
	// request timeout to keep trying before giving up.
	raisePoll     = 25 * time.Millisecond
	raiseInterval = 300 * time.Millisecond
	raiseGrace    = 3 * time.Second
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxTimeout = user32.NewProc("MessageBoxTimeoutW")
	procFindWindow        = user32.NewProc("FindWindowW")
	procPostMessage       = user32.NewProc("PostMessageW")
	procSetWindowPos      = user32.NewProc("SetWindowPos")
	procFlashWindowEx     = user32.NewProc("FlashWindowEx")
	procIsWindowVisible   = user32.NewProc("IsWindowVisible")
)

// flashInfo mirrors FLASHWINFO. The explicit padding keeps the pointer-sized
// hwnd 8-byte aligned, so cbSize equals the C struct's size on amd64.
type flashInfo struct {
	cbSize    uint32
	_         uint32
	hwnd      uintptr
	dwFlags   uint32
	uCount    uint32
	dwTimeout uint32
	_         uint32
}

// Dialog asks the operator with a native always-on-top Yes/No message box, so
// nobody has to find the console window and type. One question at a time.
type Dialog struct {
	timeout time.Duration
	mu      sync.Mutex // guards seq only; the operator's attention is uiMu
	seq     uint64
}

// NewDialog returns the message-box approver.
func NewDialog(timeout time.Duration) *Dialog {
	return &Dialog{timeout: timeout}
}

// Ask blocks until the operator clicks Yes or No, the timeout passes, or ctx
// ends (the viewer withdrew the request). Only an explicit Yes approves.
func (d *Dialog) Ask(ctx context.Context, p Prompt) Answer {
	answer := Refused
	exclusive(func() { answer = d.ask(ctx, p) })
	return answer
}

// ask shows the box. It runs inside exclusive, which is what makes the session
// hold remote input back for as long as the box is up.
func (d *Dialog) ask(ctx context.Context, p Prompt) Answer {
	d.mu.Lock()
	d.seq++
	seq := d.seq
	d.mu.Unlock()
	// A unique title lets us find and dismiss exactly this box on withdrawal.
	title := p.Title(seq)
	text := p.Text(d.timeout)
	// Echo the question to the log too: it shows in the status window's
	// activity pane, and it leaves a record of what was asked.
	log.Printf("%s>>> Answer in the dialog window.", p.ConsoleHeader())

	result := make(chan uintptr, 1)
	go func() {
		// UI calls belong to one OS thread for the life of the window.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		ret, _, _ := procMessageBoxTimeout.Call(
			0,
			uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(text))),
			uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(title))),
			mbYesNo|mbIconQuestion|mbDefButton2|mbTopmost,
			0,
			uintptr(d.timeout.Milliseconds()),
		)
		result <- ret
	}()

	// MB_TOPMOST alone leaves the box in the top-most band but not necessarily
	// at the top of it, so a request can still open behind whatever the operator
	// is using. Raise and flash it from a helper goroutine, and keep raising it
	// while it is up so a window that briefly covers it does not bury it.
	go raise(title, d.timeout)

	select {
	case ret := <-result:
		switch ret {
		case idYes:
			return Allowed
		case mbTimedOut:
			log.Println(">>> no answer; request rejected")
			return Unanswered
		}
		return Refused
	case <-ctx.Done():
		// The viewer gave up: press "No" on the box so it does not linger.
		dismiss(title)
		<-result
		return Refused
	}
}

// findDialog looks once for the box with the given title, returning its handle
// or zero when it is not up yet. The title carries a unique sequence number, so
// this can only match the exact box we mean.
func findDialog(title string) uintptr {
	className := windows.StringToUTF16Ptr(dialogClass)
	titlePtr := windows.StringToUTF16Ptr(title)
	hwnd, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(titlePtr)))
	return hwnd
}

// dismiss posts a "No" click to the dialog with the given title. Best-effort:
// if the box is not found the timeout closes it anyway.
func dismiss(title string) {
	for range 10 {
		if hwnd := findDialog(title); hwnd != 0 {
			_, _, _ = procPostMessage.Call(hwnd, wmCommand, idNo, 0)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	log.Println("[consent] could not find the request dialog to dismiss it")
}

// raise brings the box with the given title to the top of the always-on-top
// band and flashes it, so a request does not sit unnoticed behind whatever the
// operator is doing. It never activates the box (see the SetWindowPos flags):
// the operator clicks it to answer, and until they do no keypress can reach it.
// Best-effort — the box is briefly not up yet when this starts, so it polls for
// up to a second, which is well within the request timeout.
func raise(title string, timeout time.Duration) {
	// Stop a little after the box would have timed out anyway, in case it is
	// dismissed in a way findDialog cannot see; normally the loop ends the
	// moment the box closes.
	deadline := time.Now().Add(timeout + raiseGrace)
	seen, flashed := false, false
	for time.Now().Before(deadline) {
		hwnd := findDialog(title)
		if hwnd == 0 || !isWindowVisible(hwnd) {
			// Not up yet — or already answered and gone, once we have seen it.
			if seen && hwnd == 0 {
				return
			}
			time.Sleep(raisePoll)
			continue
		}
		seen = true
		// Re-assert the top of the always-on-top band each pass: MB_TOPMOST
		// leaves the box in that band but not necessarily at its top, and a
		// window that pops up over it would otherwise bury it until answered.
		// No activation, so the keyboard focus never moves to the box.
		procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0,
			swpNoMove|swpNoSize|swpNoActivate|swpShowWindow)
		if !flashed {
			// Flash the caption and taskbar once, to draw the eye without
			// stealing focus; TIMERNOFG keeps it flashing until the box is
			// brought forward.
			fi := flashInfo{
				cbSize:  uint32(unsafe.Sizeof(flashInfo{})),
				hwnd:    hwnd,
				dwFlags: flashwAll | flashwTimerNoFG,
			}
			procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
			flashed = true
		}
		time.Sleep(raiseInterval)
	}
}

// isWindowVisible reports whether the window has been shown (WS_VISIBLE).
func isWindowVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

// newPlatformApprover picks the native dialog unless the operator asked for
// the console prompt (headless or scripted runs).
func newPlatformApprover(mode string, timeout time.Duration) Approver {
	if mode == ModeConsole {
		return NewConsole(timeout)
	}
	return NewDialog(timeout)
}
