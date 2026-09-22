//go:build windows

package capture

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hideConsole keeps ffmpeg from opening a console window of its own. The
// agent is built as a windowed program (-H=windowsgui) with no console, and a
// console program started from one is given a fresh console by Windows — a
// black window popping up on the operator's screen for as long as the session
// lasts. ffmpeg talks to the agent over pipes and never needs one.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}
