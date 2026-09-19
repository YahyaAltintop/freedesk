//go:build !windows

package transfer

import (
	"os"
	"path/filepath"
)

// downloadRoot mirrors the Windows implementation for tooling and tests on
// other systems; the agent itself targets Windows.
func downloadRoot() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "Downloads", folderName)
	}
	return filepath.Join(os.TempDir(), folderName)
}
