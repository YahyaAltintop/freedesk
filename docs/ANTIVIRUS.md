# Antivirus warnings

Some antivirus products block a new FreeDesk release, or its download, before anyone has run it. This page says why, how to check the file you have, and what the project does about it.

## What happens

Norton, Avast and AVG belong to one company, Gen Digital, and share one engine. A program they have never seen, with no digital signature, goes to their cloud for an automatic verdict before it may run. A new FreeDesk release is exactly that: a new, unsigned file that almost nobody has run yet. Twice so far the verdict on such a release has been **FileRepMalware**, a verdict on the file's reputation: the product quarantines that file wherever it appears, and Norton's Safe Web then stops its download from GitHub ("Connection aborted").

On VirusTotal, release 0.8.0 was flagged by 13 of 71 engines, all under generic reputation or machine-learning names: Avast, AVG, Symantec, Bitdefender and the products built on it, McAfee. Microsoft Defender, Kaspersky and ESET found nothing.

A remote desktop also does what malware of that kind does: it captures the screen, moves the mouse, types, and talks to another computer. That is why such programs are judged harder. It is also why FreeDesk does none of it until the person at the computer clicks **Yes**.

## Checking the file you have

- Its SHA-256 must match `freedesk.exe.sha256` on the release page. In PowerShell: `Get-FileHash freedesk.exe`.
- Or check where it came from: `gh attestation verify freedesk.exe --repo YahyaAltintop/freedesk` proves that this repository's public release workflow built exactly this file from the tagged source, on GitHub's own machines.
- The source is all here. The release is built only by [`.github/workflows/release.yml`](../.github/workflows/release.yml): nothing is packed, compressed or obfuscated, and there is no installer.

## If your antivirus blocked it

- **Report it to your antivirus** as a false detection: Norton and Avast offer it in the alert itself and in their quarantine. Their web forms: [Norton](https://submissions.norton.com/reportfalsepositive), [Avast](https://www.avast.com/report-false-positive), [AVG](https://www.avg.com/en-ww/report-false-positive). Every report helps clear that release for everyone.
- **Check the file** as above before you decide anything else about it.
- A cleared release stops being blocked once the product's definitions update. Norton says it reviews reports within 48 hours.

## What the project does

- A flagged release is reported to the vendors as a false detection: to Gen Digital (Norton, Avast, AVG) first, then to the others that flag it on VirusTotal. The steps: [RELEASING.md → Antivirus false positives](RELEASING.md#antivirus-false-positives). What has been sent so far is in the table below.
- The program is not code-signed yet. A signature lets reputation carry over from one release to the next, so each release would not start from zero: [RELEASING.md → Code signing](RELEASING.md#code-signing-and-the-unknown-publisher-warning).

Reports so far:

| Date | Release | Where | What |
|---|---|---|---|
| 2026-10-09 | 0.8.0 | Norton, from the alert | the file, as a false detection |
| 2026-10-12 | 0.9.0 | Norton, from the alert | the file, as a false detection |
| 2026-10-13 | 0.9.0 | Norton, from the Safe Web alert | the download link, as a false detection |
