// Package licenses carries, inside freedesk.exe, the license of FreeDesk and
// of every piece of other people's code linked into it (libvpx, the MinGW-w64
// and GCC runtimes, Go and its modules). A release is one file, the exe, so the
// texts those licenses ask to accompany a binary travel inside it; the status
// window's Licenses button and `freedesk.exe --licenses` show them.
//
// The text is generated, not written by hand: cmd/notices gathers it from the
// build's own dependencies,
//
//	go run ./cmd/notices -o internal/licenses/generated/LICENSES.txt
//
// and the release and CI run that before building. A developer build without
// it compiles all the same and simply has no licenses to show; cmd/importcheck
// refuses to publish an exe that does not carry the text generated for it.
package licenses

import "embed"

// files is the generated directory: only a placeholder until cmd/notices has
// written LICENSES.txt next to it. The all: prefix embeds the placeholder too,
// which keeps the pattern valid in a developer build (a pattern that matches
// nothing does not compile).
//
//go:embed all:generated
var files embed.FS

// path is where cmd/notices writes the text, relative to this package.
const path = "generated/LICENSES.txt"

// Text returns the license texts, or false in a build that has none.
func Text() (string, bool) {
	b, err := files.ReadFile(path)
	if err != nil || len(b) == 0 {
		return "", false
	}
	return string(b), true
}
