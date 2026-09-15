// Wire shapes for WebRTC signaling over Realtime Database (see docs/PROTOCOL.md).
// These match the browser's RTCSessionDescription / RTCIceCandidate JSON so the
// Go host and the Vue viewer exchange them verbatim.

export interface Description {
  type: RTCSdpType
  sdp: string
}

export interface IceCandidate {
  candidate: string
  sdpMid?: string | null
  sdpMLineIndex?: number | null
  usernameFragment?: string | null
}
