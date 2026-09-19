//go:build windows

package clipboard

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	// A message-only window: never visible, never in the taskbar, but a real
	// HWND that can own the clipboard and receive its messages.
	hwndMessage = ^uintptr(2) // (HWND)-3

	// Another process can hold the clipboard — Office and clipboard managers do
	// it constantly. Retry briefly, then give up: this runs while the session's
	// message handling waits, so the budget has to stay small.
	openAttempts = 10
	openBackoff  = 25 * time.Millisecond
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard        = user32.NewProc("OpenClipboard")
	procCloseClipboard       = user32.NewProc("CloseClipboard")
	procEmptyClipboard       = user32.NewProc("EmptyClipboard")
	procGetClipboardData     = user32.NewProc("GetClipboardData")
	procSetClipboardData     = user32.NewProc("SetClipboardData")
	procIsFormatAvailable    = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardSequence = user32.NewProc("GetClipboardSequenceNumber")
	procRegisterClipboardFmt = user32.NewProc("RegisterClipboardFormatW")
	procCreateWindowEx       = user32.NewProc("CreateWindowExW")
	procDestroyWindow        = user32.NewProc("DestroyWindow")
	procDefWindowProc        = user32.NewProc("DefWindowProcW")
	procRegisterClass        = user32.NewProc("RegisterClassExW")
	procPeekMessage          = user32.NewProc("PeekMessageW")
	procTranslateMessage     = user32.NewProc("TranslateMessage")
	procDispatchMessage      = user32.NewProc("DispatchMessageW")
	procGetModuleHandle      = kernel32.NewProc("GetModuleHandleW")
	procGlobalAlloc          = kernel32.NewProc("GlobalAlloc")
	procGlobalFree           = kernel32.NewProc("GlobalFree")
	procGlobalLock           = kernel32.NewProc("GlobalLock")
	procGlobalUnlock         = kernel32.NewProc("GlobalUnlock")
	procGlobalSize           = kernel32.NewProc("GlobalSize")
)

// Formats that mean "do not put this in clipboard history or sync it".
// Password managers set them; honouring them is the cheapest real privacy win
// in this whole feature.
var (
	fmtExcludeFromMonitor  uint32
	fmtCanIncludeInHistory uint32
)

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// lock pins a global memory block and returns a pointer into it.
//
// GlobalLock hands back an address as a uintptr, and converting a uintptr to a
// pointer is normally unsound because the garbage collector may have moved the
// object out from under it. It is sound here, and only here, because this
// memory belongs to the operating system rather than to Go's heap: GlobalAlloc
// blocks do not move and the collector does not know about them. The
// conversion goes through the address of the uintptr so the reinterpretation
// is explicit rather than hidden behind a cast vet cannot check.
func lock(h uintptr) unsafe.Pointer {
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return nil
	}
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

// winBoard is the Windows clipboard. Every method must run on the worker's
// locked thread.
type winBoard struct {
	hwnd uintptr
}

func newPlatformBoard() (Board, error) {
	hwnd, err := createMessageWindow()
	if err != nil {
		return nil, err
	}
	fmtExcludeFromMonitor = registerFormat("ExcludeClipboardContentFromMonitorProcessing")
	fmtCanIncludeInHistory = registerFormat("CanIncludeInClipboardHistory")
	return &winBoard{hwnd: hwnd}, nil
}

func registerFormat(name string) uint32 {
	id, _, _ := procRegisterClipboardFmt.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(name))))
	return uint32(id)
}

// createMessageWindow makes the invisible window the clipboard writes need.
//
// Opening the clipboard with a NULL window sets its owner to NULL, and MSDN is
// explicit that this makes SetClipboardData fail. GetConsoleWindow is not a
// substitute: it is NULL in a detached run, and under Windows Terminal the
// handle belongs to conhost rather than to us.
func createMessageWindow() (uintptr, error) {
	instance, _, _ := procGetModuleHandle.Call(0)
	className := windows.StringToUTF16Ptr("FreeDeskClipboard")
	proc := func(hwnd uintptr, m uint32, w, l uintptr) uintptr {
		ret, _, _ := procDefWindowProc.Call(hwnd, uintptr(m), w, l)
		return ret
	}
	class := wndClassEx{
		size:      uint32(unsafe.Sizeof(wndClassEx{})),
		wndProc:   windows.NewCallback(proc),
		instance:  windows.Handle(instance),
		className: className,
	}
	// A class already registered from a previous board is fine.
	procRegisterClass.Call(uintptr(unsafe.Pointer(&class)))

	hwnd, _, err := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("FreeDesk"))),
		0, 0, 0, 0, 0,
		hwndMessage, 0, instance, 0,
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("could not create the clipboard window: %w", err)
	}
	return hwnd, nil
}

func (b *winBoard) Close() {
	if b.hwnd != 0 {
		procDestroyWindow.Call(b.hwnd)
		b.hwnd = 0
	}
}

func (b *winBoard) Sequence() uint32 {
	n, _, _ := procGetClipboardSequence.Call()
	return uint32(n)
}

// open takes the clipboard, retrying while another process holds it.
func (b *winBoard) open() error {
	for i := range openAttempts {
		if r, _, _ := procOpenClipboard.Call(b.hwnd); r != 0 {
			return nil
		}
		if i < openAttempts-1 {
			time.Sleep(openBackoff)
		}
	}
	return ErrBusy
}

func (b *winBoard) close() { procCloseClipboard.Call() }

func (b *winBoard) Excluded() bool {
	if available(fmtExcludeFromMonitor) {
		return true
	}
	// CanIncludeInClipboardHistory present and set to 0 means the same thing.
	if !available(fmtCanIncludeInHistory) {
		return false
	}
	if err := b.open(); err != nil {
		return true // cannot tell; assume it is private
	}
	defer b.close()
	h, _, _ := procGetClipboardData.Call(uintptr(fmtCanIncludeInHistory))
	if h == 0 {
		return false
	}
	p := lock(h)
	if p == nil {
		return false
	}
	defer procGlobalUnlock.Call(h)
	return *(*uint32)(p) == 0
}

func available(format uint32) bool {
	if format == 0 {
		return false
	}
	r, _, _ := procIsFormatAvailable.Call(uintptr(format))
	return r != 0
}

func (b *winBoard) ReadText() (string, bool, error) {
	if !available(cfUnicodeText) {
		return "", false, nil
	}
	if err := b.open(); err != nil {
		return "", false, err
	}
	defer b.close()

	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false, nil
	}
	p := lock(h)
	if p == nil {
		return "", false, nil
	}
	defer procGlobalUnlock.Call(h)

	// The handle belongs to the clipboard: never free it, and copy the data out
	// before CloseClipboard invalidates it.
	size, _, _ := procGlobalSize.Call(h)
	maxChars := int(size / 2)
	if maxChars <= 0 {
		return "", false, nil
	}
	text := windows.UTF16ToString(unsafe.Slice((*uint16)(p), maxChars))
	return text, true, nil
}

func (b *winBoard) WriteText(s string) error {
	utf16, err := windows.UTF16FromString(s)
	if err != nil {
		return err
	}
	// GMEM_MOVEABLE is required: a fixed handle is not a valid clipboard handle.
	bytes := uintptr(len(utf16) * 2)
	h, _, allocErr := procGlobalAlloc.Call(gmemMoveable, bytes)
	if h == 0 {
		return fmt.Errorf("could not allocate clipboard memory: %w", allocErr)
	}
	p := lock(h)
	if p == nil {
		procGlobalFree.Call(h)
		return fmt.Errorf("could not lock clipboard memory")
	}
	copy(unsafe.Slice((*uint16)(p), len(utf16)), utf16)
	procGlobalUnlock.Call(h)

	if err := b.open(); err != nil {
		procGlobalFree.Call(h)
		return err
	}
	defer b.close()

	procEmptyClipboard.Call()
	if r, _, setErr := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		// Ownership only transfers on success; on failure the block is still
		// ours and leaks for the life of the process if we do not free it.
		procGlobalFree.Call(h)
		return fmt.Errorf("could not set the clipboard: %w", setErr)
	}
	// On success the system owns the handle. Freeing it here would be a
	// use-after-free for anything that pastes afterwards.
	return nil
}

// pumpMessages drains the window's queue.
//
// Needed even though nothing is drawn: when another process calls
// EmptyClipboard it SENDS WM_DESTROYCLIPBOARD to the owner, which blocks THAT
// process until the owner responds. A window that never pumps would hang
// whatever application the operator is using.
func pumpMessages() {
	var m msg
	for {
		r, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 1 /* PM_REMOVE */)
		if r == 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}
