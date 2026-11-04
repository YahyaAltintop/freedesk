//go:build !windows && !(darwin && cgo)

package clipboard

// Supported: this build has no clipboard to share, and the capability is
// never advertised. These keep the package building for tooling and tests.
const Supported = false

// pasteShortcut is what pastes on this system.
const pasteShortcut = "Ctrl+V"

func newPlatformBoard() (Board, error) { return unavailable{}, ErrUnavailable }

func pumpMessages() {}

func WriteTextAs(uintptr, string) error { return ErrUnavailable }
