//go:build windows

package input

import (
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type point struct{ X, Y int32 }

// TestSendInputMovesCursor verifies the real Windows injection path: it moves
// the cursor to the screen centre and checks GetCursorPos. It moves the actual
// mouse, so it is opt-in via RC_INPUT_INTEGRATION=1 and restores the cursor.
func TestSendInputMovesCursor(t *testing.T) {
	if os.Getenv("RC_INPUT_INTEGRATION") == "" {
		t.Skip("moves the real cursor; run with RC_INPUT_INTEGRATION=1")
	}

	user32 := windows.NewLazySystemDLL("user32.dll")
	getCursorPos := user32.NewProc("GetCursorPos")
	setCursorPos := user32.NewProc("SetCursorPos")
	getSystemMetrics := user32.NewProc("GetSystemMetrics")

	var orig point
	getCursorPos.Call(uintptr(unsafe.Pointer(&orig)))
	defer setCursorPos.Call(uintptr(orig.X), uintptr(orig.Y))

	width, _, _ := getSystemMetrics.Call(0)  // SM_CXSCREEN
	height, _, _ := getSystemMetrics.Call(1) // SM_CYSCREEN

	moveMouse(0.5, 0.5)
	time.Sleep(80 * time.Millisecond)

	var pos point
	getCursorPos.Call(uintptr(unsafe.Pointer(&pos)))

	centerX, centerY := int32(width)/2, int32(height)/2
	const tolerance = 25
	if abs(pos.X-centerX) > tolerance || abs(pos.Y-centerY) > tolerance {
		t.Fatalf("cursor did not move to center: expected ~(%d,%d), got (%d,%d)", centerX, centerY, pos.X, pos.Y)
	}
}

func abs(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
