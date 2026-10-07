# Host Agent — Go + Pion WebRTC

The program that runs on the Windows computer being controlled. It signs in to Firebase **anonymously**, shows a fresh **6-digit pairing code** in a small window, serves that code to the web UI on the same machine (`http://127.0.0.1:47800/identity`, FreeDesk web origins only), and waits for connection requests. Each request is approved in a native **Yes/No window**; after approval the screen is streamed over WebRTC and incoming input is applied through the Windows API.

Nothing is stored on disk: every launch is a new identity and a new code, and both are deleted on exit.

## Prerequisites
- **Go 1.26+** — <https://go.dev/dl/> (go.mod's toolchain line names the exact release; the go command fetches it on its own)
- **MSYS2** — <https://www.msys2.org> (`winget install MSYS2.MSYS2`), with the UCRT64 C toolchain: in an *MSYS2 UCRT64* shell, `pacman -S --needed mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-nasm make git diffutils`. The screen is encoded by **libvpx**, linked into the exe through cgo; `scripts/build-libvpx.sh` builds it (see *Build & run*). A build without cgo still runs, only **without video** (the connection and the input channel still work).

## Layout
```
host-agent/
├── cmd/host/main.go        entry point: status window, auth, registration, inbox, shutdown
├── cmd/host/winres/        the exe's icon, version information and manifest (go-winres)
├── cmd/importcheck/        refuses an exe not fit to publish: a DLL Windows does not ship, no encoder, no license texts
├── cmd/notices/            writes the license texts the exe embeds, from the build's own dependencies
├── internal/
│   ├── auth/               Identity Toolkit REST (anonymous signUp, token refresh, self-delete)
│   ├── firebase/           Realtime Database REST + SSE client
│   ├── host/               /hosts/{code} registration + heartbeat + removal
│   ├── session/            inbox stream, per-request coordinator (approval → WebRTC → cleanup)
│   ├── consent/            approval: native dialog (Windows) or console prompt
│   ├── ui/                 the status window (Win32): code, status, activity log
│   ├── signaling/          offer/answer/ICE over the session node (one demultiplexed stream)
│   ├── webrtc/             Pion PeerConnection, video track, RTCP drain
│   ├── capture/            primary monitor via DXGI desktop duplication (GDI fallback), pointer drawn in
│   ├── vpx/                cgo: libvpx VP8 encoder + BGRA→I420 conversion and downscaling (plain C)
│   ├── input/              DataChannel events → SendInput, held-key tracking
│   ├── localapi/           127.0.0.1 endpoint serving the code to the web UI
│   ├── licenses/           the license texts carried inside the exe (written by cmd/notices)
│   └── config/             .env / environment / build-time embedded values
└── scripts/build-libvpx.sh builds the static libvpx (pinned commit) into third_party/libvpx
```

## Configuration
Release builds have the Firebase values embedded (`-ldflags -X`, see `.github/workflows/release.yml`). When running from source, copy `.env.example` to `.env` or set environment variables (they override the file and the embedded values):

| Variable | Meaning |
|----------|---------|
| `RC_FIREBASE_API_KEY`, `RC_FIREBASE_PROJECT_ID`, `RC_FIREBASE_DB_URL` | The Firebase project (same as the viewer). The key must have **no application restrictions**: the agent sends no website referrer, so a website-restricted key rejects it (see [firebase/README.md → API keys](../firebase/README.md#api-keys)) |
| `RC_HOST_NAME` | Display name (default: machine name, max 64 chars) |
| `RC_MAX_WIDTH` | Widest frame the encoder is given, in pixels (default 1920). A wider primary monitor (1440p, 4K) is scaled down to this, aspect kept, so the 3 Mbit/s video budget still covers what it encodes |
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
Once, from an *MSYS2 UCRT64* shell in `host-agent/` — it clones libvpx at the pinned commit, checks the commit and builds the VP8 encoder as a static library into `third_party/libvpx` (gitignored, about a minute):
```bash
bash scripts/build-libvpx.sh
```
Then, from PowerShell, with cgo pointed at the same compiler:
```powershell
$env:CGO_ENABLED = "1"; $env:CC = "C:\msys64\ucrt64\bin\gcc.exe"; $env:PATH = "C:\msys64\ucrt64\bin;$env:PATH"
go generate ./cmd/host                # icon, version info, manifest — optional for development
go run ./cmd/notices -o internal/licenses/generated/LICENSES.txt   # license texts — optional for development
go build -o bin/freedesk.exe ./cmd/host
go run ./cmd/importcheck -licenses internal/licenses/generated/LICENSES.txt bin/freedesk.exe
./bin/freedesk.exe
```
A development build is a console program that also opens the status window, so the log shows in both. The release adds `-ldflags "-H=windowsgui"` (no console): `go build -ldflags "-H=windowsgui" -o bin/freedesk.exe ./cmd/host` reproduces it.

`-static` is in `internal/vpx`'s cgo flags, so every build links libgcc, winpthreads and libvpx into the exe; `importcheck` proves it (it fails on a `libwinpthread-1.dll`, `libgcc_s_seh-1.dll` or `libvpx` import, and on a build without cgo).

The release is `freedesk.exe` alone, so the licenses of the code in it travel inside it: `cmd/notices` writes them (FreeDesk's own MIT license, then libvpx, the MinGW-w64 and GCC runtimes, Go and every module) into `internal/licenses/generated/` (gitignored), and the build embeds whatever is there. Without that step the exe builds and runs as usual, only without a **Licenses** button; `importcheck` refuses it, and also an exe built before the texts last changed, since it looks for this build's exact texts inside the exe. The program shows them from its window, or `freedesk.exe --licenses > licenses.txt` saves them without starting anything.

On a machine whose antivirus inspects HTTPS with its own certificate (Norton, for one), MSYS2's `pacman` and `git` reject the connection; Windows' own tools trust it. Clone libvpx with Git for Windows first (`git -c http.sslBackend=schannel clone --depth 1 --branch v1.17.0 https://chromium.googlesource.com/webm/libvpx third_party/.build/libvpx/src`; the script reuses that checkout after checking its commit), and give `pacman` a config whose `[options]` say `XferCommand = /c/Windows/System32/curl.exe -L -C - -f -sS -o %o %u` (package signatures are still verified).

Stop it by closing the window or with Ctrl+C: the agent finishes the current session, removes its host record and deletes its anonymous account (about 3 seconds); the window says "Stopping…" meanwhile.

## Troubleshooting
The window talks to the operator in plain sentences and never shows an error code, a status or a console command: none of those is something the person who double-clicked the exe can act on. The technical detail behind each sentence goes to the **developer's channel**: the console, when the agent was started from one, and the window's Activity box as well when `RC_DIAG=on` (a line in a `.env` next to the exe). When start-up fails the window shows **Could not start** and stays open until closed; with `RC_APPROVAL=console` the agent exits at once instead.

| The window says | What it means, and the fix |
|-----------------|----------------------------|
| *Could not sign in.* | Google rejected the anonymous sign-in. The developer's channel names the reason: `API_KEY_HTTP_REFERRER_BLOCKED` (the key is limited to websites; the agent needs one with no application restrictions, `FIREBASE_AGENT_API_KEY` for releases), `API_KEY_SERVICE_BLOCKED` (the key's API restrictions exclude *Identity Toolkit API* / *Token Service API*), `ADMIN_ONLY_OPERATION` (anonymous sign-in is disabled: Firebase Console → Authentication → Sign-in method → Anonymous). |
| *This computer could not be made available for connections right now.* | The database refused the host record three times in a row. The security rules are not deployed (`cd firebase && firebase deploy --only database`), or this build points at a project other than the one the rules are in. |
| *Could not reach the service.* | The database could not be reached at all: no internet, or a proxy in the way. |
| *Windows has not yet allowed this program to receive connections.* | Windows Defender Firewall has no inbound allow rule for this `freedesk.exe` (rules are per path: a `freedesk.exe` saved somewhere new, or as `freedesk (1).exe` next to the old one, starts from nothing; an update should replace the old file, with FreeDesk closed). The question Windows asks at start-up was missed, or the account is not an administrator and cannot answer it. Windows Security → Firewall & network protection → Allow an app through firewall → Allow another app… → `freedesk.exe`, Private and Public. The agent asks Windows again every 45 s and clears the warning by itself. Said only while Windows runs its own firewall: when a security suite has taken it over (Norton, Kaspersky, ESET, Bitdefender…, registered with Windows for the firewall rule category), Windows' profiles still read as on but its rules are not enforced and it never asks, so the agent says nothing at start-up; the developer's channel logs `firewall: owner="…"`. |
| *Windows allows this program only on Public networks, but this computer is on a Private network now.* | The allow rule exists, but for the wrong kind of network: only one of Private / Public was ticked. Tick both. |
| *Windows is blocking this program's network access.* | Windows Defender Firewall holds an inbound **block** rule for `freedesk.exe`: its "allow this app?" question was answered with Cancel, or the program was blocked by hand. A block rule beats every allow rule. Remove the blocking entry, then allow FreeDesk on Private and Public. |
| *The other computer could not reach this one.* | ICE found no working path in 30 s. On the same network: the firewall question at start-up was not answered with Allow (see the line above). When a security suite runs the firewall the advice names it (*Norton 360 runs this computer's firewall instead of Windows…*): it may have asked about FreeDesk, or, just after a download, still be deciding about the new unsigned program and keep its packets out meanwhile. Seen with Norton 360: the first start failed, Norton wrote its allow rules (outbound, inbound on the UDP port) seconds after the second start, and every connection since went through. A restart means a new code, which the advice says. On different networks: FreeDesk uses STUN only, and a symmetric NAT or carrier-grade NAT (mobile data, many shared and corporate networks) cannot be crossed without a TURN relay, which the project deliberately does not run. The developer's channel lists which candidate types each side had: no `srflx` means the STUN server was unreachable; only `mdns` from the viewer means its LAN address could not be resolved. |
| *Screen sharing could not start.* | This build has no video encoder (built with `CGO_ENABLED=0`; `importcheck` refuses such an exe), or the screen cannot be read at all (no interactive desktop). The developer's channel names which. The connection and the mouse/keyboard still work. |
| *Lost contact with the service.* | A heartbeat failed; the code stops resolving after 90 s and comes back on its own when the connection does. |

## Tests
```bash
go test ./...                                    # unit tests (input handler, inbox, demux, encoder, conversion, CORS)
RC_INPUT_INTEGRATION=1 go test ./internal/input/  # moves the real mouse cursor once
RC_CAPTURE_INTEGRATION=1 go test ./internal/capture/  # captures the real screen for 3 s (RC_CAPTURE_FORCE_GDI=1: the GDI fallback)
RC_CAPTURE_BENCH=1 go test -run CaptureBench -v ./internal/capture/  # CPU, fps, bitrate on the real screen
RC_FFMPEG_GOLDEN=<ffmpeg.exe> go test ./internal/vpx/  # colour conversion and encoder output against ffmpeg's
RC_DIALOG_INTEGRATION=1 go test ./internal/consent/  # shows two short dialogs
RC_UI_INTEGRATION=1 go test ./internal/ui/           # opens the status window three times, briefly
cd ../firebase && firebase emulators:exec --only "auth,database" --project demo-rcapp \
  "cd ../host-agent && go test ./internal/firebase/ ./internal/webrtc/ -run Emulator -v"
```
The emulator run checks the security rules end to end and performs a real host↔viewer WebRTC negotiation through the database.

For the shared contract with the viewer: [`../docs/PROTOCOL.md`](../docs/PROTOCOL.md). Design and rationale: [`../docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md).
