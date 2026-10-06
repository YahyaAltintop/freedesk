//go:build !windows

package capture

import (
	"context"
	"errors"
)

// ScreenCapture captures the screen; only Windows is supported.
type ScreenCapture struct{}

// NewScreenCapture returns a capture that cannot start off Windows.
func NewScreenCapture(Options) *ScreenCapture { return &ScreenCapture{} }

// Start reports that screen capture is Windows-only.
func (c *ScreenCapture) Start(context.Context) (<-chan Frame, error) {
	return nil, errors.New("screen capture is only supported on Windows")
}
