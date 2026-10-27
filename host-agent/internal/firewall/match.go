// Package firewall tells whether the computer's firewall (Windows Defender
// Firewall, the macOS firewall) is set to block this program — the one thing
// on the machine itself that leaves an otherwise healthy agent unreachable,
// and the one the operator can fix — or which security product has taken the
// firewall over, when one has. It also has the words for the operator: what
// the firewall is doing, and where to click.
package firewall

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// sameProgram reports whether a rule's program path names exe. Rules keep the
// path as it was written, sometimes with %ProgramFiles%-style variables, and
// Windows paths compare without regard to case.
func sameProgram(rule, exe string) bool {
	rule = strings.TrimSpace(rule)
	if rule == "" || strings.EqualFold(rule, "Any") {
		return false
	}
	return strings.EqualFold(filepath.Clean(expand(rule)), filepath.Clean(exe))
}

var envVar = regexp.MustCompile(`%([^%]+)%`)

// expand replaces %NAME% with the variable's value, the way Windows does.
func expand(s string) string {
	return envVar.ReplaceAllStringFunc(s, func(m string) string {
		return os.Getenv(m[1 : len(m)-1])
	})
}
