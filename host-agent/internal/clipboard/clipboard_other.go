//go:build !windows

package clipboard

// The agent targets Windows; away from it there is no clipboard to share and
// the capability is never advertised. These keep the package building for
// tooling and tests.
func newPlatformBoard() (Board, error) { return unavailable{}, ErrUnavailable }

func pumpMessages() {}
