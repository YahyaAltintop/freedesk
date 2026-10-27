//go:build darwin

package firewall

// What the operator reads about the firewall, in macOS's words.
const (
	// Blocking is the status line while the firewall keeps viewers out.
	Blocking = "macOS is blocking connections to this computer"
	// Unblocked is the log line once it lets them in.
	Unblocked = "macOS now lets other computers connect to this one."
	// IfAsked is the advice, after a connection failed, about the question
	// the firewall asks about a program.
	IfAsked = "If macOS asked whether FreeDesk may accept incoming network connections, choose Allow."
)

// Advice says, a log line each, what the firewall is doing to keep viewers
// out and what to click.
func (s Status) Advice() []string {
	switch {
	case s.BlockAll:
		return []string{
			"The macOS firewall is set to block all incoming connections, so nobody can connect to this computer.",
			"  System Settings > Network > Firewall > Options…: turn off \"Block all incoming connections\".",
		}
	case s.Blocked:
		return []string{
			"The macOS firewall is blocking incoming connections to this program, so nobody can connect to this computer.",
			"  System Settings > Network > Firewall > Options…: set FreeDesk to \"Allow incoming connections\".",
		}
	default:
		return []string{
			"The macOS firewall has not allowed this program to receive connections, so nobody can connect to this computer.",
			"  If macOS is asking whether FreeDesk may accept incoming network connections, click Allow.",
		}
	}
}
