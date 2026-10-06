//go:build windows

package ui

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procFindWindow      = user32.NewProc("FindWindowW")
	procFindWindowEx    = user32.NewProc("FindWindowExW")
	procGetWindowText   = user32.NewProc("GetWindowTextW")
	procIsWindow        = user32.NewProc("IsWindow")
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
)

// windowsOf counts the top-level windows of a class.
func windowsOf(class string) int {
	n := 0
	var after uintptr
	for {
		h, _, _ := procFindWindowEx.Call(0, after, uintptr(unsafe.Pointer(utf16(class))), 0)
		if h == 0 {
			return n
		}
		n++
		after = h
	}
}

// waitWindows waits up to two seconds for exactly n top-level windows of a
// class, and returns one of them (0 when n is 0).
func waitWindows(t *testing.T, class string, n int) uintptr {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for windowsOf(class) != n {
		if time.Now().After(deadline) {
			t.Fatalf("%d %s windows, want %d", windowsOf(class), class, n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	h, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(utf16(class))), 0)
	return h
}

func visible(ctl uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(ctl)
	return r != 0
}

// harness runs one window on its own locked thread, the way main does, and
// hands the test the exit code Loop returned.
type harness struct {
	win    *window
	closed chan struct{}
	exit   chan int
}

func openWindow(t *testing.T) *harness { return openWindowWith(t, Options{}) }

func openWindowWith(t *testing.T, o Options) *harness {
	t.Helper()
	h := &harness{closed: make(chan struct{}), exit: make(chan int, 1)}
	o.Title = "FreeDesk test"
	o.OnClose = func() { close(h.closed) }
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		w, err := Open(o)
		if err != nil {
			ready <- err
			return
		}
		h.win = w.(*window)
		ready <- nil
		h.exit <- w.Loop()
	}()
	if err := <-ready; err != nil {
		t.Fatalf("Open: %v", err)
	}
	return h
}

func (h *harness) wait(t *testing.T) int {
	t.Helper()
	select {
	case code := <-h.exit:
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("the window did not close in time")
		return -1
	}
}

func (h *harness) text(ctl uintptr) string {
	var buf [256]uint16
	n, _, _ := procGetWindowText.Call(ctl, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func (h *harness) postClose() {
	procPostMessage.Call(h.win.hwnd, wmClose, 0, 0)
}

// TestWindowIntegration shows a real window, so it is opt-in via
// RC_UI_INTEGRATION=1 like the consent dialog test.
func TestWindowIntegration(t *testing.T) {
	if os.Getenv("RC_UI_INTEGRATION") == "" {
		t.Skip("shows a real window; run with RC_UI_INTEGRATION=1")
	}

	t.Run("normal run closes the window on Done(0)", func(t *testing.T) {
		h := openWindow(t)
		found, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(utf16(className))), 0)
		if found == 0 {
			t.Error("FindWindowW did not find the window class")
		}
		h.win.ShowCode("123 - 456", "https://example.web.app")
		h.win.SetStatus("Waiting…")
		for i := range 1000 { // well past the ring's cap: exercises the trim path
			h.win.Append(fmt.Sprintf("line %d\n", i))
		}
		// Let the UI thread apply the queue before reading the controls back.
		time.Sleep(300 * time.Millisecond)
		if got := h.text(h.win.codeCtl); got != "123 - 456" {
			t.Errorf("code control shows %q", got)
		}
		if got := h.text(h.win.siteCtl); got != "Web page: https://example.web.app" {
			t.Errorf("site control shows %q", got)
		}
		if visible(h.win.updateBtn) {
			t.Error("the update button must stay hidden until a release is offered")
		}
		if h.win.licensesBtn != 0 {
			t.Error("a build without license texts must not offer a Licenses button")
		}
		h.win.ShowUpdate("9.9.9", "https://github.com/owner/repo/releases/latest")
		time.Sleep(200 * time.Millisecond)
		if !visible(h.win.updateBtn) {
			t.Error("ShowUpdate must reveal the update button")
		}
		if got := h.text(h.win.updateBtn); !strings.Contains(got, "9.9.9") {
			t.Errorf("update button says %q", got)
		}
		h.win.Done(0)
		if code := h.wait(t); code != 0 {
			t.Errorf("Loop returned %d", code)
		}
	})

	t.Run("a failed run stays until closed and keeps its exit code", func(t *testing.T) {
		h := openWindow(t)
		h.win.Append("[host-agent] error: it broke\n")
		h.win.Done(1)
		time.Sleep(300 * time.Millisecond)
		select {
		case <-h.exit:
			t.Fatal("the window closed on its own after a failure")
		default:
		}
		if got := h.text(h.win.codeCtl); got != "Could not start" {
			t.Errorf("code control shows %q", got)
		}
		h.postClose()
		if code := h.wait(t); code != 1 {
			t.Errorf("Loop returned %d, want the failure's exit code", code)
		}
	})

	t.Run("closing first asks the agent to stop, then Done closes", func(t *testing.T) {
		h := openWindow(t)
		h.postClose()
		select {
		case <-h.closed:
		case <-time.After(2 * time.Second):
			t.Fatal("OnClose was not called")
		}
		time.Sleep(100 * time.Millisecond)
		if got := h.text(h.win.statusCtl); got != "Stopping…" {
			t.Errorf("status shows %q", got)
		}
		h.postClose() // a second click while stopping changes nothing
		h.win.Done(0)
		if code := h.wait(t); code != 0 {
			t.Errorf("Loop returned %d", code)
		}
	})

	t.Run("the licenses window is its own: closing it leaves the agent running", func(t *testing.T) {
		const text = "FreeDesk test licenses\r\nsecond line\r\n"
		h := openWindowWith(t, Options{Licenses: text})
		if h.win.licensesBtn == 0 || !visible(h.win.licensesBtn) {
			t.Fatal("no Licenses button although there are license texts")
		}
		press := func() { procPostMessage.Call(h.win.hwnd, wmCommand, idLicenses, h.win.licensesBtn) }

		press()
		lw := waitWindows(t, licensesClass, 1)
		edit, _, _ := procFindWindowEx.Call(lw, 0, uintptr(unsafe.Pointer(utf16("Edit"))), 0)
		if got := h.text(edit); got != text {
			t.Errorf("the licenses window shows %q, want %q", got, text)
		}

		// The regression this guards: the licenses window closing like the
		// status window would stop the agent.
		procPostMessage.Call(lw, wmClose, 0, 0)
		waitWindows(t, licensesClass, 0)
		time.Sleep(100 * time.Millisecond)
		select {
		case <-h.closed:
			t.Fatal("closing the licenses window asked the agent to stop")
		default:
		}
		if alive, _, _ := procIsWindow.Call(h.win.hwnd); alive == 0 {
			t.Fatal("closing the licenses window destroyed the status window")
		}

		// Open it again; a second press only brings it forward. Then the
		// operator closes the status window: it takes the licenses window
		// with it and Loop returns cleanly.
		press()
		waitWindows(t, licensesClass, 1)
		press()
		time.Sleep(100 * time.Millisecond)
		if n := windowsOf(licensesClass); n != 1 {
			t.Errorf("%d licenses windows after a second press, want 1", n)
		}
		h.postClose()
		select {
		case <-h.closed:
		case <-time.After(2 * time.Second):
			t.Fatal("OnClose was not called")
		}
		h.win.Done(0)
		if code := h.wait(t); code != 0 {
			t.Errorf("Loop returned %d", code)
		}
		if n := windowsOf(licensesClass); n != 0 {
			t.Errorf("%d licenses windows outlived the status window", n)
		}
	})
}
