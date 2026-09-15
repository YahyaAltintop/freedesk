package input

import (
	"fmt"
	"reflect"
	"testing"
)

// recorder is a fake injector that logs every call as a compact string.
type recorder struct {
	calls []string
}

func (r *recorder) moveMouse(x, y float64) {
	r.calls = append(r.calls, fmt.Sprintf("m %.2f %.2f", x, y))
}
func (r *recorder) clickMouse(b int, down bool, x, y float64) {
	r.calls = append(r.calls, fmt.Sprintf("btn%d %v %.2f %.2f", b, down, x, y))
}
func (r *recorder) scrollWheel(dx, dy int) { r.calls = append(r.calls, fmt.Sprintf("w %d %d", dx, dy)) }
func (r *recorder) pressKey(code string, down bool) {
	r.calls = append(r.calls, fmt.Sprintf("key %s %v", code, down))
}

func feed(h *Handler, msgs ...string) {
	for _, m := range msgs {
		h.Handle([]byte(m))
	}
}

func TestReleaseAllReleasesHeldKeysAndButtons(t *testing.T) {
	rec := &recorder{}
	h := newHandler(rec)

	feed(h,
		`{"t":"kd","code":"ShiftLeft"}`,
		`{"t":"kd","code":"KeyA"}`,
		`{"t":"kd","code":"KeyA"}`, // browser auto-repeat
		`{"t":"md","b":0,"x":0.25,"y":0.75}`,
	)
	rec.calls = nil

	h.ReleaseAll()

	want := []string{
		"key KeyA false",      // most recent key first
		"key ShiftLeft false", // modifier last
		"btn0 false 0.25 0.75",
	}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("ReleaseAll calls:\n got %v\nwant %v", rec.calls, want)
	}

	rec.calls = nil
	h.ReleaseAll()
	if len(rec.calls) != 0 {
		t.Fatalf("second ReleaseAll should be a no-op, got %v", rec.calls)
	}
}

func TestReleasedInputIsNotReleasedAgain(t *testing.T) {
	rec := &recorder{}
	h := newHandler(rec)

	feed(h,
		`{"t":"kd","code":"KeyB"}`,
		`{"t":"ku","code":"KeyB"}`,
		`{"t":"md","b":1,"x":0.5,"y":0.5}`,
		`{"t":"mu","b":1,"x":0.5,"y":0.5}`,
		`{"t":"md","b":9,"x":0.5,"y":0.5}`, // invalid button: ignored entirely
	)
	rec.calls = nil

	h.ReleaseAll()
	if len(rec.calls) != 0 {
		t.Fatalf("nothing is held, expected no calls, got %v", rec.calls)
	}
}

func TestHandleForwardsEvents(t *testing.T) {
	rec := &recorder{}
	h := newHandler(rec)

	feed(h,
		`{"t":"m","x":0.1,"y":0.2}`,
		`{"t":"w","dx":0,"dy":-120}`,
		`not json`,
	)
	want := []string{"m 0.10 0.20", "w 0 -120"}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("Handle calls:\n got %v\nwant %v", rec.calls, want)
	}
}
