# Releasing

How the Windows host program is built and published, what a release holds, and how a download can be checked.

## What a tag does

Push a tag `vX.Y.Z` (for example `v0.3.1`). `.github/workflows/release.yml` then, on a Windows runner, with no module or build cache from an earlier run:

1. Checks the repository variables below and probes the agent's API key **without a referrer**. A key limited to websites would make every published exe exit at start-up, so the build refuses to go on.
2. Installs MSYS2's UCRT64 gcc and nasm (`msys2/setup-msys2` installs a fresh MSYS2 and updates it) and builds libvpx from source with `host-agent/scripts/build-libvpx.sh`: the VP8 encoder only, as a static library, from a pinned commit the script checks. Never restored from a cache.
3. Writes the license texts with `cmd/notices` — FreeDesk's own MIT license, then those of the build's own modules, libvpx and the MinGW-w64 and GCC runtimes — into `host-agent/internal/licenses/generated/LICENSES.txt`, where the build embeds them. The release is the exe alone, so the texts those licenses ask to accompany a binary travel inside it.
4. Compiles the exe's icon, version information and application manifest from `host-agent/cmd/host/winres/` with [go-winres](https://github.com/tc-hib/go-winres) (a tool dependency pinned in `go.sum`), stamped with the tag's version, and builds `freedesk.exe` with cgo as a windowed program (`-H=windowsgui`) with the Firebase project and the repository name embedded (the agent asks that repository's latest release whether it is out of date). A build whose version resource did not take is refused.
5. Runs `cmd/importcheck` on the exe: it must import no DLL that Windows does not ship (a `libwinpthread-1.dll`, `libgcc_s_seh-1.dll` or `libvpx` import would make it fail on every machine without MSYS2), must have been built with cgo (no cgo, no video) and must carry, byte for byte, the license texts of step 3.
6. Has SignPath sign the exe, but only once the repository is enrolled (see [Code signing](#code-signing-and-the-unknown-publisher-warning)): the job waits for the release to be approved on signpath.io, then checks the signed file before it replaces the unsigned one.
7. Writes `freedesk.exe.sha256` (after signing, which rewrites the file), signs a build provenance attestation for the exe and publishes both files on the GitHub release, with a few lines on running, checking and updating it above the generated notes. What those lines say about the signature follows the published file.

A tag with a hyphen (`v0.9.0-rc1`) is published as a **pre-release**. GitHub's "latest release", which the web page's Download button and the program's update check both ask for, skips pre-releases, so nobody is offered one: use them to try a release, the signing in particular.

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
| `SIGNPATH_ORGANIZATION_ID` | release (with the token below) | the SignPath organization that signs the exe; `SIGNPATH_PROJECT_SLUG` and `SIGNPATH_SIGNING_POLICY_SLUG` only if they are not `freedesk` and `release-signing` ([Code signing](#code-signing-and-the-unknown-publisher-warning)) |

**Secrets:**

| Secret | Used by | Meaning |
|---|---|---|
| `FIREBASE_SERVICE_ACCOUNT` | viewer deploy | a service-account JSON key with the Firebase Hosting Admin role |
| `SIGNPATH_API_TOKEN` | release (optional) | the API token of a SignPath user with the *submitter* role in the FreeDesk project; signing is skipped while it is missing |

Pushing to `main` deploys the viewer (`deploy-web.yml`). The database rules are deployed by hand, once and after every change to `firebase/database.rules.json`: `cd firebase && firebase deploy --only database`.

## Checking a download

- Compare the exe's SHA-256 with the `.sha256` file on the release page: `sha256sum -c freedesk.exe.sha256`, or `Get-FileHash freedesk.exe` on Windows. (A browser that saved it as `freedesk (1).exe` changed only the name.)
- Or verify the provenance attestation: `gh attestation verify freedesk.exe --repo YahyaAltintop/freedesk`. It is a signed statement that this workflow, at this commit, produced exactly this file.

## Code signing and the "Unknown publisher" warning

The exe is not signed yet. SmartScreen warns on first run ("Windows protected your PC", publisher unknown), and antivirus products that judge new files by reputation can block it outright: Norton quarantined v0.8.0 as a file "very few people have used", and on VirusTotal a dozen engines (Avast, AVG, Symantec, Bitdefender and the products built on it, McAfee's reputation service) flagged it without naming anything specific. The version resource makes Windows say *FreeDesk* in the firewall prompt and in Task Manager, but only an Authenticode signature lets reputation build up from one release to the next, and even a signed program can be warned about until it has earned some.

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

## Updating libvpx

Pick a newer release tag of [libvpx](https://chromium.googlesource.com/webm/libvpx) (also mirrored at github.com/webmproject/libvpx), look up the commit it points at (`git ls-remote https://chromium.googlesource.com/webm/libvpx refs/tags/vX.Y.Z`, the peeled `^{}` line for an annotated tag), and change `LIBVPX_TAG` and `LIBVPX_COMMIT` in `host-agent/scripts/build-libvpx.sh` together. The build refuses a checkout whose commit differs. Locally, delete `host-agent/third_party/` and run the script again. Before releasing, compare the new encoder with the old on the same frames: with an ffmpeg at hand, `RC_FFMPEG_GOLDEN=<ffmpeg.exe> go test -run EncoderMatchesFFmpeg -v ./internal/vpx/` prints bitrate and frame sizes for both.
