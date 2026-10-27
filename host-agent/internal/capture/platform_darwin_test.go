//go:build cgo

package capture

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// useGDIIfAsked: macOS has one source only, so RC_CAPTURE_FORCE_GDI, which
// asks for the Windows fallback, has nothing to switch to.
func useGDIIfAsked(t *testing.T) {
	if os.Getenv("RC_CAPTURE_FORCE_GDI") != "" {
		t.Log("RC_CAPTURE_FORCE_GDI is for Windows; macOS captures with ScreenCaptureKit only")
	}
}

// processCPU is the CPU time this process has used so far, system and user.
func processCPU(t *testing.T) time.Duration {
	t.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}
	return time.Duration(ru.Stime.Nano() + ru.Utime.Nano())
}
