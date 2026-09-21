# Firebase — Configuration & Security Rules

This folder contains the configuration and security rules for Firebase, the project's **only backend**. There is no application code here; only the configuration and rule files used by the Firebase CLI. The viewer is deployed from here as well (`public/`, produced by `npm run build` in `frontend/`).

## Contents

| File | Purpose |
|-------|------|
| `firebase.json` | Firebase CLI configuration: database rules, hosting, and local emulator ports |
| `.firebaserc` | Project alias (deploy target) |
| `database.rules.json` | Realtime Database security rules (hosts, inbox, sessions) |
| `public/` | Built viewer (generated, not committed) |

Only **Authentication (Anonymous)**, the **Realtime Database** and **Hosting** are used. There is no Firestore, no Functions, no Storage.

---

## 1. Creating the Firebase Project

1. [Firebase Console](https://console.firebase.google.com/) → **Add project**.
2. **Authentication → Get started → Sign-in method**:
   - **Anonymous** → Enable. (There is no login screen; both the web client and the host agent use anonymous identity.)
   - Recommended: **Settings → User actions** — enable *automatic clean-up of anonymous accounts* (available after the free Identity Platform upgrade). Every agent launch is a new anonymous user; graceful shutdown deletes it, but a killed agent leaves one behind.
3. **Realtime Database → Create Database** → pick a region → *Locked mode* (we will deploy the rules ourselves).
4. **Hosting → Get started** (only needed to publish the viewer).

## 2. Linking the Project to the CLI

Replace the `REPLACE_WITH_YOUR_FIREBASE_PROJECT_ID` value in `.firebaserc` with your real project ID; or:

```bash
firebase login
cd firebase
firebase use --add        # pick your project from the list, alias: default
```

## 3. Deploying the Security Rules

```bash
cd firebase
firebase deploy --only database
```

> On the first `database` deploy, the CLI may ask for the target database instance; pick the default instance.
>
> CI does not deploy the rules (only the viewer, see `.github/workflows/deploy-web.yml`): run this command once when setting the project up and again whenever `database.rules.json` changes.

## 4. Deploying the Viewer

```bash
cd frontend && npm run build      # writes ../firebase/public
cd ../firebase && firebase deploy --only hosting
```

The site is then served at `https://free-desk.web.app` (the `hosting.site` id in `firebase.json`; without one, the project id is the site id). The host agent only answers `/identity` requests from the project's and the site's `web.app` / `firebaseapp.com` origins (plus the local dev server); the release workflow reads the site id from `firebase.json`, and when running from source set `RC_HOSTING_SITE` in `host-agent/.env`.

## API keys

Neither key below is a secret (both ship to browsers or inside the exe); the security rules are what protect the data.

- **Viewer key** (`VITE_FIREBASE_API_KEY`, repository variable `FIREBASE_API_KEY`). It may be limited to websites (Google Cloud Console → APIs & Services → Credentials → the key → *Application restrictions: Websites*); then the site must be on the list: `https://free-desk.web.app/*` and `https://free-desk.firebaseapp.com/*` (and `http://localhost:9205/*` for development). Otherwise the viewer fails at start-up with `auth/requests-from-referer-...-are-blocked`. Adding the site under Authentication → Settings → Authorized domains is harmless and recommended too.
- **Host agent key** (`RC_FIREBASE_API_KEY` in `host-agent/.env`, repository variable `FIREBASE_AGENT_API_KEY`). The agent is a desktop program and sends no website referrer, so a website-restricted key rejects it with `API_KEY_HTTP_REFERRER_BLOCKED` and the agent exits at start-up. Give it its own key: Credentials → *Create credentials → API key*, *Application restrictions: None*, *API restrictions*: either *Don't restrict key* or exactly *Identity Toolkit API* and *Token Service API* (a key that lacks these answers `API_KEY_SERVICE_BLOCKED`). A new key can take a few minutes to become active. A single unrestricted key for both viewer and agent works as well; then `FIREBASE_AGENT_API_KEY` can stay unset and the release falls back to `FIREBASE_API_KEY`.

The release workflow probes the agent's key without a referrer before building and fails with the reason when the key is restricted, so a broken exe is never published. The agent itself prints the reason and the fix in its window when sign-in is refused.

## 5. Local Verification (Emulator)

To verify the rules offline, without touching the real project (needs a JDK ≥ 21 on the PATH):

```bash
cd firebase
firebase emulators:exec --only "auth,database" --project demo-rcapp "cd ../host-agent && go test ./internal/firebase/ ./internal/webrtc/ -run Emulator -v"
```

This loads the rules into the emulator and runs the Go end-to-end tests: the rules test (registration, code lookup, no enumeration, inbox privacy, frozen identity fields, cleanup) and a full host↔viewer WebRTC negotiation through the database.

> **Windows / PowerShell note:** always write `--only auth,database` **in quotes** (`--only "auth,database"`). Without quotes, PowerShell treats the comma as the array operator, splits the argument, and the emulator does not start.

For an emulator with a UI for interactive development:

```bash
firebase emulators:start --project demo-rcapp
```

Point the agent at it with `FIREBASE_AUTH_EMULATOR_HOST=localhost:9099` and `FIREBASE_DATABASE_EMULATOR_HOST=localhost:9000`, and the viewer with `VITE_USE_EMULATORS=1` (see `frontend/.env.example`).

## 6. Data Model & Rules in Short

```
/hosts/{code}                  the running agent (ownerUid, name, version, lastSeen)
/inbox/{ownerUid}/{sessionId}  a connection request (viewerUid, code, createdAt)
/sessions/{sessionId}          the session: status + WebRTC offer/answer/candidates
```

- `/hosts` cannot be listed; a single code can be resolved by any signed-in user; only its owner can change it; anyone may delete a record whose `lastSeen` is older than 5 minutes.
- An inbox is readable only by its owner; entries are created by the viewer that asks.
- A session is visible only to its viewer and the host owner; identity fields are frozen; `status` is an enum; a description's `type` is `offer`/`answer` and its `sdp` at most 64 KiB, a candidate line at most 512 characters (an unbounded string would let any anonymous identity write 16 MB per session); unknown fields are rejected; deleting an already deleted node is allowed.
- All timestamps must be server timestamps.

Full rationale: [`docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md), wire contract: [`docs/PROTOCOL.md`](../docs/PROTOCOL.md).

## 7. Frontend & Host Agent Connection

- **Frontend:** obtains the Firebase **Web SDK** configuration object (apiKey, authDomain, projectId, databaseURL, …) from Console → *Project settings → Your apps → Web app* and places it in `frontend/.env` (`frontend/README.md`).
- **Host Agent:** needs only `apiKey`, `projectId` and `databaseURL`. Release builds have them embedded; when running from source put them in `host-agent/.env`. A service account is **never** used.
