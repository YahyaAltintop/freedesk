//go:build windows

package transfer

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// downloadRoot returns the folder accepted files are written to.
//
// Downloads is asked for by identity rather than built by appending
// "Downloads" to the profile path: the folder is relocatable, it is localised,
// and it is routinely redirected into OneDrive. A guessed path would create a
// second, wrong "Downloads" next to the real one.
func downloadRoot() string {
	if p, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, windows.KF_FLAG_DEFAULT); err == nil && p != "" {
		return filepath.Join(p, folderName)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "Downloads", folderName)
	}
	return filepath.Join(os.TempDir(), folderName)
}
