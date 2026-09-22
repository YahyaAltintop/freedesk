//go:build windows

package ui

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/clipboard"
)

// The window is plain Win32 through user32/gdi32: no toolkit, no CGO, the same
// way the clipboard and consent packages already talk to Windows.

const (
	className = "FreeDeskStatus"

	// Window and control styles.
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsClipChildren = 0x02000000
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsVScroll      = 0x00200000
	wsTabStop      = 0x00010000
	wsExClientEdge = 0x00000200
	ssLeft         = 0x0000
	ssCenter       = 0x0001
	ssNoPrefix     = 0x0080
	bsPushButton   = 0x0000
	bsOwnerDraw    = 0x000B
	esMultiline    = 0x0004
	esAutoVScroll  = 0x0040
	esReadOnly     = 0x0800

	mainStyle = wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren

	// Messages.
	wmDestroy         = 0x0002
	wmGetTextLength   = 0x000E
	wmClose           = 0x0010
	wmDrawItem        = 0x002B
	wmQueryEndSession = 0x0011
	wmEndSession      = 0x0016
	wmSetFont         = 0x0030
	wmCommand         = 0x0111
	wmTimer           = 0x0113
	wmCtlColorStatic  = 0x0138
	emSetSel          = 0x00B1
	emScrollCaret     = 0x00B7
	emReplaceSel      = 0x00C2
	emSetLimitText    = 0x00C5
	wmApp             = 0x8000
	wmRefresh         = wmApp + 1 // the queue has something for the UI thread
	wmDone            = wmApp + 2 // the agent finished; wParam is the exit code

	// Everything else.
	swShow           = 5
	smCxScreen       = 0
	smCyScreen       = 1
	smCxIcon         = 11
	smCyIcon         = 12
	smCxSmIcon       = 49
	smCySmIcon       = 50
	idcArrow         = 32512
	imageIcon        = 1
	lrShared         = 0x8000
	colorBtnFace     = 15
	colorGrayText    = 17
	whiteBrush       = 0
	bkTransparent    = 1
	fwNormal         = 400
	fwBold           = 700
	defaultCharset   = 1
	clearTypeQuality = 5
	scClose          = 0xF060
	mfGrayed         = 0x0001
	bnClicked        = 0
	idCopy           = 1001
	idUpdate         = 1002
	idCopiedTimer    = 1
	swHide           = 0
	swShowNormal     = 1
	appIconID        = 1 // the icon group go-winres puts first in the exe

	// Colours are COLORREFs, which are BGR.
	red           = 0x000000C0
	white         = 0x00FFFFFF
	violet        = 0x00F65C8B // #8b5cf6, the web page's accent
	violetPressed = 0x00D83F6D // #6d3fd8

	// Owner-drawn button state and DrawText flags.
	odsSelected  = 0x0001
	dtCenter     = 0x0001
	dtVCenter    = 0x0004
	dtSingleLine = 0x0020
	psSolid      = 0

	// What the window says while the agent is still signing in.
	startingText = "Starting…"

	// endSessionGrace is how long the window waits for the agent when Windows
	// is shutting down or logging off. Windows grants about five seconds after
	// WM_ENDSESSION; the agent's own clean-up normally takes well under one.
	endSessionGrace = 4 * time.Second
	copiedFlash     = 1500 * time.Millisecond
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procShellExecute = shell32.NewProc("ShellExecuteW")

	procRegisterClassEx  = user32.NewProc("RegisterClassExW")
	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procGetMessage       = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procPostMessage      = user32.NewProc("PostMessageW")
	procSendMessage      = user32.NewProc("SendMessageW")
	procSetWindowText    = user32.NewProc("SetWindowTextW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procAdjustWindowRect = user32.NewProc("AdjustWindowRectEx")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procGetDpiForSystem  = user32.NewProc("GetDpiForSystem")
	procLoadImage        = user32.NewProc("LoadImageW")
	procLoadCursor       = user32.NewProc("LoadCursorW")
	procGetSysColorBrush = user32.NewProc("GetSysColorBrush")
	procGetSysColor      = user32.NewProc("GetSysColor")
	procEnableWindow     = user32.NewProc("EnableWindow")
	procGetSystemMenu    = user32.NewProc("GetSystemMenu")
	procEnableMenuItem   = user32.NewProc("EnableMenuItem")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
	procSetTimer         = user32.NewProc("SetTimer")
	procKillTimer        = user32.NewProc("KillTimer")
	procFillRect         = user32.NewProc("FillRect")
	procDrawText         = user32.NewProc("DrawTextW")
	procCreateFont       = gdi32.NewProc("CreateFontW")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procCreatePen        = gdi32.NewProc("CreatePen")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procRoundRect        = gdi32.NewProc("RoundRect")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetBkColor       = gdi32.NewProc("SetBkColor")
	procGetStockObject   = gdi32.NewProc("GetStockObject")
	procGetModuleHandle  = kernel32.NewProc("GetModuleHandleW")
)

// Mirrors of the C structs; the field order is the layout.
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

type rect struct{ left, top, right, bottom int32 }

// drawItem is DRAWITEMSTRUCT, what Windows hands an owner-drawn control's
// parent with WM_DRAWITEM.
type drawItem struct {
	ctlType    uint32
	ctlID      uint32
	itemID     uint32
	itemAction uint32
	itemState  uint32
	_          uint32 // padding before the first pointer-sized field
	hwndItem   uintptr
	hdc        uintptr
	rcItem     rect
	itemData   uintptr
}

// update is one change for the UI thread to apply.
type update struct {
	kind uint8
	a, b string
}

const (
	kLine uint8 = iota
	kCode
	kStatus
	kUpdate
)

// window is the one status window of the process. Fields after the handles
// belong to the UI thread; the queue is the only thing shared with others.
type window struct {
	opts     Options
	instance uintptr
	dpi      uint32
	fonts    struct{ ui, big, mono, bold uintptr }
	// GDI objects for the owner-drawn update button, made once in create.
	paint struct{ brush, brushPressed, pen, penPressed uintptr }

	hwnd        uintptr
	labelCtl    uintptr
	codeCtl     uintptr
	copyBtn     uintptr
	siteCtl     uintptr
	statusCtl   uintptr
	updateBtn   uintptr // hidden until a newer release is known
	activityCtl uintptr

	mu      sync.Mutex
	pending []update
	wake    atomic.Bool // a wmRefresh is already on its way

	ring       *lineRing
	code       string
	updateURL  string
	updateText string
	closing    bool
	done       bool
	failed     bool
	exitCode   int

	finished  chan struct{}
	finishing sync.Once
}

// current is the window wndProc works on. Win32 calls the procedure while
// CreateWindowExW is still running, before any handle could be stored, so the
// procedure finds its state here rather than through the handle.
var current *window

// wndProcPtr registers the window procedure with Win32 exactly once: every
// NewCallback takes one of a small process-wide pool for good.
var wndProcPtr = sync.OnceValue(func() uintptr { return windows.NewCallback(wndProc) })

// Open creates and shows the window on the calling thread, which must stay
// locked for the life of the window.
func Open(o Options) (Window, error) {
	if current != nil {
		return nil, fmt.Errorf("the status window is already open")
	}
	w := &window{opts: o, ring: newLineRing(), finished: make(chan struct{})}
	if err := w.create(); err != nil {
		return nil, err
	}
	return w, nil
}

// Echo is where log lines go besides the window: stderr when it is a real
// console, pipe or file, nil under -H=windowsgui without a console (the
// handle then exists but every write to it fails).
func Echo() io.Writer {
	if os.Stderr == nil {
		return nil
	}
	t, err := windows.GetFileType(windows.Handle(os.Stderr.Fd()))
	if err != nil || t == windows.FILE_TYPE_UNKNOWN {
		return nil
	}
	return os.Stderr
}

func (w *window) create() error {
	w.instance, _, _ = procGetModuleHandle.Call(0)
	w.dpi = systemDPI()
	w.fonts.ui = w.font("Segoe UI", 9, fwNormal)
	w.fonts.big = w.font("Segoe UI", 32, fwBold)
	w.fonts.mono = w.font("Consolas", 9, fwNormal)
	w.fonts.bold = w.font("Segoe UI", 10, fwBold)
	w.paint.brush, _, _ = procCreateSolidBrush.Call(violet)
	w.paint.brushPressed, _, _ = procCreateSolidBrush.Call(violetPressed)
	w.paint.pen, _, _ = procCreatePen.Call(psSolid, 1, violet)
	w.paint.penPressed, _, _ = procCreatePen.Call(psSolid, 1, violetPressed)

	class := wndClassEx{
		size:       uint32(unsafe.Sizeof(wndClassEx{})),
		wndProc:    wndProcPtr(),
		instance:   windows.Handle(w.instance),
		icon:       w.icon(smCxIcon, smCyIcon),
		cursor:     loadCursor(idcArrow),
		background: colorBtnFace + 1, // a system colour, not a brush handle
		className:  utf16(className),
		iconSm:     w.icon(smCxSmIcon, smCySmIcon),
	}
	if atom, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class))); atom == 0 && err != windows.ERROR_CLASS_ALREADY_EXISTS {
		return fmt.Errorf("could not register the window class: %w", err)
	}

	// Size the frame around the client area we lay out, then centre it.
	r := rect{right: w.scale(460), bottom: w.scale(428)}
	procAdjustWindowRect.Call(uintptr(unsafe.Pointer(&r)), mainStyle, 0, 0)
	width, height := r.right-r.left, r.bottom-r.top
	screenW, _, _ := procGetSystemMetrics.Call(smCxScreen)
	screenH, _, _ := procGetSystemMetrics.Call(smCyScreen)
	x := max((int32(screenW)-width)/2, 0)
	y := max((int32(screenH)-height)/2, 0)

	current = w
	hwnd, _, err := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(utf16(className))),
		uintptr(unsafe.Pointer(utf16(w.opts.Title))),
		mainStyle,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		0, 0, w.instance, 0,
	)
	if hwnd == 0 {
		current = nil
		return fmt.Errorf("could not create the status window: %w", err)
	}
	w.mu.Lock()
	w.hwnd = hwnd
	w.mu.Unlock()

	w.labelCtl = w.child(0, "STATIC", "This computer's code", ssCenter, 20, 16, 420, 20, 0, w.fonts.ui)
	w.codeCtl = w.child(0, "STATIC", startingText, ssCenter|ssNoPrefix, 20, 40, 420, 60, 0, w.fonts.big)
	w.copyBtn = w.child(0, "BUTTON", "Copy code", bsPushButton|wsTabStop, 175, 108, 110, 28, idCopy, w.fonts.ui)
	w.siteCtl = w.child(0, "STATIC", "", ssCenter|ssNoPrefix, 20, 148, 420, 20, 0, w.fonts.ui)
	w.statusCtl = w.child(0, "STATIC", "", ssCenter|ssNoPrefix, 20, 172, 420, 20, 0, w.fonts.ui)
	// The update button has its own row, empty until there is something to
	// offer; the window is fixed-size, so the row is simply blank until then.
	// Owner-drawn, because a themed button cannot be coloured, and this one
	// has to stand out from the rest of the window.
	w.updateBtn = w.child(0, "BUTTON", "", bsOwnerDraw|wsTabStop, 130, 196, 200, 30, idUpdate, w.fonts.bold)
	procShowWindow.Call(w.updateBtn, swHide)
	w.child(0, "STATIC", "Activity", ssLeft, 20, 234, 200, 18, 0, w.fonts.ui)
	w.activityCtl = w.child(wsExClientEdge, "EDIT", "",
		wsVScroll|wsTabStop|esMultiline|esReadOnly|esAutoVScroll, 20, 254, 420, 160, 0, w.fonts.mono)
	// A multiline edit stops accepting text at about 30,000 characters unless
	// told otherwise; the pane would then go quiet without a word.
	procSendMessage.Call(w.activityCtl, emSetLimitText, 1<<20, 0)
	procEnableWindow.Call(w.copyBtn, 0) // nothing to copy yet

	procShowWindow.Call(hwnd, swShow)
	w.drain() // whatever was queued before the window existed
	return nil
}

// child creates one control. Positions and sizes are in 96-dpi pixels.
func (w *window) child(exStyle uintptr, class, text string, style uintptr, x, y, cx, cy int32, id uintptr, font uintptr) uintptr {
	h, _, _ := procCreateWindowEx.Call(
		exStyle,
		uintptr(unsafe.Pointer(utf16(class))),
		uintptr(unsafe.Pointer(utf16(text))),
		style|wsChild|wsVisible,
		uintptr(w.scale(x)), uintptr(w.scale(y)), uintptr(w.scale(cx)), uintptr(w.scale(cy)),
		w.hwnd, id, w.instance, 0,
	)
	if h != 0 && font != 0 {
		procSendMessage.Call(h, wmSetFont, font, 1)
	}
	return h
}

func (w *window) scale(v int32) int32 { return int32(int64(v) * int64(w.dpi) / 96) }

func (w *window) font(face string, points int32, weight uintptr) uintptr {
	height := -(int64(points)*int64(w.dpi) + 36) / 72
	h, _, _ := procCreateFont.Call(
		uintptr(height), 0, 0, 0, weight, 0, 0, 0,
		defaultCharset, 0, 0, clearTypeQuality, 0,
		uintptr(unsafe.Pointer(utf16(face))),
	)
	return h
}

// icon loads the exe's own icon at the requested system size. A build without
// the resource (go generate not run) gets 0, which means the default icon.
func (w *window) icon(cxMetric, cyMetric uintptr) windows.Handle {
	cx, _, _ := procGetSystemMetrics.Call(cxMetric)
	cy, _, _ := procGetSystemMetrics.Call(cyMetric)
	h, _, _ := procLoadImage.Call(w.instance, appIconID, imageIcon, cx, cy, lrShared)
	return windows.Handle(h)
}

func loadCursor(id uintptr) windows.Handle {
	h, _, _ := procLoadCursor.Call(0, id)
	return windows.Handle(h)
}

// systemDPI is the desktop's scale, 96 meaning 100 %. A process that is not
// DPI aware (a build without the manifest) is told 96 and scaled by Windows.
func systemDPI() uint32 {
	if procGetDpiForSystem.Find() != nil {
		return 96
	}
	if d, _, _ := procGetDpiForSystem.Call(); d != 0 {
		return uint32(d)
	}
	return 96
}

// utf16 converts for Win32. A string with a NUL in it cannot be converted; a
// question mark is shown instead of failing a paint.
func utf16(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		p, _ = windows.UTF16PtrFromString("?")
	}
	return p
}

// --- calls from other goroutines --------------------------------------------

func (w *window) ShowCode(code, site string)   { w.post(update{kind: kCode, a: code, b: site}) }
func (w *window) SetStatus(text string)        { w.post(update{kind: kStatus, a: text}) }
func (w *window) Append(record string)         { w.post(update{kind: kLine, a: record}) }
func (w *window) ShowUpdate(version, u string) { w.post(update{kind: kUpdate, a: version, b: u}) }

// post queues an update and wakes the UI thread once per burst. Only small
// integers cross to Win32; the strings stay in Go memory until the UI thread
// takes them.
func (w *window) post(u update) {
	w.mu.Lock()
	w.pending = append(w.pending, u)
	hwnd := w.hwnd
	w.mu.Unlock()
	if hwnd == 0 {
		return // Open drains the queue as soon as the window exists
	}
	if !w.wake.Swap(true) {
		procPostMessage.Call(hwnd, wmRefresh, 0, 0)
	}
}

func (w *window) Done(exitCode int) {
	w.finishing.Do(func() { close(w.finished) })
	w.mu.Lock()
	hwnd := w.hwnd
	w.mu.Unlock()
	procPostMessage.Call(hwnd, wmDone, uintptr(exitCode), 0)
}

// Loop pumps messages until the window is destroyed.
func (w *window) Loop() int {
	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0: WM_QUIT; -1: error
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	for _, obj := range []uintptr{w.fonts.ui, w.fonts.big, w.fonts.mono, w.fonts.bold,
		w.paint.brush, w.paint.brushPressed, w.paint.pen, w.paint.penPressed} {
		procDeleteObject.Call(obj)
	}
	current = nil
	return w.exitCode
}

// --- the UI thread -----------------------------------------------------------

func wndProc(hwnd uintptr, m uint32, wParam, lParam uintptr) uintptr {
	w := current
	if w == nil {
		r, _, _ := procDefWindowProc.Call(hwnd, uintptr(m), wParam, lParam)
		return r
	}
	switch m {
	case wmRefresh:
		w.drain()
		return 0
	case wmDone:
		w.finish(int(wParam))
		return 0
	case wmCommand:
		if (wParam>>16)&0xFFFF == bnClicked {
			switch wParam & 0xFFFF {
			case idCopy:
				w.copyCode()
			case idUpdate:
				w.openUpdate()
			}
		}
		return 0
	case wmTimer:
		if wParam == idCopiedTimer {
			procKillTimer.Call(hwnd, idCopiedTimer)
			w.setText(w.copyBtn, "Copy code")
		}
		return 0
	case wmCtlColorStatic:
		return w.ctlColor(wParam, lParam, hwnd, m)
	case wmDrawItem:
		if w.drawUpdateButton(lParam) {
			return 1
		}
	case wmClose:
		// Never DefWindowProc here: that destroys the window at once, before
		// the agent has removed its record and identity.
		w.requestClose()
		return 0
	case wmQueryEndSession:
		w.requestClose()
		return 1
	case wmEndSession:
		if wParam != 0 {
			w.requestClose()
			if !w.done {
				select {
				case <-w.finished:
				case <-time.After(endSessionGrace):
				}
			}
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, uintptr(m), wParam, lParam)
	return r
}

// drain applies everything other goroutines queued.
func (w *window) drain() {
	w.wake.Store(false)
	w.mu.Lock()
	batch := w.pending
	w.pending = nil
	w.mu.Unlock()

	var lines []string
	for _, u := range batch {
		switch u.kind {
		case kLine:
			lines = append(lines, splitRecord(u.a)...)
		case kCode:
			w.code = u.a
			w.setText(w.codeCtl, u.a)
			w.setText(w.siteCtl, "Web page: "+u.b)
			if !w.closing && !w.failed {
				procEnableWindow.Call(w.copyBtn, 1)
			}
		case kStatus:
			w.setText(w.statusCtl, u.a)
		case kUpdate:
			w.updateURL = u.b
			w.updateText = "Update to " + u.a
			w.setText(w.updateBtn, w.updateText) // for screen readers and tests; the paint uses updateText
			procShowWindow.Call(w.updateBtn, swShowNormal)
			procInvalidateRect.Call(w.updateBtn, 0, 1)
		}
	}
	if len(lines) > 0 {
		w.appendLines(lines)
	}
}

// appendLines adds to the activity pane: one insertion per batch, or one
// rewrite when the ring dropped old lines.
func (w *window) appendLines(lines []string) {
	if w.ring.push(lines...) {
		w.setText(w.activityCtl, w.ring.text()+"\r\n")
	} else {
		w.selectEnd()
		text := utf16(strings.Join(lines, "\r\n") + "\r\n")
		procSendMessage.Call(w.activityCtl, emReplaceSel, 0, uintptr(unsafe.Pointer(text)))
	}
	w.selectEnd()
	procSendMessage.Call(w.activityCtl, emScrollCaret, 0, 0)
}

func (w *window) selectEnd() {
	end, _, _ := procSendMessage.Call(w.activityCtl, wmGetTextLength, 0, 0)
	procSendMessage.Call(w.activityCtl, emSetSel, end, end)
}

func (w *window) setText(ctl uintptr, s string) {
	procSetWindowText.Call(ctl, uintptr(unsafe.Pointer(utf16(s))))
}

// requestClose is the operator's X (or Windows ending the session). The first
// one asks the agent to stop and greys the button and the X; the window itself
// only goes once Done has been called.
func (w *window) requestClose() {
	if w.done {
		procDestroyWindow.Call(w.hwnd)
		return
	}
	if w.closing {
		return
	}
	w.closing = true
	w.setText(w.statusCtl, "Stopping…")
	procEnableWindow.Call(w.copyBtn, 0)
	menu, _, _ := procGetSystemMenu.Call(w.hwnd, 0)
	procEnableMenuItem.Call(menu, scClose, mfGrayed)
	if w.opts.OnClose != nil {
		w.opts.OnClose()
	}
}

// finish is Done, on the UI thread.
func (w *window) finish(exitCode int) {
	w.done = true
	w.exitCode = exitCode
	if w.closing || exitCode == 0 {
		procDestroyWindow.Call(w.hwnd)
		return
	}
	w.failed = true
	w.setText(w.codeCtl, "Could not start")
	w.setText(w.statusCtl, "See the activity log below, then close this window.")
	procEnableWindow.Call(w.copyBtn, 0)
	procInvalidateRect.Call(w.codeCtl, 0, 1) // repaint in red
}

// ctlColor paints the code red once the agent has failed, the caption grey,
// and the activity pane white on the grey dialog background.
func (w *window) ctlColor(hdc, ctl uintptr, hwnd uintptr, m uint32) uintptr {
	switch {
	case ctl != 0 && ctl == w.activityCtl:
		procSetBkColor.Call(hdc, 0xFFFFFF)
		brush, _, _ := procGetStockObject.Call(whiteBrush)
		return brush
	case ctl != 0 && ctl == w.codeCtl && w.failed:
		procSetTextColor.Call(hdc, red)
		procSetBkMode.Call(hdc, bkTransparent)
		brush, _, _ := procGetSysColorBrush.Call(colorBtnFace)
		return brush
	case ctl != 0 && ctl == w.labelCtl:
		grey, _, _ := procGetSysColor.Call(colorGrayText)
		procSetTextColor.Call(hdc, grey)
		procSetBkMode.Call(hdc, bkTransparent)
		brush, _, _ := procGetSysColorBrush.Call(colorBtnFace)
		return brush
	}
	r, _, _ := procDefWindowProc.Call(hwnd, uintptr(m), hdc, ctl)
	return r
}

// drawUpdateButton paints the update button: a rounded violet block with
// white bold text, darker while pressed. It reports whether the item was ours.
func (w *window) drawUpdateButton(lParam uintptr) bool {
	// lParam is Windows' pointer to a struct that lives for the duration of
	// this message, on the caller's side: not Go memory, so the collector
	// neither moves nor frees it. The reinterpretation goes through the
	// address of the uintptr, as the clipboard package does, so it is explicit
	// rather than a cast vet cannot check.
	item := *(**drawItem)(unsafe.Pointer(&lParam))
	if item == nil || w.updateBtn == 0 || item.hwndItem != w.updateBtn {
		return false
	}
	hdc, rc := item.hdc, item.rcItem

	// The corners outside the rounded shape show the window's background.
	background, _, _ := procGetSysColorBrush.Call(colorBtnFace)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), background)

	brush, pen := w.paint.brush, w.paint.pen
	if item.itemState&odsSelected != 0 {
		brush, pen = w.paint.brushPressed, w.paint.penPressed
	}
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	radius := uintptr(w.scale(10))
	procRoundRect.Call(hdc, uintptr(rc.left), uintptr(rc.top), uintptr(rc.right), uintptr(rc.bottom), radius, radius)
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)

	oldFont, _, _ := procSelectObject.Call(hdc, w.fonts.bold)
	procSetBkMode.Call(hdc, bkTransparent)
	procSetTextColor.Call(hdc, white)
	procDrawText.Call(hdc, uintptr(unsafe.Pointer(utf16(w.updateText))), ^uintptr(0), // -1: the text is NUL-terminated
		uintptr(unsafe.Pointer(&rc)), dtCenter|dtVCenter|dtSingleLine)
	procSelectObject.Call(hdc, oldFont)
	return true
}

// openUpdate opens the release page in the operator's browser. The address
// was built by the agent from its configured repository, never taken from
// the network, so it is the one place it can point.
func (w *window) openUpdate() {
	if w.updateURL == "" {
		return
	}
	r, _, err := procShellExecute.Call(w.hwnd,
		uintptr(unsafe.Pointer(utf16("open"))),
		uintptr(unsafe.Pointer(utf16(w.updateURL))),
		0, 0, swShowNormal)
	if r <= 32 { // ShellExecute reports failure as a value up to 32
		log.Printf("[host-agent] could not open the browser for %s: %v", w.updateURL, err)
	}
}

// copyCode puts the code on the clipboard as shown ("738 - 986"): easy to
// read in a message, and the web page ignores the spaces when it is pasted.
func (w *window) copyCode() {
	if w.code == "" {
		return
	}
	if err := clipboard.WriteTextAs(w.hwnd, w.code); err != nil {
		log.Printf("[host-agent] could not copy the code: %v", err)
		return
	}
	w.setText(w.copyBtn, "Copied")
	procSetTimer.Call(w.hwnd, idCopiedTimer, uintptr(copiedFlash.Milliseconds()), 0)
}
