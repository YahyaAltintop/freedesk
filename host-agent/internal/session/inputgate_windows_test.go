//go:build windows

package session

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/consent"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/input"
)

// TestInjectedClickCannotAnswerThePrompt pops a real "Incoming files" window,
// finds its Yes button and clicks it through the same path a viewer's mouse
// frames take. Before the input gate existed this approved the transfer; it
// must not.
//
// It shows a real dialog, so it is opt-in via RC_DIALOG_INTEGRATION=1 like the
// consent package's own dialog test.
func TestInjectedClickCannotAnswerThePrompt(t *testing.T) {
	if os.Getenv("RC_DIALOG_INTEGRATION") == "" {
		t.Skip("shows a real dialog; run with RC_DIALOG_INTEGRATION=1")
	}

	d := consent.NewDialog(6 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	answered := make(chan consent.Answer, 1)
	go func() {
		answered <- d.Ask(ctx, consent.IncomingFiles("viewer-x",
			[]consent.FileOffer{{Name: "evil.exe", Size: 100}}, `C:\Users\op\Downloads\FreeDesk`))
	}()

	x, y := findYesButton(t)

	// The frames a viewer's click produces, through the gate the session uses.
	h := input.NewHandler()
	frame := func(kind string) []byte {
		return fmt.Appendf(nil, `{"t":%q,"b":0,"x":%.6f,"y":%.6f}`, kind, x, y)
	}
	applyInput(h, frame("m"))
	time.Sleep(100 * time.Millisecond)
	applyInput(h, frame("md"))
	applyInput(h, frame("mu"))

	select {
	case got := <-answered:
		t.Fatalf("the injected click answered the prompt: %v", got)
	case <-time.After(700 * time.Millisecond):
		// Still up: the click did not reach it.
	}
	if !consent.Busy() {
		t.Fatal("the prompt should still be on screen")
	}

	cancel()
	if got := <-answered; got.OK() {
		t.Fatalf("dismissing the prompt answered %v", got)
	}
}

// findYesButton returns the centre of the Yes button as the [0,1] coordinates
// the input protocol uses.
func findYesButton(t *testing.T) (x, y float64) {
	t.Helper()
	u32 := windows.NewLazySystemDLL("user32.dll")
	findWindow := u32.NewProc("FindWindowW")
	findWindowEx := u32.NewProc("FindWindowExW")
	getWindowRect := u32.NewProc("GetWindowRect")
	getSystemMetrics := u32.NewProc("GetSystemMetrics")

	class := windows.StringToUTF16Ptr("#32770")
	var hwnd uintptr
	for range 40 {
		// The title carries a sequence number; scan for whichever box is up.
		for seq := 1; seq <= 5 && hwnd == 0; seq++ {
			title := windows.StringToUTF16Ptr(fmt.Sprintf("FreeDesk - Incoming files (#%d)", seq))
			hwnd, _, _ = findWindow.Call(uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)))
		}
		if hwnd != 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if hwnd == 0 {
		t.Fatal("the prompt window was not found")
	}
	btnClass := windows.StringToUTF16Ptr("Button")
	btnText := windows.StringToUTF16Ptr("&Yes")
	yes, _, _ := findWindowEx.Call(hwnd, 0, uintptr(unsafe.Pointer(btnClass)), uintptr(unsafe.Pointer(btnText)))
	if yes == 0 {
		t.Fatal("the Yes button was not found")
	}
	var r struct{ left, top, right, bottom int32 }
	getWindowRect.Call(yes, uintptr(unsafe.Pointer(&r)))
	cx, _, _ := getSystemMetrics.Call(0)
	cy, _, _ := getSystemMetrics.Call(1)
	return float64(r.left+r.right) / 2 / float64(cx), float64(r.top+r.bottom) / 2 / float64(cy)
}
