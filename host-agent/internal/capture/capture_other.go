//go:build !windows && !(darwin && cgo)

package capture

import (
	"errors"
	"time"
)

// The pipeline's hooks (pipeline.go) where there is no screen source: Start
// reports that capture is not supported, and a session goes on without a
// picture.

func openSource(int) (source, error) {
	return nil, errors.New("screen capture is only supported on Windows and macOS")
}

// cursorPainter has nothing to draw into without a source.
type cursorPainter struct{}

func (*cursorPainter) paint(*picture, int, int, bool, time.Time) bool { return false }

func setupCaptureThread() (restore func()) { return func() {} }

func flushFrame() {}

func inputDesktopReadable() bool { return true }

// unavailableWhy says, in the log, what keeps the desktop from being read.
const unavailableWhy = "locked or asleep"
