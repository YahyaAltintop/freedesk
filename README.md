# FreeDesk

A small, free remote desktop: see and control another Windows PC from your browser. No accounts, no passwords, no server to run. One person starts a small program and reads a 6-digit code, the other types it into the web page, and the first person clicks **Yes**.

**Web page:** https://free-desk.web.app — connect from any browser, or download the Windows host program there.

## Using it

**On the computer that will be controlled**

1. Press **Download for Windows** on the web page (or take `freedesk-windows-x64.zip` from the [Releases](https://github.com/YahyaAltintop/freedesk/releases) page) and unzip it anywhere.
2. Run `freedesk.exe`. A small window shows **this computer's code**. If Windows asks about network access, allow it.
3. Give the code to the person who should connect.
4. When they connect, a window asks whether to let them see your screen, control the computer, exchange files and share copied text. Click **Yes**. No answer within 45 seconds means no.
5. Close the FreeDesk window to stop. The code stops working at once; the next start gets a new one.

> **"Unknown publisher"?** The program is not code-signed yet, so Windows warns on first run. Choose **More info → Run anyway**. To check what you downloaded, compare the zip's SHA-256 with the `.sha256` file on the release page, or run `gh attestation verify freedesk-windows-x64.zip --repo YahyaAltintop/freedesk`.

**On the computer that connects**

1. Open https://free-desk.web.app, type the code, press **Connect**.
2. Wait for the other person to click Yes. The remote screen appears; click it to control. **End Session** disconnects.
3. Files: drag them onto the remote screen to send, or **Files → Get files…** to receive. Text you copy is shared both ways while connected; copied files too.

## What it does not do (yet)

- Windows hosts only (the viewer runs in any modern browser). Primary monitor only. No audio.
- No unattended access: someone must click Yes. It cannot pass UAC prompts or the lock screen.
- Files only, never folders. An interrupted transfer starts over. The clipboard carries text and files, not images.
- Some networks (mobile data, CGNAT) cannot connect directly: there is no relay server.

## How it works

- The **host** (`freedesk.exe`, Go + Pion WebRTC) captures the screen with ffmpeg and applies mouse and keyboard input. The **viewer** is a Vue 3 web page.
- **Firebase** (anonymous auth + Realtime Database) is used only to find each other and exchange the WebRTC handshake. **Screen and input never pass through Firebase**; they travel directly between the two computers over an encrypted WebRTC connection.
- Every launch of the host is a **new anonymous identity and a new code**; nothing is stored on disk and both are deleted when it exits. Session data is deleted when the session ends, even when a browser tab is closed abruptly.
- Design, data model, security model and known limits: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). The wire contract between viewer and host: [docs/PROTOCOL.md](docs/PROTOCOL.md).

## Running your own copy

You need a free Firebase project (the Spark plan is enough).

1. **Firebase:** create a project, enable Anonymous sign-in, create a Realtime Database, deploy the rules — [firebase/README.md](firebase/README.md).
2. **Viewer:** `cd frontend && npm install && cp .env.example .env && npm run dev` — [frontend/README.md](frontend/README.md).
3. **Host:** `cd host-agent && cp .env.example .env && go run ./cmd/host` — [host-agent/README.md](host-agent/README.md).
4. **Releases:** pushing to `main` deploys the viewer; pushing a tag `vX.Y.Z` builds the exe and publishes the zip — [docs/RELEASING.md](docs/RELEASING.md).

## Development

Go 1.26+, Node.js 20+, Firebase CLI 13+ (the emulators need JDK 21+), and ffmpeg 5.1+ with `gdigrab` and `libvpx` on the host at runtime.

```bash
cd host-agent && go generate ./cmd/host && go build ./... && go vet ./... && go test ./...
cd frontend && npm run type-check && npm run lint && npm run build
cd firebase && firebase emulators:exec --only "auth,database" --project demo-rcapp \
  "cd ../host-agent && go test ./internal/firebase/ ./internal/webrtc/ -run Emulator -v"
```

## Security

Knowing a code lets someone *ask*; only a click on the host grants control. Identities are anonymous and throw-away, records are deleted when no longer needed, all timestamps are server-side, the database cannot be enumerated, and the host agent is bound by the same security rules as the browser. Remaining limits: [docs/ARCHITECTURE.md §4.3](docs/ARCHITECTURE.md#43-known-limits-documented-not-hidden).

## Responsible use

For computers you **own or are explicitly authorised to control**. Unauthorised use of remote-access tools is illegal in most countries. A connection always needs a person on the host to accept it.

## License

MIT — see [LICENSE](LICENSE). ffmpeg ships unmodified under its own license (LGPL build; its notice is `ffmpeg\LICENSE.txt` inside the zip).
