package capture

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func ffmpegName() string {
	if runtime.GOOS == "windows" {
		return "ffmpeg.exe"
	}
	return "ffmpeg"
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestBundledBinaryPrefersTheFolderOverTheSibling(t *testing.T) {
	dir := t.TempDir()
	if got := bundledBinary(dir); got != "" {
		t.Fatalf("an empty folder must yield nothing, got %q", got)
	}

	sibling := filepath.Join(dir, ffmpegName())
	touch(t, sibling)
	if got := bundledBinary(dir); got != sibling {
		t.Fatalf("with only a sibling, got %q, want %q", got, sibling)
	}

	inFolder := filepath.Join(dir, "ffmpeg", ffmpegName())
	touch(t, inFolder)
	if got := bundledBinary(dir); got != inFolder {
		t.Fatalf("the ffmpeg folder must win, got %q, want %q", got, inFolder)
	}
}

func TestBundledBinaryIgnoresADirectoryNamedLikeTheBinary(t *testing.T) {
	dir := t.TempDir()
	// Only the folder, nothing inside it: that is not an ffmpeg.
	if err := os.Mkdir(filepath.Join(dir, "ffmpeg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := bundledBinary(dir); got != "" {
		t.Fatalf("a bare folder must yield nothing, got %q", got)
	}
}

func TestResolveBinaryExplicitPathWins(t *testing.T) {
	explicit := filepath.Join("somewhere", "else", ffmpegName())
	if got := resolveBinary(explicit); got != explicit {
		t.Fatalf("resolveBinary(explicit) = %q", got)
	}
}
