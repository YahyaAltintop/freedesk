//go:build !windows && !(darwin && cgo)

package transfer

// markDownloaded is a no-op where there is no download mark to give: the Mark
// of the Web is Windows', the quarantine mark macOS's (reached through cgo).
// This keeps the package building for tooling and tests.
func markDownloaded(_ string) {}
