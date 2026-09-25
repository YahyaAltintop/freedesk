package session

import (
	"context"
	"testing"
	"time"

	pion "github.com/pion/webrtc/v4"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/capture"
)

// newVideoTrack makes a real, unbound VP8 track. WriteSample on it is a no-op
// that returns nil, so pumpFrames can be exercised without ffmpeg or a peer.
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
// when ffmpeg exits its frame channel closes, and pumpFrames must return with
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
