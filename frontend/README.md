# Frontend (Viewer) — Vue 3 + TypeScript

The viewer application that runs in the browser. There is no login screen: on startup an invisible **anonymous** Firebase session is established. The home page is also the product's landing page: it explains the flow, lets you enter a remote computer's 9-digit code and connect, shows this computer's own code when the host agent is running on the same machine, and otherwise offers the host agent download (the latest zip from the GitHub Releases of `VITE_GITHUB_REPO`).

## Technologies
Vue 3 (Composition API) · TypeScript · Vite · Vue Router · Pinia · Bootstrap 5 · Firebase Web SDK (modular).

## Folder Structure
```
frontend/
├── index.html
├── package.json
├── tsconfig.json
├── vite.config.ts
├── .env.example                 # Firebase Web configuration (template)
├── .env.emu                     # `--mode emu`: run against the local emulators
└── src/
    ├── main.ts                  # App bootstrap (Pinia, Router, Bootstrap)
    ├── App.vue
    ├── assets/                  # Styles
    ├── firebase/                # Firebase SDK init (app, auth, realtime database)
    ├── constants/               # RTDB paths, route names, WebRTC settings
    ├── services/                # auth (anonymous), host (code lookup / watch),
    │                            # session (RTDB lifecycle + cleanup), signaling,
    │                            # serverTime, localAgent (local agent endpoint)
    ├── stores/                  # Pinia stores (auth)
    ├── composables/             # useViewerConnection, useInputCapture, useNow,
    │                            # useServerNow
    ├── router/                  # Vue Router (Home + Connect)
    ├── components/              # AppNavbar, FirebaseConfigAlert
    └── pages/                   # HomePage, ConnectPage
```

## Pages
| Route | Page |
|------|-------|
| `/` | Home / landing: connect to a remote code, this computer's code or the host download, how it works |
| `/connect/:hostId` | Connection: toolbar + video (`hostId` = 9-digit code) |

## Running
```bash
cd frontend
npm install
cp .env.example .env     # fill in the values from the Firebase Console
npm run dev
```

> For the "Share this computer" card to show the code, the host agent must be running
> on the same machine (the code is read from `http://127.0.0.1:47800/identity`; the page
> re-probes every few seconds). Until then the card shows the download steps and a
> **Download for Windows** button that resolves the latest release through the GitHub
> API (`api.github.com/repos/<VITE_GITHUB_REPO>/releases/latest`, cached for 10 minutes;
> falls back to the generic `releases/latest/download/...` link). The agent is not
> needed just to connect to another computer.

All data lives in the Firebase Realtime Database (host records, the per-owner session inbox and WebRTC signaling); there is no Firestore.

To develop against the local Emulator Suite, run `npm run dev -- --mode emu`: the committed `.env.emu` sets `VITE_USE_EMULATORS=1` and the `demo-rcapp` project the emulator tests use (start the emulators first with `firebase emulators:start --only "auth,database" --project demo-rcapp` in `../firebase`). The dev server listens on port 9205, which the host agent accepts as a web origin.

For the shared signaling/input contract: [`../docs/PROTOCOL.md`](../docs/PROTOCOL.md).
