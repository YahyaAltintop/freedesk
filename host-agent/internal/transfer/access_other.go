//go:build !darwin

package transfer

// CheckAccess reports whether the agent may use the folder files are saved
// in. Only macOS keeps a folder from programs until the operator lets them in
// (access_darwin.go); here the answer is yes, without looking.
func CheckAccess(string) bool { return true }

// accessHint has nothing to add here.
func accessHint(error) string { return "" }
