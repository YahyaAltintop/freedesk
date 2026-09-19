package transfer

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
	"time"
)

// Channel is the part of a WebRTC DataChannel this package uses.
// *pion.DataChannel satisfies it; tests use a recorder.
type Channel interface {
	Send([]byte) error
	SendText(string) error
	BufferedAmount() uint64
	SetBufferedAmountLowThreshold(uint64)
	OnBufferedAmountLow(func())
}

// Approver asks the operator about one batch of incoming files. It must return
// false when ctx is cancelled — the viewer withdrawing, or the session ending,
// has to close the question rather than leave it on screen.
type Approver interface {
	AskFiles(ctx context.Context, files []FileOffer, folder string) bool
}

// FileOffer is what the operator is shown for one file. The name is the
// SANITISED name: they have to be able to approve what will actually be
// written, not what was claimed.
type FileOffer struct {
	Name string
	Size int64
}

// progressEvery bounds how often the receiver reports back. Often enough to
// look live, rarely enough to keep the control stream thin — and it doubles as
// the sender's sign that the far end is still writing.
const progressEvery = 250 * time.Millisecond

// receiving is the state of the batch currently being written to disk.
type receiving struct {
	id    string
	names []string // sanitised, in offer order
	sizes []int64  // as declared

	// clip marks a batch the viewer pasted: once it is all saved the host puts
	// it on its own clipboard so the operator can paste it in Explorer.
	clip  bool
	saved []string // final paths, in order, for that clipboard hand-off

	index    int // which file of the batch
	file     *os.File
	path     string // final path, without the .part suffix
	written  int64  // bytes written to the current file
	reported time.Time
}

// Session handles the "file" channel for one WebRTC session.
//
// Only one batch is accepted at a time. A second offer arriving while one is
// being decided or written is refused as busy — the operator can only answer
// one question at a time, and the host cannot rely on the viewer to know that.
type Session struct {
	dest     *Dest
	approver Approver
	picker   Picker
	logf     func(string, ...any)

	ctx    context.Context
	cancel context.CancelFunc

	// The channel has its own guard so a frame can be sent from anywhere
	// without holding the state lock — every send under s.mu would otherwise
	// need an unlock/relock dance around it, which is exactly how a deadlock
	// gets introduced later.
	chMu sync.Mutex
	ch   Channel

	// low is signalled when the send queue drains past the low-water mark.
	low chan struct{}

	// onPasted is called with the saved paths once a pasted batch completes.
	onPasted func([]string)

	mu      sync.Mutex
	rx      *receiving
	tx      *sending
	pending bool  // an offer is in front of the operator right now
	failed  *Msg  // set when opening a file failed under the lock
	files   int   // accepted this session, against MaxSessionFiles
	bytes   int64 // written this session, against MaxSessionBytes
	closed  bool
}

// NewSession builds the transfer handler for one session. Nothing touches the
// disk until a batch is accepted.
func NewSession(ctx context.Context, approver Approver, picker Picker, logf func(string, ...any)) *Session {
	ctx, cancel := context.WithCancel(ctx)
	return &Session{
		dest:     NewDest(),
		approver: approver,
		picker:   picker,
		logf:     logf,
		low:      make(chan struct{}, 1),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Root is the folder accepted files are written to.
func (s *Session) Root() string { return s.dest.Root() }

// OnPasted registers what to do with a batch the viewer pasted, once every
// file of it is safely on disk. Called with the final paths, in offer order.
func (s *Session) OnPasted(fn func(paths []string)) { s.onPasted = fn }

// Attach binds the channel this session will talk over.
func (s *Session) Attach(ch Channel) {
	s.chMu.Lock()
	s.ch = ch
	s.chMu.Unlock()

	// Set once, because pion replaces the handler rather than adding to it.
	ch.SetBufferedAmountLowThreshold(lowWater)
	ch.OnBufferedAmountLow(func() {
		// Never block pion's goroutine: one pending wake-up is enough, and the
		// pump re-reads the queue anyway.
		select {
		case s.low <- struct{}{}:
		default:
		}
	})
}

// Handle takes one frame from the channel. isText separates a control frame
// from a payload chunk; it is the only thing that distinguishes them.
func (s *Session) Handle(data []byte, isText bool) {
	if isText {
		s.handleControl(data)
		return
	}
	s.handleChunk(data)
}

func (s *Session) handleControl(data []byte) {
	m, err := ParseMsg(data)
	if err != nil {
		s.logf("[transfer] ignoring malformed control frame: %v", err)
		return
	}
	switch m.T {
	case TypeOffer:
		if m.Dir == DirUp {
			go s.offered(m)
		}
	case TypeRequest:
		go s.requested(m.ID)
	case TypeAccept:
		// The viewer accepting means it is ready for one file of what this
		// host offered; an accept for anything else is ignored.
		go s.accepted(m.ID, m.Index)
	case TypeCancel, TypeError:
		s.abort(m.ID, "the viewer cancelled")
		s.cancelSend(m.ID)
	default:
		// Unknown types are ignored on purpose (docs/PROTOCOL.md §2.2).
	}
}

// offered decides a batch: it validates every name first, asks the operator,
// and only then opens anything. It runs on its own goroutine because the
// prompt blocks for as long as the operator takes.
func (s *Session) offered(m Msg) {
	if !s.claimPending(m.ID) {
		return
	}
	defer s.releasePending()

	// Sanitise before asking. The operator approves the names that will exist,
	// and a batch with an unusable name in it is refused as a batch — silently
	// dropping one file from something someone agreed to send is worse than
	// saying no.
	names := make([]string, len(m.Files))
	offers := make([]FileOffer, len(m.Files))
	for i, f := range m.Files {
		safe, err := SafeName(f.Name)
		if err != nil {
			s.logf("[transfer] refused a batch: unusable file name %q", f.Name)
			s.send(Reject(m.ID, ReasonBadName))
			return
		}
		names[i] = safe
		offers[i] = FileOffer{Name: safe, Size: f.Size}
	}

	if !s.withinSessionLimits(len(m.Files), m.TotalSize()) {
		s.send(Reject(m.ID, ReasonTooLarge))
		return
	}

	if !s.approver.AskFiles(s.ctx, offers, s.dest.Root()) {
		s.logf("[transfer] batch %s declined", m.ID)
		s.send(Reject(m.ID, ReasonDenied))
		return
	}

	sizes := make([]int64, len(m.Files))
	for i, f := range m.Files {
		sizes[i] = f.Size
	}
	if !s.beginBatch(m.ID, names, sizes, m.Clip) {
		s.send(s.takeFailure(Reject(m.ID, ReasonBusy)))
		return
	}
	s.logf("[transfer] accepting %d file(s) into %s", len(names), s.dest.Root())
	s.send(Msg{T: TypeAccept, ID: m.ID})
}

// claimPending enforces one question at a time.
func (s *Session) claimPending(id string) bool {
	s.mu.Lock()
	busy := s.closed || s.pending || s.rx != nil
	if !busy {
		s.pending = true
	}
	s.mu.Unlock()

	if busy {
		s.send(Reject(id, ReasonBusy))
		return false
	}
	return true
}

func (s *Session) releasePending() {
	s.mu.Lock()
	s.pending = false
	s.mu.Unlock()
}

// takeFailure returns the frame a failed open left behind, or fallback if the
// batch was refused for some other reason. It exists so the frame is sent from
// outside the lock that produced it.
func (s *Session) takeFailure(fallback Msg) Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed != nil {
		m := *s.failed
		s.failed = nil
		return m
	}
	return fallback
}

func (s *Session) withinSessionLimits(count int, size int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.files+count <= MaxSessionFiles && s.bytes+size <= MaxSessionBytes
}

// beginBatch opens the first file of an accepted batch.
func (s *Session) beginBatch(id string, names []string, sizes []int64, clip bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.rx != nil {
		return false
	}
	s.rx = &receiving{id: id, names: names, sizes: sizes, clip: clip}
	if fail := s.openLocked(); fail != nil {
		s.failed = fail
		return false
	}
	return true
}

// openLocked opens the current file of the batch. Called with s.mu held; on
// failure it clears the batch and returns the frame the caller must send once
// it has let go of the lock.
func (s *Session) openLocked() (fail *Msg) {
	f, path, err := s.dest.Create(s.rx.names[s.rx.index])
	if err != nil {
		s.logf("[transfer] could not create %q: %v", s.rx.names[s.rx.index], err)
		reason := ReasonIO
		if errors.Is(err, ErrBadName) {
			reason = ReasonBadName
		}
		m := Fail(s.rx.id, reason)
		s.rx = nil
		return &m
	}
	s.rx.file = f
	s.rx.path = path
	s.rx.written = 0
	return nil
}

// handleChunk writes one payload frame. Chunks belong to the file currently
// open: the channel is ordered and reliable, so there is no ambiguity about
// which one that is, and no per-chunk header is needed.
func (s *Session) handleChunk(data []byte) {
	s.mu.Lock()
	rx := s.rx
	if rx == nil || rx.file == nil {
		// Payload with nothing accepted to write it to. Dropped, not written.
		s.mu.Unlock()
		return
	}

	// The declared size is the sender's claim. What is enforced is the count of
	// bytes actually written, so a peer that announces 1 KB and streams 40 GB
	// stops at 1 KB rather than at 40 GB.
	if rx.written+int64(len(data)) > rx.sizes[rx.index] {
		id := rx.id
		s.abortLocked("more bytes arrived than were declared")
		s.mu.Unlock()
		s.send(Fail(id, ReasonTooLarge))
		return
	}
	if _, err := rx.file.Write(data); err != nil {
		id := rx.id
		s.logf("[transfer] write failed: %v", err)
		s.abortLocked("the disk refused the write")
		s.mu.Unlock()
		s.send(Fail(id, ReasonIO))
		return
	}
	rx.written += int64(len(data))

	var progress *Msg
	if time.Since(rx.reported) >= progressEvery {
		rx.reported = time.Now()
		progress = &Msg{T: TypeProgress, ID: rx.id, Index: rx.index, Sent: rx.written}
	}
	done := rx.written == rx.sizes[rx.index]
	s.mu.Unlock()

	if progress != nil {
		s.send(*progress)
	}
	if done {
		s.finishFile()
	}
}

// finishFile renames the completed file and moves on to the next of the batch.
func (s *Session) finishFile() {
	s.mu.Lock()
	rx := s.rx
	if rx == nil || rx.file == nil {
		s.mu.Unlock()
		return
	}
	id, index, name, size := rx.id, rx.index, rx.names[rx.index], rx.written
	if err := Finish(rx.file, rx.path); err != nil {
		s.logf("[transfer] could not complete %q: %v", name, err)
		s.abortLocked("the file could not be completed")
		s.mu.Unlock()
		s.send(Fail(id, ReasonIO))
		return
	}
	rx.file = nil
	rx.saved = append(rx.saved, rx.path)
	s.files++
	s.bytes += size

	var (
		fail   *Msg
		pasted []string
	)
	if rx.index+1 >= len(rx.names) {
		if rx.clip {
			pasted = rx.saved
		}
		s.rx = nil
	} else {
		rx.index++
		fail = s.openLocked()
	}
	s.mu.Unlock()

	s.logf("[transfer] saved %q (%d bytes)", name, size)
	s.send(Msg{T: TypeDone, ID: id, Index: index})
	if fail != nil {
		s.send(*fail)
	}
	if len(pasted) > 0 && s.onPasted != nil {
		s.onPasted(pasted)
	}
}

// abort throws away whatever is being written for the given batch. An empty id
// matches whatever is in flight (used by teardown).
func (s *Session) abort(id, why string) {
	s.mu.Lock()
	if s.rx != nil && (id == "" || s.rx.id == id) {
		s.abortLocked(why)
	}
	s.mu.Unlock()
}

// abortLocked discards the partial file. Called with s.mu held.
func (s *Session) abortLocked(why string) {
	if s.rx == nil {
		return
	}
	s.logf("[transfer] %s — discarding %q", why, s.rx.names[s.rx.index])
	Abandon(s.rx.file, s.rx.path)
	s.rx = nil
}

// Close ends the session's transfers. Nothing may be left behind on this
// machine once the viewer is gone: no half-written file, and no question still
// on the operator's screen. Safe to call more than once — the channel closing
// and the session ending both lead here.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.abortLocked("the session ended")
	s.abortSendLocked()
	s.mu.Unlock()
	s.cancel()
}

// send writes a control frame, if the channel is still there.
func (s *Session) send(m Msg) {
	s.chMu.Lock()
	ch := s.ch
	s.chMu.Unlock()
	if ch == nil {
		return
	}
	if err := ch.SendText(m.Encode()); err != nil {
		log.Printf("[transfer] could not send %s: %v", m.T, err)
	}
}
