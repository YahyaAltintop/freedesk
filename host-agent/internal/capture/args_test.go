package capture

import (
	"strconv"
	"strings"
	"testing"
)

// argValue returns the value following flag in an ffmpeg argument list.
func argValue(args []string, flag string) (string, bool) {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1], true
		}
	}
	return "", false
}

// The encoder is held to a constant rate with a one-second buffer: floor,
// ceiling and buffer all equal the bitrate. Left to libvpx's defaults (VBR, a
// six-second buffer) it produced 100 KB keyframes every two seconds, which a
// 2 Mbit/s link turns into a periodic stall.
func TestArgsHoldTheEncoderToAConstantBitrate(t *testing.T) {
	args := NewScreenCapture(Options{Binary: "ffmpeg"}).args()

	rate, ok := argValue(args, "-b:v")
	if !ok {
		t.Fatal("no -b:v")
	}
	if _, err := strconv.Atoi(rate); err != nil {
		t.Fatalf("-b:v %q is not a plain number of bits per second", rate)
	}
	for _, flag := range []string{"-minrate", "-maxrate", "-bufsize"} {
		if v, ok := argValue(args, flag); !ok || v != rate {
			t.Fatalf("%s: got %q, want %q (CBR with a one-second buffer)", flag, v, rate)
		}
	}
	for flag, want := range map[string]string{
		"-lag-in-frames": "0", // a frame leaves the moment it is encoded
		"-flush_packets": "1", // and reaches the pipe the moment it is muxed
		"-deadline":      "realtime",
	} {
		if v, ok := argValue(args, flag); !ok || v != want {
			t.Fatalf("%s: got %q, want %q", flag, v, want)
		}
	}
	if _, ok := argValue(args, "-max-intra-rate"); !ok {
		t.Fatal("keyframes are not capped (-max-intra-rate)")
	}
}

// The frame is never wider than the cap, and the cap is the only thing about
// the picture the operator can configure.
func TestArgsCapTheWidthAndKeepTheAspect(t *testing.T) {
	vf, ok := argValue(NewScreenCapture(Options{Binary: "ffmpeg", MaxWidth: 1280}).args(), "-vf")
	if !ok || !strings.Contains(vf, "min(iw,1280)") || !strings.Contains(vf, "h=-2") {
		t.Fatalf("-vf %q: expected a scale to at most 1280 wide with the height following", vf)
	}

	vf, _ = argValue(NewScreenCapture(Options{Binary: "ffmpeg"}).args(), "-vf")
	if !strings.Contains(vf, "min(iw,"+strconv.Itoa(DefaultMaxWidth)+")") {
		t.Fatalf("-vf %q: an unset cap should be the default %d", vf, DefaultMaxWidth)
	}
	vf, _ = argValue(NewScreenCapture(Options{Binary: "ffmpeg", MaxWidth: -5}).args(), "-vf")
	if !strings.Contains(vf, "min(iw,"+strconv.Itoa(DefaultMaxWidth)+")") {
		t.Fatalf("-vf %q: a nonsense cap should fall back to the default", vf)
	}
}
