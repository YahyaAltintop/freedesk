package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestCaptureBench measures the capture on the real screen: CPU, frame rate,
// bitrate, keyframe sizes and the time from a tick to its encoded frame. It
// needs an interactive desktop, so it runs only when asked:
//
//	RC_CAPTURE_BENCH=1 RC_BENCH_LABEL=video go test -run CaptureBench -v ./internal/capture/
//
// RC_BENCH_SECONDS (default 8) and RC_BENCH_ROUNDS (default 2) set the length;
// RC_CAPTURE_FORCE_GDI=1 measures the GDI fallback. Each round prints one
// JSON line. The numbers measured when this capture replaced ffmpeg are in
// docs/ARCHITECTURE.md.
func TestCaptureBench(t *testing.T) {
	if os.Getenv("RC_CAPTURE_BENCH") == "" {
		t.Skip("RC_CAPTURE_BENCH is not set")
	}
	useGDIIfAsked(t)
	label := os.Getenv("RC_BENCH_LABEL")
	seconds, rounds := 8, 2
	if v, err := strconv.Atoi(os.Getenv("RC_BENCH_SECONDS")); err == nil && v > 0 {
		seconds = v
	}
	if v, err := strconv.Atoi(os.Getenv("RC_BENCH_ROUNDS")); err == nil && v > 0 {
		rounds = v
	}
	for range rounds {
		b, _ := json.Marshal(benchCapture(t, label, time.Duration(seconds)*time.Second))
		fmt.Println("BENCH", string(b))
	}
}

type benchResult struct {
	Label     string  `json:"label"`
	Seconds   float64 `json:"seconds"`
	CPUCores  float64 `json:"cpu_cores"`
	FPS       float64 `json:"fps"`
	Kbps      float64 `json:"kbps"`
	Keyframes int     `json:"keyframes"`
	KeyAvgKB  float64 `json:"key_avg_kb"`
	KeyMaxKB  float64 `json:"key_max_kb"`
	DeltaAvgB float64 `json:"delta_avg_bytes"`
	NewImages int     `json:"new_images"` // frames that carried a new picture
	WorkP50ms float64 `json:"work_p50_ms"`
	WorkP95ms float64 `json:"work_p95_ms"`
}

func processCPU(t *testing.T) time.Duration {
	t.Helper()
	var c, e, k, u windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &c, &e, &k, &u); err != nil {
		t.Fatal(err)
	}
	return time.Duration(k.Nanoseconds() + u.Nanoseconds())
}

func benchCapture(t *testing.T, label string, d time.Duration) benchResult {
	t.Helper()
	c := NewScreenCapture(Options{})
	var (
		mu        sync.Mutex
		works     []time.Duration
		newImages int
	)
	c.sample = func(s frameSample) {
		mu.Lock()
		defer mu.Unlock()
		works = append(works, s.work)
		if s.changed {
			newImages++
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames, err := c.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var (
		got      []Frame
		first    time.Time
		firstCPU time.Duration
	)
	timer := time.NewTimer(d + 5*time.Second)
loop:
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				break loop
			}
			if first.IsZero() {
				// Counting starts at the first frame: start-up is a per-session
				// cost, not a per-second one.
				first, firstCPU = time.Now(), processCPU(t)
				timer.Reset(d)
			}
			got = append(got, f)
		case <-timer.C:
			break loop
		}
	}
	wall := time.Since(first)
	cpu := processCPU(t) - firstCPU
	cancel()
	for range frames {
	}

	r := benchResult{Label: label, Seconds: wall.Seconds(), CPUCores: cpu.Seconds() / wall.Seconds()}
	var total, keyBytes, keyMax, deltaBytes, deltas int
	for _, f := range got {
		total += len(f.Data)
		if f.Data[0]&1 == 0 {
			r.Keyframes++
			keyBytes += len(f.Data)
			keyMax = max(keyMax, len(f.Data))
		} else {
			deltaBytes += len(f.Data)
			deltas++
		}
	}
	r.FPS = float64(len(got)) / wall.Seconds()
	r.Kbps = float64(total) * 8 / 1000 / wall.Seconds()
	if r.Keyframes > 0 {
		r.KeyAvgKB = float64(keyBytes) / float64(r.Keyframes) / 1024
	}
	r.KeyMaxKB = float64(keyMax) / 1024
	if deltas > 0 {
		r.DeltaAvgB = float64(deltaBytes) / float64(deltas)
	}
	mu.Lock()
	defer mu.Unlock()
	r.NewImages = newImages
	slices.Sort(works)
	if len(works) > 0 {
		r.WorkP50ms = float64(works[len(works)/2]) / float64(time.Millisecond)
		r.WorkP95ms = float64(works[len(works)*95/100]) / float64(time.Millisecond)
	}
	return r
}
