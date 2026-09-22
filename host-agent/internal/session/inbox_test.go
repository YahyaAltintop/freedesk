package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/firebase"
)

func newTestInbox(now time.Time) (*Inbox, *[]string) {
	removed := []string{}
	in := &Inbox{
		ownerUID: "owner",
		seen:     make(map[string]time.Time),
		now:      func() time.Time { return now },
	}
	in.remove = func(_ context.Context, id string) { removed = append(removed, id) }
	return in, &removed
}

// An id is remembered only as long as a replay of it could still be handled.
// After staleRequestAge the age check refuses it anyway, so the memory is let
// go — otherwise the map would hold one entry per request the agent ever saw.
func TestInboxForgetsRequestsOnceTheyAreStale(t *testing.T) {
	start := time.UnixMilli(1_700_000_000_000)
	clock := start
	in := &Inbox{
		ownerUID: "owner",
		seen:     make(map[string]time.Time),
		now:      func() time.Time { return clock },
		remove:   func(context.Context, string) {},
	}
	handled := 0
	handle := func(Request) { handled++ }
	ctx := context.Background()

	in.handleData(ctx, event("put", "/s1", `{"viewerUid":"v1","code":"123456","createdAt":`+itoa(start.UnixMilli())+`}`), handle)
	if _, kept := in.seen["s1"]; !kept || handled != 1 {
		t.Fatalf("a fresh request must be handled and remembered (handled=%d, seen=%v)", handled, in.seen)
	}

	// Not stale yet: still remembered, and a replay is still ignored.
	clock = start.Add(staleRequestAge - time.Minute)
	in.handleData(ctx, event("put", "/s1", `{"viewerUid":"v1","code":"123456","createdAt":`+itoa(start.UnixMilli())+`}`), handle)
	if _, kept := in.seen["s1"]; !kept || handled != 1 {
		t.Fatalf("a replay inside the window must be ignored, not re-handled (handled=%d)", handled)
	}

	// Past the window, the next request sweeps it out; a replay of it is now
	// refused as stale rather than by memory.
	clock = start.Add(staleRequestAge + time.Minute)
	in.handleData(ctx, event("put", "/s2", `{"viewerUid":"v2","code":"123456","createdAt":`+itoa(clock.UnixMilli())+`}`), handle)
	if _, kept := in.seen["s1"]; kept {
		t.Fatal("an id older than staleRequestAge was not forgotten")
	}
	if _, kept := in.seen["s2"]; !kept || handled != 2 {
		t.Fatalf("the request that triggered the sweep must itself be handled and remembered (handled=%d)", handled)
	}
	in.handleData(ctx, event("put", "/s1", `{"viewerUid":"v1","code":"123456","createdAt":`+itoa(start.UnixMilli())+`}`), handle)
	if handled != 2 {
		t.Fatalf("a stale replay must be dropped by its age, got handled=%d", handled)
	}
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
	in.handleData(ctx, event("put", "/", `{"s1":{"viewerUid":"v1","code":"123456","createdAt":`+itoa(fresh)+`}}`), handle)
	// A new request at "/{id}".
	in.handleData(ctx, event("put", "/s2", `{"viewerUid":"v2","code":"123456","createdAt":`+itoa(fresh)+`}`), handle)
	// Reconnect replays both; neither may be dispatched again.
	in.handleData(ctx, event("put", "/", `{"s1":{"viewerUid":"v1","code":"123456","createdAt":`+itoa(fresh)+`},"s2":{"viewerUid":"v2","code":"123456","createdAt":`+itoa(fresh)+`}}`), handle)
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
	in.handleData(ctx, event("put", "/old", `{"viewerUid":"v1","code":"123456","createdAt":`+itoa(old)+`}`), handle)
	in.handleData(ctx, event("put", "/bad", `{"code":"123456","createdAt":`+itoa(now.UnixMilli())+`}`), handle)
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
