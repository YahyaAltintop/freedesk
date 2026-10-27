//go:build !windows

package transfer

import (
	"os"
	"path/filepath"
)

// downloadRoot returns the folder accepted files are written to. On macOS
// that is the Downloads folder, which is always ~/Downloads: unlike Windows'
// it cannot be moved, so building the path is the right way to find it.
// Elsewhere this mirrors the other two, for tooling and tests.
func downloadRoot() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "Downloads", folderName)
	}
	return filepath.Join(os.TempDir(), folderName)
}
