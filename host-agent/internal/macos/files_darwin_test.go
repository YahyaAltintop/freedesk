//go:build darwin && cgo

package macos

import (
	"os"
	"path/filepath"
	"testing"
)

// The mark is what makes macOS treat a received file as a download: checked,
// and asked about, before it runs.
func TestQuarantineMarksAFileAsADownload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.pkg")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := quarantineOf(path); ok {
		t.Fatal("a file this test wrote is already marked")
	}
	if err := Quarantine(path); err != nil {
		t.Fatalf("Quarantine: %v", err)
	}
	kind, agent, ok := quarantineOf(path)
	if !ok {
		t.Fatal("the file carries no mark after Quarantine")
	}
	if kind != "LSQuarantineTypeOtherDownload" || agent != "FreeDesk" {
		t.Errorf("marked as %q by %q, want LSQuarantineTypeOtherDownload by FreeDesk", kind, agent)
	}
}

func TestQuarantineReportsAMissingFile(t *testing.T) {
	if err := Quarantine(filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Error("marking a file that does not exist succeeded")
	}
	if err := Quarantine(""); err == nil {
		t.Error("marking no file at all succeeded")
	}
}

func TestVolumeCapacityAnswersForARealVolume(t *testing.T) {
	bytes, ok := VolumeCapacity(t.TempDir())
	if !ok || bytes == 0 {
		t.Fatalf("VolumeCapacity = %d, %v for the temporary folder's volume", bytes, ok)
	}
	if _, ok := VolumeCapacity(filepath.Join(t.TempDir(), "missing")); ok {
		t.Error("a folder that does not exist answered")
	}
}
