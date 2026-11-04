//go:build cgo

package vpx

import (
	"syscall"
	"testing"
	"time"
)

// processCPU is the CPU time this process has used so far, system and user.
func processCPU(t testing.TB) time.Duration {
	t.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}
	return time.Duration(ru.Stime.Nano() + ru.Utime.Nano())
}
