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
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/auth"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/capture"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/config"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/consent"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/diag"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/firebase"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/firewall"
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

	// firewallCheckTimeout bounds the PowerShell query behind the firewall
	// warning; a machine where that takes longer simply gets no warning.
	// firewallRecheck is how often the question is asked again while the
	// answer keeps viewers out, so the warning clears once the operator
	// has clicked Allow.
	firewallCheckTimeout = 30 * time.Second
	firewallRecheck      = 45 * time.Second
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
		// A console run is a developer's run: the technical channel goes
		// where the log goes.
		diag.SetOutput(os.Stderr)
		os.Exit(runMain(ctx, cfg, cfgErr, nil))
	}
	// From here on every log line is also a line in the window's activity
	// pane. The developer's channel reaches the console, if there is one, and
	// the pane too only when asked (RC_DIAG=on): the pane is for the operator,
	// and a Google error code is not something they can act on.
	echo := ui.Echo()
	log.SetOutput(ui.Tee(win, echo))
	if cfgErr == nil && cfg.Diagnostics {
		diag.SetOutput(ui.Tee(win, echo))
	} else {
		diag.SetOutput(echo)
	}
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
	var se *startupError
	if errors.As(err, &se) {
		diag.Printf("[host-agent] %s", se.detail)
	}
	return 1
}

// startupError is a failure the operator is told about in plain words. The
// detail — what a developer would want to know — goes to the developer's
// channel, and the cause stays unwrappable for errors.Is.
type startupError struct {
	plain  string
	detail string
	cause  error
}

func (e *startupError) Error() string { return e.plain }
func (e *startupError) Unwrap() error { return e.cause }

func run(parent context.Context, cfg *config.Config, win ui.Window) error {
	// Ctrl+C in a console and the window's close button end the same context.
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Network ---------------------------------------------------------------
	// The one UDP socket every connection will use is opened before anything
	// else. Windows asks whether to let a program through its firewall the
	// moment the program first listens: better now, while the operator is
	// looking at the window, than later behind a consent prompt where the
	// question is missed and every viewer's packets are dropped in silence.
	iceCfg := webrtc.DefaultConfig()
	if mux, err := webrtc.ListenUDP(cfg.UDPPort); err != nil {
		log.Println("[host-agent] the usual connection port could not be opened; connections will use whatever port is free")
		diag.Printf("[host-agent] UDP port %d: %v", cfg.UDPPort, err)
	} else {
		defer mux.Close()
		iceCfg.UDPMux = mux.Mux()
		log.Printf("[host-agent] connections arrive on UDP port %d", mux.Port())
	}
	go watchFirewall(ctx, win)

	// --- Newer version? Asked beside the start-up, never in front of it -------
	if cfg.UpdateCheck {
		go notifyUpdate(ctx, cfg.GitHubRepo, win)
	}

	// --- Authentication (anonymous, fresh every launch) -----------------------
	setStatus(win, "Signing in…")
	authClient := auth.NewClient(cfg.APIKey, cfg.AuthBaseURL, cfg.TokenBaseURL)
	manager, err := auth.NewManager(ctx, authClient)
	if err != nil {
		return &startupError{
			plain:  "Could not sign in. Check that this computer is online, then start the program again.",
			detail: fmt.Sprintf("sign-in failed: %v", err),
			cause:  err,
		}
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
		log.Println("[host-agent] the FreeDesk web page opened on this computer will not be able to show the code by itself; read it from this window instead")
		diag.Printf("[host-agent] local code endpoint on port %d: %v", cfg.LocalAPIPort, err)
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

	coordinator := session.NewCoordinator(rtdb, manager.UID(), iceCfg, captureOpts, appVersion, picker, cfg.ClipboardMode)
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
		log.Println("[host-agent] the code could not be withdrawn; it stops working on its own within a few minutes")
		diag.Printf("[host-agent] unregister: %v", err)
	}
	if err := manager.DeleteAccount(shutdownCtx); err != nil {
		diag.Printf("[host-agent] could not delete the anonymous identity: %v", err)
	}
	log.Println("[host-agent] shut down.")
	return nil
}

// watchFirewall tells the operator when Windows Defender Firewall would keep
// every viewer out: the one thing on the machine itself that does, and one
// nobody thinks of, because the program looks healthy in every other way and
// a viewer only sees "could not connect". The question Windows asks at
// start-up is easy to miss or to cancel, and an old allow rule may cover the
// wrong kind of network. The check runs PowerShell, so it takes a few seconds
// and happens in the background; while the answer is bad it is asked again
// now and then, so the warning clears once Allow has been clicked.
func watchFirewall(ctx context.Context, win ui.Window) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	warned := false
	for {
		st, err := checkFirewall(ctx, exe)
		if err != nil {
			diag.Printf("[host-agent] firewall check: %v", err)
			return
		}
		diag.Printf("[host-agent] firewall: enabled=%v blocked=%v allowed=%v elsewhere=%v network=%q",
			st.Enabled, st.Blocked, st.Allowed, st.Elsewhere, st.Network)
		if st.Reachable() {
			if warned && ctx.Err() == nil {
				log.Println("[host-agent] Windows now lets other computers connect to this one.")
				setStatus(win, "Waiting for connection requests…")
			}
			return
		}
		if !warned {
			warned = true
			explainFirewall(st)
			setStatus(win, "Windows is blocking connections to this computer")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(firewallRecheck):
		}
	}
}

func checkFirewall(ctx context.Context, exe string) (firewall.Status, error) {
	ctx, cancel := context.WithTimeout(ctx, firewallCheckTimeout)
	defer cancel()
	return firewall.Check(ctx, exe)
}

// explainFirewall says what Windows is doing and what to click, in words for
// the person at the machine.
func explainFirewall(st firewall.Status) {
	switch {
	case st.Blocked:
		log.Println("[host-agent] Windows is blocking this program's network access, so nobody can connect to this computer.")
		log.Println("[host-agent]   Windows Security > Firewall & network protection > Allow an app through firewall:")
		log.Println("[host-agent]   remove the entry that blocks FreeDesk, then allow it with both Private and Public ticked.")
	case len(st.Elsewhere) > 0:
		log.Printf("[host-agent] Windows allows this program only on %s networks, but this computer is on a %s network now, so nobody can connect to it.",
			strings.Join(st.Elsewhere, " and "), st.Network)
		log.Println("[host-agent]   Windows Security > Firewall & network protection > Allow an app through firewall:")
		log.Println("[host-agent]   find FreeDesk (freedesk.exe) and tick both Private and Public.")
	default:
		log.Println("[host-agent] Windows has not yet allowed this program to receive connections, so nobody can connect to this computer.")
		log.Println("[host-agent]   If a Windows Security window is asking about FreeDesk, click \"Allow access\".")
		log.Println("[host-agent]   Otherwise: Windows Security > Firewall & network protection > Allow an app through firewall >")
		log.Println("[host-agent]   Allow another app... > choose freedesk.exe, and tick both Private and Public.")
	}
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
			return "", &startupError{
				plain:  "Could not reach the service. Check that this computer is online, then start the program again.",
				detail: fmt.Sprintf("registering the host record: %v", err),
				cause:  err,
			}
		}
		if attempt < registerAttempts {
			log.Println("[host-agent] that code is taken; picking another")
			continue
		}
		return "", registrationDeniedError(attempt, projectID, err)
	}
}

// registrationDeniedError explains a run of consecutive denials. The operator
// hears that it did not work and may pass; the developer's channel gets the
// diagnosis: several random codes being taken at the same moment is
// practically impossible, so the project's security rules are almost
// certainly missing.
func registrationDeniedError(attempts int, projectID string, err error) error {
	return &startupError{
		plain: "This computer could not be made available for connections right now. Please try again in a few minutes.",
		detail: fmt.Sprintf("the database refused to publish this computer %d times in a row (%v): "+
			"the security rules are probably not deployed to project %q — run `firebase deploy --only database` in the firebase folder (see firebase/README.md)",
			attempts, err, projectID),
		cause: err,
	}
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
		log.Printf("[host-agent] %d connection requests in a row were not approved, but a new code could not be published; the code %s stays in use",
			streak, formatPairingCode(old))
		diag.Printf("[host-agent] rotating the code: %v", err)
		return
	}
	current.set(fresh)
	if api != nil {
		api.Update(localapi.Identity{Code: fresh, Name: cfg.HostName, Version: appVersion})
	}
	if err := registrar.Unregister(ctx, old); err != nil {
		log.Printf("[host-agent] the old code %s could not be retired at once; it expires on its own after 5 minutes", formatPairingCode(old))
		diag.Printf("[host-agent] retiring the old code: %v", err)
	}
	log.Printf("[host-agent] %d connection requests in a row were not approved: whoever has the old code %s can no longer use it.",
		streak, formatPairingCode(old))
	log.Println("[host-agent]   If you were expecting someone, give them the NEW code.")
	announce(win, fresh, cfg.SiteURL())
}

func runHeartbeat(ctx context.Context, registrar *host.Registrar, current *pairing, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// The operator hears once when contact is lost and once when it is back;
	// the beat-by-beat detail is for the developer's channel.
	down := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := registrar.Heartbeat(ctx, current.get())
			if ctx.Err() != nil {
				return
			}
			switch {
			case err != nil && !down:
				down = true
				log.Println("[host-agent] lost contact with the service; the code stops working until it is back")
				diag.Printf("[host-agent] heartbeat: %v", err)
			case err != nil:
				diag.Printf("[host-agent] heartbeat: %v", err)
			case down:
				down = false
				log.Println("[host-agent] back in contact with the service")
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
