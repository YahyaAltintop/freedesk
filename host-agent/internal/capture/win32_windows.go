package capture

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows APIs the capture uses, all from System32: GDI and user32 for
// the classic screen copy and the mouse pointer, DXGI and Direct3D 11 for
// desktop duplication. Nothing is loaded from anywhere else.
var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")
	dxgi   = windows.NewLazySystemDLL("dxgi.dll")
	d3d11  = windows.NewLazySystemDLL("d3d11.dll")

	procGetDC                        = user32.NewProc("GetDC")
	procReleaseDC                    = user32.NewProc("ReleaseDC")
	procGetSystemMetrics             = user32.NewProc("GetSystemMetrics")
	procGetCursorInfo                = user32.NewProc("GetCursorInfo")
	procGetCursorFrameInfo           = user32.NewProc("GetCursorFrameInfo") // undocumented; see animatedCursor
	procGetIconInfo                  = user32.NewProc("GetIconInfo")
	procDrawIconEx                   = user32.NewProc("DrawIconEx")
	procOpenInputDesktop             = user32.NewProc("OpenInputDesktop")
	procCloseDesktop                 = user32.NewProc("CloseDesktop")
	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	procCreateCompatibleDC           = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC                     = gdi32.NewProc("DeleteDC")
	procCreateDIBSection             = gdi32.NewProc("CreateDIBSection")
	procSelectObject                 = gdi32.NewProc("SelectObject")
	procDeleteObject                 = gdi32.NewProc("DeleteObject")
	procBitBlt                       = gdi32.NewProc("BitBlt")
	procGdiFlush                     = gdi32.NewProc("GdiFlush")
	procGetObject                    = gdi32.NewProc("GetObjectW")
	procCreateDXGIFactory1           = dxgi.NewProc("CreateDXGIFactory1")
	procD3D11CreateDevice            = d3d11.NewProc("D3D11CreateDevice")
)

const (
	smCxScreen = 0
	smCyScreen = 1

	srccopy    = 0x00CC0020
	captureblt = 0x40000000

	diNormal       = 0x0003
	cursorShowing  = 0x00000001
	desktopReadObj = 0x0001

	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, (DPI_AWARENESS_CONTEXT)-4.
	dpiPerMonitorAwareV2 = ^uintptr(3)
)

// setThreadPhysicalPixels makes the calling thread see the screen in physical
// pixels, whatever the scale factor, so the GDI copy, the monitor size and the
// pointer position agree with the desktop duplication (which is always in
// physical pixels). Only this thread changes; the window keeps the process's
// DPI mode. It returns a function that restores the previous mode.
func setThreadPhysicalPixels() (restore func()) {
	if procSetThreadDpiAwarenessContext.Find() != nil {
		return func() {} // before Windows 10 1607: the process mode stands
	}
	old, _, _ := procSetThreadDpiAwarenessContext.Call(dpiPerMonitorAwareV2)
	return func() {
		if old != 0 {
			_, _, _ = procSetThreadDpiAwarenessContext.Call(old)
		}
	}
}

// inputDesktopReadable reports whether this process can read the desktop that
// currently receives input. It cannot while Windows shows the secure desktop
// (a UAC prompt, Ctrl+Alt+Del) or the lock screen; nothing can be captured
// then, and trying only produces errors or black frames.
func inputDesktopReadable() bool {
	h, _, _ := procOpenInputDesktop.Call(0, 0, desktopReadObj)
	if h == 0 {
		return false
	}
	_, _, _ = procCloseDesktop.Call(h)
	return true
}

// probeInputDesktop is inputDesktopReadable, replaceable by a test.
var probeInputDesktop = inputDesktopReadable

// desktopProbeInterval spaces a source's inputDesktopReadable calls. Asked on
// every tick, a still screen would cost a kernel call thirty times a second
// for nothing; asked this often, the secure desktop is still noticed within
// a quarter of a second, before anyone misses a frame.
const desktopProbeInterval = 250 * time.Millisecond

// desktopProbe answers inputDesktopReadable from an answer at most
// desktopProbeInterval old. The zero value asks at once.
type desktopProbe struct {
	next     time.Time
	readable bool
}

// check reports whether the input desktop can be read as of now, asking
// Windows only when the previous answer is older than desktopProbeInterval.
func (p *desktopProbe) check(now time.Time) bool {
	if now.Before(p.next) {
		return p.readable
	}
	p.next = now.Add(desktopProbeInterval)
	p.readable = probeInputDesktop()
	return p.readable
}

func primaryScreenSize() (w, h int) {
	cx, _, _ := procGetSystemMetrics.Call(smCxScreen)
	cy, _, _ := procGetSystemMetrics.Call(smCyScreen)
	return int(int32(cx)), int(int32(cy))
}

func gdiFlush() { _, _, _ = procGdiFlush.Call() }

// bitmapInfo is BITMAPINFO with its BITMAPINFOHEADER spelled out.
type bitmapInfo struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
	colors        [1]uint32
}

// dib is a top-down 32-bit BGRA bitmap the size of the captured monitor,
// selected into a memory DC so GDI can draw into it: the screen copy and the
// mouse pointer. Its pixels are read directly by the encoder.
type dib struct {
	dc     uintptr
	bitmap uintptr
	old    uintptr
	bits   unsafe.Pointer
	w, h   int
}

func newDIB(w, h int) (*dib, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("no screen to capture (%dx%d)", w, h)
	}
	dc, _, err := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC: %w", err)
	}
	bi := bitmapInfo{
		size:     uint32(unsafe.Offsetof(bitmapInfo{}.colors)),
		width:    int32(w),
		height:   -int32(h), // negative: top-down rows
		planes:   1,
		bitCount: 32,
	}
	var bits unsafe.Pointer
	bm, _, err := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bm == 0 || bits == nil {
		_, _, _ = procDeleteDC.Call(dc)
		return nil, fmt.Errorf("CreateDIBSection %dx%d: %w", w, h, err)
	}
	old, _, _ := procSelectObject.Call(dc, bm)
	return &dib{dc: dc, bitmap: bm, old: old, bits: bits, w: w, h: h}, nil
}

func (d *dib) stride() int { return d.w * 4 }

func (d *dib) pixels() []byte { return unsafe.Slice((*byte)(d.bits), d.w*d.h*4) }

func (d *dib) close() {
	if d == nil {
		return
	}
	_, _, _ = procSelectObject.Call(d.dc, d.old)
	_, _, _ = procDeleteObject.Call(d.bitmap)
	_, _, _ = procDeleteDC.Call(d.dc)
}

// hresult is a failed COM call.
type hresult uint32

func (h hresult) Error() string { return fmt.Sprintf("HRESULT 0x%08X", uint32(h)) }

func failed(r uintptr) bool { return int32(uint32(r)) < 0 }

func hresultErr(what string, r uintptr) error {
	return fmt.Errorf("%s: %w", what, hresult(uint32(r)))
}

func isHRESULT(err error, codes ...uint32) bool {
	var h hresult
	if !errors.As(err, &h) {
		return false
	}
	for _, c := range codes {
		if uint32(h) == c {
			return true
		}
	}
	return false
}

// method returns the address of the i-th entry of a COM object's vtable.
func method(obj unsafe.Pointer, i int) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	return *(*uintptr)(unsafe.Add(vtbl, uintptr(i)*unsafe.Sizeof(uintptr(0))))
}

// release calls IUnknown::Release, ignoring nil.
func release(obj unsafe.Pointer) {
	if obj != nil {
		_, _, _ = syscall.SyscallN(method(obj, 2), uintptr(obj))
	}
}

// queryInterface calls IUnknown::QueryInterface.
func queryInterface(obj unsafe.Pointer, iid *windows.GUID) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(obj, 0), uintptr(obj), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if failed(r) {
		return nil, hresultErr("QueryInterface", r)
	}
	return out, nil
}
