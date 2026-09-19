//go:build windows

package transfer

import "os"

// markOfTheWeb is the alternate data stream Windows uses to remember that a
// file came from somewhere else. Zone 3 is "Internet", which is what makes
// SmartScreen warn before an executable runs and what puts the "this file came
// from another computer" notice in a file's properties.
const markOfTheWeb = "[ZoneTransfer]\r\nZoneId=3\r\n"

// markDownloaded tags a completed file the way a browser tags a download.
//
// This is the single most useful thing the agent can do about the fact that a
// file arriving here might be an installer. Blocking by extension would be
// worse than useless — the list cannot be completed, and a blocklist that
// misses one case has only bought false confidence. Letting Windows treat the
// file as what it is, something that arrived from another machine, puts the
// warning exactly where it is useful: at the moment someone runs it.
//
// Best effort. The stream cannot be written on a filesystem without alternate
// data streams (a FAT32 stick, a network share), and a file that is saved
// without the mark is still better than a transfer reported as failed.
func markDownloaded(path string) {
	f, err := os.OpenFile(path+":Zone.Identifier", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(markOfTheWeb)
}
