package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/diag"
)

// writeIVF writes VP8 frames as an IVF file (what ffmpeg reads with -f ivf).
func writeIVF(t *testing.T, path string, w, h int, frames []Frame) {
	t.Helper()
	var b bytes.Buffer
	hdr := make([]byte, 32)
	copy(hdr[0:4], "DKIF")
	binary.LittleEndian.PutUint16(hdr[6:8], 32)
	copy(hdr[8:12], "VP80")
	binary.LittleEndian.PutUint16(hdr[12:14], uint16(w))
	binary.LittleEndian.PutUint16(hdr[14:16], uint16(h))
	binary.LittleEndian.PutUint32(hdr[16:20], 1000) // timebase: milliseconds
	binary.LittleEndian.PutUint32(hdr[20:24], 1)
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(len(frames)))
	b.Write(hdr)
	var pts time.Duration
	for _, f := range frames {
		pts += f.Elapsed
		fh := make([]byte, 12)
		binary.LittleEndian.PutUint32(fh[0:4], uint32(len(f.Data)))
		binary.LittleEndian.PutUint64(fh[4:12], uint64(pts/time.Millisecond))
		b.Write(fh)
		b.Write(f.Data)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// useGDIIfAsked makes the capture skip desktop duplication when
// RC_CAPTURE_FORCE_GDI is set, to exercise the GDI fallback.
func useGDIIfAsked(t *testing.T) {
	if os.Getenv("RC_CAPTURE_FORCE_GDI") == "" {
		return
	}
	skipDuplication = true
	t.Cleanup(func() { skipDuplication = false })
	t.Log("desktop duplication skipped: capturing with GDI")
}

// vp8Size reads the size out of a VP8 keyframe header; ok is false for an
// interframe.
func vp8Size(data []byte) (w, h int, ok bool) {
	if len(data) < 10 || data[0]&1 == 1 || !bytes.Equal(data[3:6], []byte{0x9d, 0x01, 0x2a}) {
		return 0, 0, false
	}
	return int(binary.LittleEndian.Uint16(data[6:8]) & 0x3fff), int(binary.LittleEndian.Uint16(data[8:10]) & 0x3fff), true
}

// TestScreenCaptureIntegration captures the real screen for a few seconds.
// It needs an interactive desktop, so it runs only when asked:
//
//	RC_CAPTURE_INTEGRATION=1 go test -run ScreenCaptureIntegration -v ./internal/capture/
//
// RC_CAPTURE_IVF=<file> keeps the stream for a look (ffmpeg -i <file> ...).
func TestScreenCaptureIntegration(t *testing.T) {
	if os.Getenv("RC_CAPTURE_INTEGRATION") == "" {
		t.Skip("RC_CAPTURE_INTEGRATION is not set")
	}
	useGDIIfAsked(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c := NewScreenCapture(Options{})
	frames, err := c.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	var got []Frame
	for f := range frames {
		got = append(got, f)
	}
	// A still screen is encoded every idleInterval, a moving one on every
	// tick: at least a dozen frames in 3 s either way, and never a long gap.
	if len(got) < 10 {
		t.Fatalf("%d frames in 3 s, want at least 10 (one per %v while still, 30 a second while moving)", len(got), idleInterval)
	}
	w, h, ok := vp8Size(got[0].Data)
	if !ok {
		t.Fatal("the first frame is not a keyframe")
	}
	if got[0].Elapsed != 0 {
		t.Errorf("the first frame's Elapsed = %v, want 0", got[0].Elapsed)
	}
	var total time.Duration
	for i, f := range got[1:] {
		total += f.Elapsed
		if f.Elapsed > 2*idleInterval+100*time.Millisecond {
			t.Errorf("frame %d came %v after the previous one; the screen is encoded at least every %v", i+1, f.Elapsed, idleInterval)
		}
	}
	t.Logf("%d frames, %dx%d, mean gap %v, first keyframe %d bytes",
		len(got), w, h, total/time.Duration(len(got)-1), len(got[0].Data))
	if path := os.Getenv("RC_CAPTURE_IVF"); path != "" {
		writeIVF(t, path, w, h, got)
	}
}

// TestScreenCapturePause watches the capture while someone brings up the
// secure desktop (a UAC prompt, or Win+L) and dismisses it: while it is up no
// frames should go out and the capture should cost next to no CPU; afterwards
// frames resume, starting with a keyframe. It ends on its own a few seconds
// after the first full pause/resume, or after RC_CAPTURE_PAUSE_TEST minutes.
//
//	RC_CAPTURE_PAUSE_TEST=10 go test -run ScreenCapturePause -v -timeout 15m ./internal/capture/
func TestScreenCapturePause(t *testing.T) {
	minutes, _ := strconv.Atoi(os.Getenv("RC_CAPTURE_PAUSE_TEST"))
	if minutes <= 0 {
		t.Skip("RC_CAPTURE_PAUSE_TEST is not set")
	}
	diag.SetOutput(os.Stdout)
	defer diag.SetOutput(nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(minutes)*time.Minute)
	defer cancel()
	frames, err := NewScreenCapture(Options{}).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	cpu := func() time.Duration {
		var c, e, k, u windows.Filetime
		if err := windows.GetProcessTimes(windows.CurrentProcess(), &c, &e, &k, &u); err != nil {
			t.Fatal(err)
		}
		return time.Duration(k.Nanoseconds() + u.Nanoseconds())
	}
	second := time.NewTicker(time.Second)
	defer second.Stop()
	var (
		count, pausedSeconds int
		lastCPU              = cpu()
		maxPausedCPU         float64
		sawPause, resumed    bool
		resumeKey            bool
		doneAt               time.Time
	)
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatal("the capture stopped")
			}
			if sawPause && !resumed {
				resumed = true
				_, _, resumeKey = vp8Size(f.Data)
				doneAt = time.Now().Add(5 * time.Second)
				fmt.Printf("resumed: first frame after the pause is a keyframe: %v (gap %v)\n", resumeKey, f.Elapsed)
			}
			count++
		case <-second.C:
			now := cpu()
			cores := (now - lastCPU).Seconds()
			lastCPU = now
			if count == 0 {
				pausedSeconds++
				sawPause = true
				maxPausedCPU = max(maxPausedCPU, cores)
			}
			fmt.Printf("%s frames=%2d cpu=%.3f cores\n", time.Now().Format("15:04:05"), count, cores)
			count = 0
			if resumed && time.Now().After(doneAt) {
				t.Logf("paused for %d s, at most %.3f cores while paused; resumed with a keyframe: %v",
					pausedSeconds, maxPausedCPU, resumeKey)
				if !resumeKey {
					t.Error("the first frame after the pause was not a keyframe")
				}
				if maxPausedCPU > 0.02 {
					t.Errorf("the paused capture used up to %.3f cores, want next to nothing", maxPausedCPU)
				}
				return
			}
		case <-ctx.Done():
			t.Fatalf("no pause and resume seen within %d minutes", minutes)
		}
	}
}
