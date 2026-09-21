package session

import "testing"

// Three connection prompts in a row without a yes is the signal; it is raised
// once, and the count starts over so the next streak is judged on its own.
func TestRepeatedRefusalsAreReportedOncePerStreak(t *testing.T) {
	c := &Coordinator{}
	for i := 1; i < maxUnapprovedConnects; i++ {
		if c.noteConnectOutcome(false) {
			t.Fatalf("reported after %d refusal(s), expected %d", i, maxUnapprovedConnects)
		}
	}
	if !c.noteConnectOutcome(false) {
		t.Fatalf("not reported at %d refusals in a row", maxUnapprovedConnects)
	}
	if c.noteConnectOutcome(false) {
		t.Fatal("reported again on the very next refusal; a streak must start over")
	}
}

// A yes in the middle means the earlier refusals were not a campaign. Only
// refusals that follow one another count.
func TestAnApprovalEndsTheStreak(t *testing.T) {
	c := &Coordinator{}
	c.noteConnectOutcome(false)
	c.noteConnectOutcome(false)
	if c.noteConnectOutcome(true) {
		t.Fatal("a yes can never be the refusal that reaches the limit")
	}
	if c.noteConnectOutcome(false) || c.noteConnectOutcome(false) {
		t.Fatal("the refusals before the yes were still being counted")
	}
	if !c.noteConnectOutcome(false) {
		t.Fatal("three refusals after the yes should have reached the limit")
	}
}
