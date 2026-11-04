// Package macos is the agent's Objective-C: the parts of macOS that only
// Objective-C reaches (ScreenCaptureKit, AppKit), behind Go functions the
// rest of the agent calls. Every Objective-C file of the agent lives here and
// nowhere else, because the go command links libobjc once for each package
// that has one, and macOS's linker warns about every library named twice.
//
// The package is built on macOS with cgo only; elsewhere it is empty.
package macos
