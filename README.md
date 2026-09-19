# FreeDesk

A small, free remote desktop: see and control another Windows PC from your browser. No accounts, no passwords, no server to run. One person starts a tiny program, reads a 9-digit code, the other person types it into the web page, and the first person clicks **Yes**.

**Web page:** https://free-desk.web.app — connect from any browser, or download the Windows host program from the same page.

- **Viewer:** a web page (Vue 3) — shows the remote screen and sends mouse/keyboard.
- **Host:** `freedesk-host.exe` (Go + Pion WebRTC) — runs on the PC being controlled, captures the screen with ffmpeg and applies input.
- **Firebase:** anonymous identity + a Realtime Database used only to find each other and exchange the WebRTC handshake. **Screen and input never pass through Firebase**; they travel directly between the two computers over an encrypted WebRTC connection.

---

## Using it

### On the computer that will be controlled
1. Press **Download for Windows** on the web page (or take `freedesk-host-windows-x64.zip` from this repository's **Releases** page) and unzip it anywhere.
2. Run `freedesk-host.exe`. A console window shows **THIS COMPUTER'S CODE** (for example `738 986 982`). If Windows asks about network access, allow it.
3. Give the code to the person who should connect.
4. When they connect, a window pops up: **Allow them to see your screen and control this computer?** Click **Yes**. If you do not answer within 45 seconds the request is rejected.
5. Close the console window (or press Ctrl+C) to stop. The code stops working immediately; the next start gets a new code.

### On the computer that connects
1. Open the FreeDesk web page: https://free-desk.web.app (when you run your own copy it is `https://<your-site>.web.app`).
2. Type the 9-digit code and press **Connect**.
3. Wait for the other person to click Yes. The remote screen appears; click it to control. **End Session** disconnects.
4. To send files, drag them onto the remote screen (or use **Files**). The other person is asked once per batch, and accepted files land in their `Downloads\FreeDesk` folder — nothing is ever overwritten.
5. To receive files, press **Files → Get files…**. A file picker opens on the other computer; whatever that person chooses appears in your panel with a **Save** button.
6. Text you copy is shared both ways while you are connected, so Ctrl+C on one computer and Ctrl+V on the other just works. The other person is told this before they accept, and they can turn it off with `RC_CLIPBOARD=off`.
7. Copying **files** works too: copy them in Explorer on one computer and paste into the FreeDesk page on the other, and they arrive on that computer's clipboard ready for Ctrl+V. Files copied on the remote computer appear in your panel to save — a web page cannot put real files on your own clipboard, so that direction is a download.

### What it does not do (yet)
Windows hosts only (viewer runs in any modern browser) · primary monitor only · no audio · clipboard sharing is text only · file transfer and clipboard need host agent 0.3.0 or newer, and only files, not folders · no unattended access (someone must click Yes) · cannot pass UAC prompts or the lock screen · some networks (mobile data, CGNAT) cannot connect directly because there is no relay server.

---

## How it works

```
                        ┌──────────────────────────────┐
                        │        FIREBASE (free)       │
                        │  Anonymous auth              │
                        │  Realtime DB: hosts, inbox,  │
                        │  session signaling only      │
                        └──────────────┬───────────────┘
                     Web SDK           │            REST + SSE
             ┌─────────────────────────┴──────────────────────────┐
             ▼                                                    ▼
  ┌────────────────────┐            code            ┌────────────────────┐
  │  WEB VIEWER (Vue)  │◀──────────────────────────▶│  HOST AGENT (Go)   │
  │  • enter code      │                            │  • prints code     │
  │  • remote screen   │      WebRTC (direct)       │  • Yes/No window   │
  │  • mouse/keyboard  │◀══ video ══════════════════│  • ffmpeg capture  │
  │                    │══ input (DataChannel) ════▶│  • SendInput       │
  └────────────────────┘                            └────────────────────┘
```

- Every launch of the host is a **new anonymous identity and a new code**; nothing is stored on disk and both are deleted when the agent exits.
- The host **reads nothing while idle**: connection requests are pushed to it over a single streaming connection.
- All session data is **deleted when the session ends**, including when a browser tab is closed abruptly (server-side `onDisconnect`).
- The full design, data model, security model and known limits: **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)**. The wire contract between viewer and host: **[docs/PROTOCOL.md](docs/PROTOCOL.md)**.

---

## Running your own copy

You need a free Firebase project (Spark plan is enough for personal use).

1. **Firebase:** create a project, enable the *Anonymous* sign-in provider, create a Realtime Database, deploy the rules. Step by step: [firebase/README.md](firebase/README.md).
2. **Viewer:** `cd frontend && npm install && cp .env.example .env` (fill in the web config from the Firebase console) `&& npm run dev` — or deploy it with `npm run build && cd ../firebase && firebase deploy --only hosting`. Details: [frontend/README.md](frontend/README.md).
3. **Host agent:** `cd host-agent && cp .env.example .env` (API key, project id, database URL) `&& go run ./cmd/host`. ffmpeg must be next to the executable or on the PATH. Details: [host-agent/README.md](host-agent/README.md).
4. **Releases from GitHub Actions:** set the repository *variables* `FIREBASE_API_KEY`, `FIREBASE_AUTH_DOMAIN`, `FIREBASE_PROJECT_ID`, `FIREBASE_APP_ID`, `FIREBASE_DATABASE_URL`, plus `FIREBASE_AGENT_API_KEY` — an API key of the same project **without application restrictions** for the host agent (a desktop program sends no website referrer, so a website-restricted key rejects it; see [API keys](firebase/README.md#api-keys)) — and the *secret* `FIREBASE_SERVICE_ACCOUNT` (a service-account JSON key with the Firebase Hosting Admin role). The home page's Download button links to the Releases of the repository that built it (`VITE_GITHUB_REPO`, set automatically by the workflows). Then:
   - pushing to `main` deploys the viewer to Firebase Hosting (`.github/workflows/deploy-web.yml`); the database rules are deployed by hand, once and after every change to `firebase/database.rules.json` (`cd firebase && firebase deploy --only database`);
   - pushing a tag `vX.Y.Z` builds `freedesk-host.exe` with your project embedded, bundles the official ffmpeg build and publishes the zip on the release (`.github/workflows/release.yml`). The workflow first checks that the agent's key accepts a request without a referrer, so a broken exe is never published.

The Firebase web values are public by design; the security rules are what protect the data.

## Repository layout

```
freedesk/
├── README.md
├── docs/               ARCHITECTURE.md, PROTOCOL.md
├── firebase/           firebase.json, database.rules.json, README.md  (public/ = built viewer)
├── frontend/           Vue 3 viewer
├── host-agent/         Go host agent (cmd/host, internal/*)
└── .github/workflows/  ci.yml, release.yml, deploy-web.yml
```

## Development

| Tool | Version |
|------|---------|
| Go | 1.24+ |
| Node.js / npm | 20+ / 10+ |
| Firebase CLI | 13+ (emulators need a JDK 21+) |
| ffmpeg | 5.1+ with `gdigrab` and `libvpx` (only at runtime, on the host) |

```bash
# host agent
cd host-agent && go build ./... && go vet ./... && go test ./...
# viewer
cd frontend && npm run type-check && npm run lint && npm run build
# rules + WebRTC negotiation against the Firebase emulator
cd firebase && firebase emulators:exec --only "auth,database" --project demo-rcapp \
  "cd ../host-agent && go test ./internal/firebase/ ./internal/webrtc/ -run Emulator -v"
```

For a full local run against the emulator: `firebase emulators:start --only "auth,database" --project demo-rcapp`, then the agent with `FIREBASE_AUTH_EMULATOR_HOST=localhost:9099 FIREBASE_DATABASE_EMULATOR_HOST=localhost:9000` and the viewer with `npm run dev -- --mode emu` (see `frontend/.env.example`).

## Security in one paragraph

Knowing a code lets someone *ask*; only a click on the host grants control. Identities are anonymous and throw-away, records are deleted when no longer needed, all timestamps are server-side, the database cannot be enumerated, and the host agent is subject to the same security rules as the browser (it uses no service account). Remaining limits are listed honestly in [docs/ARCHITECTURE.md §4.3](docs/ARCHITECTURE.md#43-known-limits-documented-not-hidden).

## Responsible use

This software is for accessing machines that **you own or are explicitly authorised to control**. Unauthorised use of remote-access tools is illegal in most countries. A connection always requires a person on the host to accept it.

## License

MIT — see [LICENSE](LICENSE). ffmpeg is distributed unmodified under its own license (LGPL build; notice included in the release zip).
