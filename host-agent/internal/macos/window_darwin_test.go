//go:build darwin && cgo

package macos

import (
	"fmt"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"
)

// eventually reads the window until cond holds, for up to two seconds: what
// Go hands the window is applied on the main thread a moment later.
func eventually(t *testing.T, what string, cond func(windowState) bool) windowState {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s := readWindowState()
		if cond(s) {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s; the window shows %+v", what, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// reopen opens a new status window in place of one the agent is done with.
func reopen(t *testing.T, licenses, privacyURL string) {
	t.Helper()
	if err := openStatusWindow(testTitle, licenses, privacyURL, func() { closeRequests.Add(1) }, visibleWindow(), true); err != nil {
		t.Fatalf("could not open a new window: %v", err)
	}
	eventually(t, "the new window is not fresh", func(s windowState) bool {
		return !s.done && !s.closing && s.code == "Starting…"
	})
}

// The subtests run in order, as one agent's life: TestMain opened the window
// with license texts and a privacy page.
func TestStatusWindow(t *testing.T) {
	needUI(t)

	t.Run("shows what it is handed", func(t *testing.T) {
		s := readWindowState()
		if s.code != "Starting…" || s.copyEnabled {
			t.Errorf("before a code: code %q, Copy enabled %v; want Starting… and no Copy", s.code, s.copyEnabled)
		}
		if s.updateShown {
			t.Error("the update button shows before a release was offered")
		}
		if s.visible != visibleWindow() {
			t.Errorf("visible %v, want %v", s.visible, visibleWindow())
		}

		SetCode("123 - 456", "https://example.web.app")
		SetStatus("Waiting…")
		var log []string
		for i := range 400 {
			log = append(log, fmt.Sprintf("line %d", i))
		}
		SetLog(strings.Join(log, "\n"))
		s = eventually(t, "the code, status and log did not arrive", func(s windowState) bool {
			return s.code == "123 - 456" && s.status == "Waiting…" && s.logLast == "line 399"
		})
		if s.site != "Web page: https://example.web.app" {
			t.Errorf("site line %q", s.site)
		}
		if !s.copyEnabled {
			t.Error("Copy code stays disabled with a code to copy")
		}

		// AppKit takes UTF-8 without NUL bytes; anything else would show as
		// nothing at all.
		SetStatus("bad \xff byte\x00")
		eventually(t, "a status with a bad byte did not show", func(s windowState) bool {
			return s.status == "bad � byte"
		})

		ShowUpdate("9.9.9", "https://github.com/owner/repo/releases/latest")
		eventually(t, "ShowUpdate did not reveal the button", func(s windowState) bool {
			return s.updateShown && s.updateTitle == "Update to 9.9.9"
		})

		defer setAppearance(systemAppearance)
		for _, look := range []struct {
			name  string
			which int
		}{{"light", lightAppearance}, {"dark", darkAppearance}} {
			setAppearance(look.which)
			path := snapshotPath(t, "status-window-"+look.name+".png")
			if !snapshotWindow(path) {
				t.Fatal("could not draw the window")
			}
			checkPNG(t, path, 460, 428)
		}
	})

	// Pressing Privacy would open the tester's browser, so only the layout is
	// checked: both buttons on the Activity row, Privacy left of Licenses.
	t.Run("Privacy sits beside Licenses", func(t *testing.T) {
		s := readWindowState()
		if !s.hasPrivacy || !s.hasLicenses {
			t.Fatalf("Privacy %v, Licenses %v; want both", s.hasPrivacy, s.hasLicenses)
		}
		if s.privacyX+80 > s.licensesX || s.privacyY != s.licensesY {
			t.Errorf("Privacy at (%v, %v) and Licenses at (%v, %v) overlap or are not on one row",
				s.privacyX, s.privacyY, s.licensesX, s.licensesY)
		}
	})

	t.Run("the licenses window is its own", func(t *testing.T) {
		pressLicenses()
		eventually(t, "the licenses window did not open with the texts", func(s windowState) bool {
			return s.licensesShown && s.licensesLength == len(testLicenses)
		})
		pressLicenses() // a second press only brings it forward
		closeLicenses()
		s := eventually(t, "the licenses window did not close", func(s windowState) bool { return !s.licensesShown })
		time.Sleep(100 * time.Millisecond)
		if n := closeRequests.Load(); n != 0 || s.closing || s.done {
			t.Fatalf("closing the licenses window touched the agent: %d stop requests, closing %v, done %v", n, s.closing, s.done)
		}
	})

	t.Run("a failed run stays until closed and keeps its exit code", func(t *testing.T) {
		FinishStatusWindow(1)
		s := eventually(t, "the failure did not show", func(s windowState) bool { return s.failed })
		if s.code != "Could not start" || s.status != "See the activity log below, then close this window." || s.copyEnabled {
			t.Errorf("after a failure: code %q, status %q, Copy enabled %v", s.code, s.status, s.copyEnabled)
		}
		select {
		case <-loopEnded:
			t.Fatal("the window closed on its own after a failure")
		case <-time.After(300 * time.Millisecond):
		}
		pressClose()
		if code := waitLoopEnded(t); code != 1 {
			t.Errorf("RunApp returned %d, want the failure's exit code", code)
		}
		if n := closeRequests.Load(); n != 0 {
			t.Errorf("closing a window the agent is done with asked it to stop %d times", n)
		}
	})

	t.Run("closing first asks the agent to stop, then finishing closes", func(t *testing.T) {
		reopen(t, testLicenses, testPrivacyURL)
		SetCode("123 - 456", "https://example.web.app")
		eventually(t, "no code", func(s windowState) bool { return s.copyEnabled })

		pressClose()
		s := eventually(t, "the close button did not stop the agent", func(s windowState) bool {
			return s.closing && s.status == "Stopping…"
		})
		if s.copyEnabled || s.closeEnabled {
			t.Errorf("while stopping: Copy enabled %v, close button enabled %v", s.copyEnabled, s.closeEnabled)
		}
		pressClose() // a second press while stopping changes nothing
		time.Sleep(100 * time.Millisecond)
		if n := closeRequests.Load(); n != 1 {
			t.Errorf("onClose ran %d times, want once", n)
		}
		select {
		case <-loopEnded:
			t.Fatal("the window went before the agent finished")
		default:
		}
		FinishStatusWindow(0)
		if code := waitLoopEnded(t); code != 0 {
			t.Errorf("RunApp returned %d", code)
		}
	})

	t.Run("without license texts Privacy takes the Licenses place", func(t *testing.T) {
		reopen(t, "", "")
		if s := readWindowState(); s.hasPrivacy || s.hasLicenses {
			t.Errorf("Privacy %v, Licenses %v with neither a page nor texts", s.hasPrivacy, s.hasLicenses)
		}
		FinishStatusWindow(0)
		waitLoopEnded(t)

		reopen(t, "", testPrivacyURL)
		s := readWindowState()
		if !s.hasPrivacy || s.hasLicenses || s.privacyX != 360 {
			t.Errorf("Privacy %v at x=%v, Licenses %v; want Privacy alone at the row's end, x=360", s.hasPrivacy, s.privacyX, s.hasLicenses)
		}
		FinishStatusWindow(0)
		waitLoopEnded(t)
	})
}

// checkPNG checks that a picture was drawn at w x h points.
func checkPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if cfg.Width%w != 0 || cfg.Height%h != 0 || cfg.Width/w != cfg.Height/h {
		t.Errorf("%s is %d x %d pixels, not %d x %d points at one scale", path, cfg.Width, cfg.Height, w, h)
	}
	t.Logf("drew %s", path)
}
