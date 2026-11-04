//go:build !windows && !darwin

package firewall

import "context"

// Check has nothing to look at outside Windows and macOS: the program counts
// as reachable and the operator hears nothing.
func Check(_ context.Context, exe string) (Status, error) { return parse("", exe), nil }
