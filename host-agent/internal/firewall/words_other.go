//go:build !windows && !darwin

package firewall

// What the operator reads about the firewall where it is never looked at
// (Check always finds the program reachable here): plain words, for tooling
// and tests.
const (
	// Blocking is the status line while the firewall keeps viewers out.
	Blocking = "The firewall is blocking connections to this computer"
	// Unblocked is the log line once it lets them in.
	Unblocked = "The firewall now lets other computers connect to this one."
	// IfAsked is the advice, after a connection failed, about the question
	// the firewall asks about a program.
	IfAsked = "If the firewall asked about FreeDesk, allow it."
)

// Advice says, a log line each, what the firewall is doing to keep viewers
// out.
func (s Status) Advice() []string {
	return []string{"This computer's firewall keeps other computers from connecting to it."}
}
