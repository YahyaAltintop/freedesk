package vpx

import (
	"runtime"
	"testing"
)

func TestOutputSize(t *testing.T) {
	cases := []struct {
		name                   string
		srcW, srcH, maxW, w, h int
	}{
		{"fits", 1920, 1080, 1920, 1920, 1080},
		{"no cap", 3840, 2160, 0, 3840, 2160},
		{"4K halves", 3840, 2160, 1920, 1920, 1080},
		{"1440p", 2560, 1440, 1920, 1920, 1080},
		// 1920*1440/3440 = 401.86 pairs of rows, rounded to 402: what
		// ffmpeg's scale=...:h=-2 gives.
		{"ultrawide", 3440, 1440, 1920, 1920, 804},
		{"odd height fits", 1366, 767, 1920, 1366, 766},
		{"odd width fits", 1365, 768, 1920, 1364, 768},
		{"portrait", 1080, 1920, 1920, 1080, 1920},
		{"odd cap", 3840, 2160, 1921, 1920, 1080},
		{"empty", 0, 0, 1920, 0, 0},
	}
	for _, c := range cases {
		w, h := OutputSize(c.srcW, c.srcH, c.maxW)
		if w != c.w || h != c.h {
			t.Errorf("%s: OutputSize(%d, %d, %d) = %dx%d, want %dx%d",
				c.name, c.srcW, c.srcH, c.maxW, w, h, c.w, c.h)
		}
	}
}

// TestRateControlMatchesFFmpeg pins the libvpx configuration to what ffmpeg
// 9.0's libvpxenc.c derived from the arguments the agent used to pass it, at
// today's 3 Mbit/s (-b:v/-minrate/-maxrate/-bufsize 3M, -rc_init_occupancy
// 1.5M, -undershoot-pct 100, -overshoot-pct 15, -max-intra-rate 300, -g 60,
// -cpu-used 5, -error-resilient 1), except for the thread count: four, where
// ffmpeg's "threads auto" took one per CPU. A change here changes the stream
// the viewers get.
func TestRateControlMatchesFFmpeg(t *testing.T) {
	got := rateControlFor(Settings{})
	want := rateControl{
		targetKbps:     3000,
		bufMs:          1000,
		bufInitialMs:   500,
		bufOptimalMs:   833,
		undershootPct:  100,
		overshootPct:   15,
		maxIntraPct:    300,
		kfMaxDist:      60,
		cpuUsed:        5,
		threads:        min(runtime.NumCPU(), 4),
		errorResilient: 1,
	}
	if got != want {
		t.Fatalf("rateControlFor(defaults) =\n %+v\nwant\n %+v", got, want)
	}
}

func TestRateControlFollowsSettings(t *testing.T) {
	got := rateControlFor(Settings{Bitrate: 1_000_000, Framerate: 15, Threads: 200})
	if got.targetKbps != 1000 || got.bufMs != 1000 || got.bufInitialMs != 500 || got.bufOptimalMs != 833 {
		t.Errorf("buffer and target should follow the bitrate in time, got %+v", got)
	}
	if got.kfMaxDist != 30 {
		t.Errorf("kfMaxDist = %d, want two seconds at 15 fps = 30", got.kfMaxDist)
	}
	if got.threads != 64 {
		t.Errorf("threads = %d, want the cap of 64", got.threads)
	}
}
