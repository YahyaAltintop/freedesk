//go:build windows

package consent

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	mbYesNo         = 0x00000004
	mbIconQuestion  = 0x00000020
	mbDefButton2    = 0x00000100 // "No" is the default, so Enter never approves by accident
	mbSetForeground = 0x00010000
	mbTopmost       = 0x00040000

	idYes       = 6
	idNo        = 7
	mbTimedOut  = 32000
	wmCommand   = 0x0111
	dialogClass = "#32770"
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxTimeout = user32.NewProc("MessageBoxTimeoutW")
	procFindWindow        = user32.NewProc("FindWindowW")
	procPostMessage       = user32.NewProc("PostMessageW")
)

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
func (d *Dialog) Ask(ctx context.Context, p Prompt) bool {
	uiMu.Lock()
	defer uiMu.Unlock()

	d.mu.Lock()
	d.seq++
	seq := d.seq
	d.mu.Unlock()
	// A unique title lets us find and dismiss exactly this box on withdrawal.
	title := p.Title(seq)
	text := p.Text(d.timeout)
	// Echo the question to the console too: the operator may be looking there,
	// and it leaves a record of what was asked.
	fmt.Printf("%s>>> Answer in the dialog window.\n", p.ConsoleHeader())

	result := make(chan uintptr, 1)
	go func() {
		// UI calls belong to one OS thread for the life of the window.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		ret, _, _ := procMessageBoxTimeout.Call(
			0,
			uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(text))),
			uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(title))),
			mbYesNo|mbIconQuestion|mbDefButton2|mbSetForeground|mbTopmost,
			0,
			uintptr(d.timeout.Milliseconds()),
		)
		result <- ret
	}()

	select {
	case ret := <-result:
		switch ret {
		case idYes:
			return true
		case mbTimedOut:
			fmt.Println(">>> no answer; request rejected")
		}
		return false
	case <-ctx.Done():
		// The viewer gave up: press "No" on the box so it does not linger.
		dismiss(title)
		<-result
		return false
	}
}

// dismiss posts a "No" click to the dialog with the given title. Best-effort:
// if the box is not found the timeout closes it anyway.
func dismiss(title string) {
	className := windows.StringToUTF16Ptr(dialogClass)
	titlePtr := windows.StringToUTF16Ptr(title)
	for range 10 {
		hwnd, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(titlePtr)))
		if hwnd != 0 {
			_, _, _ = procPostMessage.Call(hwnd, wmCommand, idNo, 0)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	log.Println("[consent] could not find the request dialog to dismiss it")
}

// newPlatformApprover picks the native dialog unless the operator asked for
// the console prompt (headless or scripted runs).
func newPlatformApprover(mode string, timeout time.Duration) Approver {
	if mode == ModeConsole {
		return NewConsole(timeout)
	}
	return NewDialog(timeout)
}
