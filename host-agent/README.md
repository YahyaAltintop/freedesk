# Host Agent — Go + Pion WebRTC

The program that runs on the Windows computer being controlled. It signs in to Firebase **anonymously**, shows a fresh **6-digit pairing code** in a small window, serves that code to the web UI on the same machine (`http://127.0.0.1:47800/identity`, FreeDesk web origins only), and waits for connection requests. Each request is approved in a native **Yes/No window**; after approval the screen is streamed over WebRTC and incoming input is applied through the Windows API.

Nothing is stored on disk: every launch is a new identity and a new code, and both are deleted on exit.

## Prerequisites
- **Go 1.26+** — <https://go.dev/dl/> (go.mod's toolchain line names the exact release; the go command fetches it on its own)
- **ffmpeg** with `gdigrab` and `libvpx` (5.1 or newer). Release zips ship it as `ffmpeg\ffmpeg.exe` next to the agent; when running from source put it there (or right next to the binary), install it on the PATH (`winget install Gyan.FFmpeg`), or set `RC_FFMPEG_PATH`. Without ffmpeg the agent runs **without video** (the connection and the input channel still work).

## Layout
```
host-agent/
├── cmd/host/main.go        entry point: status window, auth, registration, inbox, shutdown
├── cmd/host/winres/        the exe's icon, version information and manifest (go-winres)
├── internal/
│   ├── auth/               Identity Toolkit REST (anonymous signUp, token refresh, self-delete)
│   ├── firebase/           Realtime Database REST + SSE client
│   ├── host/               /hosts/{code} registration + heartbeat + removal
│   ├── session/            inbox stream, per-request coordinator (approval → WebRTC → cleanup)
│   ├── consent/            approval: native dialog (Windows) or console prompt
│   ├── ui/                 the status window (Win32): code, status, activity log
│   ├── signaling/          offer/answer/ICE over the session node (one demultiplexed stream)
│   ├── webrtc/             Pion PeerConnection, video track, RTCP drain
│   ├── capture/            ffmpeg gdigrab → VP8 (IVF) with real capture timestamps
│   ├── input/              DataChannel events → SendInput, held-key tracking
│   ├── localapi/           127.0.0.1 endpoint serving the code to the web UI
│   └── config/             .env / environment / build-time embedded values
└── release/README.txt      the text shipped inside the release zip
```

## Configuration
Release builds have the Firebase values embedded (`-ldflags -X`, see `.github/workflows/release.yml`). When running from source, copy `.env.example` to `.env` or set environment variables (they override the file and the embedded values):

| Variable | Meaning |
|----------|---------|
| `RC_FIREBASE_API_KEY`, `RC_FIREBASE_PROJECT_ID`, `RC_FIREBASE_DB_URL` | The Firebase project (same as the viewer). The key must have **no application restrictions**: the agent sends no website referrer, so a website-restricted key rejects it (see [firebase/README.md → API keys](../firebase/README.md#api-keys)) |
| `RC_HOST_NAME` | Display name (default: machine name, max 64 chars) |
| `RC_FFMPEG_PATH` | ffmpeg executable (default: `ffmpeg\ffmpeg.exe` or `ffmpeg.exe` next to the agent, else PATH) |
| `RC_MAX_WIDTH` | Widest frame the encoder is given, in pixels (default 1920). gdigrab hands over the whole desktop, every monitor of it; anything wider is scaled down to this, aspect kept, so the 2 Mbit/s video budget still covers what it encodes |
| `RC_APPROVAL` | `dialog` (default on Windows) or `console` (`y` + Enter, and no status window; used by tests and headless runs) |
| `RC_LOCAL_PORT` | Loopback port for the code endpoint (default 47800; must match the viewer) |
| `RC_UDP_PORT` | The one UDP port every connection arrives on (default 47801; `0` = any free port, and a taken port falls back to that). Opened first thing at start-up, so Windows asks its firewall question then, while the operator is looking, and so an administrator can allow a known port |
| `RC_DIAG` | `off` (default) or `on`: mirror the developer's channel — Google error codes, HTTP statuses, ICE candidate summaries, the console commands that fix things — into the window's Activity box. It always reaches the console when the agent was started from one; the window itself only ever shows plain sentences |
| `RC_UPDATE_CHECK` | `on` (default) or `off`: at start-up the agent asks `api.github.com` once, in the background, whether a newer release exists and shows a purple **Update to …** button if so. It never downloads anything; any failure (offline, GitHub's rate limit) is silent |
| `RC_GITHUB_REPO` | `owner/name` whose releases that check looks at (default: the repository that built the exe, `YahyaAltintop/freedesk` for source builds) |
| `RC_HOSTING_SITE` | Firebase Hosting site id when it differs from the project id (`firebase.json` → `hosting.site`); release builds read it from `firebase.json` automatically |
| `RC_WEB_ORIGINS` | Extra browser origins allowed to read the code, comma separated (the project's and the site's `web.app`/`firebaseapp.com` origins and `localhost:9205` are always allowed) |
| `FIREBASE_AUTH_EMULATOR_HOST`, `FIREBASE_DATABASE_EMULATOR_HOST` | Point the agent at the Firebase emulators |

## Build & run
```bash
cd host-agent
go generate ./cmd/host                # icon, version info, manifest — optional for development
go build -o bin/freedesk.exe ./cmd/host
./bin/freedesk.exe
```
A development build is a console program that also opens the status window, so the log shows in both. The release adds `-ldflags "-H=windowsgui"` (no console): `go build -ldflags "-H=windowsgui" -o bin/freedesk.exe ./cmd/host` reproduces it.

Stop it by closing the window or with Ctrl+C: the agent finishes the current session, removes its host record and deletes its anonymous account (about 3 seconds); the window says "Stopping…" meanwhile.

## Troubleshooting
The window talks to the operator in plain sentences and never shows an error code, a status or a console command: none of those is something the person who double-clicked the exe can act on. The technical detail behind each sentence goes to the **developer's channel**: the console, when the agent was started from one, and the window's Activity box as well when `RC_DIAG=on` (a line in a `.env` next to the exe). When start-up fails the window shows **Could not start** and stays open until closed; with `RC_APPROVAL=console` the agent exits at once instead.

| The window says | What it means, and the fix |
|-----------------|----------------------------|
| *Could not sign in.* | Google rejected the anonymous sign-in. The developer's channel names the reason: `API_KEY_HTTP_REFERRER_BLOCKED` (the key is limited to websites; the agent needs one with no application restrictions, `FIREBASE_AGENT_API_KEY` for releases), `API_KEY_SERVICE_BLOCKED` (the key's API restrictions exclude *Identity Toolkit API* / *Token Service API*), `ADMIN_ONLY_OPERATION` (anonymous sign-in is disabled: Firebase Console → Authentication → Sign-in method → Anonymous). |
| *This computer could not be made available for connections right now.* | The database refused the host record three times in a row. The security rules are not deployed (`cd firebase && firebase deploy --only database`), or this build points at a project other than the one the rules are in. |
| *Could not reach the service.* | The database could not be reached at all: no internet, or a proxy in the way. |
| *Windows has not yet allowed this program to receive connections.* | Windows Defender Firewall has no inbound allow rule for this `freedesk.exe` (rules are per path: a zip unpacked somewhere new starts from nothing). The question Windows asks at start-up was missed, or the account is not an administrator and cannot answer it. Windows Security → Firewall & network protection → Allow an app through firewall → Allow another app… → `freedesk.exe`, Private and Public. The agent asks Windows again every 45 s and clears the warning by itself. |
| *Windows allows this program only on Public networks, but this computer is on a Private network now.* | The allow rule exists, but for the wrong kind of network: only one of Private / Public was ticked. Tick both. |
| *Windows is blocking this program's network access.* | Windows Defender Firewall holds an inbound **block** rule for `freedesk.exe`: its "allow this app?" question was answered with Cancel, or the program was blocked by hand. A block rule beats every allow rule. Remove the blocking entry, then allow FreeDesk on Private and Public. |
| *The other computer could not reach this one.* | ICE found no working path in 30 s. On the same network: the firewall question at start-up was not answered with Allow (see the line above). On different networks: FreeDesk uses STUN only, and a symmetric NAT or carrier-grade NAT (mobile data, many shared and corporate networks) cannot be crossed without a TURN relay, which the project deliberately does not run. The developer's channel lists which candidate types each side had: no `srflx` means the STUN server was unreachable; only `mdns` from the viewer means its LAN address could not be resolved. |
| *Screen sharing could not start.* | `ffmpeg\ffmpeg.exe` is not next to the agent (or `RC_FFMPEG_PATH` points at nothing). The connection and the mouse/keyboard still work. |
| *Lost contact with the service.* | A heartbeat failed; the code stops resolving after 90 s and comes back on its own when the connection does. |

## Tests
```bash
go test ./...                                    # unit tests (input handler, inbox, demux, IVF timing, CORS)
RC_INPUT_INTEGRATION=1 go test ./internal/input/  # moves the real mouse cursor once
RC_DIALOG_INTEGRATION=1 go test ./internal/consent/  # shows two short dialogs
RC_UI_INTEGRATION=1 go test ./internal/ui/           # opens the status window three times, briefly
cd ../firebase && firebase emulators:exec --only "auth,database" --project demo-rcapp \
  "cd ../host-agent && go test ./internal/firebase/ ./internal/webrtc/ -run Emulator -v"
```
The emulator run checks the security rules end to end and performs a real host↔viewer WebRTC negotiation through the database.

For the shared contract with the viewer: [`../docs/PROTOCOL.md`](../docs/PROTOCOL.md). Design and rationale: [`../docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md).
