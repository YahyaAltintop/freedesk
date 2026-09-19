//go:build windows

package clipboard

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

// onThread runs fn on a goroutine that holds one OS thread, the way the worker
// does. The clipboard belongs to the thread that opened it, so a test that did
// not do this would be testing something the agent never does.
func onThread(t *testing.T, fn func(b Board)) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		b, err := New()
		if err != nil {
			done <- err
			return
		}
		defer b.Close()
		fn(b)
		done <- nil
	}()
	if err := <-done; err != nil {
		if errors.Is(err, ErrBusy) {
			t.Skip("another process is holding the clipboard")
		}
		t.Fatalf("could not open the clipboard: %v", err)
	}
}

// The write path is the one that silently fails without a real window, so this
// is the test that matters most.
func TestWriteThenReadRoundTrips(t *testing.T) {
	const want = "FreeDesk clipboard probe — ünïcode ✓"
	onThread(t, func(b Board) {
		if err := b.WriteText(want); err != nil {
			if errors.Is(err, ErrBusy) {
				t.Skip("another process is holding the clipboard")
			}
			t.Fatalf("WriteText: %v", err)
		}
		got, ok, err := b.ReadText()
		if err != nil {
			t.Fatalf("ReadText: %v", err)
		}
		if !ok {
			t.Fatal("the clipboard reported no text right after a write")
		}
		if got != want {
			t.Fatalf("read back %q, expected %q", got, want)
		}
	})
}

func TestWriteHandlesEmptyAndLargeText(t *testing.T) {
	onThread(t, func(b Board) {
		if err := b.WriteText(""); err != nil {
			t.Fatalf("an empty write should succeed: %v", err)
		}
		got, _, err := b.ReadText()
		if err != nil || got != "" {
			t.Fatalf("read back %q (%v), expected empty", got, err)
		}

		big := strings.Repeat("abcdefghij", 20000) // 200 KB, under the cap
		if err := b.WriteText(big); err != nil {
			t.Fatalf("a large write should succeed: %v", err)
		}
		back, _, err := b.ReadText()
		if err != nil {
			t.Fatalf("ReadText: %v", err)
		}
		if back != big {
			t.Fatalf("large round trip lost data: %d bytes back, %d expected", len(back), len(big))
		}
	})
}

// The sequence number is what tells the poller something changed, and what
// lets a write of our own be recognised so it does not echo back.
func TestSequenceAdvancesOnWrite(t *testing.T) {
	onThread(t, func(b Board) {
		before := b.Sequence()
		if err := b.WriteText("sequence probe"); err != nil {
			t.Fatalf("WriteText: %v", err)
		}
		after := b.Sequence()
		if after == before {
			t.Fatalf("the sequence number did not change across a write (%d)", before)
		}
	})
}

// Text we put there ourselves is not marked private, so the poller must not
// treat every ordinary copy as something to skip.
func TestOrdinaryTextIsNotExcluded(t *testing.T) {
	onThread(t, func(b Board) {
		if err := b.WriteText("ordinary"); err != nil {
			t.Fatalf("WriteText: %v", err)
		}
		if b.Excluded() {
			t.Fatal("ordinary text was reported as excluded from sharing")
		}
	})
}
