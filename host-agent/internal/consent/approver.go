// Package consent asks the local operator to approve what a viewer wants to
// do: start a session, and send files once one is running. Without an explicit
// approval within the timeout the answer is no — so an unattended machine
// never silently accepts a connection, and never silently accepts a file.
package consent

import (
	"context"
	"time"
)

// Approval modes.
const (
	// ModeDialog shows a native Yes/No window (Windows only; the default there).
	ModeDialog = "dialog"
	// ModeConsole asks "y + Enter" on the console (the only mode elsewhere).
	ModeConsole = "console"
)

// Approver decides whether one request may proceed. Implementations must
// return false when ctx is cancelled (the viewer withdrew the request).
//
// One question at a time, process-wide: two native dialogs stacked on top of
// each other is worse than waiting for the first to be answered.
type Approver interface {
	Ask(ctx context.Context, p Prompt) bool
}

// New returns the approver for the given mode ("dialog" or "console"; empty
// picks the platform default).
func New(mode string, timeout time.Duration) Approver {
	return newPlatformApprover(mode, timeout)
}
