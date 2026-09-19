package session

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	pion "github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/capture"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/clipboard"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/consent"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/firebase"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/input"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/protocol"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/signaling"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/transfer"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/webrtc"
)

const (
	statusWaiting    = "waiting"
	statusConnecting = "connecting"
	statusConnected  = "connected"
	statusEnded      = "ended"

	// snapshotTimeout bounds how long we wait for the session node's initial
	// sync after the inbox announced it.
	snapshotTimeout = 5 * time.Second
	cleanupTimeout  = 10 * time.Second
)

// Approver decides whether a request the operator was asked about may
// proceed. It must return false when ctx is cancelled (the viewer withdrew the
// request). Mirrors consent.Approver so this package depends on the question,
// not on how it is asked.
type Approver interface {
	Ask(ctx context.Context, p consent.Prompt) consent.Answer
}

// maxUnansweredPrompts is how many file questions may go unanswered in a row
// before this session stops asking them.
//
// One prompt at a time already bounds the rate to one window per timeout, but
// a bound is not the same as an end: an operator who has walked away from a
// connected machine would otherwise be offered a fresh window every 45 seconds
// for as long as the viewer cares to keep asking. Three is enough to absorb a
// coffee break and short enough that nobody comes back to a screen full of
// them.
const maxUnansweredPrompts = 3

// fileApprover asks the operator about incoming files, turning what the
// transfer package knows into the question the consent package poses.
//
// It also holds this session's prompt-fatigue policy. That belongs here rather
// than in the transfer package: the resource being protected is the operator's
// attention, which is a consent concern, and the transfer state machine should
// not have opinions about how often a human can be interrupted.
type fileApprover struct {
	approver  Approver
	viewerUID string

	mu         sync.Mutex
	unanswered int
}

func (a *fileApprover) AskFiles(ctx context.Context, files []transfer.FileOffer, folder string) bool {
	a.mu.Lock()
	quiet := a.unanswered >= maxUnansweredPrompts
	a.mu.Unlock()
	if quiet {
		// Refused as if the operator said no. Telling the viewer "nobody is
		// there" would answer a question they should not get to ask.
		return false
	}

	offers := make([]consent.FileOffer, len(files))
	for i, f := range files {
		offers[i] = consent.FileOffer{Name: f.Name, Size: f.Size}
	}
	answer := a.approver.Ask(ctx, consent.IncomingFiles(a.viewerUID, offers, folder))

	a.mu.Lock()
	if answer == consent.Unanswered {
		a.unanswered++
		if a.unanswered == maxUnansweredPrompts {
			log.Printf("[session] %d file requests went unanswered; not asking again this session", a.unanswered)
		}
	} else {
		// Counted in a row, not in total. Any answer at all proves somebody is
		// at the machine, and a session where the operator missed three
		// questions over an hour is not the one this is defending against.
		a.unanswered = 0
	}
	a.mu.Unlock()

	return answer.OK()
}

// Coordinator runs the host side of connection sessions: it validates each
// request, obtains operator approval, negotiates the WebRTC connection over
// the session node and reflects the lifecycle in that node
// (connecting → connected → ended, then removal). Only one session runs at a
// time; further requests are declined while one is pending or active.
type Coordinator struct {
	rtdb       *firebase.RTDB
	ownerUID   string
	cfg        webrtc.Config
	ffmpegPath string
	// picker lets the operator choose files to send. Nil-safe: a build or a
	// mode without one simply never advertises downloads.
	picker consent.FilePicker
	// clipboardMode is RC_CLIPBOARD: "text" or "off".
	clipboardMode string
	// version is announced to the viewer in the greeting, so it can explain
	// what an old agent is missing instead of just disabling a button.
	version string

	mu     sync.Mutex
	active bool
}

// NewCoordinator builds a coordinator for the authenticated owner, with the
// ICE config and the ffmpeg executable to use for screen capture (empty =
// "ffmpeg" from PATH).
func NewCoordinator(rtdb *firebase.RTDB, ownerUID string, cfg webrtc.Config, ffmpegPath, version string, picker consent.FilePicker, clipboardMode string) *Coordinator {
	return &Coordinator{
		rtdb: rtdb, ownerUID: ownerUID, cfg: cfg,
		ffmpegPath: ffmpegPath, version: version, picker: picker,
		clipboardMode: clipboardMode,
	}
}

// capabilities lists what this agent can do, for the greeting. It is built
// from what the session actually wires up, so the list cannot drift from the
// code: a viewer that sees a capability here can rely on it being handled.
func (c *Coordinator) capabilities() []string {
	caps := []string{protocol.CapFileSend}
	// Only claimed where there is a picker to honour it: without one the viewer
	// would offer a button that can never do anything.
	if c.picker != nil && c.picker.Available() {
		caps = append(caps, protocol.CapFileRecv)
	}
	if c.clipboardMode == clipboard.ModeText {
		caps = append(caps, protocol.CapClipText)
	}
	return caps
}

// Run handles one request from approval to the end of the connection. It is
// intended to run in its own goroutine per request and returns when the
// session is over and cleaned up.
func (c *Coordinator) Run(ctx context.Context, req Request, approver Approver) {
	log.Printf("[session] %s: request from viewer %s", req.ID, req.ViewerUID)

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	transport := signaling.NewRTDBTransport(c.rtdb, req.ID, signaling.Host)
	if err := transport.Start(sessionCtx); err != nil {
		if firebase.IsPermissionDenied(err) {
			log.Printf("[session] %s: request already withdrawn", req.ID)
		} else {
			log.Printf("[session] %s: could not open session: %v", req.ID, err)
		}
		return
	}

	// The inbox entry is untrusted on its own: the session node (which only its
	// creator could have written) must agree with it and be addressed to us.
	var snap signaling.Snapshot
	select {
	case snap = <-transport.Snapshot():
	case <-transport.Gone():
		log.Printf("[session] %s: request withdrawn before it was read", req.ID)
		return
	case <-time.After(snapshotTimeout):
		log.Printf("[session] %s: session node did not arrive in time", req.ID)
		return
	case <-sessionCtx.Done():
		return
	}
	if snap.ViewerUID != req.ViewerUID || snap.OwnerUID != c.ownerUID || snap.Status != statusWaiting {
		log.Printf("[session] %s: ignoring request whose session does not match (viewer=%s owner=%s status=%s)",
			req.ID, snap.ViewerUID, snap.OwnerUID, snap.Status)
		return
	}

	release, ok := c.acquire()
	if !ok {
		log.Printf("[session] %s: declined, another session is pending or active", req.ID)
		c.finish(req)
		return
	}
	defer release()

	_ = c.setStatus(sessionCtx, req.ID, statusConnecting)

	// Ask the operator, but stop asking the moment the viewer gives up (the
	// node vanishes through onDisconnect, or the viewer marks it ended).
	promptCtx, cancelPrompt := context.WithCancel(sessionCtx)
	withdrawn := make(chan struct{})
	go func() {
		defer cancelPrompt()
		for {
			select {
			case <-transport.Gone():
				close(withdrawn)
				return
			case st := <-transport.Status():
				if st == statusEnded {
					close(withdrawn)
					return
				}
			case <-promptCtx.Done():
				return
			}
		}
	}()
	approved := approver.Ask(promptCtx, consent.ConnectRequest(req.ViewerUID, c.clipboardMode == clipboard.ModeText)).OK()
	cancelPrompt()

	select {
	case <-withdrawn:
		log.Printf("[session] %s: request withdrawn by the viewer", req.ID)
		c.finish(req)
		return
	default:
	}
	if !approved {
		log.Printf("[session] %s: request rejected", req.ID)
		c.finish(req)
		return
	}

	c.connect(sessionCtx, req, transport, approver)
	c.finish(req)
}

// connect negotiates the WebRTC connection and supervises it until it ends.
func (c *Coordinator) connect(ctx context.Context, req Request, transport *signaling.RTDBTransport, approver Approver) {
	log.Printf("[session] %s: establishing connection…", req.ID)

	closed := make(chan struct{})
	var once sync.Once
	markClosed := func() { once.Do(func() { close(closed) }) }

	// The clipboard poller runs on its own thread and needs somewhere to send;
	// the channel only exists once the peer connection hands it over.
	var fileChannel atomic.Pointer[*pion.DataChannel]

	// One input handler per session: it tracks what the viewer holds down so
	// nothing stays pressed on this machine once the viewer is gone.
	inputHandler := input.NewHandler()

	// One transfer handler per session, for the same reason: whatever it was
	// writing when the viewer left must not survive the session either.
	transfers := transfer.NewSession(ctx, &fileApprover{approver: approver, viewerUID: req.ViewerUID},
		c.picker, func(format string, args ...any) { log.Printf(format, args...) })

	// The clipboard rides the file channel too: its text can run to a couple of
	// hundred kilobytes, which on the ordered input channel would queue ahead of
	// every mouse move behind it.
	var clip *clipboard.Sync
	if c.clipboardMode == clipboard.ModeText {
		var err error
		clip, err = clipboard.NewSync(c.clipboardMode, func(raw string) {
			if ch := fileChannel.Load(); ch != nil {
				_ = (*ch).SendText(raw)
			}
		})
		if err != nil {
			// Non-fatal, like a failed capture: the session is still worth
			// having without it.
			log.Printf("[session] %s: clipboard sharing unavailable: %v", req.ID, err)
		}
	}
	if clip != nil {
		// Files the operator copies are offered to the viewer the same way the
		// file picker's are: names and sizes only, with nothing read until the
		// viewer asks for it.
		var clipOffer atomic.Uint64
		clip.OnFiles(func(paths []string) {
			transfers.OfferFiles(fmt.Sprintf("clip-%d", clipOffer.Add(1)), paths)
		})
		// And files the viewer pastes go on the operator's clipboard once they
		// are safely saved, so Ctrl+V in Explorer works.
		transfers.OnPasted(clip.PutFiles)
	}

	hooks := webrtc.Hooks{
		// One callback per channel of the session; what a channel carries is
		// decided by its label, never by arrival order.
		OnDataChannel: func(dc *pion.DataChannel) {
			// One OnOpen per channel: pion's OnOpen sets the handler rather
			// than adding to it, so a second registration would replace this.
			dc.OnOpen(func() {
				log.Printf("[session] %s: DataChannel '%s' opened", req.ID, dc.Label())
				if dc.Label() != webrtc.InputChannelLabel {
					return
				}
				// Announce what this agent can do, unprompted. A viewer has no
				// other way to find out: an agent from before this greeting
				// existed never writes to the channel at all, so its silence is
				// the signal that it is old.
				hello := protocol.NewHello(c.version, c.capabilities()...)
				if err := dc.SendText(hello.Encode()); err != nil {
					log.Printf("[session] %s: could not send hello: %v", req.ID, err)
				}
			})
			switch dc.Label() {
			case webrtc.InputChannelLabel:
				dc.OnMessage(func(msg pion.DataChannelMessage) {
					// Input events are JSON text frames. A binary frame here is
					// not something this channel carries, and feeding it to a
					// JSON parser would only fail silently.
					if msg.IsString {
						inputHandler.Handle(msg.Data)
					}
				})
				dc.OnClose(func() {
					inputHandler.ReleaseAll()
				})
			case webrtc.FileChannelLabel:
				transfers.Attach(dc)
				fileChannel.Store(&dc)
				dc.OnMessage(func(msg pion.DataChannelMessage) {
					// IsString is the only thing separating a control frame
					// from a chunk of a file.
					if msg.IsString && clip != nil && peekType(msg.Data) == clipboard.Type {
						clip.Handle(msg.Data)
						return
					}
					transfers.Handle(msg.Data, msg.IsString)
				})
				dc.OnClose(func() {
					transfers.Close()
					if clip != nil {
						clip.Stop()
					}
				})
			}
		},
		OnState: func(st pion.PeerConnectionState) {
			log.Printf("[session] %s: state=%s", req.ID, st.String())
			if st == pion.PeerConnectionStateFailed || st == pion.PeerConnectionStateClosed {
				markClosed()
			}
		},
	}

	track, err := webrtc.NewVideoTrack()
	if err != nil {
		log.Printf("[session] %s: could not create video track: %v", req.ID, err)
		return
	}

	pc, err := webrtc.Connect(ctx, signaling.Host, transport, c.cfg, hooks, track)
	if err != nil {
		log.Printf("[session] %s: could not establish connection: %v", req.ID, err)
		return
	}
	log.Printf("[session] %s: CONNECTED", req.ID)
	_ = c.setStatus(ctx, req.ID, statusConnected)

	c.streamScreen(ctx, req.ID, track)

	select {
	case <-closed:
	case <-transport.Gone():
		log.Printf("[session] %s: viewer left", req.ID)
	case <-ctx.Done():
	}
	inputHandler.ReleaseAll()
	transfers.Close()
	if clip != nil {
		clip.Stop()
	}
	_ = pc.Close()
	log.Printf("[session] %s: connection ended", req.ID)
}

// acquire reserves the single session slot.
func (c *Coordinator) acquire() (release func(), ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		return nil, false
	}
	c.active = true
	return func() {
		c.mu.Lock()
		c.active = false
		c.mu.Unlock()
	}, true
}

// finish marks the session ended and removes its node and inbox entry, using
// a fresh context so cleanup still runs after the session context is
// cancelled. Every step is best-effort: the viewer may already have removed
// the node, which the rules treat as a successful delete.
func (c *Coordinator) finish(req Request) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	_ = c.setStatus(ctx, req.ID, statusEnded)
	_ = c.rtdb.Delete(ctx, pathSessions+"/"+req.ID)
	_ = c.rtdb.Delete(ctx, pathInbox+"/"+c.ownerUID+"/"+req.ID)
}

func (c *Coordinator) setStatus(ctx context.Context, sessionID, status string) error {
	return c.rtdb.Put(ctx, pathSessions+"/"+sessionID+"/status", status)
}

// streamScreen captures the desktop and writes encoded frames into the video
// track. If ffmpeg is unavailable the connection stays up without video (the
// input DataChannel still works), so video failure is non-fatal.
func (c *Coordinator) streamScreen(ctx context.Context, sessionID string, track *pion.TrackLocalStaticSample) {
	screen := capture.NewScreenCapture(c.ffmpegPath)
	frames, err := screen.Start(ctx)
	if err != nil {
		log.Printf("[session] %s: could not start screen capture (is ffmpeg installed?): %v", sessionID, err)
		return
	}
	log.Printf("[session] %s: screen streaming started", sessionID)

	go func() {
		for frame := range frames {
			// Pion advances the RTP clock by Duration AFTER writing a sample, so
			// first move the clock forward by the real capture gap (an empty
			// sample only skips ticks, it sends nothing) and then write the
			// frame with no further advance. RTP timestamps then match the
			// actual capture times instead of a nominal 30 fps.
			if frame.Elapsed > 0 {
				if err := track.WriteSample(media.Sample{Duration: frame.Elapsed}); err != nil {
					if ctx.Err() == nil {
						log.Printf("[session] %s: could not advance video clock: %v", sessionID, err)
					}
					return
				}
			}
			if err := track.WriteSample(media.Sample{Data: frame.Data}); err != nil {
				if ctx.Err() == nil {
					log.Printf("[session] %s: could not write video sample: %v", sessionID, err)
				}
				return
			}
		}
	}()
}
