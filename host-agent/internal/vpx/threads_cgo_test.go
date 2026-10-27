//go:build cgo

package vpx

import (
	"os"
	"runtime"
	"slices"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestThreadsCost encodes the synthetic desktop in real time (one frame every
// 33 ms, as the capture clock feeds it) with different libvpx thread counts,
// and reports the CPU, the per-frame encode time and the bitrate of each.
// Opt-in: RC_VPX_THREADS_BENCH=1 go test -run ThreadsCost -v ./internal/vpx/
func TestThreadsCost(t *testing.T) {
	if os.Getenv("RC_VPX_THREADS_BENCH") == "" {
		t.Skip("RC_VPX_THREADS_BENCH is not set")
	}
	const w, h, n = 1920, 1080, 240
	gen := newScreenGen(w, h)
	cpu := func() time.Duration {
		var c, e, k, u windows.Filetime
		if err := windows.GetProcessTimes(windows.CurrentProcess(), &c, &e, &k, &u); err != nil {
			t.Fatal(err)
		}
		return time.Duration(k.Nanoseconds() + u.Nanoseconds())
	}
	frame := time.Second / 30
	for _, threads := range []int{runtime.NumCPU(), 8, 0, 2, 1} {
		conv := newTestConverter(t, w, h, 1920)
		e := newTestEncoderWith(t, Settings{Width: w, Height: h, Threads: threads})
		img := NewImage(w, h)
		var works []time.Duration
		total := 0
		start, cpu0 := time.Now(), cpu()
		var genTime time.Duration
		for i := range n {
			g0 := time.Now()
			pix := gen.frame(i)
			genTime += time.Since(g0)
			t0 := time.Now()
			conv.Convert(img, unsafe.Pointer(&pix[0]), w*4)
			p, err := e.Encode(img, time.Duration(i)*frame, frame, false)
			if err != nil {
				t.Fatal(err)
			}
			works = append(works, time.Since(t0))
			total += len(p.Data)
			if next := start.Add(time.Duration(i+1) * frame); time.Until(next) > 0 {
				time.Sleep(time.Until(next))
			}
		}
		wall := time.Since(start)
		// The generator's own work is not the encoder's: take it out.
		used := cpu() - cpu0 - genTime
		slices.Sort(works)
		t.Logf("threads=%-2d (0 = the default): CPU %.2f cores, encode p50 %v p95 %v, %.0f kbit/s",
			threads, used.Seconds()/wall.Seconds(), works[n/2].Round(100*time.Microsecond),
			works[n*95/100].Round(100*time.Microsecond), float64(total)*8/1000/wall.Seconds())
		e.Close()
	}
}
