//go:build cgo

package vpx

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/libvpx/include -O3
// -static links libgcc and the winpthreads libvpx needs into the exe instead
// of leaving them as DLLs (libgcc_s_seh-1.dll, libwinpthread-1.dll) a user's
// machine does not have. cmd/importcheck verifies the result.
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/libvpx/lib -lvpx -lm -lpthread -static
#include "fdvpx.h"
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"
)

const available = true

// Converter turns BGRA frames of one desktop size into Images of the encoded
// size (OutputSize), scaling them down first when the desktop is wider than
// the cap. Calls must not overlap, but a Converter and an Encoder may work at
// the same time on different images.
type Converter struct {
	scaler     *C.fd_scaler // nil when no scaling is needed
	scaled     []byte       // the scaler's BGRA output
	srcW, srcH int
	w, h       int
}

// NewConverter prepares the conversion of srcW x srcH BGRA frames, with the
// encoded width capped at maxWidth (0: no cap).
func NewConverter(srcW, srcH, maxWidth int) (*Converter, error) {
	w, h := OutputSize(srcW, srcH, maxWidth)
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("vpx: nothing to encode in a %dx%d frame", srcW, srcH)
	}
	c := &Converter{srcW: srcW, srcH: srcH, w: w, h: h}
	// A source that is the encoded size, give or take an odd last row or
	// column, is cropped; anything larger is scaled.
	if srcW&^1 != w || srcH&^1 != h {
		c.scaler = C.fd_scaler_new(C.int(srcW), C.int(srcH), C.int(w), C.int(h))
		if c.scaler == nil {
			return nil, fmt.Errorf("vpx: out of memory for a %dx%d scaler", srcW, srcH)
		}
		c.scaled = make([]byte, w*h*4)
	}
	return c, nil
}

// Size is the size of the Images the converter produces.
func (c *Converter) Size() (w, h int) { return c.w, c.h }

// SourceSize is the size of the BGRA frames it takes.
func (c *Converter) SourceSize() (w, h int) { return c.srcW, c.srcH }

// Convert converts one top-down BGRA frame of SourceSize, stride bytes per
// row, into dst, which must be of Size. pixels is read during the call only.
func (c *Converter) Convert(dst *Image, pixels unsafe.Pointer, stride int) {
	if dst.Width != c.w || dst.Height != c.h {
		panic(fmt.Sprintf("vpx: converting into a %dx%d image, want %dx%d", dst.Width, dst.Height, c.w, c.h))
	}
	src := (*C.uint8_t)(pixels)
	if c.scaler != nil {
		out := (*C.uint8_t)(unsafe.Pointer(&c.scaled[0]))
		C.fd_scale_bgra(c.scaler, src, C.int(stride), out)
		src, stride = out, c.w*4
	}
	C.fd_bgra_to_i420(src, C.int(stride), C.int(c.w), C.int(c.h),
		(*C.uint8_t)(unsafe.Pointer(&dst.Y[0])), C.int(dst.Width),
		(*C.uint8_t)(unsafe.Pointer(&dst.U[0])), C.int(dst.Width/2),
		(*C.uint8_t)(unsafe.Pointer(&dst.V[0])), C.int(dst.Width/2))
}

// Close releases the converter. It is safe to call more than once.
func (c *Converter) Close() {
	if c.scaler != nil {
		C.fd_scaler_free(c.scaler)
		c.scaler = nil
	}
}

// Encoder is one libvpx VP8 encoder for images of one size. Calls must not
// overlap.
type Encoder struct {
	c    *C.fd_vpx
	w, h int
}

// NewEncoder opens an encoder for s.Width x s.Height images.
func NewEncoder(s Settings) (*Encoder, error) {
	rc := rateControlFor(s)
	cs := C.fd_vpx_settings{
		width:           C.int(s.Width),
		height:          C.int(s.Height),
		target_kbps:     C.int(rc.targetKbps),
		buf_ms:          C.int(rc.bufMs),
		buf_initial_ms:  C.int(rc.bufInitialMs),
		buf_optimal_ms:  C.int(rc.bufOptimalMs),
		undershoot_pct:  C.int(rc.undershootPct),
		overshoot_pct:   C.int(rc.overshootPct),
		max_intra_pct:   C.int(rc.maxIntraPct),
		kf_max_dist:     C.int(rc.kfMaxDist),
		cpu_used:        C.int(rc.cpuUsed),
		threads:         C.int(rc.threads),
		error_resilient: C.int(rc.errorResilient),
		timebase_den:    C.int(timebaseDen),
	}
	var errbuf [256]C.char
	c := C.fd_vpx_open(&cs, &errbuf[0], C.int(len(errbuf)))
	if c == nil {
		return nil, fmt.Errorf("vpx: %s", C.GoString(&errbuf[0]))
	}
	return &Encoder{c: c, w: s.Width, h: s.Height}, nil
}

// Size is the image size the encoder takes.
func (e *Encoder) Size() (w, h int) { return e.w, e.h }

// Encode encodes img. pts is the capture time since the encoder's first
// frame, duration the time the frame stands for; keyframe forces a keyframe.
// A Packet with no Data means libvpx produced no frame for this input.
func (e *Encoder) Encode(img *Image, pts, duration time.Duration, keyframe bool) (Packet, error) {
	if img.Width != e.w || img.Height != e.h {
		return Packet{}, fmt.Errorf("vpx: a %dx%d image for a %dx%d encoder", img.Width, img.Height, e.w, e.h)
	}
	var (
		data   *C.uint8_t
		size   C.size_t
		key    C.int
		force  C.int
		errbuf [256]C.char
	)
	if keyframe {
		force = 1
	}
	dur := max(ticks(duration), 1)
	if C.fd_vpx_encode(e.c,
		(*C.uint8_t)(unsafe.Pointer(&img.Y[0])), (*C.uint8_t)(unsafe.Pointer(&img.U[0])), (*C.uint8_t)(unsafe.Pointer(&img.V[0])),
		C.int(img.Width), C.int(img.Width/2), C.int64_t(ticks(pts)), C.ulong(dur), force,
		&data, &size, &key, &errbuf[0], C.int(len(errbuf))) != 0 {
		return Packet{}, fmt.Errorf("vpx: %s", C.GoString(&errbuf[0]))
	}
	if size == 0 {
		return Packet{}, nil
	}
	return Packet{Data: C.GoBytes(unsafe.Pointer(data), C.int(size)), Keyframe: key != 0}, nil
}

// Close releases the encoder. It is safe to call more than once.
func (e *Encoder) Close() {
	if e.c != nil {
		C.fd_vpx_close(e.c)
		e.c = nil
	}
}
