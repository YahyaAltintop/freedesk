# Releasing

How the host programs, for Windows and for macOS, are built and published, what a release holds, and how a download can be checked.

## What a tag does

Push a tag `vX.Y.Z` (for example `v0.3.1`). `.github/workflows/release.yml` then checks the configuration once, builds the Windows exe and the Mac app side by side, each with no module or build cache from an earlier run, and publishes the release only when both are built and checked: a release never has one without the other.

First, on a Linux runner, it checks the repository variables below and probes the agent's API key **without a referrer**. A key limited to websites would make every published program exit at start-up, so nothing is built with it.

Then, on a Windows runner:

1. Installs MSYS2's UCRT64 gcc and nasm (`msys2/setup-msys2` installs a fresh MSYS2 and updates it) and builds libvpx from source with `host-agent/scripts/build-libvpx.sh`: the VP8 encoder only, as a static library, from a pinned commit the script checks. Never restored from a cache.
2. Writes the license texts with `cmd/notices` — FreeDesk's own MIT license, then those of the build's own modules, libvpx and the MinGW-w64 and GCC runtimes — into `host-agent/internal/licenses/generated/LICENSES.txt`, where the build embeds them. The release is the exe alone, so the texts those licenses ask to accompany a binary travel inside it.
3. Compiles the exe's icon, version information and application manifest from `host-agent/cmd/host/winres/` with [go-winres](https://github.com/tc-hib/go-winres) (a tool dependency pinned in `go.sum`), stamped with the tag's version, and builds `freedesk.exe` with cgo as a windowed program (`-H=windowsgui`) with the Firebase project and the repository name embedded (the agent asks that repository's latest release whether it is out of date). A build whose version resource did not take is refused.
4. Runs `cmd/importcheck` on the exe: it must import no DLL that Windows does not ship (a `libwinpthread-1.dll`, `libgcc_s_seh-1.dll` or `libvpx` import would make it fail on every machine without MSYS2), must have been built with cgo (no cgo, no video) and must carry, byte for byte, the license texts of step 2.
5. Has SignPath sign the exe, but only once the repository is enrolled (see [Code signing](#code-signing-and-the-unknown-publisher-warning)): the job waits for the release to be approved on signpath.io, then checks the signed file before it replaces the unsigned one.
6. Writes `freedesk.exe.sha256` (after signing, which rewrites the file) and signs a build provenance attestation for the exe.

At the same time, on a macOS runner (Apple silicon; the Intel half is built there too):

1. Installs nasm from Homebrew for the Intel half of libvpx and builds libvpx from source with the same script, for Apple silicon and for Intel, joined into one universal library. The Xcode is the runner image's; its SDK must be macOS 15.4 or newer, or the build stops with a message saying so.
2. Imports the *Developer ID Application* certificate into a keychain of the run's own, when the Apple secrets exist ([Signing the Mac app](#signing-the-mac-app)).
3. Writes the license texts as for Windows (without the MinGW sections: nothing of MinGW is in the app) and runs `host-agent/scripts/bundle-mac.sh` with the same settings embedded. The script builds `cmd/host` for Apple silicon and for Intel from an empty cache, for macOS 13, joins the two with `lipo`, checks that both halves are for macOS 13 and that `importcheck` accepts the program (only libraries and frameworks macOS ships, built with cgo, the exact license texts), puts it in `FreeDesk.app` (`host-agent/cmd/host/macos/Info.plist`, an icon made from the exe's), signs the app, with the Developer ID and the hardened runtime when there is one and ad hoc otherwise, and packs it into `FreeDesk.dmg`, a disk image with a link to Applications, signed too when there is a certificate.
4. With the Apple secrets, has Apple notarize the app (`xcrun notarytool`, waiting up to an hour) and staples the ticket to it; then makes the disk image around the stapled app (`bundle-mac.sh --dmg-only`) and has that notarized and stapled in turn. So the download opens without asking Apple, and so does the app dragged out of it, on a Mac that is offline too. A rejected submission prints Apple's log and fails the job.
5. Writes `FreeDesk.dmg.sha256`, in the same layout as the exe's, and signs a build provenance attestation for the disk image.

Last, on a Linux runner, it publishes the four files on the GitHub release, with a few lines on running, checking and updating each program above the generated notes. What those lines say about each signature follows the published file: the exe's Authenticode signature, the app's Developer ID signature with the tickets stapled to the app and to the disk image.

A tag with a hyphen (`v0.9.0-rc1`) is published as a **pre-release**. GitHub's "latest release", which the web page's Download button and the program's update check both ask for, skips pre-releases, so nobody is offered one: use them to try a release, the signing in particular.

A release holds:

```
freedesk.exe          the Windows host program: capture, VP8 encoder (libvpx), license texts and all, one file
freedesk.exe.sha256   its SHA-256, in the layout sha256sum -c reads
FreeDesk.dmg          the Mac host program: a disk image that opens on FreeDesk.app, for Apple silicon and Intel
FreeDesk.dmg.sha256   its SHA-256, in the same layout
```

The licenses are read from the program: its **Licenses** button, or `freedesk.exe --licenses > licenses.txt` (`FreeDesk.app/Contents/MacOS/freedesk --licenses` on a Mac), which writes them and exits without loading any configuration or going online.

Everything the release runs or downloads is pinned (actions by commit SHA, libvpx by commit, go-winres by `go.sum`) except the C toolchains: MSYS2's packages are signature-checked by `pacman` but come in whatever version MSYS2 currently ships, and on the Mac the Xcode is the runner image's and nasm is Homebrew's.

The web page's **Download for Windows** and **Download for Mac** buttons look up the latest release and link to the assets with exactly these names (`HOST_ASSET_NAMES` in `frontend/src/constants/links.ts`, `ASSET_NAME` and `ASSET_NAME_MAC` in `release.yml`). Rename both together or neither. Pushing to `main` deploys the web page at once, and until a release with that asset exists the button falls back to the release's page; tag the release right after pushing such a change.

## Repository settings

Settings → Secrets and variables → Actions.

**Variables** (none is secret: the security rules protect the data):

| Variable | Used by | Meaning |
|---|---|---|
| `FIREBASE_API_KEY`, `FIREBASE_AUTH_DOMAIN`, `FIREBASE_PROJECT_ID`, `FIREBASE_APP_ID`, `FIREBASE_DATABASE_URL` | viewer deploy, release | the web app's Firebase config |
| `FIREBASE_AGENT_API_KEY` | release | an API key of the same project **without application restrictions**: a desktop program sends no website referrer, so a website-restricted key rejects it. If the key has API restrictions they must allow *Identity Toolkit API* and *Token Service API*. Falls back to `FIREBASE_API_KEY`. See [firebase/README.md → API keys](../firebase/README.md#api-keys). |
| `SIGNPATH_ORGANIZATION_ID` | release (with the token below) | the SignPath organization that signs the exe; `SIGNPATH_PROJECT_SLUG` and `SIGNPATH_SIGNING_POLICY_SLUG` only if they are not `freedesk` and `release-signing` ([Code signing](#code-signing-and-the-unknown-publisher-warning)) |

**Secrets:**

| Secret | Used by | Meaning |
|---|---|---|
| `FIREBASE_SERVICE_ACCOUNT` | viewer deploy | a service-account JSON key with the Firebase Hosting Admin role |
| `SIGNPATH_API_TOKEN` | release (optional) | the API token of a SignPath user with the *submitter* role in the FreeDesk project; signing is skipped while it is missing |
| `APPLE_CERT_P12` | release (optional) | the *Developer ID Application* certificate with its private key, exported as `.p12` and base64-encoded; the Mac app is signed ad hoc and not notarized while it is missing |
| `APPLE_CERT_PASSWORD` | release, with the above | the password the `.p12` was exported with |
| `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD` | release, with the above | the Apple Account that notarizes, its team's ID and an app-specific password for it ([Signing the Mac app](#signing-the-mac-app)) |

Pushing to `main` deploys the viewer (`deploy-web.yml`). The database rules are deployed by hand, once and after every change to `firebase/database.rules.json`: `cd firebase && firebase deploy --only database`.

## Checking a download

- Compare the file's SHA-256 with the `.sha256` file on the release page: `sha256sum -c freedesk.exe.sha256`, or `Get-FileHash freedesk.exe` on Windows; `shasum -a 256 -c FreeDesk.dmg.sha256` on a Mac. (A browser that saved it as `freedesk (1).exe` changed only the name.)
- Or verify the provenance attestation: `gh attestation verify freedesk.exe --repo YahyaAltintop/freedesk` (or `FreeDesk.dmg`). It is a signed statement that this workflow, at this commit, produced exactly this file.
- On a Mac, `spctl --assess --type open --context context:primary-signature -vv FreeDesk.dmg` says `source=Notarized Developer ID` once the app is signed and notarized; before that it says `rejected`.

## Code signing and the "Unknown publisher" warning

The exe is not signed yet. SmartScreen warns on first run ("Windows protected your PC", publisher unknown), and antivirus products that judge new files by reputation can block it outright: Norton quarantined v0.8.0 as a file "very few people have used", and on VirusTotal a dozen engines (Avast, AVG, Symantec, Bitdefender and the products built on it, McAfee's reputation service) flagged it without naming anything specific. Norton then did the same to v0.9.0 and had Safe Web stop its download from GitHub. What users are told: [ANTIVIRUS.md](ANTIVIRUS.md); how each release is reported: [Antivirus false positives](#antivirus-false-positives). The version resource makes Windows say *FreeDesk* in the firewall prompt and in Task Manager, but only an Authenticode signature lets reputation build up from one release to the next, and even a signed program can be warned about until it has earned some.

The plan is the **SignPath Foundation**: free signing for open-source projects, with a certificate issued to *SignPath Foundation*, which is therefore the publisher Windows shows. Their [conditions](https://signpath.org/terms) ask for a project with verifiable reputation, so apply once FreeDesk has some (users, stars, a few months of releases); they decline projects that are too new and there is no appeal. They also scan every file before signing it, so clear any antivirus false positives on the latest release first. Alternatives, if it comes to that: Azure Artifact Signing does not take individuals outside the US and Canada; a Certum *Open Source* certificate names the developer as publisher, but signs interactively, so the SignPath steps would give way to a manual one.

**The release workflow is ready.** Once the `SIGNPATH_API_TOKEN` secret exists, every tag uploads the unsigned exe as an artifact of the run, asks SignPath to sign it with `signpath/github-action-submit-signing-request`, and **waits up to an hour for the release to be approved on signpath.io**. Before the signed exe replaces the unsigned one, the job checks that the signature is valid, timestamped and by SignPath Foundation, that the version resource is intact and that `importcheck` still passes. Only then come the checksum, the attestation and the release page, which says the program is signed and links to the code signing policy.

### Applying

1. Turn on two-factor authentication on GitHub (SignPath requires it for everyone in the project, on SignPath too).
2. On the day you apply, and not before, add the code signing policy below to the README, and a **Code signing policy** link to the web page's footer (`frontend/src/components/AppFooter.vue`, pointing at the README section).
3. Apply at https://signpath.org/apply. [PRIVACY.md](../PRIVACY.md) is the privacy policy they ask for; the program's **Privacy** button, the web page's footer and the release page link to it.

The README section, word for word as SignPath asks:

```markdown
## Code signing policy

Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org).

- Committers and reviewers: [Yahya Altıntop](https://github.com/YahyaAltintop)
- Approvers: [Yahya Altıntop](https://github.com/YahyaAltintop)

Every release is built from this repository by its public workflow (`.github/workflows/release.yml`) on GitHub's own runners, and approved by hand before it is signed. Privacy policy: [PRIVACY.md](PRIVACY.md).
```

### Once accepted

On signpath.io:

1. Add the predefined trusted build system **GitHub.com** to the organization and link it to the project (installing the SignPath GitHub App lets it read the audit log too). The project slug the workflow expects is `freedesk`.
2. Make this the project's default artifact configuration. The workflow uploads the exe itself (`archive: false`), so the root is the `<pe-file>`, and it passes the tag's version as `version`:

   ```xml
   <?xml version="1.0" encoding="utf-8"?>
   <artifact-configuration xmlns="http://signpath.io/artifact-configuration/v1">
     <parameters>
       <parameter name="version" required="true" />
     </parameters>
     <pe-file product-name="FreeDesk" product-version="${version}">
       <authenticode-sign />
     </pe-file>
   </artifact-configuration>
   ```

   `version` is the tag without its `v` (`0.9.0`, `0.9.0-rc1`), the exe's ProductVersion string. Its numeric version resource drops a `-rc1`; if SignPath compares against that, the first pre-release will say so.
3. Signing policies: `release-signing` (the SignPath Foundation certificate, approval required) is what the workflow uses. Restrict it to GitHub-hosted runners and to the release tags (`refs/tags/v*`), and check on the first run that a tag passes the branch rule.
4. Create an API token for a user with the *submitter* role, store it as the `SIGNPATH_API_TOKEN` secret, and set the `SIGNPATH_ORGANIZATION_ID` variable (plus `SIGNPATH_PROJECT_SLUG` / `SIGNPATH_SIGNING_POLICY_SLUG` if they differ from `freedesk` / `release-signing`).

Then try it with a pre-release tag (`v0.9.0-rc1`): approve it on signpath.io, download the pre-release, look at the signature in the exe's Properties → Digital Signatures, and only then tag the real release. Setting `SIGNPATH_SIGNING_POLICY_SLUG=test-signing` for a dry run goes through the whole round trip but stops at *Check the signature* (the test certificate is not SignPath Foundation's), so nothing is published.

**Every release after that:** push the tag, then approve the signing request on signpath.io within the hour (the job waits; after that it fails, and re-running it asks again).

When the first signed release is out, drop the "not code-signed yet" notes from the README and from the web page's download card (`frontend/src/pages/HomePage.vue`).

## Signing the Mac app

Until the Apple secrets exist, `FreeDesk.app` is signed **ad hoc**: a signature with no publisher, the least macOS needs before it lets the operator give a program Screen Recording and Accessibility. Gatekeeper refuses such an app from the internet ("Apple could not verify…"); the operator opens it once, then chooses **Open Anyway** in System Settings → Privacy & Security. And because an ad-hoc signature names the exact build, macOS treats every release as a new program and asks for its permissions again after each update. The release page and the web page say so.

A **Developer ID** signature and Apple's **notarization** remove both: macOS opens the app like any other from the internet, and the permissions stay with it from one release to the next (they belong to the bundle identifier and the Developer ID; the identifier, `io.github.yahyaaltintop.freedesk`, is in `host-agent/cmd/host/macos/Info.plist`, and changing it makes every Mac ask again). It needs a membership of the [Apple Developer Program](https://developer.apple.com/programs/) (99 USD a year); SignPath's free signing covers Windows only.

Two more things come only with a Developer ID. Apple's [TN3179](https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy) says local network privacy (macOS 15 and newer) tracks a program by its code signature and tracks ad-hoc signed code unreliably: with a Developer ID the operator's Local Network answer sticks like the others. And macOS asks every month or so whether a program may keep recording the screen; for remote desktops Apple has the **Persistent Content Capture** entitlement (`com.apple.developer.persistent-content-capture`, macOS 14.4 and newer, "a VNC app [that] needs persistent access to screen capture"), the route it offers against those reminders. It is requested with [Apple's form](https://developer.apple.com/contact/request/persistent-content-capture/) once the membership exists, and using it means signing the app with an entitlements file and embedding the provisioning profile Apple issues for it, which `scripts/bundle-mac.sh` does not do yet.

**The release workflow is ready.** Once the secrets below exist, every tag signs the app with the Developer ID and the hardened runtime (the app needs no entitlements: it is not sandboxed, and Screen Recording, Accessibility and the rest are the operator's to grant), has Apple notarize it and staples the ticket to it, then makes the disk image around it, signed, notarized and stapled in turn.

1. Join the Apple Developer Program, as an individual or an organization; the name on the certificate is the publisher macOS shows.
2. Create a **Developer ID Application** certificate: in Xcode, Settings → Accounts → Manage Certificates → +, or on developer.apple.com with a signing request from Keychain Access.
3. In Keychain Access, export the certificate with its private key as a `.p12`, with a password. Store the output of `base64 -i certificate.p12` as the `APPLE_CERT_P12` secret and the password as `APPLE_CERT_PASSWORD`. Delete the `.p12` file afterwards.
4. At [account.apple.com](https://account.apple.com) → Sign-In and Security → App-Specific Passwords, create one for the workflow: `APPLE_APP_PASSWORD`. `APPLE_ID` is that account's e-mail address, `APPLE_TEAM_ID` the team's ID (developer.apple.com → Account → Membership details).

Then try it with a pre-release tag (`v0.9.0-rc1`): download `FreeDesk.dmg` on a Mac that never had FreeDesk, open it, drag FreeDesk onto Applications and open it: macOS should only ask whether to open an app downloaded from the internet. When the first notarized release is out, drop the "not signed with an Apple Developer ID yet" notes from the README and from the web page's download card (`frontend/src/pages/HomePage.vue`).

## Antivirus false positives

Products that judge files by reputation hold back every new, unsigned release until they have seen enough of it, and Gen Digital's (Norton, Avast, AVG) has called two of them `FileRepMalware` ([ANTIVIRUS.md](ANTIVIRUS.md)). A verdict belongs to one file, so every release starts from zero. Report each one right after the workflow publishes it, before announcing it:

1. **Gen Digital.** Once the project is in Avast's whitelisting program (register at https://www.avast.com/whitelist-program-registration; the form says its files count for Avast, AVG, Norton and Avira), upload the release's `freedesk.exe`, zipped, to the program's SFTP server: it is processed within a day, without a reply. Download it for that on a computer without Norton, Avast or AVG: on one with them the new file is held for their own verdict, and may be quarantined, before the upload has been processed, and Safe Web may stop the download altogether. (With the SFTP credentials stored as a secret, the release workflow could upload it itself.) Until then, and for a release that is flagged anyway, use Norton's form, https://submissions.norton.com/reportfalsepositive, once on the File tab and once on the URL tab with the asset's download link, and Avast's, https://www.avast.com/report-false-positive. The detection name is the one the product shows (`FileRepMalware [Misc]` so far); the form gives no receipt, so note the date.
2. **VirusTotal.** Let it scan the asset's download link (https://www.virustotal.com/gui/home/url), then report the release to each vendor that flags it: Symantec at https://symsubmit.symantec.com/false_positive, Bitdefender at https://www.bitdefender.com/consumer/support/answer/29358/ (the products built on its engine follow it), the others through the contacts VirusTotal lists ([docs.virustotal.com/docs/false-positive](https://docs.virustotal.com/docs/false-positive)). Microsoft only if Defender flags it.
3. **Log** each report in the table at the end of [ANTIVIRUS.md](ANTIVIRUS.md#what-the-project-does).

Never rebuild or re-release only to get a new hash, pack or obfuscate the exe, or host it somewhere else: the verdict follows the file, and a new hash starts from zero again.

The description every form asks for, with the version and the hash filled in:

```text
FreeDesk X.Y.Z (freedesk.exe) is an open-source (MIT) remote desktop host for Windows.
Source: https://github.com/YahyaAltintop/freedesk
Built only by the public GitHub Actions workflow .github/workflows/release.yml from tag vX.Y.Z.
Build provenance: gh attestation verify freedesk.exe --repo YahyaAltintop/freedesk
SHA-256: <the line in freedesk.exe.sha256 on the release page>
It shows the computer's screen to one viewer and applies their mouse and keyboard input only
after the person at the computer clicks Yes in a consent window. Direct WebRTC connection,
paired through Firebase. No installer, service, autostart, elevation, packing or obfuscation.
Privacy policy: https://github.com/YahyaAltintop/freedesk/blob/main/PRIVACY.md
```

## Updating libvpx

Pick a newer release tag of [libvpx](https://chromium.googlesource.com/webm/libvpx) (also mirrored at github.com/webmproject/libvpx), look up the commit it points at (`git ls-remote https://chromium.googlesource.com/webm/libvpx refs/tags/vX.Y.Z`, the peeled `^{}` line for an annotated tag), and change `LIBVPX_TAG` and `LIBVPX_COMMIT` in `host-agent/scripts/build-libvpx.sh` together. The build refuses a checkout whose commit differs. Locally, delete `host-agent/third_party/` and run the script again. Before releasing, compare the new encoder with the old on the same frames: with an ffmpeg at hand, `RC_FFMPEG_GOLDEN=<ffmpeg.exe> go test -run EncoderMatchesFFmpeg -v ./internal/vpx/` prints bitrate and frame sizes for both.
