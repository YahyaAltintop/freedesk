//go:build windows

package transfer

import (
	"errors"

	"golang.org/x/sys/windows"
)

// freeSpace reports how many bytes may still be written where dir lives.
//
// The "available to caller" figure is the one that matters: on a volume with a
// disk quota it is smaller than the volume's own free space, and the quota is
// what the write will actually hit.
func freeSpace(dir string) (uint64, bool) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	var free uint64
	// The last two arguments are the volume's total and free bytes, which this
	// does not need; the API documents them as optional.
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, false
	}
	return free, true
}

// diskFull reports whether err is Windows saying the volume ran out of room.
//
// Go does not map either of these onto ENOSPC, so a plain errors.Is against
// the portable name silently answers false. They are named explicitly here so
// a disk that fills mid-transfer is reported as what it is rather than as a
// generic write failure — the pre-flight check can be overtaken by any other
// process writing to the same volume.
func diskFull(err error) bool {
	return errors.Is(err, windows.ERROR_DISK_FULL) || errors.Is(err, windows.ERROR_HANDLE_DISK_FULL)
}
