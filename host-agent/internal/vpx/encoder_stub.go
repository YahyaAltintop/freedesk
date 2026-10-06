//go:build !windows || !cgo

package vpx

import (
	"time"
	"unsafe"
)

const available = false

// Converter is unavailable in this build; NewConverter always fails.
type Converter struct{}

// NewConverter reports ErrUnavailable: this build has no libvpx.
func NewConverter(int, int, int) (*Converter, error) { return nil, ErrUnavailable }

// Size is never called on a build without an encoder.
func (c *Converter) Size() (w, h int) { return 0, 0 }

// SourceSize is never called on a build without an encoder.
func (c *Converter) SourceSize() (w, h int) { return 0, 0 }

// Convert is never called on a build without an encoder.
func (c *Converter) Convert(*Image, unsafe.Pointer, int) {}

// Close does nothing.
func (c *Converter) Close() {}

// Encoder is unavailable in this build; NewEncoder always fails.
type Encoder struct{}

// NewEncoder reports ErrUnavailable: this build has no libvpx.
func NewEncoder(Settings) (*Encoder, error) { return nil, ErrUnavailable }

// Size is never called on a build without an encoder.
func (e *Encoder) Size() (w, h int) { return 0, 0 }

// Encode reports ErrUnavailable.
func (e *Encoder) Encode(*Image, time.Duration, time.Duration, bool) (Packet, error) {
	return Packet{}, ErrUnavailable
}

// Close does nothing.
func (e *Encoder) Close() {}
