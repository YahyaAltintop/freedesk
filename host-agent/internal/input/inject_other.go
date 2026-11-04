//go:build !windows && !(darwin && cgo)

package input

// The agent injects input on Windows and, built with cgo, on macOS; these
// stubs keep the package buildable for tooling and tests everywhere else.
// Nothing is injected.

func moveMouse(_, _ float64)                 {}
func clickMouse(_ int, _ bool, _, _ float64) {}
func scrollWheel(_, _ int)                   {}
func pressKey(_ string, _ bool)              {}
