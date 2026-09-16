package signaling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/firebase"
)

// ErrSessionGone is returned when the session node disappears (the viewer
// cleaned up or its onDisconnect fired) before negotiation completed.
var ErrSessionGone = errors.New("session withdrawn by the remote peer")

// Snapshot is the identity/lifecycle part of the session node as first seen.
type Snapshot struct {
	ViewerUID string
	OwnerUID  string
	Status    string
}

// sessionNode mirrors /sessions/{id} (docs/PROTOCOL.md).
type sessionNode struct {
	ViewerUID        string                  `json:"viewerUid"`
	OwnerUID         string                  `json:"ownerUid"`
	Status           string                  `json:"status"`
	Offer            *Description            `json:"offer"`
	Answer           *Description            `json:"answer"`
	HostCandidates   map[string]ICECandidate `json:"hostCandidates"`
	ViewerCandidates map[string]ICECandidate `json:"viewerCandidates"`
}

// RTDBTransport implements Transport over Firebase Realtime Database, following
// the layout in docs/PROTOCOL.md:
//
//	/sessions/{id}/offer | answer | hostCandidates/* | viewerCandidates/* | status
//
// The role decides which paths are "local" vs "remote". A single SSE stream on
// the session node is demultiplexed into the remote description, the remote
// ICE candidates, status changes and a "gone" signal, so one connection per
// session covers negotiation and liveness.
type RTDBTransport struct {
	rtdb     *firebase.RTDB
	basePath string
	role     Role

	startOnce sync.Once
	startErr  error

	snapshot   chan Snapshot
	status     chan string
	remoteDesc chan Description
	remoteCand chan ICECandidate
	gone       chan struct{}

	snapOnce sync.Once
	descOnce sync.Once
	goneOnce sync.Once

	mu       sync.Mutex
	seenCand map[string]bool
}

// NewRTDBTransport builds a transport bound to one session and role.
func NewRTDBTransport(rtdb *firebase.RTDB, sessionID string, role Role) *RTDBTransport {
	return &RTDBTransport{
		rtdb:       rtdb,
		basePath:   "sessions/" + sessionID,
		role:       role,
		snapshot:   make(chan Snapshot, 1),
		status:     make(chan string, 16),
		remoteDesc: make(chan Description, 1),
		remoteCand: make(chan ICECandidate, 256),
		gone:       make(chan struct{}),
		seenCand:   make(map[string]bool),
	}
}

func (t *RTDBTransport) localDescKey() string {
	if t.role == Host {
		return "offer"
	}
	return "answer"
}

func (t *RTDBTransport) remoteDescKey() string {
	if t.role == Host {
		return "answer"
	}
	return "offer"
}

func (t *RTDBTransport) localCandKey() string {
	if t.role == Host {
		return "hostCandidates"
	}
	return "viewerCandidates"
}

func (t *RTDBTransport) remoteCandKey() string {
	if t.role == Host {
		return "viewerCandidates"
	}
	return "hostCandidates"
}

// Start opens the session stream. It is safe to call repeatedly; the first
// call wins and its ctx bounds the stream's lifetime. A permission-denied
// error means the node no longer exists (or never did).
func (t *RTDBTransport) Start(ctx context.Context) error {
	t.startOnce.Do(func() {
		events, err := t.rtdb.Stream(ctx, t.basePath)
		if err != nil {
			t.startErr = err
			t.markGone()
			return
		}
		go t.run(ctx, events)
	})
	return t.startErr
}

// Snapshot delivers the session's identity fields once the initial sync arrives.
func (t *RTDBTransport) Snapshot() <-chan Snapshot { return t.snapshot }

// Status delivers lifecycle changes. The channel is buffered and lossy: if
// nobody is reading, older values are dropped, which is fine because only the
// latest state matters.
func (t *RTDBTransport) Status() <-chan string { return t.status }

// Gone is closed when the session node is removed or can no longer be read.
func (t *RTDBTransport) Gone() <-chan struct{} { return t.gone }

// PublishLocalDescription writes the local SDP (offer for host, answer for viewer).
func (t *RTDBTransport) PublishLocalDescription(ctx context.Context, d Description) error {
	return t.rtdb.Put(ctx, t.basePath+"/"+t.localDescKey(), d)
}

// AwaitRemoteDescription blocks until the remote SDP appears, then returns it.
func (t *RTDBTransport) AwaitRemoteDescription(ctx context.Context) (Description, error) {
	if err := t.Start(ctx); err != nil {
		return Description{}, err
	}
	select {
	case d := <-t.remoteDesc:
		return d, nil
	case <-t.gone:
		return Description{}, ErrSessionGone
	case <-ctx.Done():
		return Description{}, ctx.Err()
	}
}

// PublishLocalCandidate appends a local ICE candidate.
func (t *RTDBTransport) PublishLocalCandidate(ctx context.Context, c ICECandidate) error {
	_, err := t.rtdb.Push(ctx, t.basePath+"/"+t.localCandKey(), c)
	return err
}

// RemoteCandidates streams the remote peer's ICE candidates. The channel stays
// open for the life of the transport; consumers stop on their own ctx.
func (t *RTDBTransport) RemoteCandidates(ctx context.Context) (<-chan ICECandidate, error) {
	if err := t.Start(ctx); err != nil {
		return nil, err
	}
	return t.remoteCand, nil
}

// Close is a no-op: the stream is tied to the Start context.
func (t *RTDBTransport) Close() error { return nil }

// run consumes stream events, reconnecting with a fresh token when the server
// revokes the old one or the connection drops. A cancel (rules no longer allow
// the read: the node was deleted) or a permission-denied reopen means the
// session is gone.
func (t *RTDBTransport) run(ctx context.Context, events <-chan firebase.StreamEvent) {
	delay := time.Second
	for {
	consume:
		for ev := range events {
			switch ev.Event {
			case firebase.EventPut, firebase.EventPatch:
				t.demux(ctx, ev)
			case firebase.EventAuthRevoked:
				break consume
			case firebase.EventCancel:
				t.markGone()
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		for range events {
		}

		// Reopen; the initial sync re-sends the node, which dedupe absorbs.
		for {
			if !wait(ctx, delay) {
				return
			}
			next, err := t.rtdb.Stream(ctx, t.basePath)
			if err == nil {
				events = next
				delay = time.Second
				break
			}
			if ctx.Err() != nil {
				return
			}
			if firebase.IsPermissionDenied(err) {
				t.markGone()
				return
			}
			log.Printf("[signaling] could not reopen session stream: %v", err)
			delay = min(delay*2, 30*time.Second)
		}
	}
}

// demux routes one put/patch event to the right channel. Paths are relative
// to the session node.
func (t *RTDBTransport) demux(ctx context.Context, ev firebase.StreamEvent) {
	trimmed := strings.TrimSpace(string(ev.Data))
	if ev.Path == "/" && (trimmed == "" || trimmed == "null") {
		t.markGone()
		return
	}
	if trimmed == "" || trimmed == "null" {
		return // a child was removed; nothing we consume is ever removed mid-session
	}

	if ev.Event == firebase.EventPatch && ev.Path == "/" {
		// A patch lists changed children keyed by (possibly nested) path.
		var children map[string]json.RawMessage
		if err := json.Unmarshal(ev.Data, &children); err != nil {
			return
		}
		for key, data := range children {
			t.route("/"+strings.Trim(key, "/"), data, ctx)
		}
		return
	}
	t.route(ev.Path, ev.Data, ctx)
}

func (t *RTDBTransport) route(path string, data json.RawMessage, ctx context.Context) {
	remoteDesc := "/" + t.remoteDescKey()
	remoteCand := "/" + t.remoteCandKey()

	switch {
	case path == "/":
		var node sessionNode
		if err := json.Unmarshal(data, &node); err != nil {
			return
		}
		if node.ViewerUID != "" || node.OwnerUID != "" {
			t.snapOnce.Do(func() {
				t.snapshot <- Snapshot{ViewerUID: node.ViewerUID, OwnerUID: node.OwnerUID, Status: node.Status}
			})
		}
		if node.Status != "" {
			t.emitStatus(node.Status)
		}
		if d := t.pickRemote(node); d != nil {
			t.emitDescription(*d)
		}
		for key, c := range t.pickRemoteCandidates(node) {
			t.emitCandidate(ctx, key, c)
		}

	case path == "/status":
		var status string
		if err := json.Unmarshal(data, &status); err == nil && status != "" {
			t.emitStatus(status)
		}

	case path == remoteDesc:
		var d Description
		if err := json.Unmarshal(data, &d); err == nil && d.Type != "" && d.SDP != "" {
			t.emitDescription(d)
		}

	case path == remoteCand:
		var children map[string]ICECandidate
		if err := json.Unmarshal(data, &children); err == nil {
			for key, c := range children {
				t.emitCandidate(ctx, key, c)
			}
		}

	case strings.HasPrefix(path, remoteCand+"/"):
		key := strings.TrimPrefix(path, remoteCand+"/")
		if strings.Contains(key, "/") {
			return // field-level change inside a candidate; never happens
		}
		var c ICECandidate
		if err := json.Unmarshal(data, &c); err == nil {
			t.emitCandidate(ctx, key, c)
		}
	}
}

func (t *RTDBTransport) pickRemote(node sessionNode) *Description {
	if t.role == Host {
		return node.Answer
	}
	return node.Offer
}

func (t *RTDBTransport) pickRemoteCandidates(node sessionNode) map[string]ICECandidate {
	if t.role == Host {
		return node.ViewerCandidates
	}
	return node.HostCandidates
}

func (t *RTDBTransport) emitStatus(status string) {
	select {
	case t.status <- status:
	default:
		// Nobody is reading; drop the oldest so the newest is always available.
		select {
		case <-t.status:
		default:
		}
		select {
		case t.status <- status:
		default:
		}
	}
}

func (t *RTDBTransport) emitDescription(d Description) {
	if d.Type == "" || d.SDP == "" {
		return
	}
	t.descOnce.Do(func() { t.remoteDesc <- d })
}

func (t *RTDBTransport) emitCandidate(ctx context.Context, key string, c ICECandidate) {
	if c.Candidate == "" {
		return
	}
	t.mu.Lock()
	dup := t.seenCand[key]
	t.seenCand[key] = true
	t.mu.Unlock()
	if dup {
		return
	}
	select {
	case t.remoteCand <- c:
	case <-ctx.Done():
	}
}

func (t *RTDBTransport) markGone() {
	t.goneOnce.Do(func() { close(t.gone) })
}

func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// String helps log lines identify the transport.
func (t *RTDBTransport) String() string {
	return fmt.Sprintf("rtdb:%s(%d)", t.basePath, t.role)
}
