package webrtc

import pion "github.com/pion/webrtc/v4"

// NewVideoTrack creates a VP8 video track that the host writes captured screen
// frames into. The viewer receives it via the negotiated peer connection.
func NewVideoTrack() (*pion.TrackLocalStaticSample, error) {
	return pion.NewTrackLocalStaticSample(
		pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8},
		"video",
		"screen",
	)
}
