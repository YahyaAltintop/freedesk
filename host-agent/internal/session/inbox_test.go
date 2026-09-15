package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/yahya/remote-control-app/host-agent/internal/firebase"
)

func newTestInbox(now time.Time) (*Inbox, *[]string) {
	removed := []string{}
	in := &Inbox{
		ownerUID: "owner",
		seen:     make(map[string]bool),
		now:      func() time.Time { return now },
	}
	in.remove = func(_ context.Context, id string) { removed = append(removed, id) }
	return in, &removed
}

func event(kind, path, data string) firebase.StreamEvent {
	return firebase.StreamEvent{Event: kind, Path: path, Data: json.RawMessage(data)}
}

func TestInboxDispatchesSnapshotAndNewEntriesOnce(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	in, removed := newTestInbox(now)

	var got []Request
	handle := func(r Request) { got = append(got, r) }
	ctx := context.Background()

	fresh := now.Add(-10 * time.Second).UnixMilli()

	// Initial sync: a map of entries at "/".
	in.handleData(ctx, event("put", "/", `{"s1":{"viewerUid":"v1","code":"123456789","createdAt":`+itoa(fresh)+`}}`), handle)
	// A new request at "/{id}".
	in.handleData(ctx, event("put", "/s2", `{"viewerUid":"v2","code":"123456789","createdAt":`+itoa(fresh)+`}`), handle)
	// Reconnect replays both; neither may be dispatched again.
	in.handleData(ctx, event("put", "/", `{"s1":{"viewerUid":"v1","code":"123456789","createdAt":`+itoa(fresh)+`},"s2":{"viewerUid":"v2","code":"123456789","createdAt":`+itoa(fresh)+`}}`), handle)
	// Removal notifications are ignored.
	in.handleData(ctx, event("put", "/s1", `null`), handle)

	if len(got) != 2 || got[0].ID != "s1" || got[0].ViewerUID != "v1" || got[1].ID != "s2" || got[1].ViewerUID != "v2" {
		t.Fatalf("unexpected requests: %+v", got)
	}
	if len(*removed) != 2 {
		t.Fatalf("every consumed entry must be removed exactly once, got %v", *removed)
	}
}

func TestInboxDropsStaleAndMalformedEntries(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	in, removed := newTestInbox(now)

	var got []Request
	handle := func(r Request) { got = append(got, r) }
	ctx := context.Background()

	old := now.Add(-staleRequestAge - time.Minute).UnixMilli()
	in.handleData(ctx, event("put", "/old", `{"viewerUid":"v1","code":"123456789","createdAt":`+itoa(old)+`}`), handle)
	in.handleData(ctx, event("put", "/bad", `{"code":"123456789","createdAt":`+itoa(now.UnixMilli())+`}`), handle)
	in.handleData(ctx, event("put", "/deep/field", `"x"`), handle)

	if len(got) != 0 {
		t.Fatalf("stale/malformed entries must not be dispatched, got %+v", got)
	}
	if len(*removed) != 2 {
		t.Fatalf("stale/malformed entries are still removed from the inbox, got %v", *removed)
	}
}

func itoa(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
