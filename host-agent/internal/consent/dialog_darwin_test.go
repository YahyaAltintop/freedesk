//go:build darwin && cgo

package consent

import (
	"testing"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/macos"
)

// Without the application the status window opens (a login over ssh, or this
// test binary) an alert has nowhere to appear, so a question goes to the
// console and there is no picker: the agent must never wait on a dialog that
// cannot show.
func TestWithoutTheApplicationTheConsoleAsks(t *testing.T) {
	if macos.AppOpen() {
		t.Skip("the application is open in this process")
	}
	for _, mode := range []string{"", ModeDialog, ModeConsole} {
		if _, ok := newPlatformApprover(mode, time.Second).(*Console); !ok {
			t.Errorf("mode %q without the application: not the console prompt", mode)
		}
		if NewFilePicker(mode).Available() {
			t.Errorf("mode %q without the application offers a file picker", mode)
		}
	}
}
