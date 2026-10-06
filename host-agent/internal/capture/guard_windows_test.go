package capture

import (
	"strings"
	"testing"
	"time"
)

// TestGuardedTurnsAPanicIntoAnError: a goroutine of the capture that panics
// ends the capture, not the program, and leaves Start an answer instead of a
// wait.
func TestGuardedTurnsAPanicIntoAnError(t *testing.T) {
	started := make(chan error, 1)
	guarded("test", started, func() { panic("boom") })
	select {
	case err := <-started:
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("started got %v, want an error naming the panic", err)
		}
	default:
		t.Fatal("the panic was not reported to started")
	}

	// Start already has its answer (the panic came later): reporting again
	// must not block.
	answered := make(chan error, 1)
	answered <- nil
	done := make(chan struct{})
	go func() {
		defer close(done)
		guarded("test", answered, func() { panic("later") })
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("guarded blocked on a started channel that already held a value")
	}

	// The encoder goroutine has no started channel.
	guarded("test", nil, func() { panic("no channel") })

	// A goroutine that returns reports nothing.
	quiet := make(chan error, 1)
	guarded("test", quiet, func() {})
	if len(quiet) != 0 {
		t.Fatal("a normal return must report nothing")
	}
}
