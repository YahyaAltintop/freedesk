//go:build !(darwin && cgo)

package input

// CheckAccess reports whether the agent may control this computer: always,
// where the system asks no permission for it.
func CheckAccess(bool) bool { return true }
