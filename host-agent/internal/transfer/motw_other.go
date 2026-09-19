//go:build !windows

package transfer

// markDownloaded is a no-op away from Windows: the mark of the web is an NTFS
// alternate data stream and means nothing elsewhere. The agent targets Windows;
// this keeps the package building for tooling and tests.
func markDownloaded(_ string) {}
