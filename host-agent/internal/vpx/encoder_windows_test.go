//go:build cgo

package vpx

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
	"unsafe"
)

func newTestEncoder(t testing.TB, w, h int) *Encoder {
	t.Helper()
	return newTestEncoderWith(t, Settings{Width: w, Height: h})
}

func newTestEncoderWith(t testing.TB, s Settings) *Encoder {
	t.Helper()
	e, err := NewEncoder(s)
	if err != nil {
		t.Fatalf("NewEncoder(%+v): %v", s, err)
	}
	t.Cleanup(e.Close)
	return e
}

func newTestConverter(t testing.TB, srcW, srcH, maxWidth int) *Converter {
	t.Helper()
	c, err := NewConverter(srcW, srcH, maxWidth)
	if err != nil {
		t.Fatalf("NewConverter(%d, %d, %d): %v", srcW, srcH, maxWidth, err)
	}
	t.Cleanup(c.Close)
	return c
}

// convert converts img with c into a new image of c's size.
func convert(c *Converter, img *bgra) *Image {
	w, h := c.Size()
	out := NewImage(w, h)
	c.Convert(out, unsafe.Pointer(&img.pix[0]), img.w*4)
	return out
}

// bgra is a top-down BGRA frame held in Go memory.
type bgra struct {
	w, h int
	pix  []byte
}

func newBGRA(w, h int) *bgra { return &bgra{w: w, h: h, pix: make([]byte, w*h*4)} }

func (b *bgra) set(x, y int, r, g, bl byte) {
	i := (y*b.w + x) * 4
	b.pix[i], b.pix[i+1], b.pix[i+2], b.pix[i+3] = bl, g, r, 255
}

func (b *bgra) fill(r, g, bl byte) {
	for y := range b.h {
		for x := range b.w {
			b.set(x, y, r, g, bl)
		}
	}
}

// The reference formulas the C converter implements (BT.601, limited range).
func refY(r, g, b int) byte { return byte((66*r + 129*g + 25*b + 0x1080) >> 8) }
func refU(r, g, b int) byte { return byte((112*b - 74*g - 38*r + 0x8080) >> 8) }
func refV(r, g, b int) byte { return byte((112*r - 94*g - 18*b + 0x8080) >> 8) }

func TestConvertSolidColours(t *testing.T) {
	colours := [][3]int{
		{0, 0, 0}, {255, 255, 255}, {255, 0, 0}, {0, 255, 0}, {0, 0, 255},
		{128, 128, 128}, {12, 200, 77}, {139, 92, 246}, // the brand violet
	}
	conv := newTestConverter(t, 16, 16, 0)
	for _, c := range colours {
		r, g, b := c[0], c[1], c[2]
		img := newBGRA(16, 16)
		img.fill(byte(r), byte(g), byte(b))
		out := convert(conv, img)
		y, u, v := out.Y, out.U, out.V
		wantY, wantU, wantV := refY(r, g, b), refU(r, g, b), refV(r, g, b)
		if !allEqual(y, wantY) || !allEqual(u, wantU) || !allEqual(v, wantV) {
			t.Errorf("rgb(%d,%d,%d): got Y %d U %d V %d, want %d %d %d",
				r, g, b, y[0], u[0], v[0], wantY, wantU, wantV)
		}
	}
	// The extremes stay inside the limited range.
	if refY(0, 0, 0) != 16 || refY(255, 255, 255) != 235 {
		t.Errorf("luma range is %d..%d, want 16..235", refY(0, 0, 0), refY(255, 255, 255))
	}
}

func TestConvertAveragesChromaOverEachBlock(t *testing.T) {
	img := newBGRA(16, 16)
	img.fill(0, 0, 0)
	img.set(0, 0, 255, 0, 0)
	img.set(1, 0, 0, 255, 0)
	img.set(0, 1, 0, 0, 255)
	img.set(1, 1, 255, 255, 255)
	out := convert(newTestConverter(t, 16, 16, 0), img)
	y, u, v := out.Y, out.U, out.V
	if y[0] != refY(255, 0, 0) || y[1] != refY(0, 255, 0) || y[16] != refY(0, 0, 255) || y[17] != refY(255, 255, 255) {
		t.Errorf("luma of the first block = %d %d %d %d", y[0], y[1], y[16], y[17])
	}
	// (255+0+0+255+2)/4 = 128 for each channel.
	if u[0] != refU(128, 128, 128) || v[0] != refV(128, 128, 128) {
		t.Errorf("chroma of the first block = U %d V %d, want the average colour's %d %d",
			u[0], v[0], refU(128, 128, 128), refV(128, 128, 128))
	}
	if u[1] != refU(0, 0, 0) {
		t.Errorf("the next block must not see the first block's colour, got U %d", u[1])
	}
}

func TestScaleHalvesAreaAveraged(t *testing.T) {
	// A one-pixel checkerboard averages to mid-grey when every 2x2 block
	// becomes one pixel.
	conv := newTestConverter(t, 3840, 2160, 1920)
	if w, h := conv.Size(); w != 1920 || h != 1080 {
		t.Fatalf("Size = %dx%d, want 1920x1080", w, h)
	}
	img := newBGRA(3840, 2160)
	for y := range img.h {
		for x := range img.w {
			if (x+y)%2 == 0 {
				img.set(x, y, 255, 255, 255)
			} else {
				img.set(x, y, 0, 0, 0)
			}
		}
	}
	y := convert(conv, img).Y
	lo, hi := refY(127, 127, 127), refY(128, 128, 128)
	for i, l := range y {
		if l < lo || l > hi {
			t.Fatalf("luma[%d] = %d, want mid-grey %d..%d", i, l, lo, hi)
		}
	}
}

func TestScaleKeepsEdgesAtFractionalRatios(t *testing.T) {
	// 2560 -> 1920 is 3:4. A black left half and white right half stay black
	// and white away from the seam, which is the one column that blends.
	conv := newTestConverter(t, 2560, 1440, 1920)
	img := newBGRA(2560, 1440)
	for y := range img.h {
		for x := range img.w {
			if x < 1280 {
				img.set(x, y, 0, 0, 0)
			} else {
				img.set(x, y, 255, 255, 255)
			}
		}
	}
	y := convert(conv, img).Y
	for row := 0; row < 1080; row += 97 {
		line := y[row*1920 : (row+1)*1920]
		if line[0] != 16 || line[958] != 16 || line[961] != 235 || line[1919] != 235 {
			t.Fatalf("row %d: edges %d %d | %d %d, want 16 16 | 235 235",
				row, line[0], line[958], line[961], line[1919])
		}
	}
}

// vp8Key reports whether an encoded frame is a VP8 keyframe, checking the
// keyframe header (start code and dimensions) as a decoder would.
func vp8Key(t *testing.T, data []byte, w, h int) bool {
	t.Helper()
	if len(data) < 10 {
		t.Fatalf("frame of %d bytes is too short for VP8", len(data))
	}
	if data[0]&1 == 1 {
		return false
	}
	if !bytes.Equal(data[3:6], []byte{0x9d, 0x01, 0x2a}) {
		t.Fatalf("keyframe without the VP8 start code: % x", data[:10])
	}
	gotW := int(binary.LittleEndian.Uint16(data[6:8]) & 0x3fff)
	gotH := int(binary.LittleEndian.Uint16(data[8:10]) & 0x3fff)
	if gotW != w || gotH != h {
		t.Fatalf("keyframe says %dx%d, want %dx%d", gotW, gotH, w, h)
	}
	return true
}

func gradient(w, h, shift int) *bgra {
	img := newBGRA(w, h)
	for y := range h {
		for x := range w {
			img.set(x, y, byte(x+shift), byte(y), byte(x+y))
		}
	}
	return img
}

func TestEncodeKeyframeThenDelta(t *testing.T) {
	conv, e := newTestConverter(t, 320, 240, 0), newTestEncoder(t, 320, 240)
	frame := time.Second / 30
	var img *Image
	for i, want := range []bool{true, false, false} {
		img = convert(conv, gradient(320, 240, i))
		p, err := e.Encode(img, time.Duration(i)*frame, frame, false)
		if err != nil {
			t.Fatal(err)
		}
		if p.Keyframe != want || vp8Key(t, p.Data, 320, 240) != want {
			t.Fatalf("frame %d: keyframe = %v (flag %v), want %v", i, vp8Key(t, p.Data, 320, 240), p.Keyframe, want)
		}
	}
	p, err := e.Encode(img, 3*frame, frame, true)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Keyframe || !vp8Key(t, p.Data, 320, 240) {
		t.Fatal("a forced keyframe was not a keyframe")
	}
}

func TestKeyframeEveryTwoSeconds(t *testing.T) {
	e := newTestEncoder(t, 320, 240)
	img := convert(newTestConverter(t, 320, 240, 0), gradient(320, 240, 0))
	frame := time.Second / 30
	var keys []int
	for i := range 121 {
		p, err := e.Encode(img, time.Duration(i)*frame, frame, false)
		if err != nil {
			t.Fatal(err)
		}
		if p.Keyframe {
			keys = append(keys, i)
		}
	}
	if len(keys) != 3 || keys[0] != 0 || keys[1] != 60 || keys[2] != 120 {
		t.Fatalf("keyframes at %v, want [0 60 120] (-g 60 at 30 fps)", keys)
	}
}

// TestConvertMatchesFFmpeg compares the converter with what ffmpeg's swscale
// produced for the agent (the same scale filter and pix_fmt it used to pass).
// It runs only when RC_FFMPEG_GOLDEN names an ffmpeg executable:
//
//	RC_FFMPEG_GOLDEN=C:\path\to\ffmpeg.exe go test -run FFmpeg ./internal/vpx/
func TestConvertMatchesFFmpeg(t *testing.T) {
	ffmpeg := os.Getenv("RC_FFMPEG_GOLDEN")
	if ffmpeg == "" {
		t.Skip("RC_FFMPEG_GOLDEN is not set")
	}
	const w, h = 96, 64
	rng := rand.New(rand.NewPCG(1, 2))
	img := newBGRA(w, h)
	for y := range h {
		for x := range w {
			switch {
			case y < h/2: // smooth gradients
				img.set(x, y, byte(x*255/(w-1)), byte(y*255/(h/2-1)), byte(255-x*255/(w-1)))
			default: // noise: every pixel different, the worst case for chroma
				img.set(x, y, byte(rng.IntN(256)), byte(rng.IntN(256)), byte(rng.IntN(256)))
			}
		}
	}
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.bgra"), filepath.Join(dir, "out.yuv")
	if err := os.WriteFile(in, img.pix, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "rawvideo", "-pix_fmt", "bgra", "-s", strconv.Itoa(w)+"x"+strconv.Itoa(h), "-i", in,
		"-vf", "scale=w='min(iw,1920)':h=-2", "-pix_fmt", "yuv420p", "-f", "rawvideo", "-y", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, b)
	}
	ref, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref) != w*h*3/2 {
		t.Fatalf("ffmpeg wrote %d bytes, want %d", len(ref), w*h*3/2)
	}
	ours := convert(newTestConverter(t, w, h, 0), img)
	y, u, v := ours.Y, ours.U, ours.V
	refU, refV := ref[w*h:w*h+w*h/4], ref[w*h+w*h/4:]
	// The colour matrix must match: luma everywhere, and chroma over the
	// smooth gradients, to within rounding. Chroma rows near the noise are
	// left out of that: swscale downsamples chroma with a multi-tap filter
	// that reaches across the seam, where this converter (like libyuv, which
	// Chrome's own screen sharing uses) averages each 2x2 block. On the noise
	// itself that filter difference is all there is; it is logged, not
	// asserted.
	const smoothRows = h/4 - 3
	cw := w / 2
	dy := maxDiff(y, ref[:w*h])
	smoothU := maxDiff(u[:smoothRows*cw], refU[:smoothRows*cw])
	smoothV := maxDiff(v[:smoothRows*cw], refV[:smoothRows*cw])
	noiseU := maxDiff(u[h/4*cw:], refU[h/4*cw:])
	noiseV := maxDiff(v[h/4*cw:], refV[h/4*cw:])
	t.Logf("max difference from ffmpeg: luma %d; chroma on gradients U %d V %d, on noise U %d V %d",
		dy, smoothU, smoothV, noiseU, noiseV)
	if dy > 1 {
		t.Errorf("luma differs from ffmpeg by up to %d, want at most 1", dy)
	}
	if smoothU > 1 || smoothV > 1 {
		t.Errorf("chroma on gradients differs from ffmpeg by up to U %d V %d, want at most 1", smoothU, smoothV)
	}
}

func allEqual(b []byte, v byte) bool {
	for _, x := range b {
		if x != v {
			return false
		}
	}
	return true
}

func maxDiff(a, b []byte) int {
	m := 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		m = max(m, d)
	}
	return m
}

func BenchmarkConvert1080p(b *testing.B) {
	benchConvert(b, 1920, 1080)
}

func BenchmarkConvert1440pScaled(b *testing.B) {
	benchConvert(b, 2560, 1440)
}

func BenchmarkConvert4KScaled(b *testing.B) {
	benchConvert(b, 3840, 2160)
}

func benchConvert(b *testing.B, w, h int) {
	conv := newTestConverter(b, w, h, 1920)
	img := gradient(w, h, 0)
	cw, ch := conv.Size()
	out := NewImage(cw, ch)
	b.SetBytes(int64(len(img.pix)))
	for b.Loop() {
		conv.Convert(out, unsafe.Pointer(&img.pix[0]), w*4)
	}
}
