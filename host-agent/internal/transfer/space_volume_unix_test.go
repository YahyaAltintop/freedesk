//go:build darwin || linux

package transfer

import (
	"os"
	"syscall"
	"testing"
)

// The disk filling mid-transfer has to be told apart from every other write
// failure: it is the one the person at the other end can do something about.
// Running out of one's disk quota is the same story.
func TestDiskFullRecognisesNoSpaceAndQuota(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.ENOSPC, syscall.EDQUOT} {
		if err := (&os.PathError{Op: "write", Path: "x", Err: errno}); !diskFull(err) {
			t.Errorf("%v is not recognised as a full disk", err)
		}
	}
	if diskFull(os.ErrPermission) {
		t.Error("a permission error was taken for a full disk")
	}
}
