package webrtc

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	pion "github.com/pion/webrtc/v4"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/auth"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/firebase"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/signaling"
)

// TestConnectOverRTDBEmulator runs the full host↔viewer negotiation through the
// real Realtime Database REST API against the Firebase emulator, with the
// security rules enforced. It is skipped unless the emulator env vars are set
// (they are injected automatically by `firebase emulators:exec`).
func TestConnectOverRTDBEmulator(t *testing.T) {
	dbHost := os.Getenv("FIREBASE_DATABASE_EMULATOR_HOST")
	authHost := os.Getenv("FIREBASE_AUTH_EMULATOR_HOST")
	if dbHost == "" || authHost == "" {
		t.Skip("no emulator environment (run inside emulators:exec)")
	}

	const projectID = "demo-rcapp"
	const apiKey = "demo"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	authClient := auth.NewClient(apiKey,
		"http://"+authHost+"/identitytoolkit.googleapis.com",
		"http://"+authHost+"/securetoken.googleapis.com")
	manager, err := auth.NewManager(ctx, authClient)
	if err != nil {
		t.Fatalf("anonymous signUp: %v", err)
	}
	uid := manager.UID()

	rtdb := firebase.NewRTDB("http://"+dbHost, projectID+"-default-rtdb", manager)
	sessionID := fmt.Sprintf("e2e-%d", time.Now().UnixNano())

	// The viewer would create this node; the rules need the participant uids,
	// a lifecycle status and a server-stamped creation time.
	if err := rtdb.Put(ctx, "sessions/"+sessionID, map[string]any{
		"viewerUid": uid,
		"ownerUid":  uid,
		"status":    "waiting",
		"createdAt": firebase.ServerTimestamp(),
	}); err != nil {
		t.Fatalf("could not create session node: %v", err)
	}
	defer rtdb.Delete(context.Background(), "sessions/"+sessionID)

	hostT := signaling.NewRTDBTransport(rtdb, sessionID, signaling.Host)
	viewerT := signaling.NewRTDBTransport(rtdb, sessionID, signaling.Viewer)

	hostOpen := make(chan struct{})
	viewerOpen := make(chan struct{})
	viewerMsg := make(chan string, 1)

	var (
		wg                 sync.WaitGroup
		hostPC, viewerPC   *pion.PeerConnection
		hostErr, viewerErr error
		hostDC             *pion.DataChannel
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		hostPC, hostErr = Connect(ctx, signaling.Host, hostT, DefaultConfig(), Hooks{
			OnDataChannel: func(dc *pion.DataChannel) {
				hostDC = dc
				dc.OnOpen(func() { close(hostOpen) })
			},
		})
	}()
	go func() {
		defer wg.Done()
		viewerPC, viewerErr = Connect(ctx, signaling.Viewer, viewerT, DefaultConfig(), Hooks{
			OnDataChannel: func(dc *pion.DataChannel) {
				dc.OnOpen(func() { close(viewerOpen) })
				dc.OnMessage(func(msg pion.DataChannelMessage) { viewerMsg <- string(msg.Data) })
			},
		})
	}()
	wg.Wait()

	if hostErr != nil {
		t.Fatalf("host could not connect: %v", hostErr)
	}
	if viewerErr != nil {
		t.Fatalf("viewer could not connect: %v", viewerErr)
	}
	defer hostPC.Close()
	defer viewerPC.Close()

	mustClose(t, "host DataChannel", hostOpen)
	mustClose(t, "viewer DataChannel", viewerOpen)

	if err := hostDC.SendText("hello"); err != nil {
		t.Fatalf("could not send message: %v", err)
	}
	select {
	case got := <-viewerMsg:
		if got != "hello" {
			t.Fatalf("expected %q, got %q", "hello", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("viewer did not receive the message in time")
	}

	// The single session stream also tracks liveness: removing the node (what
	// the viewer's onDisconnect does) must surface as Gone on the host side.
	if err := rtdb.Delete(ctx, "sessions/"+sessionID); err != nil {
		t.Fatalf("could not delete session node: %v", err)
	}
	select {
	case <-hostT.Gone():
	case <-time.After(10 * time.Second):
		t.Fatal("host transport did not notice the session node disappearing")
	}
}
