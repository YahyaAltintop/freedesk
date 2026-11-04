// The kind of computer this page runs on: which host program a Download
// button offers first, whether a host program can be running here to ask for
// its code, and which shortcuts stay on this computer during a session.
//
// The host program runs on Windows and macOS. Everything else, phones and
// tablets included, is 'other': it can connect, but not be connected to.

export type Platform = 'windows' | 'mac' | 'other'

interface NavigatorUAData {
  platform?: string
}

export function localPlatform(nav: Navigator = navigator): Platform {
  // Chromium's client hint, then the older property, then the user agent.
  const hint = (nav as Navigator & { userAgentData?: NavigatorUAData }).userAgentData?.platform
  const platform = hint || nav.platform || ''
  if (/iPhone|iPad|iPod|Android/i.test(`${platform} ${nav.userAgent}`)) {
    return 'other'
  }
  if (/^Win/i.test(platform) || /Windows NT/i.test(nav.userAgent)) {
    return 'windows'
  }
  if (/^Mac/i.test(platform) || /Macintosh/i.test(nav.userAgent)) {
    // An iPad asks for the desktop site as a Mac; its touch screen is all
    // that gives it away.
    return nav.maxTouchPoints > 1 ? 'other' : 'mac'
  }
  return 'other'
}
