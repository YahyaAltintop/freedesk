// Package consent asks the local operator to approve incoming connection
// requests. Without an explicit approval within the timeout, the request is
// rejected — so an unattended machine never silently accepts a connection.
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

// Approver decides whether one incoming request may proceed. Implementations
// must return false when ctx is cancelled (the viewer withdrew the request).
type Approver interface {
	Ask(ctx context.Context, viewerUID string) bool
}

// New returns the approver for the given mode ("dialog" or "console"; empty
// picks the platform default).
func New(mode string, timeout time.Duration) Approver {
	return newPlatformApprover(mode, timeout)
}
