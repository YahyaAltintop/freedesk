//go:build windows

package transfer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A file that arrives here may be an installer. Windows only warns about that
// if it can see the file came from elsewhere, which is what this stream says.
func TestFinishedFileCarriesTheMarkOfTheWeb(t *testing.T) {
	d := destIn(t)

	f, final, err := d.Create("setup.exe")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Write([]byte("MZ")); err != nil {
		t.Fatal(err)
	}
	if err := Finish(f, final); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	zone, err := os.ReadFile(final + ":Zone.Identifier")
	if err != nil {
		t.Fatalf("the completed file should carry a Zone.Identifier stream: %v", err)
	}
	if !strings.Contains(string(zone), "ZoneId=3") {
		t.Fatalf("expected the internet zone, got %q", zone)
	}
}

// The mark is best effort: a file that could not be tagged is still a file the
// operator accepted, so nothing about it may fail.
func TestMarkDownloadedIsQuietWhenItCannotWrite(t *testing.T) {
	markDownloaded(filepath.Join(t.TempDir(), "does-not-exist", "nope.txt"))
}
