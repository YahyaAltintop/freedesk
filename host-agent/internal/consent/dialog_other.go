//go:build !windows

package consent

import "time"

// Non-Windows builds only have the console prompt.
func newPlatformApprover(_ string, timeout time.Duration) Approver {
	return NewConsole(timeout)
}
