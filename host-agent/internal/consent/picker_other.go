//go:build !windows

package consent

// There is no native file picker away from Windows, so the agent does not
// advertise downloads there and the viewer's button stays disabled with an
// explanation. The agent targets Windows; this keeps the package building for
// tooling and tests.
func newPlatformPicker() FilePicker { return unavailable{} }
