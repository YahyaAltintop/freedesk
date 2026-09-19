package transfer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	// highWater / lowWater bound the channel's send queue. Send() does not
	// block and the queue is unbounded, so without this a whole file would be
	// handed over as fast as the disk can read it and sit in memory waiting for
	// the network.
	highWater = 1 << 20
	lowWater  = 1 << 18

	// stallTimeout gives up on a peer that has stopped draining. Without it a
	// dead connection parks the sending goroutine until the session ends.
	stallTimeout = 30 * time.Second

	// MaxDownloadBytes is the per-file cap for host → viewer. It is lower than
	// the upload cap because a browser without the File System Access API holds
	// the whole file in memory before it can be saved.
	MaxDownloadBytes = 256 << 20
)

var (
	errCancelled = errors.New("transfer cancelled")
	errStalled   = errors.New("the viewer stopped accepting data")
	errGone      = errors.New("the channel is gone")
)

// Picker asks the operator which files to send. Mirrors consent.FilePicker so
// this package does not depend on how the question is put.
type Picker interface {
	Pick(ctx context.Context) (paths []string, ok bool)
	Available() bool
}

// sending is one batch the operator chose, waiting to be pulled by the viewer.
type sending struct {
	id     string
	paths  []string // absolute, on this machine — never sent
	names  []string // base names, which is all the viewer is told
	sizes  []int64
	cancel chan struct{}
}

// requested opens the picker and offers whatever the operator chose.
//
// There is no approval prompt in front of this: the picker IS the consent. The
// viewer cannot name a path, only ask; what leaves the machine is what the
// operator selected in a dialog they could have cancelled.
func (s *Session) requested(id string) {
	if s.picker == nil || !s.picker.Available() {
		s.send(Reject(id, ReasonBusy))
		return
	}
	if !s.claimSend(id) {
		return
	}

	paths, ok := s.picker.Pick(s.ctx)
	if !ok || len(paths) == 0 {
		s.clearSend()
		s.send(Reject(id, ReasonDenied))
		return
	}

	files := make([]FileMeta, 0, len(paths))
	keep := make([]string, 0, len(paths))
	names := make([]string, 0, len(paths))
	sizes := make([]int64, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > MaxDownloadBytes {
			s.logf("[transfer] skipping %q: not a readable file within the size limit", filepath.Base(p))
			continue
		}
		// Only the base name goes on the wire. A full path would hand over the
		// operator's user name and the shape of their folders, which is not
		// something asking for a file should cost them.
		name := filepath.Base(p)
		keep = append(keep, p)
		names = append(names, name)
		sizes = append(sizes, info.Size())
		files = append(files, FileMeta{Name: name, Size: info.Size()})
	}
	if len(files) == 0 {
		s.clearSend()
		s.send(Reject(id, ReasonTooLarge))
		return
	}

	s.mu.Lock()
	if s.tx != nil && s.tx.id == id {
		s.tx.paths, s.tx.names, s.tx.sizes = keep, names, sizes
	}
	s.mu.Unlock()

	s.logf("[transfer] offering %d file(s) to the viewer", len(files))
	s.send(Msg{T: TypeOffer, ID: id, Dir: DirDown, Files: files})
}

// claimSend reserves the single outbound slot.
func (s *Session) claimSend(id string) bool {
	s.mu.Lock()
	busy := s.closed || s.tx != nil
	if !busy {
		s.tx = &sending{id: id, cancel: make(chan struct{})}
	}
	s.mu.Unlock()
	if busy {
		s.send(Reject(id, ReasonBusy))
		return false
	}
	return true
}

// cancelSend stops an outbound batch the viewer gave up on.
func (s *Session) cancelSend(id string) {
	s.mu.Lock()
	if s.tx != nil && (id == "" || s.tx.id == id) {
		s.abortSendLocked()
	}
	s.mu.Unlock()
}

func (s *Session) clearSend() {
	s.mu.Lock()
	s.abortSendLocked()
	s.mu.Unlock()
}

// abortSendLocked stops an outbound batch. Called with s.mu held.
func (s *Session) abortSendLocked() {
	if s.tx == nil {
		return
	}
	close(s.tx.cancel)
	s.tx = nil
}

// accepted streams one file of the offered batch. The viewer accepts a file at
// a time, because saving each one needs a click on its side and that click is
// what lets the browser open a save dialog at all.
func (s *Session) accepted(id string, index int) {
	s.mu.Lock()
	tx := s.tx
	if tx == nil || tx.id != id || index < 0 || index >= len(tx.paths) {
		s.mu.Unlock()
		return
	}
	path, name, size, cancel := tx.paths[index], tx.names[index], tx.sizes[index], tx.cancel
	s.mu.Unlock()

	if err := s.streamFile(path, size, cancel); err != nil {
		if errors.Is(err, errCancelled) {
			s.logf("[transfer] sending %q was cancelled", name)
			return
		}
		s.logf("[transfer] could not send %q: %v", name, err)
		s.send(Fail(id, ReasonIO))
		return
	}
	s.logf("[transfer] sent %q (%d bytes)", name, size)
	s.send(Msg{T: TypeComplete, ID: id, Index: index, Size: size})
}

// streamFile reads the file and paces it against the channel's send queue.
func (s *Session) streamFile(path string, size int64, cancel <-chan struct{}) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, ChunkBytes)
	var sent int64
	for sent < size {
		if err := s.waitForDrain(cancel); err != nil {
			return err
		}
		n, err := f.Read(buf)
		if n > 0 {
			s.chMu.Lock()
			ch := s.ch
			s.chMu.Unlock()
			if ch == nil {
				return errGone
			}
			if err := ch.Send(buf[:n]); err != nil {
				return err
			}
			sent += int64(n)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	if sent != size {
		// The file changed under us between the offer and now.
		return errors.New("the file changed while it was being sent")
	}
	return nil
}

// waitForDrain blocks until the send queue has room.
//
// The queue is re-read on every pass rather than simply waiting for the event:
// the callback fires only when the buffered amount crosses DOWN through the
// threshold, so arming it while already below would wait for a crossing that
// never comes.
func (s *Session) waitForDrain(cancel <-chan struct{}) error {
	for {
		s.chMu.Lock()
		ch := s.ch
		s.chMu.Unlock()
		if ch == nil {
			return errGone
		}
		if ch.BufferedAmount() < highWater {
			return nil
		}
		select {
		case <-s.low:
		case <-cancel:
			return errCancelled
		case <-s.ctx.Done():
			return errCancelled
		case <-time.After(stallTimeout):
			return errStalled
		}
	}
}
