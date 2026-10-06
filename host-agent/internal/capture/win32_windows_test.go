package capture

import (
	"testing"
	"time"
)

// TestDesktopProbeAsksAtMostOncePerInterval: a still screen must not cost a
// kernel call per tick, and a change must be seen once the interval is up.
func TestDesktopProbeAsksAtMostOncePerInterval(t *testing.T) {
	calls, answer := 0, true
	orig := probeInputDesktop
	probeInputDesktop = func() bool {
		calls++
		return answer
	}
	defer func() { probeInputDesktop = orig }()

	var p desktopProbe
	t0 := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	if got := p.check(t0); !got || calls != 1 {
		t.Fatalf("first check: %v after %d calls, want true after 1", got, calls)
	}
	answer = false
	if got := p.check(t0.Add(desktopProbeInterval - time.Nanosecond)); !got || calls != 1 {
		t.Fatalf("within the interval: %v after %d calls, want the cached true after 1", got, calls)
	}
	if got := p.check(t0.Add(desktopProbeInterval)); got || calls != 2 {
		t.Fatalf("at the interval: %v after %d calls, want a fresh false after 2", got, calls)
	}
	if got := p.check(t0.Add(desktopProbeInterval + time.Millisecond)); got || calls != 2 {
		t.Fatalf("just after: %v after %d calls, want the cached false after 2", got, calls)
	}
}
