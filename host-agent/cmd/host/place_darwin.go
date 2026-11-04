package main

import "golang.org/x/sys/unix"

// volumeReadOnly reports whether the volume holding path is mounted read-only:
// a disk image as downloaded, or the translocated copy of an app. A path
// that cannot be looked at counts as writable, so no false warning.
func volumeReadOnly(path string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return false
	}
	return st.Flags&unix.MNT_RDONLY != 0
}
