//go:build darwin && cgo

package transfer

import "github.com/YahyaAltintop/freedesk/host-agent/internal/macos"

// freeSpace reports how many bytes may still be written where dir lives.
//
// The figure that matters on macOS counts the room macOS frees as soon as it
// is needed: caches, local Time Machine snapshots, files also kept in iCloud.
// The Finder's "available" counts it too; the file system's own free count
// does not, and on a nearly full Mac, the only one this check ever refuses,
// it can be tens of gigabytes short. That count is still the answer for a
// volume the other cannot be had for.
func freeSpace(dir string) (uint64, bool) {
	if bytes, ok := macos.VolumeCapacity(dir); ok {
		return bytes, true
	}
	return statfsFree(dir)
}
