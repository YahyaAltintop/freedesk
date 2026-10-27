//go:build darwin && cgo

package macos

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func init() {
	// AppKit works on the process's first thread only. Locked here, it is
	// TestMain's, which runs the application there while the tests run beside
	// it, as the agent's goroutines do.
	runtime.LockOSThread()
}

// The window and dialog tests are opt-in: they run AppKit, and the dialogs
// show on screen for a moment each, without taking the keyboard. The window
// stays hidden unless RC_UI_VISIBLE=1; RC_UI_SNAPSHOT names a folder for PNG
// pictures of what was drawn.
func uiTests() bool { return os.Getenv("RC_UI_INTEGRATION") != "" }

func needUI(t *testing.T) {
	t.Helper()
	if !uiTests() {
		t.Skip("runs AppKit and shows dialogs; run with RC_UI_INTEGRATION=1 (RC_UI_VISIBLE=1 to see the window too)")
	}
}

func visibleWindow() bool { return os.Getenv("RC_UI_VISIBLE") != "" }

const (
	testTitle      = "FreeDesk test"
	testLicenses   = "FreeDesk test licenses\nsecond line\n"
	testPrivacyURL = "https://example.invalid/PRIVACY.md"
)

var (
	// closeRequests counts the status window's onClose calls.
	closeRequests atomic.Int32
	// loopEnded hears each end of the application's loop a test caused, with
	// the code RunApp returned.
	loopEnded = make(chan int, 4)
)

func TestMain(m *testing.M) {
	if !uiTests() {
		os.Exit(m.Run())
	}
	if err := openStatusWindow(testTitle, testLicenses, testPrivacyURL, func() { closeRequests.Add(1) }, visibleWindow(), false); err != nil {
		fmt.Fprintf(os.Stderr, "could not open the status window: %v\n", err)
		os.Exit(1)
	}
	result := make(chan int, 1)
	go func() {
		result <- m.Run()
		FinishStatusWindow(0) // ends the loop, unless a test already has
	}()
	for {
		code := RunApp()
		select {
		case r := <-result:
			os.Exit(r)
		default:
		}
		// A test ended the window. The loop goes on for the other tests (and
		// for that one's next window), and the test hears how it ended.
		select {
		case loopEnded <- code:
		default:
		}
	}
}

// waitLoopEnded waits for the application's loop to end and returns the code
// RunApp returned.
func waitLoopEnded(t *testing.T) int {
	t.Helper()
	select {
	case code := <-loopEnded:
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("the application's loop did not end")
		return -1
	}
}

// snapshotPath is where a test's picture goes: the RC_UI_SNAPSHOT folder, or
// the test's own temporary one.
func snapshotPath(t *testing.T, name string) string {
	t.Helper()
	dir := os.Getenv("RC_UI_SNAPSHOT")
	if dir == "" {
		dir = t.TempDir()
	}
	return dir + "/" + name
}
