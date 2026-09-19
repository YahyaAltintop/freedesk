//go:build windows

package consent

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestDialogTimesOutAndCanBeDismissed pops real message boxes, so it is opt-in
// via RC_DIALOG_INTEGRATION=1. It checks that an unanswered box rejects on
// timeout and that a withdrawn request closes the box early.
func TestDialogTimesOutAndCanBeDismissed(t *testing.T) {
	if os.Getenv("RC_DIALOG_INTEGRATION") == "" {
		t.Skip("shows a real dialog; run with RC_DIALOG_INTEGRATION=1")
	}

	d := NewDialog(2 * time.Second)

	start := time.Now()
	if got := d.Ask(context.Background(), ConnectRequest("viewer-timeout", false)); got != Unanswered {
		t.Fatalf("an unanswered dialog must report Unanswered, got %v", got)
	}
	if elapsed := time.Since(start); elapsed < 1500*time.Millisecond || elapsed > 5*time.Second {
		t.Fatalf("expected the dialog to close on its 2 s timeout, took %v", elapsed)
	}

	d = NewDialog(20 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start = time.Now()
	if got := d.Ask(ctx, ConnectRequest("viewer-withdrawn", false)); got.OK() {
		t.Fatalf("a withdrawn request must not be approved, got %v", got)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("expected the dialog to be dismissed right after the context ended, took %v", elapsed)
	}
}
