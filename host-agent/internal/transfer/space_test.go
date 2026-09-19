package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasRoomLeavesHeadroomAndAllowsTheUnknown(t *testing.T) {
	const mb = 1 << 20
	cases := []struct {
		name  string
		free  uint64
		known bool
		need  int64
		want  bool
	}{
		{"exactly enough with the margin", spaceHeadroom + 10*mb, true, 10 * mb, true},
		{"one byte short of the margin", spaceHeadroom + 10*mb - 1, true, 10 * mb, false},
		{"room for the file but not the margin", 10 * mb, true, 10 * mb, false},
		{"plenty", 500 * mb, true, mb, true},
		{"an empty volume cannot take anything", 0, true, 1, false},
		// The query failing must not become a second way for a transfer to die:
		// the check is here to save the operator a pointless question, not to
		// add a new refusal.
		{"unanswerable, so allowed", 0, false, 10 * mb, true},
		{"unanswerable and enormous, still allowed", 0, false, 1 << 40, true},
		// Sizes are validated long before this, but a negative one reaching
		// uint64 conversion would wrap into "plenty of room".
		{"a negative size is not room", 500 * mb, true, -1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasRoom(c.free, c.known, c.need); got != c.want {
				t.Fatalf("hasRoom(%d, %v, %d) = %v, want %v", c.free, c.known, c.need, got, c.want)
			}
		})
	}
}

// The destination folder does not exist until a batch is accepted, so the
// question is almost always asked about a path that is not there yet.
func TestNearestExistingFindsARealAncestor(t *testing.T) {
	dir := t.TempDir()

	if got := nearestExisting(dir); got != dir {
		t.Fatalf("an existing directory should answer for itself: got %q", got)
	}
	if got := nearestExisting(filepath.Join(dir, folderName)); got != dir {
		t.Fatalf("the not-yet-created destination should fall back to %q, got %q", dir, got)
	}
	deep := filepath.Join(dir, "a", "b", "c", "d")
	if got := nearestExisting(deep); got != dir {
		t.Fatalf("a deep missing path should fall back to %q, got %q", dir, got)
	}
}

// The walk must stop. Whatever it returns either exists or is its own parent —
// there is no third outcome, and a path with no existing ancestor at all (an
// unmounted drive letter) would otherwise loop forever.
func TestNearestExistingAlwaysTerminates(t *testing.T) {
	for _, p := range []string{
		"",
		".",
		string(os.PathSeparator),
		filepath.Join("no-such-directory-here", "deeper", "still"),
		filepath.Join(t.TempDir(), "x", "y"),
	} {
		got := nearestExisting(p)
		if _, err := os.Stat(got); err == nil {
			continue
		}
		if filepath.Dir(got) != got {
			t.Fatalf("nearestExisting(%q) = %q, which neither exists nor is a root", p, got)
		}
	}
}

func TestRoomAllowsWhatCannotBeMeasured(t *testing.T) {
	d := &Dest{root: t.TempDir(), free: func(string) (uint64, bool) { return 0, false }}
	if !d.Room(1 << 30) {
		t.Fatal("a volume that cannot be queried must not refuse a transfer")
	}
}

func TestRoomAsksAboutAPathThatExists(t *testing.T) {
	dir := t.TempDir()
	var asked string
	d := &Dest{
		root: filepath.Join(dir, folderName),
		free: func(p string) (uint64, bool) { asked = p; return 1 << 30, true },
	}
	d.Room(1)
	if asked != dir {
		// Asking about the destination itself would fail on Windows, and the
		// failure would be silently read as "unknown, allow" — the check would
		// look like it worked while never once refusing anything.
		t.Fatalf("asked about %q, which does not exist; expected %q", asked, dir)
	}
}
