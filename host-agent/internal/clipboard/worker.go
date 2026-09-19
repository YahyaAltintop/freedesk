package clipboard

import (
	"runtime"
	"time"
)

const (
	// pumpEvery is how often the window's message queue is drained. Short,
	// because another process calling EmptyClipboard SENDS a message to the
	// owner and blocks until it is answered — a window that does not pump would
	// hang whatever application the operator is using.
	pumpEvery = 25 * time.Millisecond

	// pollEvery is how often the clipboard is checked for a change. Fast enough
	// that a copy on the host feels immediate next to the video's own latency,
	// slow enough to be invisible in CPU. GetClipboardSequenceNumber is a single
	// call that takes no lock, so this costs almost nothing.
	pollEvery = 400 * time.Millisecond
)

// job is one piece of work for the locked thread.
type job struct {
	run  func(Board)
	done chan struct{}
}

// worker owns the clipboard: one goroutine, one OS thread, for its whole life.
//
// Everything goes through here because the Windows clipboard belongs to the
// THREAD that opened it. Go moves goroutines between threads at any preemption
// point, so a close could land on a different thread than its open and fail —
// intermittently, which is the worst way for it to fail.
type worker struct {
	jobs chan job
	quit chan struct{}
	dead chan struct{}
}

// startWorker opens the clipboard on its own thread and begins pumping
// messages. onPoll is called on that thread every pollEvery.
func startWorker(onPoll func(Board)) (*worker, error) {
	w := &worker{
		jobs: make(chan job),
		quit: make(chan struct{}),
		dead: make(chan struct{}),
	}
	ready := make(chan error, 1)
	go w.loop(onPoll, ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return w, nil
}

func (w *worker) loop(onPoll func(Board), ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(w.dead)

	board, err := New()
	ready <- err
	if err != nil {
		return
	}
	defer board.Close()

	ticker := time.NewTicker(pumpEvery)
	defer ticker.Stop()
	ticks := 0
	perPoll := int(pollEvery / pumpEvery)

	for {
		select {
		case j := <-w.jobs:
			j.run(board)
			close(j.done)
		case <-ticker.C:
			pumpMessages()
			ticks++
			if onPoll != nil && ticks%perPoll == 0 {
				onPoll(board)
			}
		case <-w.quit:
			return
		}
	}
}

// do runs fn on the clipboard thread and waits for it.
//
// The wait is deliberate. A clipboard write arriving from the viewer has to be
// finished before the keystroke that pastes it is handled, and blocking here is
// what orders the two. It costs the session's message handling a few
// milliseconds in the normal case, and at most the open retry budget when
// another process is holding the clipboard.
func (w *worker) do(fn func(Board)) bool {
	j := job{run: fn, done: make(chan struct{})}
	select {
	case w.jobs <- j:
	case <-w.dead:
		return false
	}
	select {
	case <-j.done:
		return true
	case <-w.dead:
		return false
	}
}

// stop ends the worker and waits for the thread to let go of the clipboard.
func (w *worker) stop() {
	select {
	case <-w.dead:
		return
	default:
	}
	close(w.quit)
	<-w.dead
}
