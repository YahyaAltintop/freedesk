# Host Agent — Go + Pion WebRTC

The program that runs on the Windows computer being controlled. It signs in to Firebase **anonymously**, prints a fresh **9-digit pairing code**, serves that code to the web UI on the same machine (`http://127.0.0.1:47800/identity`, FreeDesk web origins only), and waits for connection requests. Each request is approved in a native **Yes/No window**; after approval the screen is streamed over WebRTC and incoming input is applied through the Windows API.

Nothing is stored on disk: every launch is a new identity and a new code, and both are deleted on exit.

## Prerequisites
- **Go 1.26+** — <https://go.dev/dl/> (go.mod's toolchain line names the exact release; the go command fetches it on its own)
- **ffmpeg** with `gdigrab` and `libvpx` (5.1 or newer). Release zips ship `ffmpeg.exe` next to the agent; when running from source either put `ffmpeg.exe` next to the built binary, install it on the PATH (`winget install Gyan.FFmpeg`), or set `RC_FFMPEG_PATH`. Without ffmpeg the agent runs **without video** (the connection and the input channel still work).

## Layout
```
host-agent/
├── cmd/host/main.go        entry point: auth, registration, inbox, shutdown
├── internal/
│   ├── auth/               Identity Toolkit REST (anonymous signUp, token refresh, self-delete)
│   ├── firebase/           Realtime Database REST + SSE client
│   ├── host/               /hosts/{code} registration + heartbeat + removal
│   ├── session/            inbox stream, per-request coordinator (approval → WebRTC → cleanup)
│   ├── consent/            approval: native dialog (Windows) or console prompt
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
| `RC_FFMPEG_PATH` | ffmpeg executable (default: `ffmpeg.exe` next to the agent, else PATH) |
| `RC_APPROVAL` | `dialog` (default on Windows) or `console` (`y` + Enter; used by tests and headless runs) |
| `RC_LOCAL_PORT` | Loopback port for the code endpoint (default 47800; must match the viewer) |
| `RC_HOSTING_SITE` | Firebase Hosting site id when it differs from the project id (`firebase.json` → `hosting.site`); release builds read it from `firebase.json` automatically |
| `RC_WEB_ORIGINS` | Extra browser origins allowed to read the code, comma separated (the project's and the site's `web.app`/`firebaseapp.com` origins and `localhost:9205` are always allowed) |
| `FIREBASE_AUTH_EMULATOR_HOST`, `FIREBASE_DATABASE_EMULATOR_HOST` | Point the agent at the Firebase emulators |

## Build & run
```bash
cd host-agent
go build -o bin/freedesk-host.exe ./cmd/host
./bin/freedesk-host.exe
```
Stop it with Ctrl+C or by closing the window: the agent finishes the current session, removes its host record and deletes its anonymous account (about 3 seconds).

## Troubleshooting
When start-up fails, the agent prints the reason and, for the known ones, what to fix. A double-clicked window stays open until Enter so the message can be read (builds before 0.2.1 closed at once; start those from a terminal to see it).

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
cd ../firebase && firebase emulators:exec --only "auth,database" --project demo-rcapp \
  "cd ../host-agent && go test ./internal/firebase/ ./internal/webrtc/ -run Emulator -v"
```
The emulator run checks the security rules end to end and performs a real host↔viewer WebRTC negotiation through the database.

For the shared contract with the viewer: [`../docs/PROTOCOL.md`](../docs/PROTOCOL.md). Design and rationale: [`../docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md).
