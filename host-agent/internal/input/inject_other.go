//go:build !windows

package input

// The host agent targets Windows; these stubs keep the package buildable for
// tooling and tests on other systems. Nothing is injected.

func moveMouse(_, _ float64)                 {}
func clickMouse(_ int, _ bool, _, _ float64) {}
func scrollWheel(_, _ int)                   {}
func pressKey(_ string, _ bool)              {}
