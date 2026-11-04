package capture

import (
	"errors"
	"fmt"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/diag"
)

// The Windows hooks of the pipeline (pipeline.go): desktop duplication with
// the GDI copy as its fallback, the pointer drawn in with GDI
// (cursor_windows.go), and the capture thread in physical pixels.

// skipDuplication makes openSource go straight to the GDI copy, so tests can
// exercise the fallback on a machine where duplication works.
var skipDuplication bool

// openSource prefers desktop duplication and falls back to the GDI copy. Both
// deliver the monitor at its full size, which the converter scales down.
func openSource(int) (source, error) {
	err := errors.New("skipped")
	if !skipDuplication {
		var d *dxgiSource
		if d, err = openDXGI(); err == nil {
			return d, nil
		}
	}
	diag.Printf("[capture] desktop duplication is not available (%v); using the GDI screen copy", err)
	g, gerr := openGDI()
	if gerr != nil {
		return nil, fmt.Errorf("no screen capture method works: duplication: %v; GDI: %w", err, gerr)
	}
	return g, nil
}

// setupCaptureThread makes the capture thread see the screen in physical
// pixels (setThreadPhysicalPixels).
func setupCaptureThread() (restore func()) { return setThreadPhysicalPixels() }

// flushFrame makes the pointer GDI drew into the frame part of its pixels
// before the converter reads them.
func flushFrame() { gdiFlush() }

// unavailableWhy says, in the log, what keeps the desktop from being read.
const unavailableWhy = "locked, asleep, or a UAC prompt"
