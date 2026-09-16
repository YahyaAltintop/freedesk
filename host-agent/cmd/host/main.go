// Command host is the FreeDesk host agent. It signs in to Firebase anonymously,
// publishes this machine under a fresh 9-digit pairing code, serves that code
// to the local web UI, and waits for incoming connection requests. Every
// request must be approved on this machine before WebRTC starts.
//
// Nothing is persisted: each launch is a new identity and a new code, and both
// are removed again on shutdown.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/auth"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/capture"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/config"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/consent"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/firebase"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/host"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/localapi"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/session"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/webrtc"
)

// appVersion is overwritten by the release build (-ldflags "-X main.appVersion=…").
var appVersion = "0.2.0-dev"

const (
	envFile = ".env"

	// approvalTimeout must stay comfortably below the viewer's connection
	// timeout (frontend/src/constants/webrtc.ts) so a rejection is always
	// delivered before the viewer gives up on its own.
	approvalTimeout = 45 * time.Second

	// Shutdown budget: a closed console window grants roughly 5 s on Windows.
	sessionDrainTimeout = 3 * time.Second
	shutdownTimeout     = 4 * time.Second
	registerAttempts    = 3
)

func main() {
	log.SetFlags(log.LstdFlags)
	if err := run(); err != nil {
		log.Fatalf("[host-agent] error: %v", err)
	}
}

func run() error {
	cfg, err := config.Load(envFile)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Authentication (anonymous, fresh every launch) -----------------------
	authClient := auth.NewClient(cfg.APIKey, cfg.AuthBaseURL, cfg.TokenBaseURL)
	manager, err := auth.NewManager(ctx, authClient)
	if err != nil {
		return err
	}
	log.Printf("[host-agent] authenticated (anonymous, uid=%s)", manager.UID())

	rtdb := firebase.NewRTDB(cfg.DatabaseURL, cfg.DatabaseNamespace, manager)

	// --- Pairing code + host record --------------------------------------------
	registrar := host.NewRegistrar(rtdb, manager.UID(), appVersion)
	code, err := registerWithFreshCode(ctx, registrar, cfg.HostName)
	if err != nil {
		return err
	}
	log.Printf("[host-agent] host registered (name=%q)", cfg.HostName)

	log.Println("[host-agent] ==========================================")
	log.Printf("[host-agent]   THIS COMPUTER'S CODE: %s", formatPairingCode(code))
	log.Println("[host-agent]   The person connecting enters this code in the web UI;")
	log.Println("[host-agent]   every request is approved on this computer.")
	log.Println("[host-agent]   The code changes every time the agent starts.")
	log.Println("[host-agent] ==========================================")

	// --- Local identity endpoint (web UI shows the code from here) ------------
	api, err := localapi.Start(cfg.LocalAPIPort, localapi.Identity{
		Code:    code,
		Name:    cfg.HostName,
		Version: appVersion,
	}, cfg.WebOrigins)
	if err != nil {
		log.Printf("[host-agent] failed to start local code endpoint (port %d): %v — the web UI will not be able to show this machine's code", cfg.LocalAPIPort, err)
	} else {
		defer api.Close()
		log.Printf("[host-agent] local code endpoint ready: http://127.0.0.1:%d/identity", cfg.LocalAPIPort)
	}

	// --- Heartbeat -------------------------------------------------------------
	go runHeartbeat(ctx, registrar, code, cfg.HeartbeatInterval)

	// --- Incoming requests -----------------------------------------------------
	approver := consent.New(cfg.ApprovalMode, approvalTimeout)
	log.Printf("[host-agent] screen capture will use: %s", capture.NewScreenCapture(cfg.FFmpegPath).Binary())
	coordinator := session.NewCoordinator(rtdb, manager.UID(), webrtc.DefaultConfig(), cfg.FFmpegPath)
	inbox := session.NewInbox(rtdb, manager.UID())

	var sessions sessionTracker
	log.Println("[host-agent] waiting for connection requests…")
	go inbox.Listen(ctx, func(req session.Request) {
		if !sessions.start() {
			return // shutting down
		}
		go func() {
			defer sessions.done()
			coordinator.Run(ctx, req, approver)
		}()
	})

	<-ctx.Done()
	log.Println("[host-agent] shutting down…")

	// Let running sessions write their final state while the identity still
	// exists, then remove the host record and finally the identity itself.
	sessions.drain(sessionDrainTimeout)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := registrar.Unregister(shutdownCtx, code); err != nil {
		log.Printf("[host-agent] could not remove host record: %v", err)
	}
	if err := manager.DeleteAccount(shutdownCtx); err != nil {
		log.Printf("[host-agent] could not delete anonymous identity: %v", err)
	}
	log.Println("[host-agent] shut down.")
	return nil
}

// registerWithFreshCode publishes the host under a random code, picking a new
// one if the rules refuse it (a live record owned by someone else).
func registerWithFreshCode(ctx context.Context, registrar *host.Registrar, name string) (string, error) {
	for attempt := 1; ; attempt++ {
		code := newPairingCode()
		err := registrar.Register(ctx, code, name)
		if err == nil {
			return code, nil
		}
		if attempt < registerAttempts && firebase.IsPermissionDenied(err) {
			log.Printf("[host-agent] code %s is taken — generating a new code", code)
			continue
		}
		return "", err
	}
}

func runHeartbeat(ctx context.Context, registrar *host.Registrar, code string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := registrar.Heartbeat(ctx, code); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("[host-agent] heartbeat error: %v", err)
			}
		}
	}
}

// sessionTracker counts running sessions so shutdown can wait for their
// cleanup writes. The mutex makes "no new sessions after drain began" an
// explicit rule instead of a race between Add and Wait.
type sessionTracker struct {
	mu       sync.Mutex
	wg       sync.WaitGroup
	draining bool
}

func (t *sessionTracker) start() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.draining {
		return false
	}
	t.wg.Add(1)
	return true
}

func (t *sessionTracker) done() { t.wg.Done() }

func (t *sessionTracker) drain(timeout time.Duration) {
	t.mu.Lock()
	t.draining = true
	t.mu.Unlock()

	finished := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(timeout):
		log.Println("[host-agent] sessions did not finish cleanup in time")
	}
}

// newPairingCode returns 9 random digits (leading zeros allowed). The code is
// both this machine's address and its database key.
func newPairingCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000_000))
	if err != nil {
		log.Fatalf("[host-agent] could not generate code: %v", err)
	}
	return fmt.Sprintf("%09d", n)
}

// formatPairingCode renders "123456789" as the human-friendly "123 456 789".
func formatPairingCode(code string) string {
	if len(code) != 9 {
		return code
	}
	return code[0:3] + " " + code[3:6] + " " + code[6:9]
}
