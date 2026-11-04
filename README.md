# FreeDesk

A small, free remote desktop: see and control another Windows PC or Mac from your browser. No accounts, no passwords, no server to run. One person starts a small program and reads a 6-digit code, the other types it into the web page, and the first person clicks **Yes**.

**Web page:** https://free-desk.web.app — connect from any browser, or download the host program there, for Windows or for a Mac.

## Using it

**On the computer that will be controlled: Windows**

1. Press **Download for Windows** on the web page (or take `freedesk.exe` from the [Releases](https://github.com/YahyaAltintop/freedesk/releases) page). That one file is the whole program: nothing to install or unpack.
2. Run `freedesk.exe`. A small window shows **this computer's code**. If Windows asks about network access, allow it, with both Private and Public ticked. If your security software asks instead (Norton and the like run the firewall in Windows' place), allow FreeDesk there.
3. Give the code to the person who should connect.
4. When they connect, a window asks whether to let them see your screen, control the computer, exchange files and share copied text. Click **Yes**. No answer within 45 seconds means no.
5. Close the FreeDesk window to stop. The code stops working at once; the next start gets a new one.

> **"Unknown publisher"?** The program is not code-signed yet, so the browser may ask whether to keep the download and Windows warns on first run. Keep it, then choose **More info → Run anyway**. To check what you downloaded, compare its SHA-256 with `freedesk.exe.sha256` on the release page, or run `gh attestation verify freedesk.exe --repo YahyaAltintop/freedesk`.

> **Blocked by antivirus?** Norton, Avast and AVG can block a new release, or its download, before anyone has run it: a verdict on a new, unsigned file's reputation ("FileRepMalware"). Check the file as above and report the detection from your antivirus. Why it happens and what the project does about it: [docs/ANTIVIRUS.md](docs/ANTIVIRUS.md).

**On the computer that will be controlled: a Mac** (macOS 13 or newer, Apple silicon or Intel)

1. Press **Download for Mac** on the web page (or take `FreeDesk.dmg` from the [Releases](https://github.com/YahyaAltintop/freedesk/releases) page). Open it and drag **FreeDesk** onto **Applications** in the window that appears. Run it from Applications, not from the disk image: from there macOS runs a hidden copy of it, and a `.env` next to it is not found.
2. Open FreeDesk. The first time, macOS refuses ("Apple could not verify…"): the app is not signed with an Apple Developer ID yet. Choose **Done** (**Cancel** on macOS 13 and 14), then **Open Anyway** in System Settings → Privacy & Security, and confirm.
3. FreeDesk asks for **Screen & System Audio Recording** (called Screen Recording before macOS 15; to show the screen) and **Accessibility** (to apply the other side's mouse and keyboard). Allow both in System Settings → Privacy & Security, then quit FreeDesk and open it again: macOS applies Screen Recording at the next start. macOS may also ask about the **Downloads** folder (where files you accept go) and, from macOS 15, about devices on your **local network** (the other computer may be one): allow those too. Every month or so macOS asks again whether FreeDesk may keep recording the screen, as it does for every screen-sharing program; choose to keep allowing it.
4. A small window shows **this computer's code**. Give it to the person who should connect.
5. When they connect, a box asks whether to let them in, above every window, and the Dock icon bounces. Click **Yes**; Return answers No. No answer within 45 seconds means no.
6. Close the window or quit FreeDesk (⌘Q) to stop. As on Windows, the code stops working at once.

> **Copied text on a Mac.** From macOS 15.4, macOS asks before a program reads what you copied. FreeDesk sends what you copy to the other side only once you have allowed it: System Settings → Privacy & Security → **Paste from Other Apps** → FreeDesk → **Always Allow**. FreeDesk shows up there after the first time you copy something during a session. Text and files the other side sends you arrive either way.

> **Is the Mac's firewall on?** If macOS asks whether FreeDesk may accept incoming network connections, choose **Allow**. If you said Deny, or turned on *Block all incoming connections*, FreeDesk says so in its window, with where to change it.

Good to know:

- **Updating.** When the window shows a purple **Update to …** button, download the new `freedesk.exe`, close FreeDesk (a running program cannot be replaced), and put the new file in place of the old one: same folder, same name (a browser may have saved it as `freedesk (1).exe`). Network permissions, Windows' or your security software's, belong to that place; anywhere else they start over. On a Mac, quit FreeDesk and replace FreeDesk in Applications with the new one. While the app is not signed with a Developer ID, macOS treats each update as a new program: it asks for Screen Recording and Accessibility again, and the old FreeDesk may need removing from those lists (select it, then **–**) before the new one works. The program never updates itself.
- **The first connection after a download fails?** Security software with its own firewall (Norton and the like) can take a while to decide about a new program it has never seen, and keep connections out until it has. Close FreeDesk, start it again, and give the other person the new code.
- **A new code by itself.** Three connection requests in a row without a yes retire the code and the window shows a new one: whoever you expect needs that one.
- **Switches.** A text file named `.env` next to `freedesk.exe`, or next to `FreeDesk.app` on a Mac, can hold `RC_CLIPBOARD=off` (no clipboard sharing) or `RC_UPDATE_CHECK=off` (no question to GitHub at start-up).
- **Controlling a Mac from Windows.** The Mac's ⌘ key is the Windows key on a Windows keyboard, and Windows keeps that key to itself: shortcuts with ⌘ (copy, paste, switching apps) cannot be sent from there. Use the Mac's menus for those (Edit → Copy), or connect from a Mac. Control, Option (Alt) and Shift go through as they are.

**On the computer that connects**

1. Open https://free-desk.web.app, type the code, press **Connect**.
2. Wait for the other person to click Yes. The remote screen appears; click it to control. **End Session** disconnects.
3. Files: drag them onto the remote screen to send, or **Files → Get files…** to receive. Text you copy is shared both ways while connected; copied files too.

## What it does not do (yet)

- Windows 10/11 and macOS 13+ hosts (the viewer runs in any modern browser). Primary monitor only. No audio.
- No unattended access: someone must click Yes. It cannot pass UAC prompts, a Mac's password prompts or the lock screen.
- Files only, never folders. An interrupted transfer starts over. The clipboard carries text and files, not images.
- Some networks (mobile data, CGNAT) cannot connect directly: there is no relay server.

## How it works

- The **host** (Go + Pion WebRTC: `freedesk.exe`, one self-contained exe, or `FreeDesk.app`) captures the screen (Windows' desktop duplication, macOS's ScreenCaptureKit), encodes it with libvpx built into the program, and applies mouse and keyboard input. The **viewer** is a Vue 3 web page.
- **Firebase** (anonymous auth + Realtime Database) is used only to find each other and exchange the WebRTC handshake. **Screen and input never pass through Firebase**; they travel directly between the two computers over an encrypted WebRTC connection.
- Every launch of the host is a **new anonymous identity and a new code**; nothing is stored on disk and both are deleted when it exits. Session data is deleted when the session ends, even when a browser tab is closed abruptly.
- What leaves your computer, where it goes and how long it stays: [PRIVACY.md](PRIVACY.md), also behind the **Privacy** button in the program's window.
- Design, data model, security model and known limits: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). The wire contract between viewer and host: [docs/PROTOCOL.md](docs/PROTOCOL.md).

## Running your own copy

You need a free Firebase project (the Spark plan is enough).

1. **Firebase:** create a project, enable Anonymous sign-in, create a Realtime Database, deploy the rules — [firebase/README.md](firebase/README.md).
2. **Viewer:** `cd frontend && npm install && cp .env.example .env && npm run dev` — [frontend/README.md](frontend/README.md).
3. **Host:** in `host-agent/`, build libvpx once (`bash scripts/build-libvpx.sh`, from an MSYS2 UCRT64 shell on Windows, from any shell on a Mac), then `cp .env.example .env && go run ./cmd/host` with cgo on — [host-agent/README.md](host-agent/README.md).
4. **Releases:** pushing to `main` deploys the viewer; pushing a tag `vX.Y.Z` builds and publishes `freedesk.exe` and `FreeDesk.dmg` — [docs/RELEASING.md](docs/RELEASING.md).

## Development

Go 1.26+, Node.js 20+, Firebase CLI 13+ (the emulators need JDK 21+), and a C toolchain for the host's cgo build of libvpx: MSYS2's UCRT64 gcc and nasm on Windows, Xcode 16.3 or newer (or its command line tools) and nasm on a Mac ([host-agent/README.md](host-agent/README.md#build--run)).

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

MIT — see [LICENSE](LICENSE). The licenses of the code from other projects built into the program (libvpx, Go, Pion, and in `freedesk.exe` the MinGW-w64 runtime) are inside it: the **Licenses** button in its window shows them, and `freedesk.exe --licenses > licenses.txt` (on a Mac, `/Applications/FreeDesk.app/Contents/MacOS/freedesk --licenses > licenses.txt`) saves them without starting anything.
