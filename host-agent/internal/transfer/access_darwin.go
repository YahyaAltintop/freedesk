//go:build darwin

package transfer

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// CheckAccess reports whether macOS lets the agent into the folder that
// holds root, the Downloads folder. macOS opens that folder to a program only
// once the operator has allowed it, and a first look inside is what makes it
// ask. So the agent looks at start-up, while the operator is at the window,
// rather than when the first file arrives with a viewer watching.
func CheckAccess(root string) bool {
	f, err := os.Open(filepath.Dir(root))
	if err == nil {
		f.Close()
	}
	return !errors.Is(err, fs.ErrPermission)
}

// accessHint is what to add to the report of a file that could not be saved
// because of err: where the operator lets the agent into the folder.
func accessHint(err error) string {
	if errors.Is(err, fs.ErrPermission) {
		return "; allow FreeDesk in System Settings > Privacy & Security > Files and Folders > Downloads Folder"
	}
	return ""
}
