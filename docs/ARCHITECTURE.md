# Architecture

This document describes the system's end-to-end design, data model, security model, and the rationale behind the technical decisions taken. It serves as the reference while developing the Frontend and the Host Agent.

---

## 1. Components and Responsibilities

The system consists of three independent components. Each focuses on a single responsibility.

### 1.1 Vue Web Client (Viewer)
The viewer application that runs in the browser (deployed on Firebase Hosting). There is no login screen.
- On startup, an invisible **anonymous** Firebase session (Auth Web SDK, `signInAnonymously`).
- Displaying this machine's pairing code on the home page (read from the loopback endpoint of the host agent on the same machine).
- Resolving the remote computer's 9-digit code (an individual `/hosts/{code}` read) and creating a session + request.
- Displaying the **video track** received over WebRTC in a `<video>` element.
- Capturing mouse/keyboard events and forwarding them to the host over the **`input` DataChannel**, but only while the video element actually holds the focus — an unfocused page must not type on somebody else's machine.
- Sending files dropped on the remote screen and saving files the host offers, over the **`file` DataChannel**: written straight to disk through the File System Access API rather than held in memory.
- Keeping the two clipboards in step: text in both directions, and files pasted into the page forwarded as an ordinary transfer.
- Cleaning up after itself: everything it writes is armed with `onDisconnect().remove()`.

### 1.2 Go Host Agent (Host)
The agent that runs on the remotely controlled Windows machine (`freedesk-host.exe` + `ffmpeg.exe`).
- Authenticates **anonymously** to Firebase (Identity Toolkit REST). **Every launch is a new identity and a new 9-digit code; nothing is stored on disk.** On shutdown it removes its host record and deletes its own anonymous account.
- Publishes itself as `/hosts/{code}` and refreshes `lastSeen` every 30 s (server timestamp).
- Prints the code to the console and serves it to the local web UI on `127.0.0.1` (`/identity`, allowed web origins only).
- Streams its inbox (`/inbox/{uid}`) over Server-Sent Events; **no polling, zero reads while idle.**
- Every request is approved in a native **Yes/No window** (or `y` on the console); no answer within 45 s = rejected.
- On approval, establishes the WebRTC connection (as the offerer), captures the screen with ffmpeg and streams it as a **video track**.
- Applies input events arriving over the **`input` DataChannel** via the Windows API; releases every held key/button when the viewer goes away.
- Announces what it can do in a `hello` greeting the moment that channel opens, so the viewer enables features from the advertised capabilities rather than from a version number.
- Receives files on the **`file` DataChannel** into a fixed `Downloads\FreeDesk` folder — one Yes/No window per batch, never overwriting, and tagged with the Mark of the Web once complete. Sends files back only from a native picker the operator drives.
- Keeps its clipboard in step with the viewer's while `RC_CLIPBOARD` allows it (`text`, the default, or `off`), skipping anything the copying application marked private.

### 1.3 Firebase (Backend — serverless)
- **Authentication:** the **Anonymous** provider only. There are no accounts/passwords; identity exists to satisfy the security rules' `auth != null` condition and for ownership isolation.
- **Realtime Database (RTDB):** the only database. Host registry, connection-request inbox and WebRTC signaling. Nothing is persistent: hosts vanish at shutdown, sessions are deleted when they end.
- **Hosting:** serves the viewer.
- There is **no Firestore**, no Cloud Functions, no server.

---

## 2. End-to-End Flow

### 2.1 Identity & Discovery (pairing by code)
```
App opens ──(invisible anonymous session)──▶ Home page
   ├── "This computer": code is read from the local agent's 127.0.0.1 endpoint
   └── "Connect": 9-digit code ──▶ /hosts/{code} individual GET ──▶ create session + inbox entry
```
The only way to reach a host is to know its code; there is no host list/discovery
(the `/hosts` root cannot be read). Nor is knowing the code enough on its own —
the connection is established only after approval on the host machine.

### 2.2 Connection Setup (Signaling → WebRTC)
```
 VIEWER (Vue)                        FIREBASE RTDB                   HOST (Go)
    │                                    │                              │
    │ 1. arm onDisconnect().remove()     │                              │
    │    for both nodes                  │                              │
    │ 2. atomic write:                   │                              │
    │    /sessions/{id}  (waiting)       │                              │
    │    /inbox/{owner}/{id}             │   3. SSE push of the inbox   │
    │───────────────────────────────────▶│─────────────────────────────▶│
    │                                    │ 4. host reads /sessions/{id} │
    │                                    │    (must match the request)  │
    │                                    │◀─────────────────────────────│
    │                                    │ 5. status = connecting       │ 5b. YES/NO WINDOW
    │◀───────────────────────────────────│◀─────────────────────────────│    (45 s; no → status
    │   "Waiting for host approval…"     │                              │     ended + node deleted)
    │                                    │                              │ 6. PeerConnection +
    │                                    │                              │    video track +
    │                                    │                              │    DataChannels "file"
    │                                    │                              │    and "input"
    │                                    │ 7. write OFFER               │
    │◀───────────────────────────────────│◀─────────────────────────────│
    │ 8. write ANSWER                    │                              │
    │───────────────────────────────────▶│─────────────────────────────▶│
    │ 9. ICE candidates back and forth (hostCandidates / viewerCandidates)│
    │◀──────────────────────────────────────────────────────────────────▶│
    │                                                                    │
    │ 10. WebRTC P2P connection established (status = connected)         │
    │◀══════════════════ Video Track (host → viewer) ════════════════════│
    │◀══════════════ DataChannel "input" — keys and mouse ══════════════▶│
    │◀══════════ DataChannel "file" — transfers and clipboard ══════════▶│
    │                                                                    │
    │ 11. end: status = ended, /sessions/{id} and the inbox entry deleted │
```

**Important:** After step 10, everything — video, input, files and clipboard — flows entirely P2P. The host keeps a single SSE stream on `/sessions/{id}` open for the life of the session: it carries the answer and the viewer's candidates during negotiation and afterwards acts as the liveness signal (the node disappearing means the viewer is gone).

### 2.3 Direction Decision: Why is the host the "offerer"?
In WebRTC, the cleanest flow is for the side that adds the media track to generate the offer. Since the **host** produces the video, the host creates the offer and the viewer responds (answer). The host also opens both DataChannels, and must: there is no renegotiation anywhere in this design — one offer, one answer, and every channel a session will use exists before the offer is created. The viewer receives them via `ondatachannel`, tells them apart by label, and sends over them (a DataChannel is bidirectional). A viewer therefore cannot add a channel of its own; a new one is a host change plus a capability in the greeting.

---

## 3. Data Model (Realtime Database)

```
/hosts/{code}
    ownerUid   string   the agent's anonymous uid for this run
    name       string   display name (machine name), 1–64 chars
    version    string   agent version
    lastSeen   number   server timestamp (ms); refreshed every 30 s

/inbox/{ownerUid}/{sessionId}
    viewerUid  string   who is asking
    code       string   the code they entered
    createdAt  number   server timestamp (ms)

/sessions/{sessionId}
    viewerUid  string   immutable
    ownerUid   string   immutable
    status     string   waiting → connecting → connected → ended
    createdAt  number   server timestamp (ms), immutable
    offer      { type, sdp }                        host writes
    answer     { type, sdp }                        viewer writes
    hostCandidates/{pushId}    { candidate, sdpMid, sdpMLineIndex, usernameFragment }
    viewerCandidates/{pushId}  { candidate, sdpMid, sdpMLineIndex, usernameFragment }
```

Lifetimes:
- **Host record:** exists while the agent runs; deleted on graceful shutdown. If the agent dies, the record goes stale (`lastSeen` stops); viewers treat >90 s as offline and may delete it after 5 min (rules allow it).
- **Inbox entry:** deleted by the host the moment it consumes the request; also removed by the viewer's `onDisconnect`.
- **Session:** deleted by whichever side ends it; also removed by the viewer's `onDisconnect`. A session that is not `connected` may be deleted by anyone after 1 h (safety net).

> **Why RTDB only?** The old design kept hosts and sessions in Firestore and polled it every 3 s (28,800 reads per idle host per day). RTDB is billed on storage and bandwidth, not per operation; the host now receives requests through one push stream and does nothing while idle. RTDB's `onDisconnect` also gives free cleanup for the viewer. The trade-off is the Spark plan's 100-simultaneous-connections cap: each running host holds one stream (plus one per active session) and each open viewer tab holds one WebSocket.

---

## 4. Security Model

### 4.1 Principles
1. **Identity mandatory:** if `auth == null`, no access at all (identity is anonymous but required).
2. **Code = address, approval = authorization:** whoever knows the code can resolve the host and *request* a connection; establishing the connection depends on the operator's Yes on the host machine.
3. **Ownership isolation:** a host record is writable only by the identity that registered it; an inbox is readable only by its owner; a session only by its two participants.
4. **Immutable identity fields:** `viewerUid`, `ownerUid`, `createdAt` of a session cannot be changed after creation; `ownerUid` of a host must equal the writer.
5. **Server clocks only:** every timestamp is a server value and validated against `now`; client clocks are never trusted.
6. **Ephemeral everything:** a fresh uid and code per launch, and self-deletion on shutdown, leave nothing behind to steal or squat.

### 4.2 Rules — overview (`firebase/database.rules.json`)
- `/hosts`: the root is not readable (no enumeration). `/hosts/{code}` is readable by any signed-in user; writable only when the record is free, owned by the writer, or abandoned (`lastSeen` older than 5 min — a dead host's code can be reused). Code format is enforced (9 digits).
- `/inbox/{uid}`: readable only by `uid`. Entries are created by their `viewerUid`, deleted by the owner or the creator, or by anyone once older than 2 min.
- `/sessions/{id}`: created by its `viewerUid`; read/written by the two participants; status is an enum; SDP/candidate shapes are validated; unknown fields rejected. Deleting an already-deleted node is allowed so cleanup from both sides never errors.
- The host verifies every inbox entry against the session node (same viewer, addressed to this owner, status `waiting`) before acting on it — an inbox entry alone proves nothing.

### 4.3 Known limits (documented, not hidden)
- **Code guessing:** the 10⁹ code space can be probed one `GET` at a time by anyone with the public API key; what is found is a host name and that a request may be sent. Approval still gates control. Firebase App Check would cut probing from the web but the Go agent cannot present App Check tokens.
- **Prompt spam:** whoever knows a code can queue requests. The host handles one at a time and auto-declines the rest while one is pending or active, so the operator sees at most one window every 45 s.
- **Trust anchor:** Firebase carries the SDP (and therefore the DTLS fingerprints). Whoever can write the session node could interpose; the rules limit that to the two participants and project administrators.
- **Orphans:** if both peers die before the viewer armed `onDisconnect`, the session node stays until the 1 h rule lets a client delete it; since nothing can list `/sessions`, such leftovers only cost storage.
- **Local API:** `127.0.0.1:47800/identity` answers only to the FreeDesk web origins; any other website open on the host gets 403.
- **UAC / lock screen / multi-monitor:** out of scope in v1 (user-mode agent, primary monitor only).
- **No resume:** a transfer interrupted by an ICE failure or a reconnect starts over. `offset` is in the message schema from the start but is always 0 today, so adding resume later does not change the contract. The row fails visibly rather than hanging — a transfer that quietly stalls is worse than one that says it stopped.
- **Transfers widen the no-relay problem (5.1) rather than adding a new one.** Video failing on a symmetric-NAT path is obvious within seconds; a transfer can run for minutes, so the same missing TURN relay is exposed for far longer. Nothing else about a transfer is riskier than the video that is already flowing.
- **The clipboard sync carries whatever the operator copies.** That is what makes it work without a keystroke to trigger it, and it is a real exposure: anything copied on the host during a session reaches the viewer's browser. Three things bound it — applications that mark content private (every serious password manager does) are skipped entirely, the connection prompt says clipboard sharing is part of what is being approved, and `RC_CLIPBOARD=off` removes the capability from the greeting so the feature does not exist for that run. Contents are never logged; only lengths.
- **Files on the clipboard are only one-way as files.** A web page cannot write `CF_HDROP`: `ClipboardItem` accepts a short list of MIME types and there is no API that puts real files on the operating system's clipboard. So copying a file on the host and pasting it into the viewer's own Explorer will never work; that direction is offered as a download instead. Folders never reach the page at all — no browser puts them in a drop or a paste.
- **The operator's attention is the scarce resource.** One question per batch and one question at a time process-wide, so the rate is bounded by the prompt timeout rather than by how fast a viewer can offer. A bound is not an end, though: three file questions going unanswered in a row means nobody is at the machine, and the session stops asking. The count is consecutive — answering, including saying no, proves somebody is there and starts it again.

### 4.4 Host Agent authentication
The Go Host Agent does not use the Firebase **Admin SDK**. Instead, an **anonymous** user is created via the **Identity Toolkit REST API** (`accounts:signUp`, no email/password); the database is accessed with this user's **ID token** and the account is deleted with `accounts:delete` at shutdown.

**Rationale:**
- The Admin SDK requires a **service-account key file** that cannot be shipped inside a public binary.
- The Admin SDK **completely bypasses** the security rules — the isolation rules would be meaningless.
- The REST + ID token approach stays serverless, keeps the agent subject to the same rules as a browser, and needs nothing persisted.

> Anonymous accounts of agents that were killed (no graceful shutdown) linger in Authentication. Enable **automatic clean-up of anonymous users** in the Firebase console (Authentication → Settings; requires the free Identity Platform upgrade) so they are purged after 30 days. Firebase also limits anonymous sign-ups to about 100 per IP per hour, which only matters when restarting the agent in a tight loop.

---

## 5. Summary of Technical Decisions

| # | Decision | Rationale |
|---|-------|---------|
| 1 | Identity Toolkit REST + ID token instead of the Admin SDK | Stays subject to the security rules; no service-account leak risk |
| 2 | **RTDB only**, Firestore removed | No per-operation billing, push instead of polling, `onDisconnect` cleanup, one backend to configure |
| 3 | New uid + new code every launch, self-delete on exit | Nothing to persist, nothing to steal; matches "no accounts" |
| 4 | Inbox per owner uid, streamed over SSE | The host learns about a request the moment it is written and reads nothing while idle |
| 5 | Host = offerer, Viewer = answerer | The cleanest flow since the host produces the media track |
| 6 | One SSE stream per session on the host, demultiplexed | Negotiation and liveness from a single connection (Spark plan caps connections at 100) |
| 7 | Mouse coordinates normalized to `[0,1]` | Resolution independence; scales to `0..65535` with Windows `MOUSEEVENTF_ABSOLUTE` |
| 8 | Held keys/buttons tracked on both sides, released on blur/disconnect | A key pressed just before a tab switch must not stay pressed on the host |
| 9 | Frame timing from IVF pts (`-fps_mode passthrough`) | RTP timestamps follow real capture times; no drift when capture falls below 30 fps |
| 10 | RTCP is read from every sender | Pion's NACK/RR interceptors only work while somebody reads |
| 11 | ICE: public STUN only (no TURN) | No server to run; the config is left open to TURN (see 5.1) |
| 12 | Approval in a native Yes/No window | No console interaction; silence still means no |
| 13 | A shared protocol contract under `docs/` | Prevents Frontend/Host version drift |
| 14 | Files get a **second DataChannel**, rather than sharing the input one | `input` is ordered and reliable: behind 1 MiB of queued file data a mouse move lands ~1.6 s late on a 5 Mbit/s link, which makes remote control unusable |
| 15 | Text frames are control, binary frames are payload | One discriminator (`IsString`) instead of a header on every chunk; the channel is ordered, so the receiver already knows which file it is on |
| 16 | Transfers get their own parser instead of more fields on `input.Message` | That struct is decoded for every mouse move; a key reused with a different JSON type would break remote control itself |
| 17 | The viewer gates on the greeting's `caps`, never on the version string | A later agent can add a capability without the viewer knowing its version, and `0.2.1-dev`-style builds cannot be misread |
| 18 | One approval window per **batch**, not per file | Twenty windows teach the operator to click Yes without reading, which destroys the only real control in the system |
| 19 | Fixed destination, never overwrite, `.part` until complete, Mark of the Web | The destination cannot be talked upwards; a half-written installer cannot be run; Windows warns at the moment someone runs what arrived |
| 20 | Downloads have no Yes/No — the native picker **is** the consent | The viewer can only ask "choose me something"; a prompt in front of the picker carries no information the picker does not, and only adds a click |
| 21 | Clipboard as a sync, not an interception of Ctrl+C/Ctrl+V | The keystrokes keep being forwarded, so an agent that does not understand the feature still pastes normally instead of losing Ctrl+V entirely |

### 5.1 ICE / NAT traversal strategy
This project uses **public STUN only**:

- **STUN** (`stun:stun.l.google.com:19302` + fallbacks) is *not a relay*; it only lets the ends discover their own public `IP:port`. Media/data still flow **directly end-to-end**.
- **Known limit:** behind **symmetric NAT / CGNAT** (many mobile and some fibre ISPs) STUN may not be sufficient and the connection fails; a **TURN relay** would be needed.
- **Upgrade path:** the ICE server list is kept in a **single configuration point** (frontend `constants/webrtc.ts` and host `internal/webrtc/config.go`). Adding a `turn:` entry (+ credentials) is enough; the code does not change.

### 5.2 Desktop capture & video encoding
ffmpeg (`gdigrab` → `libvpx` VP8 → IVF over a pipe) keeps the agent free of CGO and native codecs; only `ffmpeg.exe` is needed at runtime and it ships in the release zip. `-fps_mode passthrough` preserves real capture timestamps (ffmpeg ≥ 5.1). Faster capture (`ddagrab`) and hardware H.264 are possible later without touching the WebRTC side.

---

## 6. Technology Versions (target)

| Technology | Version (target) |
|-----------|---------------|
| Vue | 3.5+ |
| Vite | 8+ |
| TypeScript | 5+ |
| Pinia | 3+ |
| Vue Router | 5+ |
| Bootstrap | 5.3+ |
| Firebase Web SDK | 12+ (modular) |
| Go | 1.24+ |
| Pion WebRTC | v4 |
| ffmpeg | 5.1+ (gdigrab, libvpx) |
