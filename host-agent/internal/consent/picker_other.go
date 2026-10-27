//go:build !windows && !(darwin && cgo)

package consent

// There is no native file picker away from Windows and macOS, so the agent
// does not advertise downloads there and the viewer's button stays disabled
// with an explanation. The agent targets those two; this keeps the package
// building for tooling and tests.
func newPlatformPicker() FilePicker { return unavailable{} }
