//go:build windows

package input

import (
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseeventfMove       = 0x0001
	mouseeventfLeftDown   = 0x0002
	mouseeventfLeftUp     = 0x0004
	mouseeventfRightDown  = 0x0008
	mouseeventfRightUp    = 0x0010
	mouseeventfMiddleDown = 0x0020
	mouseeventfMiddleUp   = 0x0040
	mouseeventfWheel      = 0x0800
	mouseeventfHWheel     = 0x1000
	mouseeventfAbsolute   = 0x8000

	keyeventfKeyUp = 0x0002

	absoluteMax = 65535
)

var (
	user32        = windows.NewLazySystemDLL("user32.dll")
	procSendInput = user32.NewProc("SendInput")
)

// Windows INPUT layout (amd64): type (4) + 4 padding + 32-byte union = 40 bytes.
// A [4]uint64 union keeps the struct 8-byte aligned for the ULONG_PTR fields.
type rawInput struct {
	inputType uint32
	_         uint32
	union     [4]uint64
}

type mouseInputData struct {
	dx          int32
	dy          int32
	mouseData   int32
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type keybdInputData struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

func moveMouse(x, y float64) {
	sendMouse(mouseInputData{
		dx:      int32(clamp01(x) * absoluteMax),
		dy:      int32(clamp01(y) * absoluteMax),
		dwFlags: mouseeventfMove | mouseeventfAbsolute,
	})
}

func clickMouse(button int, down bool, x, y float64) {
	var flag uint32
	switch button {
	case 0:
		flag = pick(down, mouseeventfLeftDown, mouseeventfLeftUp)
	case 1:
		flag = pick(down, mouseeventfRightDown, mouseeventfRightUp)
	case 2:
		flag = pick(down, mouseeventfMiddleDown, mouseeventfMiddleUp)
	default:
		return
	}
	sendMouse(mouseInputData{
		dx:      int32(clamp01(x) * absoluteMax),
		dy:      int32(clamp01(y) * absoluteMax),
		dwFlags: mouseeventfMove | mouseeventfAbsolute | flag,
	})
}

func scrollWheel(dx, dy int) {
	if dy != 0 {
		sendMouse(mouseInputData{mouseData: int32(dy), dwFlags: mouseeventfWheel})
	}
	if dx != 0 {
		sendMouse(mouseInputData{mouseData: int32(dx), dwFlags: mouseeventfHWheel})
	}
}

func pressKey(code string, down bool) {
	vk, ok := codeToVK(code)
	if !ok {
		return
	}
	var flags uint32
	if !down {
		flags = keyeventfKeyUp
	}
	sendKey(keybdInputData{wVk: vk, dwFlags: flags})
}

func sendMouse(mi mouseInputData) {
	in := rawInput{inputType: inputMouse}
	*(*mouseInputData)(unsafe.Pointer(&in.union[0])) = mi
	send(&in)
}

func sendKey(ki keybdInputData) {
	in := rawInput{inputType: inputKeyboard}
	*(*keybdInputData)(unsafe.Pointer(&in.union[0])) = ki
	send(&in)
}

func send(in *rawInput) {
	ret, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(in)), unsafe.Sizeof(*in))
	if ret != 1 {
		log.Printf("[input] SendInput failed: %v", err)
	}
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

func pick(cond bool, a, b uint32) uint32 {
	if cond {
		return a
	}
	return b
}
