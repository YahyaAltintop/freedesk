package session

import (
	"context"
	"sync"
	"testing"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/consent"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/transfer"
)

// scriptedApprover answers from a list and counts how often it was reached.
// What matters is the count: the policy's whole job is to stop calling.
type scriptedApprover struct {
	mu      sync.Mutex
	answers []consent.Answer
	asked   int
}

func (a *scriptedApprover) Ask(context.Context, consent.Prompt) consent.Answer {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked++
	if len(a.answers) == 0 {
		return consent.Refused
	}
	next := a.answers[0]
	a.answers = a.answers[1:]
	return next
}

func (a *scriptedApprover) timesAsked() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.asked
}

func repeat(a consent.Answer, n int) []consent.Answer {
	out := make([]consent.Answer, n)
	for i := range out {
		out[i] = a
	}
	return out
}

func ask(t *testing.T, fa *fileApprover) bool {
	t.Helper()
	return fa.AskFiles(context.Background(), []transfer.FileOffer{{Name: "a.txt", Size: 1}}, `C:\Downloads\FreeDesk`)
}

// An operator who has walked away must not be offered a fresh window every
// timeout for as long as the viewer keeps asking.
func TestUnansweredPromptsEventuallyStopBeingAsked(t *testing.T) {
	ap := &scriptedApprover{answers: repeat(consent.Unanswered, 10)}
	fa := &fileApprover{approver: ap, viewerUID: "v1"}

	for i := range maxUnansweredPrompts {
		if ask(t, fa) {
			t.Fatalf("an unanswered prompt must not approve (attempt %d)", i+1)
		}
	}
	if ap.timesAsked() != maxUnansweredPrompts {
		t.Fatalf("the operator was asked %d times, expected %d", ap.timesAsked(), maxUnansweredPrompts)
	}

	for range 5 {
		if ask(t, fa) {
			t.Fatal("a batch must not be approved once the session has stopped asking")
		}
	}
	if ap.timesAsked() != maxUnansweredPrompts {
		t.Fatalf("the operator was asked %d times after the limit; nothing more should have reached them", ap.timesAsked())
	}
}

// The failure this guards against: counting every false, so an operator who
// answers "no" to a few batches is treated exactly like one who is not there.
// Saying no is being present, and being present must never cost anything.
func TestSayingNoNeverStopsTheQuestions(t *testing.T) {
	ap := &scriptedApprover{answers: repeat(consent.Refused, 20)}
	fa := &fileApprover{approver: ap, viewerUID: "v1"}

	for i := range 20 {
		if ask(t, fa) {
			t.Fatalf("a refusal must not approve (attempt %d)", i+1)
		}
	}
	if ap.timesAsked() != 20 {
		t.Fatalf("the operator was asked %d times, expected all 20 — refusing is answering", ap.timesAsked())
	}
}

// The count is consecutive, not cumulative: any answer proves somebody is at
// the machine, so the tally starts again.
func TestAnsweringResetsTheCount(t *testing.T) {
	answers := []consent.Answer{
		consent.Unanswered, consent.Unanswered,
		consent.Allowed, // back at the keyboard
		consent.Unanswered, consent.Unanswered,
		consent.Refused, // still there
		consent.Unanswered, consent.Unanswered,
	}
	ap := &scriptedApprover{answers: answers}
	fa := &fileApprover{approver: ap, viewerUID: "v1"}

	for i := range answers {
		got := ask(t, fa)
		if want := answers[i] == consent.Allowed; got != want {
			t.Fatalf("attempt %d returned %v, expected %v", i+1, got, want)
		}
	}
	if ap.timesAsked() != len(answers) {
		t.Fatalf("the operator was asked %d times, expected all %d", ap.timesAsked(), len(answers))
	}
}

// Whatever the policy decides, only an explicit yes may let bytes onto the
// disk. This is the assertion that must never be relaxed.
func TestOnlyAllowedApproves(t *testing.T) {
	for _, answer := range []consent.Answer{consent.Refused, consent.Unanswered, consent.Allowed} {
		ap := &scriptedApprover{answers: []consent.Answer{answer}}
		fa := &fileApprover{approver: ap, viewerUID: "v1"}
		if got := ask(t, fa); got != (answer == consent.Allowed) {
			t.Fatalf("%v produced %v", answer, got)
		}
	}
}
