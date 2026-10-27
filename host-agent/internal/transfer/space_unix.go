//go:build darwin || linux

package transfer

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

// statfsFree reports the bytes the file system says may still be written
// where dir lives, by anyone but root.
func statfsFree(dir string) (uint64, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}

// diskFull reports whether err is the volume running out of room, or the
// operator's share of it (a disk quota): either way the write cannot go on,
// and it is the one failure the person at the other end can do something
// about.
func diskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}
