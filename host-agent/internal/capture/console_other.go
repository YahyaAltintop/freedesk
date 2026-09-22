//go:build !windows

package capture

import "os/exec"

// hideConsole is a no-op outside Windows: no other platform gives a child
// process a console window of its own.
func hideConsole(*exec.Cmd) {}
