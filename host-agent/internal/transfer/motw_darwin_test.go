//go:build darwin && cgo

package transfer

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// A file that arrives here may be an installer. macOS only checks it before
// it runs if it can see the file came from elsewhere, which is what the
// quarantine attribute says.
func TestFinishedFileCarriesTheQuarantineMark(t *testing.T) {
	d := destIn(t)

	f, final, err := d.Create("setup.pkg")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Write([]byte("xar!")); err != nil {
		t.Fatal(err)
	}
	if err := Finish(f, final); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	buf := make([]byte, 256)
	n, err := unix.Getxattr(final, "com.apple.quarantine", buf)
	if err != nil {
		t.Fatalf("the completed file should carry com.apple.quarantine: %v", err)
	}
	if n == 0 {
		t.Fatal("the quarantine attribute is empty")
	}
}

// The mark is best effort: a file that could not be tagged is still a file the
// operator accepted, so nothing about it may fail.
func TestMarkDownloadedIsQuietWhenItCannotWrite(t *testing.T) {
	markDownloaded(filepath.Join(t.TempDir(), "does-not-exist", "nope.txt"))
}
