package capture

import (
	"bytes"
	"encoding/binary"
	"unsafe"
)

// newTestPicture returns a black w x h picture in Go memory, for a test
// source to hand the pipeline.
func newTestPicture(w, h int) *picture {
	pix := make([]byte, w*h*4)
	return &picture{w: w, h: h, stride: w * 4, bits: unsafe.Pointer(&pix[0])}
}

// vp8Size reads the size out of a VP8 keyframe header; ok is false for an
// interframe.
func vp8Size(data []byte) (w, h int, ok bool) {
	if len(data) < 10 || data[0]&1 == 1 || !bytes.Equal(data[3:6], []byte{0x9d, 0x01, 0x2a}) {
		return 0, 0, false
	}
	return int(binary.LittleEndian.Uint16(data[6:8]) & 0x3fff), int(binary.LittleEndian.Uint16(data[8:10]) & 0x3fff), true
}
