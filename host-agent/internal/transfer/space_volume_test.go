//go:build windows

package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

// Without this, freeSpace could fail on every call and the whole check would
// read as "unknown, allow" — passing every other test while never once doing
// its job.
func TestFreeSpaceAnswersForARealVolume(t *testing.T) {
	free, known := freeSpace(t.TempDir())
	if !known {
		t.Fatal("the free space on the temp volume could not be read")
	}
	if free == 0 {
		t.Fatal("the temp volume reported zero bytes free, which cannot be right for a machine running tests")
	}
}

// The destination is normally asked about before it exists, so the query has to
// be aimed at an ancestor. This proves the raw call really does fail on a
// missing directory, which is the reason nearestExisting exists at all.
func TestFreeSpaceCannotAnswerForAMissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created-yet")
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Skip("the directory unexpectedly exists")
	}
	if _, known := freeSpace(missing); known {
		t.Fatal("a missing directory answered; nearestExisting would then be unnecessary")
	}
}
