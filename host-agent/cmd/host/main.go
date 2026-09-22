// Command host is the FreeDesk host agent. It signs in to Firebase anonymously,
// publishes this machine under a fresh 6-digit pairing code, shows that code in
// a small window (and serves it to the local web UI), and waits for incoming
// connection requests. Every request must be approved on this machine before
// WebRTC starts.
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
	"runtime"
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
	"github.com/YahyaAltintop/freedesk/host-agent/internal/ui"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/update"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/webrtc"
)

// The exe's icon, version information and application manifest live in
// winres/ and are compiled into rsrc_windows_amd64.syso by go-winres (a tool
// dependency in go.mod). The release build passes the tag as the version; the
// versions here are for development builds and follow appVersion below (a
// version resource of all zeros is one Windows does not show at all). A plain
// `go build` without this step just has no icon.
//go:generate go tool go-winres make --in winres/winres.json --out rsrc --arch amd64 --file-version 0.3.0 --product-version 0.3.0-dev

// appVersion is overwritten by the release build (-ldflags "-X main.appVersion=…").
var appVersion = "0.3.0-dev"

const (
	envFile = ".env"

	// approvalTimeout must stay comfortably below the viewer's connection
	// timeout (frontend/src/constants/webrtc.ts) so a rejection is always
	// delivered before the viewer gives up on its own.
	approvalTimeout = 45 * time.Second

	// Shutdown budget: when Windows logs off or shuts down it grants roughly
	// 5 s after WM_ENDSESSION, and the status window waits about that long
	// for the agent before letting the process go.
	sessionDrainTimeout = 3 * time.Second
	shutdownTimeout     = 4 * time.Second
	registerAttempts    = 3
)

func init() {
	// The status window's message loop has to run on the thread that created
	// the window, and Windows wants that to be the process's first thread.
	// Locking in init is how Go promises main() that thread. Harmless where
	// there is no window.
	runtime.LockOSThread()
}

func main() {
	log.SetFlags(log.LstdFlags)
	cfg, cfgErr := config.Load(envFile)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// RC_APPROVAL=console marks a headless or scripted run: no window, the
	// console carries everything as before. Every other run gets the window,
	// including one whose configuration failed, so the reason can be read.
	var win ui.Window
	if cfgErr != nil || cfg.ApprovalMode != consent.ModeConsole {
		w, err := ui.Open(ui.Options{Title: "FreeDesk", OnClose: cancel})
		if err != nil {
			log.Printf("[host-agent] no status window (%v); using the console", err)
		} else {
			win = w
		}
	}
	if win == nil {
		os.Exit(runMain(ctx, cfg, cfgErr, nil))
	}
	// From here on every log line is also a line in the window's activity pane.
	log.SetOutput(ui.Tee(win, ui.Echo()))
	log.Printf("[host-agent] FreeDesk host agent %s", appVersion)
	go func() { win.Done(runMain(ctx, cfg, cfgErr, win)) }()
	os.Exit(win.Loop())
}

// runMain turns the agent's result into an exit code. A run the operator ended
// by closing the window is a clean exit, however early it was cut off.
func runMain(ctx context.Context, cfg *config.Config, cfgErr error, win ui.Window) int {
	err := cfgErr
	if err == nil {
		err = run(ctx, cfg, win)
	}
	if err == nil || ctx.Err() != nil {
		return 0
	}
	log.Printf("[host-agent] error: %v", err)
	return 1
}

func run(parent context.Context, cfg *config.Config, win ui.Window) error {
	// Ctrl+C in a console and the window's close button end the same context.
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Newer version? Asked beside the start-up, never in front of it -------
	if cfg.UpdateCheck {
		go notifyUpdate(ctx, cfg.GitHubRepo, win)
	}

	// --- Authentication (anonymous, fresh every launch) -----------------------
	setStatus(win, "Signing in…")
	authClient := auth.NewClient(cfg.APIKey, cfg.AuthBaseURL, cfg.TokenBaseURL)
	manager, err := auth.NewManager(ctx, authClient)
	if err != nil {
		return err
	}
	log.Printf("[host-agent] authenticated (anonymous, uid=%s)", manager.UID())

	rtdb := firebase.NewRTDB(cfg.DatabaseURL, cfg.DatabaseNamespace, manager)

	// --- Pairing code + host record --------------------------------------------
	setStatus(win, "Publishing this computer's code…")
	registrar := host.NewRegistrar(rtdb, manager.UID(), appVersion)
	code, err := registerWithFreshCode(ctx, registrar, cfg.HostName, cfg.ProjectID)
	if err != nil {
		return err
	}
	log.Printf("[host-agent] host registered (name=%q)", cfg.HostName)
	current := &pairing{code: code}
	announce(win, code, cfg.SiteURL())

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
	captureOpts := capture.Options{Binary: cfg.FFmpegPath, MaxWidth: cfg.MaxWidth}
	screen := capture.NewScreenCapture(captureOpts)
	log.Printf("[host-agent] screen capture will use: %s", screen.Binary())
	// The first run of a freshly downloaded ffmpeg pays for reading a
	// 100 MB executable off the disk and for Windows Defender looking it over
	// — seconds, on some machines. Spend them now, in the background, rather
	// than in front of the first viewer's first frame.
	go screen.Warm(ctx)

	// Say where accepted files would go before anyone sends one — the operator
	// should not first learn this from a prompt. The folder itself is only
	// created if a file is ever accepted. Any half-written file left by a run
	// that was killed is cleaned up here.
	downloads := transfer.NewDest().Root()
	log.Printf("[host-agent] files you accept will be saved to: %s", downloads)
	transfer.SweepPartials(downloads)

	coordinator := session.NewCoordinator(rtdb, manager.UID(), webrtc.DefaultConfig(), captureOpts, appVersion, picker, cfg.ClipboardMode)
	inbox := session.NewInbox(rtdb, manager.UID())

	// A code that keeps producing windows nobody approves has reached the
	// wrong people. Replace it: the operator reads the new one off the
	// window, and whoever was probing the old one has to start over.
	coordinator.OnRepeatedRefusals(func(streak int) {
		rotateCode(ctx, registrar, current, api, cfg, win, streak)
	})

	var sessions sessionTracker
	setStatus(win, "Waiting for connection requests…")
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
	setStatus(win, "Stopping…")
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
		code, err := newPairingCode()
		if err != nil {
			return "", err
		}
		err = registrar.Register(ctx, code, name)
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

// announce shows the operator the one thing they need to pass on. With a
// window the code goes there and the log keeps a single line, so a console or
// a pipe still carries it; without one it is the console banner.
func announce(win ui.Window, code, site string) {
	if win == nil {
		printBanner(code, site)
		return
	}
	log.Printf("[host-agent] this computer's code: %s (web page: %s)", formatPairingCode(code), site)
	win.ShowCode(formatPairingCode(code), site)
}

// notifyUpdate asks GitHub whether a newer release exists and, if so, tells
// the operator. It runs in its own goroutine and must never slow the start or
// show an error: no network, GitHub's rate limit (a 403) or an odd answer all
// mean "nothing to say", and the agent carries on exactly as without it.
func notifyUpdate(ctx context.Context, repo string, win ui.Window) {
	latest, ok, _ := update.Check(ctx, repo, appVersion)
	if !ok {
		return
	}
	log.Printf("[host-agent] version %s is available (this is %s): %s", latest.Version, appVersion, latest.URL)
	if win != nil {
		win.ShowUpdate(latest.Version, latest.URL)
	}
}

// setStatus updates the window's status line, if there is a window.
func setStatus(win ui.Window, text string) {
	if win != nil {
		win.SetStatus(text)
	}
}

// printBanner is the console's version of the code.
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
func rotateCode(ctx context.Context, registrar *host.Registrar, current *pairing, api *localapi.Server, cfg *config.Config, win ui.Window, streak int) {
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
	log.Println("[host-agent]   If you were expecting someone, give them the NEW code.")
	announce(win, fresh, cfg.SiteURL())
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

// pairingCodeDigits is the length of a code. Six digits are enough for an
// address that lives only while the agent runs and is replaced on every
// start; the database rules (firebase/database.rules.json) and the viewer
// (frontend/src/utils/pairingCode.ts) agree on this number.
const pairingCodeDigits = 6

// newPairingCode returns random digits (leading zeros allowed). The code is
// both this machine's address and its database key.
func newPairingCode() (string, error) {
	limit := new(big.Int).Exp(big.NewInt(10), big.NewInt(pairingCodeDigits), nil)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", fmt.Errorf("could not generate a code: %w", err)
	}
	return fmt.Sprintf("%0*d", pairingCodeDigits, n), nil
}

// formatPairingCode renders "123456" as the human-friendly "123 - 456".
func formatPairingCode(code string) string {
	if len(code) != pairingCodeDigits {
		return code
	}
	return code[:3] + " - " + code[3:]
}
