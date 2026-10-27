//go:build windows

package firewall

import (
	"fmt"
	"strings"
)

// What the operator reads about the firewall, in Windows' words.
const (
	// Blocking is the status line while the firewall keeps viewers out.
	Blocking = "Windows is blocking connections to this computer"
	// Unblocked is the log line once it lets them in.
	Unblocked = "Windows now lets other computers connect to this one."
	// IfAsked is the advice, after a connection failed, about the question
	// the firewall asks about a program.
	IfAsked = "If Windows asked whether to allow FreeDesk through the firewall, choose Allow (Private and Public)."
)

// Advice says, a log line each, what the firewall is doing to keep viewers
// out and what to click.
func (s Status) Advice() []string {
	switch {
	case s.Blocked:
		return []string{
			"Windows is blocking this program's network access, so nobody can connect to this computer.",
			"  Windows Security > Firewall & network protection > Allow an app through firewall:",
			"  remove the entry that blocks FreeDesk, then allow it with both Private and Public ticked.",
		}
	case len(s.Elsewhere) > 0:
		return []string{
			fmt.Sprintf("Windows allows this program only on %s networks, but this computer is on a %s network now, so nobody can connect to it.",
				strings.Join(s.Elsewhere, " and "), s.Network),
			"  Windows Security > Firewall & network protection > Allow an app through firewall:",
			"  find FreeDesk (freedesk.exe) and tick both Private and Public.",
		}
	default:
		return []string{
			"Windows has not yet allowed this program to receive connections, so nobody can connect to this computer.",
			"  If a Windows Security window is asking about FreeDesk, click \"Allow access\".",
			"  Otherwise: Windows Security > Firewall & network protection > Allow an app through firewall >",
			"  Allow another app... > choose freedesk.exe, and tick both Private and Public.",
		}
	}
}
