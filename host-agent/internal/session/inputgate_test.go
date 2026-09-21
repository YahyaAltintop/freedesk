package session

import (
	"context"
	"testing"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/consent"
)

type recordingSink struct {
	handled  int
	released int
}

func (r *recordingSink) Handle([]byte) { r.handled++ }
func (r *recordingSink) ReleaseAll()   { r.released++ }

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// The whole point of a consent prompt is that the operator answers it. While
// one is on screen, nothing the viewer sends may reach the machine.
func TestInputIsHeldBackWhileTheOperatorIsAsked(t *testing.T) {
	click := []byte(`{"t":"md","b":0,"x":0.5,"y":0.5}`)
	sink := &recordingSink{}

	applyInput(sink, click)
	if sink.handled != 1 {
		t.Fatalf("with no prompt up the frame must go through; handled=%d", sink.handled)
	}

	// Put a question in front of the operator. The console prompt waits on
	// stdin, where nothing arrives under `go test`, so it stays up until the
	// context ends — the same shape as a dialog nobody has clicked yet.
	c := consent.NewConsole(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	answered := make(chan consent.Answer, 1)
	go func() { answered <- c.Ask(ctx, consent.ConnectRequest("v", false)) }()
	waitUntil(t, "the prompt to be up", consent.Busy)

	for range 3 {
		applyInput(sink, click)
	}
	if sink.handled != 1 {
		t.Fatalf("%d frame(s) reached the handler while a prompt was up", sink.handled-1)
	}
	if sink.released == 0 {
		t.Fatal("held input must be released when a prompt takes over")
	}

	cancel()
	if got := <-answered; got.OK() {
		t.Fatalf("a withdrawn prompt answered %v", got)
	}
	waitUntil(t, "the prompt to be gone", func() bool { return !consent.Busy() })

	applyInput(sink, click)
	if sink.handled != 2 {
		t.Fatalf("after the prompt closes frames must flow again; handled=%d", sink.handled)
	}
}
