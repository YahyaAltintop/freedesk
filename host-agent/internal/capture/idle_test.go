package capture

import (
	"context"
	"testing"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/vpx"
)

// stillSource is a source whose picture never changes after the first grab,
// placed far from any monitor so the real pointer is never drawn into it.
type stillSource struct {
	frame *dib
	grabs int
}

func (s *stillSource) name() string { return "still test source" }
func (s *stillSource) grab() (bool, error) {
	s.grabs++
	return s.grabs == 1, nil
}
func (s *stillSource) current() *dib      { return s.frame }
func (s *stillSource) origin() (int, int) { return 1 << 20, 1 << 20 }
func (s *stillSource) close()             { s.frame.close() }

// TestStillScreenIsEncodedAtIdleRate: with nothing changing, the encoder
// must repeat the picture about every idleInterval, not on every tick, and
// each repeat must carry the real gap.
func TestStillScreenIsEncodedAtIdleRate(t *testing.T) {
	if !vpx.Available() {
		t.Skip("no encoder in this build")
	}
	frame, err := newDIB(320, 200)
	if err != nil {
		t.Fatal(err)
	}
	newTestSource = func() (source, error) { return &stillSource{frame: frame}, nil }
	defer func() { newTestSource = nil }()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	frames, err := NewScreenCapture(Options{}).Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, ok := <-frames
	if !ok {
		t.Fatal("the capture ended before its first frame")
	}
	if _, _, key := vp8Size(first.Data); !key {
		t.Fatal("the first frame must be a keyframe")
	}

	const window = time.Second
	deadline := time.After(window)
	var gaps []time.Duration
loop:
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatal("the capture ended")
			}
			gaps = append(gaps, f.Elapsed)
		case <-deadline:
			break loop
		}
	}
	cancel()
	for range frames { // until the capture has closed its source
	}

	want := int(window / idleInterval)
	if len(gaps) < want-2 || len(gaps) > want+2 {
		t.Fatalf("%d repeats in %v, want about %d (one every %v); gaps %v", len(gaps), window, want, idleInterval, gaps)
	}
	for _, g := range gaps {
		if g < idleInterval*3/4 {
			t.Fatalf("a repeat carried a gap of %v, want about %v: %v", g, idleInterval, gaps)
		}
	}
}

// TestFrameDuration pins what the encoder is told a frame stands for.
func TestFrameDuration(t *testing.T) {
	const tick = time.Second / 30
	cases := []struct {
		name   string
		repeat bool
		gap    time.Duration
		want   time.Duration
	}{
		{"first frame", false, 0, tick},
		{"on time", false, tick, tick},
		{"slow encoder", false, 2 * tick, 2 * tick},
		{"after a quiet spell", false, 5 * time.Second, maxTicksPerFrame * tick},
		{"repeat on time", true, tick, tick},
		{"repeat after a quiet spell", true, idleInterval, tick},
	}
	for _, c := range cases {
		if got := frameDuration(c.repeat, c.gap, tick); got != c.want {
			t.Errorf("%s: frameDuration(%v, %v, tick) = %v, want %v", c.name, c.repeat, c.gap, got, c.want)
		}
	}
}
