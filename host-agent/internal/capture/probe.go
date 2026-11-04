package capture

import "time"

// probeInputDesktop is inputDesktopReadable, replaceable by a test.
var probeInputDesktop = inputDesktopReadable

// desktopProbeInterval spaces a source's inputDesktopReadable calls. Asked on
// every tick, a still screen would cost a kernel call thirty times a second
// for nothing; asked this often, the secure desktop is still noticed within
// a quarter of a second, before anyone misses a frame.
const desktopProbeInterval = 250 * time.Millisecond

// desktopProbe answers inputDesktopReadable from an answer at most
// desktopProbeInterval old. The zero value asks at once.
type desktopProbe struct {
	next     time.Time
	readable bool
}

// check reports whether the input desktop can be read as of now, asking the
// system only when the previous answer is older than desktopProbeInterval.
func (p *desktopProbe) check(now time.Time) bool {
	if now.Before(p.next) {
		return p.readable
	}
	p.next = now.Add(desktopProbeInterval)
	p.readable = probeInputDesktop()
	return p.readable
}
