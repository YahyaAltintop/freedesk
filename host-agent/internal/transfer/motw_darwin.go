//go:build darwin && cgo

package transfer

import "github.com/YahyaAltintop/freedesk/host-agent/internal/macos"

// markDownloaded tags a completed file the way a browser tags a download: on
// macOS the quarantine mark, what the Mark of the Web is on Windows.
//
// For the same reason as there (motw_windows.go): a file arriving here might
// be an installer, and no list of extensions can tell which. With the mark,
// Gatekeeper treats it as what it is, something from another computer, and
// checks it, and asks the operator, before anything of it runs.
//
// Best effort, as on Windows: a file that could not be marked (on a volume
// that keeps no extended attributes, say) is still better than a transfer
// reported as failed.
func markDownloaded(path string) {
	_ = macos.Quarantine(path)
}
