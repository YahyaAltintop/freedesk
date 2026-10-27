//go:build darwin && cgo

package ui

import (
	"io"
	"os"
	"strings"
	"sync"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/macos"
)

// window is the status window on macOS, drawn by AppKit in internal/macos.
// The activity log is kept here, as on Windows, and handed over whole.
type window struct {
	mu     sync.Mutex
	ring   *lineRing
	setLog func(text string) // macos.SetLog; a recorder in tests
}

// Open creates the application and shows the window. It must be called on
// the process's first thread, which main keeps for itself (it locks it in
// init) and which then runs Loop: AppKit works there and nowhere else.
func Open(o Options) (Window, error) {
	if err := macos.OpenStatusWindow(o.Title, o.Licenses, o.PrivacyURL, o.OnClose); err != nil {
		return nil, err
	}
	return newWindow(macos.SetLog), nil
}

func newWindow(setLog func(string)) *window {
	return &window{ring: newLineRing(), setLog: setLog}
}

// Echo is where log lines go besides the window: stderr, which is the
// terminal FreeDesk was started from, if it was; started from the Finder,
// it leads nowhere.
func Echo() io.Writer { return os.Stderr }

func (w *window) ShowCode(code, site string)     { macos.SetCode(code, site) }
func (w *window) SetStatus(text string)          { macos.SetStatus(text) }
func (w *window) ShowUpdate(version, url string) { macos.ShowUpdate(version, url) }
func (w *window) Done(exitCode int)              { macos.FinishStatusWindow(exitCode) }
func (w *window) Loop() int                      { return macos.RunApp() }

// Append adds a record to the log and hands the whole log over. The window
// shows the newest text it was handed; doing that under the lock means one
// handed over later is never the older.
func (w *window) Append(record string) {
	lines := splitRecord(record)
	if len(lines) == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ring.push(lines...)
	w.setLog(strings.Join(w.ring.lines, "\n"))
}
