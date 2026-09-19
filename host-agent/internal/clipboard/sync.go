package clipboard

import (
	"encoding/json"
	"log"
	"sync"
	"unicode/utf8"
)

// Type is the clipboard verb on the wire. It travels on the file channel
// rather than the input channel: clipboard text can be a couple of hundred
// kilobytes, and the input channel is ordered and reliable, so a large paste
// would queue ahead of every mouse move behind it.
const Type = "cb"

// Msg is one clipboard message, the same shape in both directions.
type Msg struct {
	T string `json:"t"`
	// Text is the clipboard's contents. Never logged.
	Text string `json:"text"`
	// Trunc is set when the text was cut to the size cap, so the viewer can say
	// so rather than quietly handing over a partial value.
	Trunc bool `json:"trunc,omitempty"`
}

// Encode renders a clipboard message.
func (m Msg) Encode() string {
	m.T = Type
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// Sync keeps one session's clipboard in step with the viewer's.
type Sync struct {
	worker  *worker
	maxText int
	send    func(string)

	mu sync.Mutex
	// ownSeq is the sequence number produced by our own last write. The poller
	// skips it, which is what stops a value bouncing between the two machines.
	// A sequence number is better than comparing text: copying the same thing
	// twice is a real thing people do and should still sync.
	ownSeq uint32
	// lastSeq is the last clipboard state already dealt with.
	lastSeq uint32
	// lastApplied is what the viewer last sent us, kept as a second guard in
	// case another owner bumps the sequence in the same tick.
	lastApplied string
	stopped     bool
}

// NewSync starts the clipboard worker for one session. mode is RC_CLIPBOARD:
// anything but ModeText shares nothing, and the caller should not advertise the
// capability in that case.
func NewSync(mode string, send func(string)) (*Sync, error) {
	if mode != ModeText {
		return nil, ErrUnavailable
	}
	s := &Sync{maxText: MaxTextBytes, send: send}
	w, err := startWorker(s.poll)
	if err != nil {
		return nil, err
	}
	s.worker = w

	// Take the current state as already seen: connecting should not push
	// whatever happened to be on the operator's clipboard beforehand.
	s.worker.do(func(b Board) {
		seq := b.Sequence()
		s.mu.Lock()
		s.lastSeq = seq
		s.mu.Unlock()
	})
	return s, nil
}

// poll runs on the clipboard thread. It sends the host's clipboard to the
// viewer when it changes.
func (s *Sync) poll(b Board) {
	seq := b.Sequence()

	s.mu.Lock()
	unchanged := seq == s.lastSeq
	ours := seq == s.ownSeq
	if !unchanged {
		s.lastSeq = seq
	}
	s.mu.Unlock()

	if unchanged || ours {
		return
	}
	// Password managers mark what they copy as not for history or sync.
	// Honouring that means a password copied from one never reaches the viewer
	// — the cheapest real privacy win available here.
	if b.Excluded() {
		log.Printf("[clipboard] skipped a change marked private by the application that made it")
		return
	}

	text, ok, err := b.ReadText()
	if err != nil || !ok || text == "" {
		return
	}

	s.mu.Lock()
	same := text == s.lastApplied
	s.mu.Unlock()
	if same {
		return // it is what the viewer sent us a moment ago
	}

	msg := Msg{Text: text}
	if len(msg.Text) > s.maxText {
		msg.Text = truncateUTF8(msg.Text, s.maxText)
		msg.Trunc = true
	}
	// Lengths only, never contents. This is exactly the kind of line a
	// debugging session adds and forgets to remove.
	log.Printf("[clipboard] sent %d characters to the viewer", utf8.RuneCountInString(msg.Text))
	s.send(msg.Encode())
}

// Handle applies a clipboard message from the viewer. It BLOCKS until the write
// has happened, which is what orders it ahead of the keystroke that pastes it.
func (s *Sync) Handle(data []byte) {
	var m Msg
	if err := json.Unmarshal(data, &m); err != nil || m.T != Type {
		return
	}
	if m.Text == "" || len(m.Text) > s.maxText {
		return
	}

	s.mu.Lock()
	if s.stopped || m.Text == s.lastApplied {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	s.worker.do(func(b Board) {
		if err := b.WriteText(m.Text); err != nil {
			log.Printf("[clipboard] could not set the clipboard: %v", err)
			return
		}
		// Read the sequence back immediately, on the same thread, so the poller
		// recognises this change as ours and does not send it straight back.
		seq := b.Sequence()
		s.mu.Lock()
		s.ownSeq = seq
		s.lastSeq = seq
		s.lastApplied = m.Text
		s.mu.Unlock()
		log.Printf("[clipboard] received %d characters from the viewer", utf8.RuneCountInString(m.Text))
	})
}

// Stop ends the worker and releases the clipboard window.
func (s *Sync) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.mu.Unlock()
	s.worker.stop()
}

// truncateUTF8 cuts to at most n bytes without splitting a rune.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
