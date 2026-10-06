// Package capture grabs the primary monitor and encodes it to VP8 inside the
// agent, handing the encoded frames over a channel. The screen comes from
// desktop duplication (DXGI) or, where that is not available, a GDI copy; the
// mouse pointer is drawn in; libvpx, linked into the exe (internal/vpx),
// encodes. No other program is started and nothing is loaded from outside
// System32.
package capture

import (
	"sync/atomic"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/vpx"
)

const (
	defaultFramerate = vpx.DefaultFramerate

	// DefaultMaxWidth caps the encoded frame's width. A 1440p or 4K monitor
	// would otherwise be squeezed into the same bitrate at two to four times
	// the pixels, and come out as mush; 1080p stays legible, and anything
	// wider is scaled down to it, aspect kept.
	DefaultMaxWidth = 1920

	// maxElapsed caps the gap attributed to a single frame, so a long time
	// on the secure desktop never jumps the RTP clock by minutes.
	maxElapsed = 5 * time.Second

	// idleInterval is how often the last picture is encoded again while
	// nothing on the screen changes. Encoding it on every tick would cost
	// nearly as much as a moving picture (VP8 still searches every block);
	// encoding it now and then keeps the periodic keyframes coming (one every
	// sixty frames: two seconds of motion, up to twelve on a still screen),
	// so a viewer that loses one eventually recovers even with no report. The
	// prompt path is the keyframe a lost-picture report asks for directly
	// (see KeyframeRequest).
	idleInterval = 200 * time.Millisecond

	// maxTicksPerFrame caps how many ticks one new picture is encoded as
	// standing for (frameDuration).
	maxTicksPerFrame = 4

	// keyframeMinInterval is the least time between two keyframes forced by a
	// viewer's requests. A keyframe costs about fifteen times a delta frame,
	// so a burst of requests on a lossy link — exactly when the budget is
	// tightest — must not turn into a storm of them; a request that arrives
	// within this of the last forced keyframe waits for it to pass rather
	// than being dropped. libvpx's own two-second keyframes are unaffected.
	keyframeMinInterval = 500 * time.Millisecond
)

// Frame is one encoded VP8 frame. Elapsed is the capture-time gap since the
// previous frame (zero for the first frame), so a late or skipped tick shows
// up as a wider gap instead of being hidden behind a constant frame rate. The
// writer advances the media clock by Elapsed BEFORE sending the frame, which
// keeps RTP timestamps equal to real capture times.
type Frame struct {
	Data    []byte
	Elapsed time.Duration
}

// Options selects how the desktop is encoded.
type Options struct {
	// MaxWidth caps the encoded width in pixels; 0 means DefaultMaxWidth.
	MaxWidth int
	// Keyframe, when set, lets the session ask the encoder for a keyframe
	// (the viewer sent a lost-picture report). Nil means the stream relies on
	// its periodic keyframes alone.
	Keyframe *KeyframeRequest
}

// KeyframeRequest carries a viewer's request for the next frame to be a
// keyframe — raised from the RTCP reader (a Picture Loss Indication) and
// consumed by the capture — so a viewer that has lost too much to repair by
// retransmission recovers without waiting for the next periodic keyframe.
// Its methods are safe to call from any goroutine.
type KeyframeRequest struct {
	want atomic.Bool
}

// NewKeyframeRequest returns a request with nothing pending.
func NewKeyframeRequest() *KeyframeRequest { return &KeyframeRequest{} }

// Request records that a keyframe is wanted. Several before the capture takes
// it coalesce into one.
func (k *KeyframeRequest) Request() { k.want.Store(true) }

// take reports whether a keyframe was requested, clearing the request.
func (k *KeyframeRequest) take() bool { return k.want.Swap(false) }

// keyframeDue reports whether a requested keyframe may be forced now, sinceLast
// being the time since the previous forced one. A request that arrives within
// keyframeMinInterval is left pending (take is not called) so it fires once the
// interval has passed, rather than being lost.
func keyframeDue(k *KeyframeRequest, sinceLast time.Duration) bool {
	return k != nil && sinceLast >= keyframeMinInterval && k.take()
}
