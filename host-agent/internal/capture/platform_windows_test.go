package capture

import (
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// useGDIIfAsked makes the capture skip desktop duplication when
// RC_CAPTURE_FORCE_GDI is set, to exercise the GDI fallback.
func useGDIIfAsked(t *testing.T) {
	if os.Getenv("RC_CAPTURE_FORCE_GDI") == "" {
		return
	}
	skipDuplication = true
	t.Cleanup(func() { skipDuplication = false })
	t.Log("desktop duplication skipped: capturing with GDI")
}

// processCPU is the CPU time this process has used so far, kernel and user.
func processCPU(t *testing.T) time.Duration {
	t.Helper()
	var c, e, k, u windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &c, &e, &k, &u); err != nil {
		t.Fatal(err)
	}
	return time.Duration(k.Nanoseconds() + u.Nanoseconds())
}
