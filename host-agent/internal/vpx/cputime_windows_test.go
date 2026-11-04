//go:build cgo

package vpx

import (
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// processCPU is the CPU time this process has used so far, kernel and user.
func processCPU(t testing.TB) time.Duration {
	t.Helper()
	var c, e, k, u windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &c, &e, &k, &u); err != nil {
		t.Fatal(err)
	}
	return time.Duration(k.Nanoseconds() + u.Nanoseconds())
}
