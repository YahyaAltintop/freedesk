package session

import (
	"context"
	"sync"
	"testing"
	"time"

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

// fakeClock lets a test move through the backoff without waiting for it.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newFileApprover(ap Approver) (*fileApprover, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	return &fileApprover{approver: ap, viewerUID: "v1", now: clock.now}, clock
}

func ask(t *testing.T, fa *fileApprover) bool {
	t.Helper()
	return fa.AskFiles(context.Background(), []transfer.FileOffer{{Name: "a.txt", Size: 1}}, `C:\Downloads\FreeDesk`)
}

// An operator who has walked away must not be offered a fresh window every
// timeout for as long as the viewer keeps asking.
func TestUnansweredPromptsEventuallyStopBeingAsked(t *testing.T) {
	ap := &scriptedApprover{answers: repeat(consent.Unanswered, 10)}
	fa, clock := newFileApprover(ap)

	for i := range maxUnansweredPrompts {
		// Well past any backoff, so only the unanswered count is in play.
		clock.advance(refusalBackoffMax)
		if ask(t, fa) {
			t.Fatalf("an unanswered prompt must not approve (attempt %d)", i+1)
		}
	}
	if ap.timesAsked() != maxUnansweredPrompts {
		t.Fatalf("the operator was asked %d times, expected %d", ap.timesAsked(), maxUnansweredPrompts)
	}

	for range 5 {
		clock.advance(refusalBackoffMax)
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
// Saying no is being present, and being present must never end the session's
// questions — it may only space them out.
func TestSayingNoNeverStopsTheQuestions(t *testing.T) {
	ap := &scriptedApprover{answers: repeat(consent.Refused, 20)}
	fa, clock := newFileApprover(ap)

	for i := range 20 {
		clock.advance(refusalBackoffMax)
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
	fa, clock := newFileApprover(ap)

	for i := range answers {
		clock.advance(refusalBackoffMax)
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
		fa, _ := newFileApprover(ap)
		if got := ask(t, fa); got != (answer == consent.Allowed) {
			t.Fatalf("%v produced %v", answer, got)
		}
	}
}

// A viewer who re-offers the moment they hear "no" must not get a fresh
// window: each refusal buys the operator a growing stretch of quiet, and a
// yes forgets it.
func TestRefusalsBackOff(t *testing.T) {
	ap := &scriptedApprover{answers: repeat(consent.Refused, 6)}
	fa, clock := newFileApprover(ap)

	ask(t, fa) // no
	if ask(t, fa); ap.timesAsked() != 1 {
		t.Fatalf("an immediate re-offer reached the operator (asked %d)", ap.timesAsked())
	}
	clock.advance(refusalBackoffMin - time.Second)
	if ask(t, fa); ap.timesAsked() != 1 {
		t.Fatal("re-offered inside the first backoff and still reached the operator")
	}
	clock.advance(time.Second)
	if ask(t, fa); ap.timesAsked() != 2 {
		t.Fatalf("after the backoff the operator should be asked again (asked %d)", ap.timesAsked())
	}

	// The second refusal in a row doubles the gap.
	clock.advance(refusalBackoffMin)
	if ask(t, fa); ap.timesAsked() != 2 {
		t.Fatal("the second refusal should have bought a longer quiet than the first")
	}
	clock.advance(refusalBackoffMin)
	if ask(t, fa); ap.timesAsked() != 3 {
		t.Fatalf("after twice the minimum the operator should be asked again (asked %d)", ap.timesAsked())
	}

	// A yes forgets the backoff: the next offer is asked at once.
	ap.answers = []consent.Answer{consent.Allowed, consent.Refused}
	clock.advance(refusalBackoffMax)
	if !ask(t, fa) {
		t.Fatal("the scripted yes should have approved")
	}
	if ask(t, fa); ap.timesAsked() != 5 {
		t.Fatalf("after a yes an immediate offer must be asked (asked %d)", ap.timesAsked())
	}
}

// The gap grows, but not without end: whatever the history, five minutes of
// quiet is always enough for the next question to be asked.
func TestBackoffIsCapped(t *testing.T) {
	ap := &scriptedApprover{answers: repeat(consent.Refused, 12)}
	fa, clock := newFileApprover(ap)

	for i := range 12 {
		clock.advance(refusalBackoffMax)
		ask(t, fa)
		if ap.timesAsked() != i+1 {
			t.Fatalf("after %d refusals the cap no longer held: asked %d", i, ap.timesAsked())
		}
	}
	if fa.backoff != refusalBackoffMax {
		t.Fatalf("backoff is %v, expected to settle at the cap %v", fa.backoff, refusalBackoffMax)
	}
}
