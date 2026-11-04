//go:build darwin && cgo

package macos

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// floatingLevel is NSFloatingWindowLevel: above every ordinary window, of
// any application.
const floatingLevel = 3

// waitDialog waits up to two seconds for a dialog to be on screen.
func waitDialog(t *testing.T) dialogState {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if s := readDialogState(); s.shown {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatal("no dialog came up")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// checkPresented checks a dialog is where the operator sees it, without
// FreeDesk having taken the keyboard: in front of everything, on every Space,
// and the application still not the active one (the hidden test application
// is never brought forward).
func checkPresented(t *testing.T, s dialogState) {
	t.Helper()
	if s.level < floatingLevel {
		t.Errorf("the dialog is at window level %d, below the floating level", s.level)
	}
	if !s.allSpaces {
		t.Error("the dialog does not show on every Space")
	}
	if s.appActive && !visibleWindow() {
		t.Error("showing the dialog made FreeDesk the active application")
	}
}

// ask runs Ask in the background.
func ask(ctx context.Context, timeout time.Duration) <-chan Answer {
	answer := make(chan Answer, 1)
	go func() {
		answer <- Ask(ctx, "FreeDesk test - Connection request (#1)", "Allow it?\n\n(a test)", timeout)
	}()
	return answer
}

func TestAsk(t *testing.T) {
	needUI(t)

	t.Run("nobody answering times out", func(t *testing.T) {
		start := time.Now()
		answer := ask(context.Background(), 1500*time.Millisecond)
		s := waitDialog(t)
		checkPresented(t, s)
		if s.title != "FreeDesk test - Connection request (#1)" {
			t.Errorf("the alert says %q", s.title)
		}
		if s.returnButton != "No" || s.otherButton != "Yes" {
			t.Errorf("Return presses %q and the other button is %q; want No and Yes", s.returnButton, s.otherButton)
		}
		path := snapshotPath(t, "alert.png")
		if !snapshotDialog(path) {
			t.Error("could not draw the alert")
		}
		t.Logf("drew %s", path)

		if got := <-answer; got != AnswerTimedOut {
			t.Fatalf("an unanswered alert ended as %v, want timed out", got)
		}
		if elapsed := time.Since(start); elapsed < 1200*time.Millisecond || elapsed > 4*time.Second {
			t.Errorf("the alert timed out after %v, want about 1.5 s", elapsed)
		}
		if readDialogState().shown {
			t.Error("the alert is still up after timing out")
		}
	})

	t.Run("a withdrawn question closes at once", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		answer := ask(ctx, 20*time.Second)
		waitDialog(t)
		start := time.Now()
		cancel()
		if got := <-answer; got != AnswerWithdrawn {
			t.Fatalf("a withdrawn alert ended as %v", got)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("the alert took %v to close after the request was withdrawn", elapsed)
		}
		if readDialogState().shown {
			t.Error("the alert is still up after the request was withdrawn")
		}
	})

	t.Run("a question already withdrawn is never shown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if got := Ask(ctx, "never", "shown", time.Minute); got != AnswerWithdrawn {
			t.Fatalf("got %v", got)
		}
	})

	for _, c := range []struct {
		button string
		want   Answer
	}{{"Yes", AnswerYes}, {"No", AnswerNo}} {
		t.Run("pressing "+c.button, func(t *testing.T) {
			answer := ask(context.Background(), 20*time.Second)
			waitDialog(t)
			pressDialogButton(c.button)
			select {
			case got := <-answer:
				if got != c.want {
					t.Fatalf("pressing %s answered %v", c.button, got)
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("pressing %s did not end the alert", c.button)
			}
		})
	}

	// The alert runs a loop of its own on the main thread; the status window
	// is changed on that thread too and must not wait for the alert to go.
	t.Run("the status window keeps changing while a question is up", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		answer := ask(ctx, 20*time.Second)
		waitDialog(t)
		SetStatus("changed under the alert")
		deadline := time.Now().Add(2 * time.Second)
		for readWindowState().status != "changed under the alert" {
			if time.Now().After(deadline) {
				t.Fatal("the status window did not change while the alert was up")
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
		<-answer
	})
}

func TestChooseFiles(t *testing.T) {
	needUI(t)

	t.Run("withdrawing closes the panel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		type chosen struct {
			paths []string
			ok    bool
		}
		result := make(chan chosen, 1)
		go func() {
			paths, ok := ChooseFiles(ctx, "FreeDesk test - choose files", "A test; it closes by itself.", "Send")
			result <- chosen{paths, ok}
		}()
		s := waitDialog(t)
		checkPresented(t, s)
		if s.title != "FreeDesk test - choose files" {
			t.Errorf("the panel is titled %q", s.title)
		}
		start := time.Now()
		cancel()
		if got := <-result; got.ok || len(got.paths) != 0 {
			t.Fatalf("a withdrawn panel chose %q", got.paths)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("the panel took %v to close after the request was withdrawn", elapsed)
		}
		if readDialogState().shown {
			t.Error("the panel is still up after the request was withdrawn")
		}
	})
}

// Files can be chosen, and folders opened to choose inside them; a package,
// which the Finder shows as one file, cannot be sent and cannot be chosen.
func TestOnlyFilesCanBeChosen(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(dir, "Thing.app")
	if err := os.MkdirAll(filepath.Join(pkg, "Contents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !choosable(file) {
		t.Error("a file cannot be chosen")
	}
	if !choosable(dir) {
		t.Error("a folder cannot be opened")
	}
	if choosable(pkg) {
		t.Error("a package can be chosen")
	}
}
