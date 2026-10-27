package capture

import (
	"context"
	"testing"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/vpx"
)

// TestKeyframeOnRequest: on a still screen the next natural keyframe is many
// seconds away (kf_max_dist frames at the idle rate), so a keyframe arriving
// soon after Request can only be the forced one.
func TestKeyframeOnRequest(t *testing.T) {
	if !vpx.Available() {
		t.Skip("no encoder in this build")
	}
	frame, err := newDIB(320, 200)
	if err != nil {
		t.Fatal(err)
	}
	newTestSource = func() (source, error) { return &stillSource{frame: frame}, nil }
	defer func() { newTestSource = nil }()

	kf := NewKeyframeRequest()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	frames, err := NewScreenCapture(Options{Keyframe: kf}).Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	drain := func() { // let the capture close its source after cancel
		for range frames {
		}
	}

	first, ok := <-frames
	if !ok {
		t.Fatal("the capture ended before its first frame")
	}
	if _, _, key := vp8Size(first.Data); !key {
		t.Fatal("the first frame must be a keyframe")
	}

	// A short baseline: with nothing changing and nothing requested, the
	// repeats are delta frames, not keyframes.
	baseline := time.After(600 * time.Millisecond)
baselineLoop:
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatal("the capture ended")
			}
			if _, _, key := vp8Size(f.Data); key {
				cancel()
				drain()
				t.Fatal("a keyframe arrived on a still screen with nothing requested")
			}
		case <-baseline:
			break baselineLoop
		}
	}

	// Now ask for one; it must come promptly (a job flows every idleInterval).
	kf.Request()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatal("the capture ended")
			}
			if _, _, key := vp8Size(f.Data); key {
				cancel()
				drain()
				return // the requested keyframe
			}
		case <-deadline:
			cancel()
			drain()
			t.Fatal("no keyframe within two seconds of the request")
		}
	}
}
