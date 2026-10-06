//go:build windows

package ui

import (
	"log"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The licenses window: the license texts the exe carries, in a resizable,
// read-only text box. It is a window of its own class with its own procedure,
// never the status window's, because that one's close button stops the agent
// and closing the licenses must do nothing of the kind. It is owned by the
// status window, so it stays in front of it, takes no taskbar button and goes
// when the status window goes. Everything here runs on the UI thread.

const (
	licensesClass      = "FreeDeskLicenses"
	licensesTitle      = "FreeDesk licenses"
	wsOverlappedWindow = 0x00CF0000
	wmSize             = 0x0005
	swRestore          = 9
)

var (
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procMoveWindow          = user32.NewProc("MoveWindow")
	procSetFocus            = user32.NewProc("SetFocus")
)

// licenses is the open licenses window and its text box, zero when closed.
var licenses struct{ hwnd, edit uintptr }

// licensesProcPtr registers the procedure with Win32 exactly once, as
// wndProcPtr does: NewCallback slots are a small, process-wide pool.
var licensesProcPtr = sync.OnceValue(func() uintptr { return windows.NewCallback(licensesProc) })

// showLicenses opens the licenses window, or brings the open one forward.
func (w *window) showLicenses() {
	if licenses.hwnd != 0 {
		procShowWindow.Call(licenses.hwnd, swRestore)
		procSetForegroundWindow.Call(licenses.hwnd)
		return
	}
	class := wndClassEx{
		size:       uint32(unsafe.Sizeof(wndClassEx{})),
		wndProc:    licensesProcPtr(),
		instance:   windows.Handle(w.instance),
		icon:       w.icon(smCxIcon, smCyIcon),
		cursor:     loadCursor(idcArrow),
		background: colorBtnFace + 1,
		className:  utf16(licensesClass),
		iconSm:     w.icon(smCxSmIcon, smCySmIcon),
	}
	if atom, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class))); atom == 0 && err != windows.ERROR_CLASS_ALREADY_EXISTS {
		log.Printf("[host-agent] could not show the licenses: %v", err)
		return
	}

	cw, ch := w.scale(640), w.scale(480)
	r := rect{right: cw, bottom: ch}
	procAdjustWindowRect.Call(uintptr(unsafe.Pointer(&r)), wsOverlappedWindow, 0, 0)
	width, height := r.right-r.left, r.bottom-r.top
	screenW, _, _ := procGetSystemMetrics.Call(smCxScreen)
	screenH, _, _ := procGetSystemMetrics.Call(smCyScreen)
	x := max((int32(screenW)-width)/2, 0)
	y := max((int32(screenH)-height)/2, 0)

	hwnd, _, err := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(utf16(licensesClass))),
		uintptr(unsafe.Pointer(utf16(licensesTitle))),
		wsOverlappedWindow,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		w.hwnd, // the owner, not a parent: a top-level window that goes with it
		0, w.instance, 0,
	)
	if hwnd == 0 {
		log.Printf("[host-agent] could not show the licenses: %v", err)
		return
	}
	// Made at the full client size; WM_SIZE keeps it so from then on.
	edit, _, _ := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(utf16("EDIT"))),
		0,
		wsChild|wsVisible|wsVScroll|wsTabStop|esMultiline|esReadOnly|esAutoVScroll,
		0, 0, uintptr(cw), uintptr(ch),
		hwnd, 0, w.instance, 0,
	)
	licenses.hwnd, licenses.edit = hwnd, edit
	if edit != 0 {
		procSendMessage.Call(edit, wmSetFont, w.fonts.mono, 1)
		// No length limit (0 means the largest there is). The texts are set,
		// not typed, so the limit would not bite; it is said all the same.
		procSendMessage.Call(edit, emSetLimitText, 0, 0)
		w.setText(edit, editText(w.opts.Licenses))
	}
	procShowWindow.Call(hwnd, swShow)
	if edit != 0 {
		procSetFocus.Call(edit)
	}
}

// editText makes s fit an edit control: CRLF line ends, and no NUL, which
// would turn the whole text into a question mark on its way to UTF-16.
func editText(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

func licensesProc(hwnd uintptr, m uint32, wParam, lParam uintptr) uintptr {
	switch m {
	case wmSize:
		// The first WM_SIZE comes while the window is being created, before
		// the text box exists.
		if hwnd == licenses.hwnd && licenses.edit != 0 {
			procMoveWindow.Call(licenses.edit, 0, 0, lParam&0xFFFF, (lParam>>16)&0xFFFF, 1)
		}
		return 0
	case wmCtlColorStatic:
		// A read-only edit asks its parent for colours as a static does:
		// white, like the activity pane, not the grey of a dialog.
		if lParam != 0 && lParam == licenses.edit {
			procSetBkColor.Call(wParam, 0xFFFFFF)
			brush, _, _ := procGetStockObject.Call(whiteBrush)
			return brush
		}
	case wmDestroy:
		// Only this window goes. Never PostQuitMessage here: the message loop
		// is the status window's, and it must go on.
		if hwnd == licenses.hwnd {
			licenses.hwnd, licenses.edit = 0, 0
		}
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, uintptr(m), wParam, lParam)
	return r
}
