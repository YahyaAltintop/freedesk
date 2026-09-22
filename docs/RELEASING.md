# Releasing

How the Windows host program is built and published, what the zip contains, and how a download can be checked.

## What a tag does

Push a tag `vX.Y.Z` (for example `v0.3.1`). `.github/workflows/release.yml` then, on a Windows runner:

1. Checks the repository variables below and probes the agent's API key **without a referrer**. A key limited to websites would make every published exe exit at start-up, so the build refuses to go on.
2. Compiles the exe's icon, version information and application manifest from `host-agent/cmd/host/winres/` with [go-winres](https://github.com/tc-hib/go-winres) (a tool dependency pinned in `go.sum`), stamped with the tag's version, and builds `freedesk.exe` as a windowed program (`-H=windowsgui`) with the Firebase project and the repository name embedded (the agent asks that repository's latest release whether it is out of date). A build whose version resource did not take is refused.
3. Signs the exe, but only when the signing secrets exist (see below).
4. Downloads the pinned ffmpeg build, verifies its SHA-256 before unpacking, and checks it has `gdigrab` and `libvpx`.
5. Assembles `freedesk-windows-x64.zip`, verifies its layout is exactly the one below, writes `freedesk-windows-x64.zip.sha256`, signs a build provenance attestation and publishes both files on the GitHub release with generated notes.

The zip:

```
freedesk.exe          the host program
README.txt            what to do with it (host-agent/release/README.txt)
LICENSE.txt           MIT
ffmpeg/ffmpeg.exe     screen capture and VP8 encoding (unmodified BtbN LGPL build)
ffmpeg/LICENSE.txt    ffmpeg's license
```

The web page's **Download for Windows** button looks up the latest release and links to the asset with exactly this name (`frontend/src/constants/links.ts`). Rename both together or neither.

## Repository settings

Settings → Secrets and variables → Actions.

**Variables** (none is secret: the security rules protect the data):

| Variable | Used by | Meaning |
|---|---|---|
| `FIREBASE_API_KEY`, `FIREBASE_AUTH_DOMAIN`, `FIREBASE_PROJECT_ID`, `FIREBASE_APP_ID`, `FIREBASE_DATABASE_URL` | viewer deploy, release | the web app's Firebase config |
| `FIREBASE_AGENT_API_KEY` | release | an API key of the same project **without application restrictions**: a desktop program sends no website referrer, so a website-restricted key rejects it. If the key has API restrictions they must allow *Identity Toolkit API* and *Token Service API*. Falls back to `FIREBASE_API_KEY`. See [firebase/README.md → API keys](../firebase/README.md#api-keys). |

**Secrets:**

| Secret | Used by | Meaning |
|---|---|---|
| `FIREBASE_SERVICE_ACCOUNT` | viewer deploy | a service-account JSON key with the Firebase Hosting Admin role |
| `CODESIGN_PFX_BASE64`, `CODESIGN_PFX_PASSWORD` | release (optional) | a code-signing certificate as a base64-encoded PFX, and its password |

Pushing to `main` deploys the viewer (`deploy-web.yml`). The database rules are deployed by hand, once and after every change to `firebase/database.rules.json`: `cd firebase && firebase deploy --only database`.

## Checking a download

- Compare the zip's SHA-256 with the `.sha256` file on the release page: `sha256sum -c freedesk-windows-x64.zip.sha256`, or `Get-FileHash freedesk-windows-x64.zip` on Windows.
- Or verify the provenance attestation: `gh attestation verify freedesk-windows-x64.zip --repo YahyaAltintop/freedesk`. It is a signed statement that this workflow, at this commit, produced exactly this file.

## Code signing and the "Unknown publisher" warning

The exe is not signed, so SmartScreen warns on first run ("Windows protected your PC", publisher unknown). The version resource makes Windows say *FreeDesk* in the firewall prompt and in Task Manager, but only an Authenticode signature changes the warning, and a freshly signed program can still be warned about until it has earned reputation.

The workflow already has the signing step. It runs when the two `CODESIGN_*` secrets exist and is skipped otherwise, so adding a certificate later means adding the secrets, nothing else. Ways to get one:

- **SignPath Foundation**: free signing for open-source projects after an application. It signs through its own GitHub Action rather than a PFX, so the step would be swapped for that action.
- **Azure Trusted Signing**: Microsoft's signing service (subscription, identity validation), used through `azure/trusted-signing-action`; the same swap applies.
- **A certificate from a CA** (yearly fee): if the key can be exported, base64 the PFX into `CODESIGN_PFX_BASE64`. Certificates issued since mid-2023 usually keep the key on a hardware token or an HSM, in which case the CA's cloud-signing action replaces the PFX step.

## Updating ffmpeg

Pick a newer `autobuild-*` release of [BtbN/FFmpeg-Builds](https://github.com/BtbN/FFmpeg-Builds/releases) (the `win64-lgpl` zip), take its SHA-256 from the release page, and change `FFMPEG_ZIP_URL` and `FFMPEG_ZIP_SHA256` in `release.yml` together. The build fails when the hash does not match or the build lacks `gdigrab` or `libvpx`.
