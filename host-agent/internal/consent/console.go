package consent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// Console serialises approval prompts on stdin: one question at a time, each
// with its own deadline.
type Console struct {
	timeout time.Duration
	lines   chan string
}

// NewConsole starts the single stdin reader goroutine and returns the prompt.
func NewConsole(timeout time.Duration) *Console {
	c := &Console{timeout: timeout, lines: make(chan string)}
	go c.readLoop()
	return c
}

// readLoop is the only reader of stdin for the whole process. When stdin
// closes (e.g. the agent runs detached), the loop ends and every Ask simply
// falls through to its timeout.
func (c *Console) readLoop() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		c.lines <- strings.TrimSpace(scanner.Text())
	}
	// stdin closed (detached run): every Ask now falls through to its timeout.
	_ = scanner.Err()
}

// Ask blocks until the operator answers, the timeout passes, or ctx ends.
// Only an explicit yes ("y", "yes") approves; everything else —
// including silence — rejects.
func (c *Console) Ask(ctx context.Context, p Prompt) bool {
	uiMu.Lock()
	defer uiMu.Unlock()

	// Drop any line typed after a previous prompt already timed out, so a
	// stale answer cannot approve the wrong request.
	draining := true
	for draining {
		select {
		case <-c.lines:
		default:
			draining = false
		}
	}

	fmt.Print(p.ConsoleLine(c.timeout))

	timer := time.NewTimer(c.timeout)
	defer timer.Stop()

	select {
	case line := <-c.lines:
		answer := strings.ToLower(line)
		return answer == "y" || answer == "yes"
	case <-timer.C:
		fmt.Println()
		return false
	case <-ctx.Done():
		return false
	}
}
