//go:build !windows && !(darwin && cgo)

package consent

import "time"

// Without a native dialog (neither Windows nor macOS built with cgo) there is
// only the console prompt.
func newPlatformApprover(_ string, timeout time.Duration) Approver {
	return NewConsole(timeout)
}
