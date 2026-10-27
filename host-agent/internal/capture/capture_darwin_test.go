//go:build cgo

package capture

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/vpx"
)

// TestMainDisplay: the stream is sized from the main display's pixels.
func TestMainDisplay(t *testing.T) {
	id, w, h, ok := mainDisplay()
	if !ok {
		t.Skip("no display: a machine without a screen")
	}
	if w <= 0 || h <= 0 {
		t.Fatalf("main display %d is %dx%d pixels", id, w, h)
	}
	ew, eh := vpx.OutputSize(w, h, DefaultMaxWidth)
	t.Logf("main display %d: %dx%d pixels, captured and encoded as %dx%d; the screen can be shown now: %v",
		id, w, h, ew, eh, inputDesktopReadable())
}

// TestOpenSourceWithoutPermissionSaysWhy: without the Screen Recording
// permission the source neither opens nor asks for it (that is asked once, at
// start-up); it says what to do instead.
func TestOpenSourceWithoutPermissionSaysWhy(t *testing.T) {
	if CheckAccess(false) {
		t.Skip("this process may record the screen; RC_CAPTURE_INTEGRATION=1 tests the capture itself")
	}
	src, err := openSource(DefaultMaxWidth)
	if src != nil {
		src.close()
	}
	if !errors.Is(err, errScreenRecordingDenied) {
		t.Fatalf("openSource without the permission: %v, want errScreenRecordingDenied", err)
	}
}

// TestSourceDeliversTheEncodedSize: ScreenCaptureKit scales the display on
// the GPU, so its pictures come at the size the encoder takes and the
// converter has nothing left to scale. It needs the Screen Recording
// permission for the program it runs from, so it runs only when asked:
//
//	RC_CAPTURE_INTEGRATION=1 go test -run SourceDelivers -v ./internal/capture/
func TestSourceDeliversTheEncodedSize(t *testing.T) {
	if os.Getenv("RC_CAPTURE_INTEGRATION") == "" {
		t.Skip("RC_CAPTURE_INTEGRATION is not set")
	}
	_, pw, ph, ok := mainDisplay()
	if !ok {
		t.Skip("no display: a machine without a screen")
	}
	src, err := openSource(DefaultMaxWidth)
	if err != nil {
		t.Fatal(err)
	}
	defer src.close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		changed, err := src.grab()
		if err != nil && !errors.Is(err, errDesktopUnavailable) {
			t.Fatal(err)
		}
		if changed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no picture within 3 s: is the screen locked or asleep?")
		}
		time.Sleep(50 * time.Millisecond)
	}
	got := src.current()
	w, h := vpx.OutputSize(pw, ph, DefaultMaxWidth)
	if got.w != w || got.h != h {
		t.Fatalf("ScreenCaptureKit delivered %dx%d, want %dx%d, the encoded size of the %dx%d display", got.w, got.h, w, h, pw, ph)
	}
	if got.stride < got.w*4 || got.bits == nil {
		t.Fatalf("a picture of %d pixels a row has a stride of %d bytes and pixels at %v", got.w, got.stride, got.bits)
	}
	t.Logf("%dx%d display delivered as %dx%d, %d bytes a row", pw, ph, got.w, got.h, got.stride)
}
