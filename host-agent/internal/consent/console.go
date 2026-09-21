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
func (c *Console) Ask(ctx context.Context, p Prompt) Answer {
	answer := Refused
	exclusive(func() { answer = c.ask(ctx, p) })
	return answer
}

// ask runs the prompt. It runs inside exclusive for the same reason the dialog
// does: if the console happens to be the foreground window, injected
// keystrokes type into it, and "y" followed by Enter is an approval.
func (c *Console) ask(ctx context.Context, p Prompt) Answer {
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
		if answer := strings.ToLower(line); answer == "y" || answer == "yes" {
			return Allowed
		}
		return Refused
	case <-timer.C:
		fmt.Println()
		// Typing anything at all is an answer; this is the case where nobody
		// is at the keyboard, which is a different fact about the machine.
		return Unanswered
	case <-ctx.Done():
		return Refused
	}
}
