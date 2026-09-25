package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"
)

type testFrame struct {
	pts  uint64
	data []byte
}

// buildIVF assembles a minimal valid IVF stream (timebase num/den) around the
// given VP8 payloads so readIVF can be tested without ffmpeg.
func buildIVF(num, den uint32, frames []testFrame) []byte {
	var buf bytes.Buffer

	header := make([]byte, 32)
	copy(header[0:4], "DKIF")
	binary.LittleEndian.PutUint16(header[4:6], 0)  // version
	binary.LittleEndian.PutUint16(header[6:8], 32) // header length
	copy(header[8:12], "VP80")                     // codec fourcc
	binary.LittleEndian.PutUint16(header[12:14], 640)
	binary.LittleEndian.PutUint16(header[14:16], 480)
	binary.LittleEndian.PutUint32(header[16:20], den) // timebase denominator
	binary.LittleEndian.PutUint32(header[20:24], num) // timebase numerator
	binary.LittleEndian.PutUint32(header[24:28], uint32(len(frames)))
	buf.Write(header)

	for _, f := range frames {
		fh := make([]byte, 12)
		binary.LittleEndian.PutUint32(fh[0:4], uint32(len(f.data)))
		binary.LittleEndian.PutUint64(fh[4:12], f.pts)
		buf.Write(fh)
		buf.Write(f.data)
	}
	return buf.Bytes()
}

func collectFrames(t *testing.T, stream []byte) []Frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	frames := make(chan Frame, 16)
	go func() {
		readIVF(ctx, bytes.NewReader(stream), frames, t.Logf)
		close(frames)
	}()

	var got []Frame
	for f := range frames {
		got = append(got, f)
	}
	return got
}

func TestReadIVFPayloads(t *testing.T) {
	in := []testFrame{{0, []byte{0x10, 0x20, 0x30}}, {1, []byte{0x40, 0x50}}}
	got := collectFrames(t, buildIVF(1, 30, in))

	if len(got) != len(in) {
		t.Fatalf("expected %d frames, got %d", len(in), len(got))
	}
	for i := range in {
		if !bytes.Equal(got[i].Data, in[i].data) {
			t.Fatalf("frame %d mismatch: expected %v, got %v", i, in[i].data, got[i].Data)
		}
	}
}

// TestReadIVFElapsed checks that the gap between consecutive pts values (in
// the header timebase) becomes Frame.Elapsed: a dropped capture (pts 1 -> 3 at
// 1/30 s) must widen the gap instead of being reported as a regular frame.
func TestReadIVFElapsed(t *testing.T) {
	in := []testFrame{{0, []byte{1}}, {1, []byte{2}}, {3, []byte{3}}}
	got := collectFrames(t, buildIVF(1, 30, in))

	if len(got) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(got))
	}
	tick := time.Second / 30
	want := []time.Duration{0, tick, 2 * tick}
	for i, w := range want {
		if diff := got[i].Elapsed - w; diff > time.Millisecond || diff < -time.Millisecond {
			t.Fatalf("frame %d elapsed: expected %v, got %v", i, w, got[i].Elapsed)
		}
	}
}

func TestElapsedBetween(t *testing.T) {
	tick := 1.0 / 30
	if got := elapsedBetween(5, 5, tick); got != 0 {
		t.Fatalf("equal pts should give 0, got %v", got)
	}
	if got := elapsedBetween(7, 3, tick); got != 0 {
		t.Fatalf("backwards pts should give 0, got %v", got)
	}
	if got := elapsedBetween(0, 1_000_000, tick); got != maxElapsed {
		t.Fatalf("huge gap should be capped at %v, got %v", maxElapsed, got)
	}
}
