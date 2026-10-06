package capture

import (
	"time"
	"unsafe"
)

// The mouse pointer is not part of the captured desktop image (neither
// desktop duplication nor BitBlt includes it), so it is drawn into the frame:
// GetCursorInfo for where and which, DrawIconEx to paint it, which handles
// every kind of cursor (colour, alpha, the monochrome ones that invert what
// is under them, and the frames of an animated one).

// Mirrors of the C structs; the field order is the layout.
type cursorInfo struct {
	size   uint32
	flags  uint32
	handle uintptr
	x, y   int32
}

type iconInfo struct {
	icon     int32
	hotspotX uint32
	hotspotY uint32
	mask     uintptr
	color    uintptr
}

type bitmapHeader struct {
	kind       int32
	width      int32
	height     int32
	widthBytes int32
	planes     uint16
	bitsPixel  uint16
	bits       uintptr
}

// cursorPainter keeps the pointer drawn into a frame. When only the pointer
// moves (or an animated one reaches its next frame), it puts back the pixels
// it covered and draws it anew, so the desktop image need not be copied
// again.
type cursorPainter struct {
	handle     uintptr // the cursor whose measurements follow
	hotX, hotY int
	cw, ch     int
	anim       animatedCursor

	frame *dib         // the bitmap the rest of this describes
	last  pointerState // what the frame shows, once valid
	valid bool
	drawn bool // the pointer is in the frame (not hidden, not off-monitor)

	rx, ry, rw, rh int    // the rectangle it covers, clipped to the frame
	under          []byte // what that rectangle held before
}

// pointerState is what decides how the pointer looks in the frame.
type pointerState struct {
	visible bool
	handle  uintptr
	step    int // the frame of an animated cursor
	x, y    int // frame coordinates
}

// paint brings the pointer in frame up to date as of now. fresh says the
// frame was just overwritten with a new desktop image (so the old pointer is
// already gone). It reports whether it changed the frame: a pointer that is
// hidden or on another monitor leaves it as it is, whatever the pointer
// does.
func (p *cursorPainter) paint(frame *dib, originX, originY int, fresh bool, now time.Time) bool {
	if frame != p.frame {
		// Another bitmap (the source replaced its frame after a mode
		// change): what was drawn, and the rectangle saved from under it,
		// belong to the old one and must not be put into this one.
		p.frame, p.valid, p.drawn = frame, false, false
	}
	ci := cursorInfo{size: uint32(unsafe.Sizeof(cursorInfo{}))}
	ok, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci)))
	var cur pointerState
	if ok != 0 && ci.flags&cursorShowing != 0 && ci.handle != 0 {
		cur = pointerState{visible: true, handle: ci.handle, step: p.anim.step(ci.handle, now),
			x: int(ci.x) - originX, y: int(ci.y) - originY}
	}
	if !fresh && p.valid && cur == p.last {
		return false
	}
	changed := false
	if fresh {
		p.drawn = false
	} else if p.drawn {
		p.restore(frame)
		changed = true
	}
	if cur.visible && p.draw(frame, cur.handle, cur.step, cur.x, cur.y) {
		changed = true
	}
	p.last, p.valid = cur, true
	return changed
}

// jiffy is the unit an animated cursor's frame rate comes in: 1/60 s.
const jiffy = time.Second / 60

// defaultAniRate is the frame time of an animated cursor that states none,
// the .ani format's default of ten jiffies.
const defaultAniRate = 10 * jiffy

// animatedCursor steps through the frames of an animated cursor (the busy
// spinner) on the clock, as the screen does. How many frames a cursor has
// and how long each lasts comes from GetCursorFrameInfo, which Windows does
// not document but has shipped in user32 since Vista; without it, every
// cursor shows its first frame.
type animatedCursor struct {
	handle uintptr       // the cursor whose frames follow
	steps  int           // 1 for a still cursor
	rate   time.Duration // one frame's time
	start  time.Time     // when the cursor appeared, showing its frame 0
}

// step returns the frame of handle due at now; 0 for a still cursor.
func (a *animatedCursor) step(handle uintptr, now time.Time) int {
	if handle != a.handle {
		a.handle, a.steps, a.rate, a.start = handle, 1, 0, now
		if procGetCursorFrameInfo.Find() == nil {
			var rate, steps uint32
			h, _, _ := procGetCursorFrameInfo.Call(handle, 0, 0, uintptr(unsafe.Pointer(&rate)), uintptr(unsafe.Pointer(&steps)))
			if h != 0 && steps > 1 {
				a.steps = int(steps)
				a.rate = time.Duration(rate) * jiffy
				if a.rate <= 0 {
					a.rate = defaultAniRate
				}
			}
		}
	}
	if a.steps <= 1 {
		return 0
	}
	return int(now.Sub(a.start)/a.rate) % a.steps
}

// measure caches the hotspot and size of a cursor. GetIconInfo hands back
// copies of the cursor's bitmaps, which must be deleted.
func (p *cursorPainter) measure(handle uintptr) {
	p.handle, p.hotX, p.hotY, p.cw, p.ch = handle, 0, 0, 32, 32
	var ii iconInfo
	if ok, _, _ := procGetIconInfo.Call(handle, uintptr(unsafe.Pointer(&ii))); ok == 0 {
		return
	}
	defer func() {
		if ii.mask != 0 {
			_, _, _ = procDeleteObject.Call(ii.mask)
		}
		if ii.color != 0 {
			_, _, _ = procDeleteObject.Call(ii.color)
		}
	}()
	p.hotX, p.hotY = int(ii.hotspotX), int(ii.hotspotY)
	var bm bitmapHeader
	switch {
	case ii.color != 0:
		if n, _, _ := procGetObject.Call(ii.color, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); n != 0 {
			p.cw, p.ch = int(bm.width), int(bm.height)
		}
	case ii.mask != 0:
		// A monochrome cursor's mask holds the AND and XOR halves stacked.
		if n, _, _ := procGetObject.Call(ii.mask, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); n != 0 {
			p.cw, p.ch = int(bm.width), int(bm.height)/2
		}
	}
}

// draw paints frame step of the cursor with its hotspot at (x, y), and
// reports whether it did: a pointer on another monitor is not in the frame.
// The frames of an animated cursor share its size and hotspot, so the cursor
// is measured once.
func (p *cursorPainter) draw(frame *dib, handle uintptr, step, x, y int) bool {
	if handle != p.handle {
		p.measure(handle)
	}
	left, top := x-p.hotX, y-p.hotY
	rx, ry := max(left, 0), max(top, 0)
	rw, rh := min(left+p.cw, frame.w)-rx, min(top+p.ch, frame.h)-ry
	if rw <= 0 || rh <= 0 {
		return false
	}
	p.rx, p.ry, p.rw, p.rh = rx, ry, rw, rh
	p.save(frame)
	_, _, _ = procDrawIconEx.Call(frame.dc, uintptr(left), uintptr(top), handle, 0, 0, uintptr(step), 0, diNormal)
	p.drawn = true
	return true
}

func (p *cursorPainter) save(frame *dib) {
	gdiFlush() // the frame's pixels must be current before reading them
	row := p.rw * 4
	if cap(p.under) < row*p.rh {
		p.under = make([]byte, row*p.rh)
	}
	p.under = p.under[:row*p.rh]
	pix, stride := frame.pixels(), frame.stride()
	for i := range p.rh {
		off := (p.ry+i)*stride + p.rx*4
		copy(p.under[i*row:(i+1)*row], pix[off:off+row])
	}
}

func (p *cursorPainter) restore(frame *dib) {
	gdiFlush()
	row := p.rw * 4
	pix, stride := frame.pixels(), frame.stride()
	for i := range p.rh {
		off := (p.ry+i)*stride + p.rx*4
		copy(pix[off:off+row], p.under[i*row:(i+1)*row])
	}
	p.drawn = false
}
