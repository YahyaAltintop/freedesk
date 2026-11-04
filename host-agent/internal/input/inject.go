package input

// clamp01 keeps a normalised coordinate on the screen: the viewer sends 0..1,
// and a pointer dragged past the picture's edge must stop at the screen's.
func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
