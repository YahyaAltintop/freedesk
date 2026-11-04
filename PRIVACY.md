# Privacy

FreeDesk has no accounts, no analytics, no ads and no server of its own. This page lists everything that leaves your computer, where it goes, and how long it stays there. It covers the host program from this repository's releases, `freedesk.exe` on Windows and `FreeDesk.app` on a Mac, and the web page at https://free-desk.web.app. Someone who runs their own copy uses their own Firebase project in place of ours.

## What goes where

**Between the two computers, and nowhere else:** the screen, mouse and keyboard input, copied text and files, and the files you send. They travel directly between the two computers over an encrypted WebRTC connection. No server sees them, and nothing records them.

**Firebase (Google), to find each other.** FreeDesk uses Firebase Authentication and the Firebase Realtime Database only to pair the two sides and exchange the WebRTC handshake.

- Each start of the host program creates a new anonymous Firebase account (a random id, no name or e-mail).
- While it runs, the database holds a record under its 6-digit code: that random id, the **computer's name** (the Windows computer name, or the Mac's network name without `.local`, unless `RC_HOST_NAME` says otherwise), the program's version and the time it was last seen. Anyone signed in to the web page who types the code can read this record. That is how they can see which computer they are asking.
- A connection request holds the code, both random ids, its status and the WebRTC handshake. The handshake contains both computers' **IP addresses**, local network addresses included, because that is what lets them reach each other. The other side sees them; so does anyone who can read the database (the operator of the Firebase project).
- The browser gets an anonymous Firebase account too. It keeps that random id in its storage and uses it again on the next visit.

**Google's public STUN servers** (`stun.l.google.com`) see your public IP address. Both sides ask them how they appear from the internet, nothing more.

**GitHub.** At start-up, the host program asks `api.github.com` once whether a newer release exists. The web page asks the same to show the newest version. GitHub sees your IP address and, from the program, its version.

**Google's web hosting** serves the web page and sees your IP address, like any web server.

**On your own computer.** The web page asks the host program on the same computer (`127.0.0.1:47800`) for its code, so it can show it to you. That request never leaves the computer, and other websites open in your browser cannot read the answer.

To tell you when the firewall would keep the other computer out, the host program also reads, and never changes, the firewall's settings. On Windows that is the firewall's rules, the kind of network you are connected to (private, public or domain) and which security product, if any, runs the firewall in Windows' place; on a Mac, whether the firewall is on, whether it blocks all incoming connections and whether it blocks FreeDesk (asked of macOS's own `socketfilterfw`). None of it leaves the computer: a product's name appears only in the program's window, when a connection fails.

A file the other side sends you is marked as come from another computer, the way a browser marks a download, so the system checks it before anything of it runs: on Windows with the "Mark of the Web", on a Mac with the quarantine mark, which macOS also notes in its own log of downloaded files on the Mac, as it does for every browser download.

## What is kept, and for how long

- **The host program writes nothing to disk**: no settings and no log file. The Activity pane in its window is the only log, and it disappears when the window closes. The only files it writes are files the other side sends you, after you accept them. (The permissions you give it on a Mac, Screen Recording and the like, macOS keeps in its own settings.)
- **When the host program exits normally**, it ends any session, deletes its code's record and deletes its anonymous account.
- **When it is killed or crashes**, nothing cleans up after it at once: the code's record may stay. Viewers treat it as offline after 90 seconds, a viewer that looks the code up after 5 minutes removes it, and a new start that draws the same code replaces it. The anonymous account stays in the Firebase project.
- **A session's records** are deleted when it ends, also when a browser tab is closed abruptly. If both sides die before the session is set up, its record may stay: after an hour any client that knows its random id may delete it, but nothing does so on its own.
- **The browser** keeps its anonymous id, the theme you picked, and for the current tab the newest version number. Clearing the site's data removes them.

## What you can turn off

A text file named `.env` next to `freedesk.exe`, or next to `FreeDesk.app` on a Mac, can hold:

- `RC_UPDATE_CHECK=off`: no question to GitHub at start-up.
- `RC_HOST_NAME=<a name>`: show this name instead of the computer's name.
- `RC_CLIPBOARD=off`: no clipboard sharing. On a Mac, macOS also lets FreeDesk read what you copy only once you allow it (Privacy & Security → Paste from Other Apps); until then nothing you copy is sent.

Pairing through Firebase cannot be turned off: without it the two computers cannot find each other.

## The services involved

- Google Firebase: https://firebase.google.com/support/privacy
- Google: https://policies.google.com/privacy
- GitHub: https://docs.github.com/en/site-policy/privacy-policies/github-general-privacy-statement

## Questions

FreeDesk is a personal open-source project by Yahya Altıntop. Ask anything by opening an issue at https://github.com/YahyaAltintop/freedesk/issues.
