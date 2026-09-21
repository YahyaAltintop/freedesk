// Command host is the FreeDesk host agent. It signs in to Firebase anonymously,
// publishes this machine under a fresh 9-digit pairing code, serves that code
// to the local web UI, and waits for incoming connection requests. Every
// request must be approved on this machine before WebRTC starts.
//
// Nothing is persisted: each launch is a new identity and a new code, and both
// are removed again on shutdown.
package main

import (
	"bufio"
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
	"github.com/YahyaAltintop/freedesk/host-agent/internal/transfer"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/webrtc"
)

// appVersion is overwritten by the release build (-ldflags "-X main.appVersion=…").
var appVersion = "0.3.0-dev"

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
		log.Printf("[host-agent] error: %v", err)
		exitWithError()
	}
}

// exitWithError keeps a double-clicked console window open until Enter is
// pressed, so the message above can actually be read; started from a
// terminal, the agent exits at once.
func exitWithError() {
	if ownsConsole() {
		fmt.Fprint(os.Stderr, "\nPress Enter to close this window.")
		_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
	}
	os.Exit(1)
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
	code, err := registerWithFreshCode(ctx, registrar, cfg.HostName, cfg.ProjectID)
	if err != nil {
		return err
	}
	log.Printf("[host-agent] host registered (name=%q)", cfg.HostName)
	current := &pairing{code: code}
	printBanner(code, cfg.SiteURL())

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
	go runHeartbeat(ctx, registrar, current, cfg.HeartbeatInterval)

	// --- Incoming requests -----------------------------------------------------
	approver := consent.New(cfg.ApprovalMode, approvalTimeout)
	picker := consent.NewFilePicker(cfg.ApprovalMode)
	log.Printf("[host-agent] screen capture will use: %s", capture.NewScreenCapture(cfg.FFmpegPath).Binary())

	// Say where accepted files would go before anyone sends one — the operator
	// should not first learn this from a prompt. The folder itself is only
	// created if a file is ever accepted. Any half-written file left by a run
	// that was killed is cleaned up here.
	downloads := transfer.NewDest().Root()
	log.Printf("[host-agent] files you accept will be saved to: %s", downloads)
	transfer.SweepPartials(downloads)

	coordinator := session.NewCoordinator(rtdb, manager.UID(), webrtc.DefaultConfig(), cfg.FFmpegPath, appVersion, picker, cfg.ClipboardMode)
	inbox := session.NewInbox(rtdb, manager.UID())

	// A code that keeps producing windows nobody approves has reached the
	// wrong people. Replace it: the operator reads the new one off the
	// console, and whoever was probing the old one has to start over.
	coordinator.OnRepeatedRefusals(func(streak int) {
		rotateCode(ctx, registrar, current, api, cfg, streak)
	})

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
	if err := registrar.Unregister(shutdownCtx, current.get()); err != nil {
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
func registerWithFreshCode(ctx context.Context, registrar *host.Registrar, name, projectID string) (string, error) {
	for attempt := 1; ; attempt++ {
		code := newPairingCode()
		err := registrar.Register(ctx, code, name)
		if err == nil {
			return code, nil
		}
		if !firebase.IsPermissionDenied(err) {
			return "", err
		}
		if attempt < registerAttempts {
			log.Printf("[host-agent] code %s is taken — generating a new code", code)
			continue
		}
		return "", registrationDeniedError(attempt, projectID, err)
	}
}

// registrationDeniedError explains a run of consecutive denials: several
// random codes being taken at the same moment is practically impossible, so
// the project's security rules are almost certainly missing.
func registrationDeniedError(attempts int, projectID string, err error) error {
	return fmt.Errorf("the database refused to publish this computer %d times in a row (%w)\n"+
		"  -> The security rules are probably not deployed to project %q: run `firebase deploy --only database` in the firebase folder (see firebase/README.md).",
		attempts, err, projectID)
}

// pairing is the code this machine is currently published under. It changes
// when the coordinator reports a run of refused connection requests, so every
// reader — the heartbeat, the loopback endpoint, shutdown — takes the current
// value rather than the one from start-up.
type pairing struct {
	mu   sync.Mutex
	code string
}

func (p *pairing) get() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code
}

func (p *pairing) set(code string) {
	p.mu.Lock()
	p.code = code
	p.mu.Unlock()
}

// printBanner shows the operator the one thing they need to pass on.
func printBanner(code, site string) {
	log.Println("[host-agent] ==========================================")
	log.Printf("[host-agent]   THIS COMPUTER'S CODE: %s", formatPairingCode(code))
	log.Printf("[host-agent]   Web page:             %s", site)
	log.Println("[host-agent]   The person connecting opens the web page and enters this code;")
	log.Println("[host-agent]   every request is approved on this computer.")
	log.Println("[host-agent]   The code changes every time the agent starts.")
	log.Println("[host-agent] ==========================================")
}

// rotateCode publishes this machine under a fresh code and retires the old
// one. The new record is written first, so the machine is never without a
// code; if that fails the old code stays and the operator is told why.
//
// A viewer already connected under the old code is not disturbed: a live
// session lives on its own /sessions node, independent of the host record, so
// removing that record ends no session. It only stops the old code being
// resolved again. The heartbeat follows the new code from here on.
func rotateCode(ctx context.Context, registrar *host.Registrar, current *pairing, api *localapi.Server, cfg *config.Config, streak int) {
	old := current.get()
	fresh, err := registerWithFreshCode(ctx, registrar, cfg.HostName, cfg.ProjectID)
	if err != nil {
		log.Printf("[host-agent] %d connection requests in a row were not approved, but a new code could not be published (%v); the code %s stays in use",
			streak, err, formatPairingCode(old))
		return
	}
	current.set(fresh)
	if api != nil {
		api.Update(localapi.Identity{Code: fresh, Name: cfg.HostName, Version: appVersion})
	}
	if err := registrar.Unregister(ctx, old); err != nil {
		log.Printf("[host-agent] could not retire the old code %s: %v (it expires on its own after 5 minutes)", formatPairingCode(old), err)
	}
	log.Printf("[host-agent] %d connection requests in a row were not approved: whoever has the old code %s can no longer use it.",
		streak, formatPairingCode(old))
	log.Println("[host-agent]   If you were expecting someone, give them the NEW code below.")
	printBanner(fresh, cfg.SiteURL())
}

func runHeartbeat(ctx context.Context, registrar *host.Registrar, current *pairing, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := registrar.Heartbeat(ctx, current.get()); err != nil {
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
