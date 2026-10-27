//go:build (darwin && !cgo) || linux

package transfer

// freeSpace reports how many bytes may still be written where dir lives: the
// file system's own count. macOS built with cgo asks for a better one
// (space_darwin.go).
func freeSpace(dir string) (uint64, bool) { return statfsFree(dir) }
