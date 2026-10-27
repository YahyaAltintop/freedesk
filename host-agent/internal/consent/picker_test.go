package consent

import (
	"testing"
	"time"
)

// A viewer could otherwise reopen the picker as fast as the operator closes
// it, which makes the machine unusable rather than fetching a file.
func TestPickerGateEnforcesTheCooldown(t *testing.T) {
	var g pickerGate
	if !g.claim() {
		t.Fatal("the first picker was refused")
	}
	if g.claim() {
		t.Fatal("a second picker right after the first was allowed")
	}
	g.lastShow = time.Now().Add(-pickerCooldown)
	if !g.claim() {
		t.Fatal("a picker after the cooldown was refused")
	}
}
