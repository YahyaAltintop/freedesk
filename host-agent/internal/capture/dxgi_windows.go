package capture

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Desktop duplication (DXGI): Windows hands over each new desktop image
// straight from the GPU, and only when something changed. It is the method
// Chrome's screen sharing and OBS use first; GDI (gdi_windows.go) is the
// fallback when it is not available.

var (
	iidIDXGIFactory1   = windows.GUID{Data1: 0x770aae78, Data2: 0xf26f, Data3: 0x4dba, Data4: [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}
	iidIDXGIOutput1    = windows.GUID{Data1: 0x00cddea8, Data2: 0x939b, Data3: 0x4b83, Data4: [8]byte{0xa3, 0x40, 0xa6, 0x85, 0x22, 0x66, 0x66, 0xcc}}
	iidID3D11Texture2D = windows.GUID{Data1: 0x6f15aaf2, Data2: 0xd208, Data3: 0x4e89, Data4: [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}}
)

// vtable slots, from the MinGW-w64 dxgi.h / dxgi1_2.h / d3d11.h declarations.
const (
	factory1EnumAdapters1     = 12
	adapterEnumOutputs        = 7
	outputGetDesc             = 7
	output1DuplicateOutput    = 22
	duplicationGetDesc        = 7
	duplicationAcquireFrame   = 8
	duplicationReleaseFrame   = 14
	deviceCreateTexture2D     = 5
	deviceGetImmediateContext = 40
	contextMap                = 14
	contextUnmap              = 15
	contextCopyResource       = 47
)

const (
	dxgiErrorNotFound               = 0x887A0002
	dxgiErrorAccessLost             = 0x887A0026
	dxgiErrorWaitTimeout            = 0x887A0027
	dxgiErrorSessionDisconnected    = 0x887A0028
	dxgiErrorAccessDenied           = 0x887A002B
	eAccessDenied                   = 0x80070005
	dxgiFormatB8G8R8A8Unorm         = 87
	dxgiModeRotationUnspecified     = 0
	dxgiModeRotationIdentity        = 1
	d3dDriverTypeUnknown            = 0
	d3d11SDKVersion                 = 7
	d3d11UsageStaging               = 3
	d3d11CPUAccessRead              = 0x20000
	d3d11MapRead                    = 1
	dxgiUnavailableRetryInterval    = 500 * time.Millisecond
	dxgiFirstFrameTimeoutMillis     = 100
	errMsgPrimaryOutputNotFound     = "no DXGI output at the primary monitor's position"
	errMsgRotatedOutputNotSupported = "the primary monitor is rotated"
	errMsgFormatNotSupported        = "the desktop is not duplicated as B8G8R8A8"
)

// Mirrors of the C structs; the field order is the layout.
type dxgiOutputDesc struct {
	deviceName [32]uint16
	left       int32
	top        int32
	right      int32
	bottom     int32
	attached   int32
	rotation   uint32
	monitor    uintptr
}

type dxgiOutduplDesc struct {
	width, height    uint32
	refreshNum       uint32
	refreshDen       uint32
	format           uint32
	scanlineOrdering uint32
	scaling          uint32
	rotation         uint32
	inSystemMemory   int32
}

type dxgiOutduplFrameInfo struct {
	lastPresentTime     int64
	lastMouseUpdateTime int64
	accumulatedFrames   uint32
	rectsCoalesced      int32
	protectedMaskedOut  int32
	pointerX, pointerY  int32
	pointerVisible      int32
	totalMetadataSize   uint32
	pointerShapeSize    uint32
}

type d3d11Texture2DDesc struct {
	width, height  uint32
	mipLevels      uint32
	arraySize      uint32
	format         uint32
	sampleCount    uint32
	sampleQuality  uint32
	usage          uint32
	bindFlags      uint32
	cpuAccessFlags uint32
	miscFlags      uint32
}

type d3d11MappedSubresource struct {
	data       unsafe.Pointer
	rowPitch   uint32
	depthPitch uint32
}

// errDesktopUnavailable means the desktop cannot be read right now (the lock
// screen, a UAC prompt, a disconnected session) and is expected back.
var errDesktopUnavailable = errors.New("the desktop is not readable right now")

// dxgiUnavailable lists the duplication failures that pass by themselves.
func dxgiUnavailable(err error) bool {
	return isHRESULT(err, eAccessDenied, dxgiErrorAccessDenied, dxgiErrorAccessLost, dxgiErrorSessionDisconnected)
}

// dxgiSource duplicates the primary monitor. All of its methods must run on
// the one OS thread that opened it (the D3D11 immediate context is not
// thread-safe).
type dxgiSource struct {
	output1 unsafe.Pointer // IDXGIOutput1 of the primary monitor
	device  unsafe.Pointer // ID3D11Device on the adapter that drives it
	context unsafe.Pointer // ID3D11DeviceContext
	dup     unsafe.Pointer // IDXGIOutputDuplication; nil while lost
	staging unsafe.Pointer // ID3D11Texture2D the CPU can read
	frame   *dib
	nextTry time.Time
	probe   desktopProbe // spaces the secure-desktop checks
}

// openDXGI finds the output at the primary monitor's position (0,0), opens a
// Direct3D device on the adapter driving it (on a laptop with two GPUs that
// is whichever one the panel is wired to) and starts duplicating it. A
// desktop that is merely unavailable right now (a UAC prompt at the moment the
// session starts) is not an error: the source starts lost and recovers.
func openDXGI() (*dxgiSource, error) {
	var factory unsafe.Pointer
	r, _, _ := procCreateDXGIFactory1.Call(uintptr(unsafe.Pointer(&iidIDXGIFactory1)), uintptr(unsafe.Pointer(&factory)))
	if failed(r) {
		return nil, hresultErr("CreateDXGIFactory1", r)
	}
	defer release(factory)

	adapter, output, desc, err := findPrimaryOutput(factory)
	if err != nil {
		return nil, err
	}
	defer release(adapter)
	defer release(output)
	if desc.rotation != dxgiModeRotationUnspecified && desc.rotation != dxgiModeRotationIdentity {
		return nil, errors.New(errMsgRotatedOutputNotSupported)
	}

	s := &dxgiSource{}
	if s.output1, err = queryInterface(output, &iidIDXGIOutput1); err != nil {
		return nil, fmt.Errorf("IDXGIOutput1: %w", err)
	}
	var level uint32
	r, _, _ = procD3D11CreateDevice.Call(uintptr(adapter), d3dDriverTypeUnknown, 0, 0, 0, 0, d3d11SDKVersion,
		uintptr(unsafe.Pointer(&s.device)), uintptr(unsafe.Pointer(&level)), uintptr(unsafe.Pointer(&s.context)))
	if failed(r) {
		s.close()
		return nil, hresultErr("D3D11CreateDevice", r)
	}
	if err := s.duplicate(); err != nil && !dxgiUnavailable(err) {
		s.close()
		return nil, err
	}
	return s, nil
}

// findPrimaryOutput walks the adapters and their outputs for the one attached
// to the desktop at (0,0), which is where Windows puts the primary monitor.
func findPrimaryOutput(factory unsafe.Pointer) (adapter, output unsafe.Pointer, desc dxgiOutputDesc, err error) {
	for i := uintptr(0); ; i++ {
		var a unsafe.Pointer
		r, _, _ := syscall.SyscallN(method(factory, factory1EnumAdapters1), uintptr(factory), i, uintptr(unsafe.Pointer(&a)))
		if uint32(r) == dxgiErrorNotFound {
			return nil, nil, desc, errors.New(errMsgPrimaryOutputNotFound)
		}
		if failed(r) {
			return nil, nil, desc, hresultErr("EnumAdapters1", r)
		}
		for j := uintptr(0); ; j++ {
			var o unsafe.Pointer
			r, _, _ := syscall.SyscallN(method(a, adapterEnumOutputs), uintptr(a), j, uintptr(unsafe.Pointer(&o)))
			if failed(r) {
				break // DXGI_ERROR_NOT_FOUND: no more outputs on this adapter
			}
			var d dxgiOutputDesc
			r, _, _ = syscall.SyscallN(method(o, outputGetDesc), uintptr(o), uintptr(unsafe.Pointer(&d)))
			if !failed(r) && d.attached != 0 && d.left == 0 && d.top == 0 {
				return a, o, d, nil
			}
			release(o)
		}
		release(a)
	}
}

// duplicate (re)starts the duplication and makes sure the staging texture and
// the frame match the monitor's current mode. An output that is no longer at
// (0,0) is no longer the primary monitor, which input is mapped to: that is
// an error, and the caller reopens the source to find the new one.
func (s *dxgiSource) duplicate() error {
	var od dxgiOutputDesc
	r, _, _ := syscall.SyscallN(method(s.output1, outputGetDesc), uintptr(s.output1), uintptr(unsafe.Pointer(&od)))
	if failed(r) {
		return hresultErr("GetDesc", r)
	}
	if od.left != 0 || od.top != 0 || od.attached == 0 {
		return errors.New("the primary monitor changed")
	}
	var dup unsafe.Pointer
	r, _, _ = syscall.SyscallN(method(s.output1, output1DuplicateOutput), uintptr(s.output1), uintptr(s.device), uintptr(unsafe.Pointer(&dup)))
	if failed(r) {
		return hresultErr("DuplicateOutput", r)
	}
	var desc dxgiOutduplDesc
	_, _, _ = syscall.SyscallN(method(dup, duplicationGetDesc), uintptr(dup), uintptr(unsafe.Pointer(&desc)))
	if err := checkDuplicationDesc(desc); err != nil {
		release(dup)
		return err
	}
	w, h := int(desc.width), int(desc.height)
	if s.frame == nil || s.frame.w != w || s.frame.h != h {
		release(s.staging)
		s.staging = nil
		s.frame.close()
		s.frame = nil
		td := d3d11Texture2DDesc{
			width: desc.width, height: desc.height, mipLevels: 1, arraySize: 1,
			format: dxgiFormatB8G8R8A8Unorm, sampleCount: 1,
			usage: d3d11UsageStaging, cpuAccessFlags: d3d11CPUAccessRead,
		}
		r, _, _ := syscall.SyscallN(method(s.device, deviceCreateTexture2D), uintptr(s.device), uintptr(unsafe.Pointer(&td)), 0, uintptr(unsafe.Pointer(&s.staging)))
		if failed(r) {
			release(dup)
			return hresultErr("CreateTexture2D", r)
		}
		frame, err := newDIB(w, h)
		if err != nil {
			release(dup)
			return err
		}
		s.frame = frame
	}
	s.dup = dup
	return nil
}

// checkDuplicationDesc refuses a duplication the capture cannot read: a
// rotated desktop (the image would need turning), or one in a format other
// than the B8G8R8A8 the staging texture and the converter take. CopyResource
// reports nothing when the formats differ, so an unchecked mismatch would be
// encoded as garbage; refused, the GDI copy takes over. IDXGIOutput1's
// DuplicateOutput is documented to give B8G8R8A8 even for an HDR desktop, so
// the format check is insurance, as in Chromium's capturer.
func checkDuplicationDesc(desc dxgiOutduplDesc) error {
	if desc.rotation != dxgiModeRotationUnspecified && desc.rotation != dxgiModeRotationIdentity {
		return errors.New(errMsgRotatedOutputNotSupported)
	}
	if desc.format != dxgiFormatB8G8R8A8Unorm {
		return fmt.Errorf("%s (DXGI format %d)", errMsgFormatNotSupported, desc.format)
	}
	return nil
}

// lose drops a duplication Windows has invalidated; the next grab starts a
// new one.
func (s *dxgiSource) lose() {
	release(s.dup)
	s.dup = nil
	s.nextTry = time.Time{}
}

func (s *dxgiSource) name() string { return "DXGI desktop duplication" }

func (s *dxgiSource) current() *dib { return s.frame }

func (s *dxgiSource) origin() (x, y int) { return 0, 0 }

// grab brings the frame up to date. changed reports that the desktop image
// in it is new; a tick with only the pointer moving, or nothing at all, is
// not a change. errDesktopUnavailable means try again later.
func (s *dxgiSource) grab() (changed bool, err error) {
	now := time.Now()
	timeout := uintptr(0)
	fresh := false
	if s.dup == nil {
		if now.Before(s.nextTry) {
			return false, errDesktopUnavailable
		}
		s.nextTry = now.Add(dxgiUnavailableRetryInterval)
		if !s.probe.check(now) {
			return false, errDesktopUnavailable
		}
		if err := s.duplicate(); err != nil {
			if dxgiUnavailable(err) {
				return false, errDesktopUnavailable
			}
			return false, err
		}
		// A new duplication starts with the whole desktop as its first
		// frame; give it a moment to arrive.
		timeout = dxgiFirstFrameTimeoutMillis
		fresh = true
	}

	var info dxgiOutduplFrameInfo
	var resource unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(s.dup, duplicationAcquireFrame), uintptr(s.dup), timeout,
		uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&resource)))
	switch {
	case uint32(r) == dxgiErrorWaitTimeout:
		if fresh {
			// Nothing on screen has moved since the duplication started, so
			// no first frame came: take the still desktop with GDI instead
			// of encoding an empty one.
			return copyScreenGDI(s.frame), nil
		}
		if !s.probe.check(now) {
			// The secure desktop is up but the duplication reports no change
			// instead of losing access: pause all the same rather than keep
			// encoding the last picture.
			s.lose()
			return false, errDesktopUnavailable
		}
		return false, nil
	case failed(r):
		s.lose()
		err := hresultErr("AcquireNextFrame", r)
		if dxgiUnavailable(err) {
			return false, errDesktopUnavailable
		}
		return false, err
	}
	defer release(resource)
	releaseFrame := func() { _, _, _ = syscall.SyscallN(method(s.dup, duplicationReleaseFrame), uintptr(s.dup)) }
	if info.lastPresentTime == 0 {
		releaseFrame() // only the pointer moved; the cursor is drawn separately
		if fresh {
			// A new duplication that has not shown the desktop yet: seed
			// the frame (blank after a mode change, stale otherwise) from
			// GDI, as when nothing at all came.
			return copyScreenGDI(s.frame), nil
		}
		return false, nil
	}

	texture, err := queryInterface(resource, &iidID3D11Texture2D)
	if err != nil {
		releaseFrame()
		s.lose()
		return false, err
	}
	_, _, _ = syscall.SyscallN(method(s.context, contextCopyResource), uintptr(s.context), uintptr(s.staging), uintptr(texture))
	release(texture)
	// Map waits for the copy; only then is the duplicated surface free to go
	// back to Windows.
	var mapped d3d11MappedSubresource
	r, _, _ = syscall.SyscallN(method(s.context, contextMap), uintptr(s.context), uintptr(s.staging), 0, d3d11MapRead, 0,
		uintptr(unsafe.Pointer(&mapped)))
	releaseFrame()
	if failed(r) {
		s.lose()
		return false, hresultErr("Map", r)
	}
	src := unsafe.Slice((*byte)(mapped.data), int(mapped.rowPitch)*s.frame.h)
	dst := s.frame.pixels()
	row := s.frame.stride()
	for y := range s.frame.h {
		copy(dst[y*row:(y+1)*row], src[y*int(mapped.rowPitch):])
	}
	_, _, _ = syscall.SyscallN(method(s.context, contextUnmap), uintptr(s.context), uintptr(s.staging), 0)
	return true, nil
}

func (s *dxgiSource) close() {
	release(s.dup)
	release(s.staging)
	release(s.context)
	release(s.device)
	release(s.output1)
	s.dup, s.staging, s.context, s.device, s.output1 = nil, nil, nil, nil, nil
	s.frame.close()
	s.frame = nil
}
