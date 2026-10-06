package session

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	pion "github.com/pion/webrtc/v4"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/capture"
)

// newVideoTrack makes a real, unbound VP8 track. WriteSample on it is a no-op
// that returns nil, so pumpFrames can be exercised without a capture or a peer.
func newVideoTrack(t *testing.T) *pion.TrackLocalStaticSample {
	t.Helper()
	track, err := pion.NewTrackLocalStaticSample(
		pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8}, "video", "freedesk")
	if err != nil {
		t.Fatalf("could not create track: %v", err)
	}
	return track
}

// TestPumpFramesReportsCaptureEnd checks the case the restart loop depends on:
// when the capture ends its frame channel closes, and pumpFrames must return with
// writeFailed=false (so streamScreen restarts) and a non-zero lastFrameAt.
func TestPumpFramesReportsCaptureEnd(t *testing.T) {
	c := &Coordinator{}
	frames := make(chan capture.Frame, 2)
	frames <- capture.Frame{Data: []byte{1}}
	frames <- capture.Frame{Data: []byte{2}, Elapsed: 33 * time.Millisecond}
	close(frames)

	last, writeFailed := c.pumpFrames(context.Background(), "sess", newVideoTrack(t), frames, time.Time{})
	if writeFailed {
		t.Fatal("a closed capture channel must not be reported as a write failure")
	}
	if last.IsZero() {
		t.Fatal("lastFrameAt must be set after frames were written")
	}
}

// TestSleepCtx covers both outcomes of the restart backoff wait.
func TestSleepCtx(t *testing.T) {
	if !sleepCtx(context.Background(), time.Millisecond) {
		t.Fatal("a full wait must report true")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepCtx(ctx, time.Hour) {
		t.Fatal("a cancelled context must report false without waiting")
	}
}

// TestStreamScreenStopsACaptureNobodyReads: once pumpFrames returns — the
// capture ended, or the track could not be written any more — the capture's
// context must end at once, before any backoff. A capture left on the
// session's context would go on grabbing the screen for nobody until the
// session ends.
func TestStreamScreenStopsACaptureNobodyReads(t *testing.T) {
	var calls atomic.Int32
	first := make(chan context.Context, 1)
	frames := make(chan capture.Frame)
	c := &Coordinator{startCapture: func(ctx context.Context, _ capture.Options) (<-chan capture.Frame, error) {
		if calls.Add(1) == 1 {
			first <- ctx
			return frames, nil
		}
		return nil, errors.New("no screen in this test")
	}}
	ctx := t.Context() // ends after the test, which also ends streamScreen's loop
	c.streamScreen(ctx, "sess", newVideoTrack(t), capture.NewKeyframeRequest())

	var captureCtx context.Context
	select {
	case captureCtx = <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("streamScreen did not start a capture")
	}
	close(frames) // the capture ends, so pumpFrames returns
	select {
	case <-captureCtx.Done():
	case <-time.After(captureRetryBackoff / 2):
		t.Fatal("the capture's context was not ended when pumpFrames returned")
	}
	if ctx.Err() != nil {
		t.Fatal("the session's own context must be untouched")
	}
}
