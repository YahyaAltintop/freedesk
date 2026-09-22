//go:build !windows

package ui

import (
	"io"
	"os"
)

// Open reports that there is no window here; the agent keeps using the
// console.
func Open(Options) (Window, error) { return nil, ErrUnavailable }

// Echo is where log lines go besides the window: always the console here.
func Echo() io.Writer { return os.Stderr }
