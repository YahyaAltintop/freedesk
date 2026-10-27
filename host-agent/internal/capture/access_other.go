//go:build !(darwin && cgo)

package capture

// CheckAccess reports whether the agent may record the screen: always, where
// the system asks no permission for it.
func CheckAccess(bool) bool { return true }
