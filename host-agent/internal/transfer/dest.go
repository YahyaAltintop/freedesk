package transfer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// folderName is the single directory accepted files land in. It is fixed on
	// purpose: a configurable destination is a setting the person on the other
	// end of the connection could talk the operator into changing, and "where do
	// my files go" should be something you learn once.
	folderName = "FreeDesk"

	// partSuffix marks a file that is still arriving. A half-written transfer
	// must never be mistakable for a finished file — that is how someone ends up
	// running an installer that is missing its second half.
	partSuffix = ".part"

	// maxCollisions bounds the " (n)" search. Past this, something is wrong that
	// renaming will not fix.
	maxCollisions = 99
)

// ErrTooManyCollisions means the directory already holds every name we would
// have picked.
var ErrTooManyCollisions = errors.New("too many files with that name")

// Dest is where one session writes the files it accepts.
type Dest struct{ root string }

// NewDest returns the destination for accepted files. The directory is NOT
// created here: an agent nobody has sent a file to should leave nothing behind.
func NewDest() *Dest { return &Dest{root: downloadRoot()} }

// Root reports the folder accepted files are written to, for the approval
// prompt and the startup log. The operator is told this before they say yes.
func (d *Dest) Root() string { return d.root }

// Create makes the destination folder if needed and opens the ".part" file that
// `name` will be written to, returning it with the final path it will be
// renamed to on completion.
//
// The file is opened O_EXCL, which does two jobs: it closes the gap between
// "does this name exist" and "create it", and it refuses to follow a symlink or
// junction that was put there first — so a planted link cannot redirect the
// write somewhere else.
func (d *Dest) Create(name string) (f *os.File, finalPath string, err error) {
	safe, err := SafeName(name)
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(d.root, 0o755); err != nil {
		return nil, "", fmt.Errorf("could not create %s: %w", d.root, err)
	}

	stem, ext := splitExt(safe)
	for i := 0; i <= maxCollisions; i++ {
		candidate := safe
		if i > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, i+1, ext)
		}
		final := filepath.Join(d.root, candidate)
		if !d.within(final) {
			return nil, "", ErrBadName
		}
		// The final name is probed through the .part file: if the .part exists
		// another transfer is using this name right now, and if the final name
		// exists it is someone's file. Neither may be touched.
		if _, err := os.Lstat(final); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, "", err
		}
		part, err := os.OpenFile(final+partSuffix, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return part, final, nil
	}
	return nil, "", ErrTooManyCollisions
}

// within reports whether p is really inside the destination folder. SafeName
// has already rejected anything that could escape; this is the second lock on
// the same door, because the cost of being wrong here is writing outside the
// one directory the operator agreed to.
func (d *Dest) within(p string) bool {
	root := filepath.Clean(d.root)
	cleaned := filepath.Clean(p)
	return strings.HasPrefix(cleaned, root+string(os.PathSeparator))
}

// Finish flushes the file to disk and moves it to its final name. Renaming
// within one directory is atomic, so the completed name never appears until the
// bytes behind it are all there.
func Finish(f *os.File, finalPath string) error {
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(finalPath+partSuffix, finalPath)
}

// Abandon closes and deletes a partial file. It is safe to call more than once
// and on every failure path — a cancelled transfer, a dropped channel, a
// session that ended — because all of them mean the same thing: nothing of this
// file may be left behind.
func Abandon(f *os.File, finalPath string) {
	if f != nil {
		_ = f.Close()
	}
	if finalPath != "" {
		_ = os.Remove(finalPath + partSuffix)
	}
}

// SweepPartials deletes leftover ".part" files. A transfer interrupted by a
// process that never got to clean up (a kill, a power cut) leaves one behind,
// and it would otherwise sit in the operator's Downloads folder forever.
func SweepPartials(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), partSuffix) {
			_ = os.Remove(filepath.Join(root, e.Name()))
		}
	}
}
