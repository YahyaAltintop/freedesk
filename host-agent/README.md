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
| `RC_APPROVAL` | `dialog` (default on Windows) or `console` (`y` + Enter, and no status window; used by tests and headless runs) |
| `RC_LOCAL_PORT` | Loopback port for the code endpoint (default 47800; must match the viewer) |
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
When start-up fails, the window shows **Could not start** and, in its Activity box, the reason and (for the known ones) what to fix; it stays open until you close it. With `RC_APPROVAL=console` the message goes to the console instead and the agent exits at once.

| Message | Meaning |
|---------|---------|
| `API_KEY_HTTP_REFERRER_BLOCKED` | The API key is limited to websites. The agent needs a key with no application restrictions (`FIREBASE_AGENT_API_KEY` for releases). |
| `API_KEY_SERVICE_BLOCKED` | The key's API restrictions exclude *Identity Toolkit API* / *Token Service API*. Allow both or don't restrict the key. |
| `ADMIN_ONLY_OPERATION` | Anonymous sign-in is disabled: Firebase Console → Authentication → Sign-in method → Anonymous. |
| `refused to publish this computer 3 times in a row` | The security rules are not deployed: `cd firebase && firebase deploy --only database`. |

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
