package firebase_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/auth"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/firebase"
)

// TestRTDBRulesEmulator exercises the security model against the Realtime
// Database emulator with the real rules loaded:
//
//   - a host publishes /hosts/{code} and any signed-in user can resolve it,
//     but nobody can list /hosts or touch someone else's live record;
//   - a viewer creates a session + inbox entry atomically, only the owner can
//     read the inbox, only participants can read the session, identity
//     fields are frozen and the status is an enum;
//   - both sides can clean up, and a stranger cannot remove a live host.
//
// Skipped unless run inside `firebase emulators:exec` (which injects the env).
func TestRTDBRulesEmulator(t *testing.T) {
	dbHost := os.Getenv("FIREBASE_DATABASE_EMULATOR_HOST")
	authHost := os.Getenv("FIREBASE_AUTH_EMULATOR_HOST")
	if dbHost == "" || authHost == "" {
		t.Skip("no emulator environment (run inside emulators:exec)")
	}

	const projectID = "demo-rcapp"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	newAnon := func(label string) *auth.Manager {
		t.Helper()
		client := auth.NewClient("demo",
			"http://"+authHost+"/identitytoolkit.googleapis.com",
			"http://"+authHost+"/securetoken.googleapis.com")
		manager, err := auth.NewManager(ctx, client)
		if err != nil {
			t.Fatalf("%s anonymous signUp: %v", label, err)
		}
		return manager
	}
	db := func(m *auth.Manager) *firebase.RTDB {
		return firebase.NewRTDB("http://"+dbHost, projectID+"-default-rtdb", m)
	}

	owner, viewer, stranger := newAnon("owner"), newAnon("viewer"), newAnon("stranger")
	ownerDB, viewerDB, strangerDB := db(owner), db(viewer), db(stranger)

	code := fmt.Sprintf("%09d", time.Now().UnixNano()%1_000_000_000)
	hostPath := "hosts/" + code

	mustDeny := func(label string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: was allowed; it should have been denied", label)
		}
		if !firebase.IsPermissionDenied(err) {
			t.Fatalf("%s: expected a permission denial, got: %v", label, err)
		}
	}
	mustAllow := func(label string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}

	// ---- hosts ---------------------------------------------------------------
	mustAllow("owner registers host", ownerDB.Put(ctx, hostPath, map[string]any{
		"ownerUid": owner.UID(), "name": "e2e-host", "version": "test", "lastSeen": firebase.ServerTimestamp(),
	}))
	mustAllow("owner heartbeat", ownerDB.Patch(ctx, hostPath, map[string]any{"lastSeen": firebase.ServerTimestamp()}))

	var record struct {
		OwnerUID string `json:"ownerUid"`
		LastSeen int64  `json:"lastSeen"`
	}
	found, err := viewerDB.Get(ctx, hostPath, &record)
	mustAllow("viewer resolves code", err)
	if !found || record.OwnerUID != owner.UID() || record.LastSeen == 0 {
		t.Fatalf("viewer code lookup: unexpected record %+v (found=%v)", record, found)
	}

	_, err = viewerDB.Get(ctx, "hosts", nil)
	mustDeny("viewer lists hosts", err)
	mustDeny("viewer edits host", viewerDB.Patch(ctx, hostPath, map[string]any{"name": "hijack"}))
	mustDeny("viewer takes over live code", viewerDB.Put(ctx, hostPath, map[string]any{
		"ownerUid": viewer.UID(), "name": "mine", "lastSeen": firebase.ServerTimestamp(),
	}))
	mustDeny("stranger removes live host", strangerDB.Delete(ctx, hostPath))
	mustDeny("bad code format", ownerDB.Put(ctx, "hosts/abc", map[string]any{
		"ownerUid": owner.UID(), "name": "x", "lastSeen": firebase.ServerTimestamp(),
	}))
	mustDeny("local clock timestamp rejected", ownerDB.Patch(ctx, hostPath, map[string]any{"lastSeen": 12345}))

	// ---- session + inbox (atomic multi-path write) ----------------------------
	sessionID := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
	sessionPath := "sessions/" + sessionID
	inboxPath := "inbox/" + owner.UID() + "/" + sessionID

	mustAllow("viewer creates session and inbox entry", viewerDB.Patch(ctx, "", map[string]any{
		sessionPath: map[string]any{
			"viewerUid": viewer.UID(), "ownerUid": owner.UID(), "status": "waiting", "createdAt": firebase.ServerTimestamp(),
		},
		inboxPath: map[string]any{
			"viewerUid": viewer.UID(), "code": code, "createdAt": firebase.ServerTimestamp(),
		},
	}))

	mustDeny("forged viewerUid", viewerDB.Put(ctx, sessionPath+"-forged", map[string]any{
		"viewerUid": owner.UID(), "ownerUid": owner.UID(), "status": "waiting", "createdAt": firebase.ServerTimestamp(),
	}))
	mustDeny("inbox entry not matching writer", strangerDB.Put(ctx, "inbox/"+owner.UID()+"/x", map[string]any{
		"viewerUid": viewer.UID(), "code": code, "createdAt": firebase.ServerTimestamp(),
	}))

	var entries map[string]map[string]any
	found, err = ownerDB.Get(ctx, "inbox/"+owner.UID(), &entries)
	mustAllow("owner reads inbox", err)
	if !found || entries[sessionID]["viewerUid"] != viewer.UID() {
		t.Fatalf("owner inbox: unexpected content %+v (found=%v)", entries, found)
	}
	_, err = viewerDB.Get(ctx, "inbox/"+owner.UID(), nil)
	mustDeny("viewer reads owner inbox", err)
	_, err = strangerDB.Get(ctx, sessionPath, nil)
	mustDeny("stranger reads session", err)
	_, err = ownerDB.Get(ctx, sessionPath, nil)
	mustAllow("owner reads session", err)

	mustAllow("owner advances status", ownerDB.Put(ctx, sessionPath+"/status", "connecting"))
	mustAllow("owner writes offer", ownerDB.Put(ctx, sessionPath+"/offer", map[string]string{"type": "offer", "sdp": "x"}))

	// Anyone with the public key can mint an identity and write sessions, so
	// what one write may weigh is what stands between a stranger and a full
	// database: 16 MB per unvalidated string, 1 GB of quota on the Spark plan.
	mustDeny("oversized sdp", ownerDB.Put(ctx, sessionPath+"/offer", map[string]string{"type": "offer", "sdp": strings.Repeat("a", 65537)}))
	mustDeny("description type is an enum", ownerDB.Put(ctx, sessionPath+"/offer", map[string]string{"type": "rollback", "sdp": "x"}))
	mustAllow("viewer writes answer", viewerDB.Put(ctx, sessionPath+"/answer", map[string]string{"type": "answer", "sdp": "y"}))
	_, err = viewerDB.Push(ctx, sessionPath+"/viewerCandidates", map[string]any{
		"candidate": "candidate:1 1 udp 2122260223 192.0.2.1 54321 typ host", "sdpMid": "0", "sdpMLineIndex": 0, "usernameFragment": "abcd",
	})
	mustAllow("viewer adds a candidate", err)
	_, err = viewerDB.Push(ctx, sessionPath+"/viewerCandidates", map[string]any{"candidate": strings.Repeat("c", 513)})
	mustDeny("oversized candidate", err)
	_, err = viewerDB.Push(ctx, sessionPath+"/viewerCandidates", map[string]any{"candidate": "c", "usernameFragment": strings.Repeat("u", 257)})
	mustDeny("oversized ufrag", err)
	mustDeny("bogus status", viewerDB.Put(ctx, sessionPath+"/status", "bogus"))
	mustDeny("viewerUid is frozen", viewerDB.Put(ctx, sessionPath+"/viewerUid", stranger.UID()))
	mustDeny("required field cannot be removed", ownerDB.Delete(ctx, sessionPath+"/viewerUid"))
	mustDeny("unknown field", viewerDB.Put(ctx, sessionPath+"/extra", "x"))

	// ---- cleanup -------------------------------------------------------------
	mustAllow("owner removes inbox entry", ownerDB.Delete(ctx, inboxPath))
	mustAllow("viewer removes session", viewerDB.Delete(ctx, sessionPath))
	mustAllow("removing an already removed session is idempotent", ownerDB.Delete(ctx, sessionPath))
	mustAllow("owner removes host", ownerDB.Delete(ctx, hostPath))

	// ---- identity cleanup ----------------------------------------------------
	mustAllow("owner deletes its anonymous account", owner.DeleteAccount(ctx))
}
