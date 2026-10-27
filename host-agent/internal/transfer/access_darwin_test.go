//go:build darwin

package transfer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestCheckAccessLetsAnOrdinaryFolderBe(t *testing.T) {
	if !CheckAccess(filepath.Join(t.TempDir(), folderName)) {
		t.Fatal("a folder this test made was reported as closed to the agent")
	}
}

// A file macOS would not let the agent save is reported with where to let it
// in; any other failure is reported as it is.
func TestAccessHintNamesTheSetting(t *testing.T) {
	denied := fmt.Errorf("could not create the folder: %w", &os.PathError{Op: "mkdir", Path: "x", Err: syscall.EPERM})
	if hint := accessHint(denied); !strings.Contains(hint, "Files and Folders > Downloads Folder") {
		t.Errorf("no setting named for %v: %q", denied, hint)
	}
	if hint := accessHint(errors.New("disk on fire")); hint != "" {
		t.Errorf("a hint for an unrelated failure: %q", hint)
	}
}
