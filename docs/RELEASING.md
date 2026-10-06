# Releasing

How the Windows host program is built and published, what a release holds, and how a download can be checked.

## What a tag does

Push a tag `vX.Y.Z` (for example `v0.3.1`). `.github/workflows/release.yml` then, on a Windows runner:

1. Checks the repository variables below and probes the agent's API key **without a referrer**. A key limited to websites would make every published exe exit at start-up, so the build refuses to go on.
2. Installs MSYS2's UCRT64 gcc and nasm (`msys2/setup-msys2` installs a fresh MSYS2 and updates it) and builds libvpx from source with `host-agent/scripts/build-libvpx.sh`: the VP8 encoder only, as a static library, from a pinned commit the script checks. Never restored from a cache.
3. Writes the license texts with `cmd/notices` — FreeDesk's own MIT license, then those of the build's own modules, libvpx and the MinGW-w64 and GCC runtimes — into `host-agent/internal/licenses/generated/LICENSES.txt`, where the build embeds them. The release is the exe alone, so the texts those licenses ask to accompany a binary travel inside it.
4. Compiles the exe's icon, version information and application manifest from `host-agent/cmd/host/winres/` with [go-winres](https://github.com/tc-hib/go-winres) (a tool dependency pinned in `go.sum`), stamped with the tag's version, and builds `freedesk.exe` with cgo as a windowed program (`-H=windowsgui`) with the Firebase project and the repository name embedded (the agent asks that repository's latest release whether it is out of date). A build whose version resource did not take is refused.
5. Runs `cmd/importcheck` on the exe: it must import no DLL that Windows does not ship (a `libwinpthread-1.dll`, `libgcc_s_seh-1.dll` or `libvpx` import would make it fail on every machine without MSYS2), must have been built with cgo (no cgo, no video) and must carry, byte for byte, the license texts of step 3.
6. Signs the exe, but only when the signing secrets exist (see below).
7. Writes `freedesk.exe.sha256` (after signing, which rewrites the file), signs a build provenance attestation for the exe and publishes both files on the GitHub release, with a few lines on running and checking it above the generated notes.

A release holds:

```
freedesk.exe          the host program: capture, VP8 encoder (libvpx), license texts and all, one file
freedesk.exe.sha256   its SHA-256, in the layout sha256sum -c reads
```

The licenses are read from the program: its **Licenses** button, or `freedesk.exe --licenses > licenses.txt`, which writes them and exits without loading any configuration or going online.

Everything the release runs or downloads is pinned (actions by commit SHA, libvpx by commit, go-winres by `go.sum`) except the C compiler: MSYS2's packages are signature-checked by `pacman` but come in whatever version MSYS2 currently ships.

The web page's **Download for Windows** button looks up the latest release and links to the asset with exactly this name (`HOST_ASSET_NAME` in `frontend/src/constants/links.ts`, `ASSET_NAME` in `release.yml`). Rename both together or neither. Pushing to `main` deploys the web page at once, and until a release with that asset exists the button falls back to the release's page; tag the release right after pushing such a change.

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

- Compare the exe's SHA-256 with the `.sha256` file on the release page: `sha256sum -c freedesk.exe.sha256`, or `Get-FileHash freedesk.exe` on Windows. (A browser that saved it as `freedesk (1).exe` changed only the name.)
- Or verify the provenance attestation: `gh attestation verify freedesk.exe --repo YahyaAltintop/freedesk`. It is a signed statement that this workflow, at this commit, produced exactly this file.

## Code signing and the "Unknown publisher" warning

The exe is not signed, so SmartScreen warns on first run ("Windows protected your PC", publisher unknown). The version resource makes Windows say *FreeDesk* in the firewall prompt and in Task Manager, but only an Authenticode signature changes the warning, and a freshly signed program can still be warned about until it has earned reputation.

The workflow already has the signing step. It runs when the two `CODESIGN_*` secrets exist and is skipped otherwise, so adding a certificate later means adding the secrets, nothing else. Ways to get one:

- **SignPath Foundation**: free signing for open-source projects after an application. It signs through its own GitHub Action rather than a PFX, so the step would be swapped for that action.
- **Azure Trusted Signing**: Microsoft's signing service (subscription, identity validation), used through `azure/trusted-signing-action`; the same swap applies.
- **A certificate from a CA** (yearly fee): if the key can be exported, base64 the PFX into `CODESIGN_PFX_BASE64`. Certificates issued since mid-2023 usually keep the key on a hardware token or an HSM, in which case the CA's cloud-signing action replaces the PFX step.

## Updating libvpx

Pick a newer release tag of [libvpx](https://chromium.googlesource.com/webm/libvpx) (also mirrored at github.com/webmproject/libvpx), look up the commit it points at (`git ls-remote https://chromium.googlesource.com/webm/libvpx refs/tags/vX.Y.Z`, the peeled `^{}` line for an annotated tag), and change `LIBVPX_TAG` and `LIBVPX_COMMIT` in `host-agent/scripts/build-libvpx.sh` together. The build refuses a checkout whose commit differs. Locally, delete `host-agent/third_party/` and run the script again. Before releasing, compare the new encoder with the old on the same frames: with an ffmpeg at hand, `RC_FFMPEG_GOLDEN=<ffmpeg.exe> go test -run EncoderMatchesFFmpeg -v ./internal/vpx/` prints bitrate and frame sizes for both.
