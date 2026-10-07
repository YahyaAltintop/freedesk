//go:build windows

package firewall

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestCheckRunsTheRealQuery runs the PowerShell query on this machine. What
// it answers depends on the machine — Windows' own firewall on a CI runner,
// a security suite's on many desktops — so only that it ran and was read is
// checked: a typo in the query fails here instead of in the field, where it
// would silently mean no firewall advice at all.
func TestCheckRunsTheRealQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("runs PowerShell for a few seconds")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	st, err := Check(ctx, exe)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	t.Logf("owner=%q enabled=%v blocked=%v allowed=%v elsewhere=%v network=%q",
		st.Owner, st.Enabled, st.Blocked, st.Allowed, st.Elsewhere, st.Network)
}
