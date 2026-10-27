//go:build cgo

package vpx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
	"unsafe"
)

// TestEncoderMatchesFFmpeg feeds the same synthetic desktop sequence to ffmpeg
// (the libvpx arguments the agent used to pass it) and to this encoder, and
// compares what comes out: bitrate, keyframe sizes, interframe sizes. The
// screen-like content holds still in most places and moves in a few, like a
// desktop: a dragged window, typing, a small video. Opt-in, as it needs an
// ffmpeg:
//
//	RC_FFMPEG_GOLDEN=C:\path\to\ffmpeg.exe go test -run EncoderMatchesFFmpeg -v ./internal/vpx/
func TestEncoderMatchesFFmpeg(t *testing.T) {
	ffmpeg := os.Getenv("RC_FFMPEG_GOLDEN")
	if ffmpeg == "" {
		t.Skip("RC_FFMPEG_GOLDEN is not set")
	}
	const w, h, n = 1920, 1080, 300 // ten seconds at 30 fps
	gen := newScreenGen(w, h)

	// ffmpeg, reading raw frames on stdin: the agent's arguments with the
	// gdigrab input swapped for rawvideo, at today's bitrate and on the
	// agent's thread count (ffmpeg's own default was one per CPU).
	rate := strconv.Itoa(DefaultBitrate)
	threads := strconv.Itoa(rateControlFor(Settings{}).threads)
	out := filepath.Join(t.TempDir(), "ffmpeg.ivf")
	input := []string{"-hide_banner", "-loglevel", "error"}
	if os.Getenv("RC_PARITY_LIVE_TIMESTAMPS") != "" {
		// Timestamps the way gdigrab gave them: wall-clock microseconds,
		// frames arriving in real time.
		input = append(input, "-re", "-use_wallclock_as_timestamps", "1")
	}
	input = append(input, "-f", "rawvideo", "-pix_fmt", "bgra", "-s", fmt.Sprintf("%dx%d", w, h), "-framerate", "30", "-i", "-")
	cmd := exec.Command(ffmpeg, append(input,
		"-an", "-vf", "scale=w='min(iw,1920)':h=-2", "-c:v", "libvpx",
		"-b:v", rate, "-minrate", rate, "-maxrate", rate,
		"-bufsize", rate, "-rc_init_occupancy", strconv.Itoa(DefaultBitrate/2),
		"-undershoot-pct", "100", "-overshoot-pct", "15", "-max-intra-rate", "300",
		"-lag-in-frames", "0", "-deadline", "realtime", "-cpu-used", "5", "-error-resilient", "1",
		"-g", "60", "-threads", threads, "-pix_fmt", "yuv420p", "-fps_mode", "passthrough", "-f", "ivf", "-y", out)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if _, err := stdin.Write(gen.frame(i)); err != nil {
			t.Fatalf("writing frame %d to ffmpeg: %v\n%s", i, err, stderr.String())
		}
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, stderr.String())
	}
	ref := readIVFSizes(t, out)

	conv, e := newTestConverter(t, w, h, 1920), newTestEncoder(t, w, h)
	img := NewImage(w, h)
	frame := time.Second / 30
	var ours []frameStat
	for i := range n {
		pix := gen.frame(i)
		conv.Convert(img, unsafe.Pointer(&pix[0]), w*4)
		p, err := e.Encode(img, time.Duration(i)*frame, frame, false)
		if err != nil {
			t.Fatal(err)
		}
		ours = append(ours, frameStat{size: len(p.Data), key: p.Keyframe})
	}

	a, b := statsOf(ref), statsOf(ours)
	t.Logf("ffmpeg: %s", a)
	t.Logf("ours:   %s", b)
	if a.frames != b.frames || a.keys != b.keys {
		t.Errorf("frame/keyframe counts differ: ffmpeg %d/%d, ours %d/%d", a.frames, a.keys, b.frames, b.keys)
	}
	within := func(name string, x, y float64, pct float64) {
		if d := (y - x) / x * 100; d > pct || d < -pct {
			t.Errorf("%s differs by %+.1f%% (ffmpeg %.0f, ours %.0f), want within ±%.0f%%", name, d, x, y, pct)
		}
	}
	within("total size", float64(a.total), float64(b.total), 10)
	within("mean keyframe", a.keyAvg, b.keyAvg, 10)
	within("largest keyframe", float64(a.keyMax), float64(b.keyMax), 10)
}

type frameStat struct {
	size int
	key  bool
}

type seqStats struct {
	frames, keys, total, keyMax int
	keyAvg, deltaAvg            float64
}

func (s seqStats) String() string {
	return fmt.Sprintf("%d frames, %d keyframes, %.0f kbit/s, keyframes avg %.1f KB max %.1f KB, interframes avg %.0f B",
		s.frames, s.keys, float64(s.total)*8/1000/(float64(s.frames)/30), s.keyAvg/1024, float64(s.keyMax)/1024, s.deltaAvg)
}

func statsOf(fs []frameStat) seqStats {
	var s seqStats
	var keyBytes, deltaBytes, deltas int
	for _, f := range fs {
		s.frames++
		s.total += f.size
		if f.key {
			s.keys++
			keyBytes += f.size
			s.keyMax = max(s.keyMax, f.size)
		} else {
			deltas++
			deltaBytes += f.size
		}
	}
	if s.keys > 0 {
		s.keyAvg = float64(keyBytes) / float64(s.keys)
	}
	if deltas > 0 {
		s.deltaAvg = float64(deltaBytes) / float64(deltas)
	}
	return s
}

func readIVFSizes(t *testing.T, path string) []frameStat {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(b[32:])
	var fs []frameStat
	hdr := make([]byte, 12)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			break
		}
		size := int(binary.LittleEndian.Uint32(hdr[0:4]))
		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			t.Fatal(err)
		}
		fs = append(fs, frameStat{size: size, key: data[0]&1 == 0})
	}
	return fs
}

// screenGen draws a deterministic desktop-like sequence: a page of text that
// holds still, a window being dragged across it, a line being typed and a
// small video playing in a corner.
type screenGen struct {
	w, h       int
	background []byte
	window     []byte // 480x320
	buf        []byte
}

const genWinW, genWinH = 480, 320

func newScreenGen(w, h int) *screenGen {
	g := &screenGen{w: w, h: h, background: make([]byte, w*h*4), window: make([]byte, genWinW*genWinH*4), buf: make([]byte, w*h*4)}
	rng := rand.New(rand.NewPCG(3, 4))
	fill := func(buf []byte, bw, x0, y0, x1, y1 int, r, gr, b byte) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				i := (y*bw + x) * 4
				buf[i], buf[i+1], buf[i+2], buf[i+3] = b, gr, r, 255
			}
		}
	}
	text := func(buf []byte, bw, x0, y0, x1, y1 int) {
		for y := y0; y+14 < y1; y += 22 {
			for x := x0; x < x1-8; {
				gw := 3 + rng.IntN(6)
				if rng.IntN(7) == 0 {
					x += 8 // a space
					continue
				}
				fill(buf, bw, x, y+2+rng.IntN(3), min(x+gw, x1), y+12, 30, 30, 40)
				x += gw + 2
			}
		}
	}
	fill(g.background, w, 0, 0, w, h, 243, 243, 246)
	fill(g.background, w, 0, 0, w, 48, 40, 44, 70)         // title bar
	fill(g.background, w, 0, h-48, w, h, 32, 32, 40)       // taskbar
	fill(g.background, w, 0, 48, 260, h-48, 228, 230, 238) // sidebar
	text(g.background, w, 290, 80, w-40, h-80)
	fill(g.window, genWinW, 0, 0, genWinW, genWinH, 255, 255, 255)
	fill(g.window, genWinW, 0, 0, genWinW, 32, 100, 70, 220)
	text(g.window, genWinW, 12, 44, genWinW-12, genWinH-8)
	return g
}

func (g *screenGen) frame(i int) []byte {
	copy(g.buf, g.background)
	// The dragged window.
	wx, wy := 300+(i*7)%1100, 120+(i*3)%500
	for y := range genWinH {
		copy(g.buf[((wy+y)*g.w+wx)*4:((wy+y)*g.w+wx+genWinW)*4], g.window[y*genWinW*4:(y+1)*genWinW*4])
	}
	// The line being typed: one more glyph every fourth frame.
	for k := range (i / 4) % 120 {
		x := 300 + k*9
		for y := g.h - 120; y < g.h-108; y++ {
			for xx := x; xx < x+6; xx++ {
				p := (y*g.w + xx) * 4
				g.buf[p], g.buf[p+1], g.buf[p+2] = 40, 30, 30
			}
		}
	}
	// The video: a 320x180 gradient that shifts every frame.
	for y := range 180 {
		for x := range 320 {
			p := ((100+y)*g.w + 1560 + x) * 4
			g.buf[p] = byte(x + i*5)
			g.buf[p+1] = byte(y*2 + i*3)
			g.buf[p+2] = byte(x + y + i*7)
		}
	}
	return g.buf
}
