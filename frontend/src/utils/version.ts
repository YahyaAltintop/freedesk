// Orders "1.2.3"-style versions the way the host agent does
// (host-agent/internal/update): a leading "v" and anything from a hyphen or
// plus on ("-dev", "-rc1", "+build") are ignored, so a development build counts
// as the release it is heading for; missing parts are zero, so "1.2" is "1.2.0".

function versionParts(version: string): number[] {
  const core = version.trim().replace(/^v/, '').split(/[-+]/, 1)[0] ?? ''
  return core.split('.').map((part) => {
    const n = Number.parseInt(part, 10)
    return Number.isFinite(n) && n >= 0 ? n : 0
  })
}

/** Negative when a is older than b, zero when equal, positive when newer. */
export function compareVersions(a: string, b: string): number {
  const pa = versionParts(a)
  const pb = versionParts(b)
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i] ?? 0
    const y = pb[i] ?? 0
    if (x !== y) {
      return x < y ? -1 : 1
    }
  }
  return 0
}

export function isNewerVersion(latest: string, current: string): boolean {
  return compareVersions(latest, current) > 0
}
