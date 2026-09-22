// Package session receives connection requests addressed to this host and
// runs each one from operator approval through to the end of the WebRTC
// connection.
package session

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/firebase"
)

const (
	pathInbox    = "inbox"
	pathSessions = "sessions"

	// staleRequestAge is generous because it is measured with the host's local
	// clock against a server timestamp; it only needs to drop leftovers, not
	// be precise.
	staleRequestAge = 5 * time.Minute

	reconnectMinDelay = time.Second
	reconnectMaxDelay = 30 * time.Second
)

// Request is one connection request left in this host's inbox by a viewer.
type Request struct {
	ID        string // session id (also the RTDB key under /inbox/{uid} and /sessions)
	ViewerUID string
	Code      string
	CreatedAt int64 // server timestamp, ms
}

type inboxEntry struct {
	ViewerUID string `json:"viewerUid"`
	Code      string `json:"code"`
	CreatedAt int64  `json:"createdAt"`
}

// Inbox streams /inbox/{ownerUid} over Server-Sent Events, so a request
// reaches the host the moment the viewer writes it: no polling, and nothing is
// read while the host sits idle.
type Inbox struct {
	rtdb     *firebase.RTDB
	ownerUID string
	// seen is every request id already handled, with when, so a stream
	// reconnect's replay cannot dispatch one twice. Entries older than
	// staleRequestAge are dropped as they go: a replay that old is refused as
	// stale anyway, and without the sweep the map would grow by one entry per
	// request for as long as the agent runs.
	seen   map[string]time.Time
	now    func() time.Time
	remove func(ctx context.Context, sessionID string)
}

// NewInbox builds an inbox listener for the given owner.
func NewInbox(rtdb *firebase.RTDB, ownerUID string) *Inbox {
	in := &Inbox{rtdb: rtdb, ownerUID: ownerUID, seen: make(map[string]time.Time), now: time.Now}
	in.remove = func(ctx context.Context, sessionID string) {
		if err := rtdb.Delete(ctx, pathInbox+"/"+ownerUID+"/"+sessionID); err != nil {
			log.Printf("[inbox] could not remove request %s: %v", sessionID, err)
		}
	}
	return in
}

// Listen blocks until ctx is cancelled, invoking handle once per new request.
// The stream is reopened (with a fresh token) whenever it ends, is cancelled by
// the server, or the token expires.
func (in *Inbox) Listen(ctx context.Context, handle func(Request)) {
	delay := reconnectMinDelay
	for {
		events, err := in.rtdb.Stream(ctx, pathInbox+"/"+in.ownerUID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[inbox] could not open request stream: %v (retrying in %s)", err, delay)
			if !sleep(ctx, delay) {
				return
			}
			delay = min(delay*2, reconnectMaxDelay)
			continue
		}
		delay = reconnectMinDelay

	consume:
		for ev := range events {
			switch ev.Event {
			case firebase.EventPut, firebase.EventPatch:
				in.handleData(ctx, ev, handle)
			case firebase.EventAuthRevoked:
				log.Println("[inbox] auth token expired; reconnecting")
				break consume
			case firebase.EventCancel:
				log.Printf("[inbox] stream cancelled by server: %s", strings.TrimSpace(string(ev.Data)))
				break consume
			}
		}
		if ctx.Err() != nil {
			return
		}
		// Drain whatever remains so the reader goroutine can exit, then reopen.
		for range events {
		}
		if !sleep(ctx, reconnectMinDelay) {
			return
		}
	}
}

// handleData turns one put/patch event into zero or more requests. On initial
// sync (and after every reconnect) the path is "/" and the payload is a map of
// entries; a single new request arrives at "/{sessionId}".
func (in *Inbox) handleData(ctx context.Context, ev firebase.StreamEvent, handle func(Request)) {
	trimmed := strings.TrimSpace(string(ev.Data))
	if trimmed == "" || trimmed == "null" {
		return // removal (our own delete, or the viewer's cleanup)
	}

	if ev.Path == "/" {
		var entries map[string]inboxEntry
		if err := json.Unmarshal(ev.Data, &entries); err != nil {
			return
		}
		for id, entry := range entries {
			in.dispatch(ctx, id, entry, handle)
		}
		return
	}

	// "/{sessionId}" — anything deeper is a field-level change we do not use.
	id := strings.Trim(ev.Path, "/")
	if id == "" || strings.Contains(id, "/") {
		return
	}
	var entry inboxEntry
	if err := json.Unmarshal(ev.Data, &entry); err != nil {
		return
	}
	in.dispatch(ctx, id, entry, handle)
}

func (in *Inbox) dispatch(ctx context.Context, id string, entry inboxEntry, handle func(Request)) {
	now := in.now()
	in.forget(now)
	if _, done := in.seen[id]; done {
		return
	}
	in.seen[id] = now

	// The entry is consumed either way: a request is handled exactly once, and
	// a reconnect must never replay it.
	in.remove(ctx, id)

	age := now.Sub(time.UnixMilli(entry.CreatedAt))
	if entry.ViewerUID == "" || age > staleRequestAge {
		log.Printf("[inbox] dropping stale or malformed request %s (age %s)", id, age.Round(time.Second))
		return
	}
	handle(Request{ID: id, ViewerUID: entry.ViewerUID, Code: entry.Code, CreatedAt: entry.CreatedAt})
}

// forget drops ids handled longer ago than staleRequestAge: a replay of one of
// those is dropped as stale before it could be handled again, so remembering
// it any longer only costs memory.
func (in *Inbox) forget(now time.Time) {
	for id, at := range in.seen {
		if now.Sub(at) > staleRequestAge {
			delete(in.seen, id)
		}
	}
}

// sleep waits for d or until ctx is done; it reports whether the wait completed.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
