package transfer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// destIn returns a Dest writing into a throwaway directory.
func destIn(t *testing.T) *Dest {
	t.Helper()
	return &Dest{root: filepath.Join(t.TempDir(), folderName)}
}

func TestCreateWritesPartThenFinishes(t *testing.T) {
	d := destIn(t)

	f, final, err := d.Create("report.pdf")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if filepath.Base(final) != "report.pdf" {
		t.Fatalf("final name is %q, expected report.pdf", filepath.Base(final))
	}
	if _, err := os.Stat(final + partSuffix); err != nil {
		t.Fatalf("the .part file should exist while the transfer runs: %v", err)
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatal("the final name must not exist until the transfer completes")
	}

	if _, err := f.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := Finish(f, final); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatalf("the finished file should exist: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("contents are %q, expected %q", got, "hello")
	}
	if _, err := os.Stat(final + partSuffix); !os.IsNotExist(err) {
		t.Fatal("no .part file may survive a completed transfer")
	}
}

func TestAbandonLeavesNothingBehind(t *testing.T) {
	d := destIn(t)

	f, final, err := d.Create("report.pdf")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Write([]byte("half a f")); err != nil {
		t.Fatalf("write: %v", err)
	}
	Abandon(f, final)

	for _, p := range []string{final, final + partSuffix} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist after Abandon", filepath.Base(p))
		}
	}
	// Every failure path calls it, and some call it twice.
	Abandon(nil, final)
}

func TestCreateNeverOverwrites(t *testing.T) {
	d := destIn(t)

	// An existing file keeps its name and its contents.
	if err := os.MkdirAll(d.root, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(d.root, "report.pdf")
	if err := os.WriteFile(existing, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	f, final, err := d.Create("report.pdf")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if filepath.Base(final) != "report (2).pdf" {
		t.Fatalf("expected report (2).pdf, got %q", filepath.Base(final))
	}
	if _, err := f.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := Finish(f, final); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("the existing file was modified: %q", got)
	}

	// A third one steps past both.
	f3, final3, err := d.Create("report.pdf")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer Abandon(f3, final3)
	if filepath.Base(final3) != "report (3).pdf" {
		t.Fatalf("expected report (3).pdf, got %q", filepath.Base(final3))
	}
}

// A transfer already using a name must not be joined by a second one.
func TestCreateSkipsNamesAnotherTransferHolds(t *testing.T) {
	d := destIn(t)

	f1, final1, err := d.Create("a.txt")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer Abandon(f1, final1)

	f2, final2, err := d.Create("a.txt")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer Abandon(f2, final2)

	if final1 == final2 {
		t.Fatalf("two concurrent transfers were given the same path %q", final1)
	}
	if filepath.Base(final2) != "a (2).txt" {
		t.Fatalf("expected a (2).txt, got %q", filepath.Base(final2))
	}
}

func TestCreateRejectsUnsafeNames(t *testing.T) {
	d := destIn(t)
	for _, name := range []string{`..\..\evil.exe`, "../evil", "CON", "", "a:b.txt"} {
		f, final, err := d.Create(name)
		if err == nil {
			Abandon(f, final)
			t.Fatalf("Create(%q) was allowed, writing %q", name, final)
		}
		if !errors.Is(err, ErrBadName) {
			t.Fatalf("Create(%q) returned %v, expected ErrBadName", name, err)
		}
	}
	// A rejected name must not have created the folder either.
	if _, err := os.Stat(d.root); !os.IsNotExist(err) {
		t.Fatal("the destination folder should not be created for a rejected name")
	}
}

// Nothing may be written until a file is actually accepted.
func TestNewDestDoesNotCreateTheFolder(t *testing.T) {
	d := destIn(t)
	if _, err := os.Stat(d.root); !os.IsNotExist(err) {
		t.Fatal("the destination folder must not exist before the first transfer")
	}
}

func TestSweepPartialsRemovesOnlyPartials(t *testing.T) {
	d := destIn(t)
	if err := os.MkdirAll(d.root, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(d.root, "keep.txt")
	stale := filepath.Join(d.root, "stale.txt"+partSuffix)
	for _, p := range []string{keep, stale} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	SweepPartials(d.root)

	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("a real file was deleted: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("the leftover .part file should have been swept")
	}
	// A folder that was never created is not an error.
	SweepPartials(filepath.Join(t.TempDir(), "nope"))
}

func TestRootEndsInTheFixedFolderName(t *testing.T) {
	root := NewDest().Root()
	if !strings.HasSuffix(root, string(os.PathSeparator)+folderName) {
		t.Fatalf("Root() = %q, expected it to end in %q", root, folderName)
	}
	if !filepath.IsAbs(root) {
		t.Fatalf("Root() = %q, expected an absolute path", root)
	}
}
