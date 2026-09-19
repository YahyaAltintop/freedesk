package transfer

import (
	"os"
	"path/filepath"
)

// spaceHeadroom is what a batch must leave behind on the volume it lands on.
//
// Accepting a transfer that fits to the last byte is its own kind of damage:
// Windows needs room to page, to log, and to let the operator delete the very
// file that filled the disk. The margin costs nothing — anybody this close to
// full has a problem a refused transfer did not create.
const spaceHeadroom = 64 << 20

// nearestExisting walks up from dir to the first path that exists.
//
// The destination folder is deliberately not created until a batch is accepted
// (see NewDest), so at the moment the question is asked it usually is not
// there. Free space is a property of the volume rather than of the directory,
// so any existing ancestor answers the same question.
func nearestExisting(dir string) string {
	for {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// The root itself. Nothing above it to ask about, and the query
			// will simply fail, which is handled as "unknown".
			return dir
		}
		dir = parent
	}
}

// hasRoom decides whether need bytes may be written, given what the operating
// system reported.
//
// An unanswerable query allows the transfer. The check exists to spare the
// operator a question about a batch that cannot land, not to become a second
// way for a transfer to fail — the same posture as the Mark of the Web, where
// not being able to do the extra thing must not break the ordinary one.
func hasRoom(free uint64, known bool, need int64) bool {
	if !known {
		return true
	}
	if need < 0 {
		return false
	}
	return free >= uint64(need)+spaceHeadroom
}
