package consent

import (
	"fmt"
	"strings"
	"time"
)

// Kind is what the operator is being asked about.
type Kind int

const (
	// KindConnect is the request to start a session at all.
	KindConnect Kind = iota
	// KindFiles is one batch of files the connected viewer wants to send.
	KindFiles
)

// maxListedFiles is how many names a prompt spells out before summarising. A
// list longer than this stops being read, and a prompt nobody reads is not
// consent.
const maxListedFiles = 5

// FileOffer is one file in an incoming batch. Name is the SANITISED name —
// what will actually be created — because the operator has to be able to
// approve the thing that happens, not the thing that was claimed.
type FileOffer struct {
	Name string
	Size int64
}

// Prompt is one question for the operator.
//
// The text is derived from this value rather than passed in with it: what the
// operator reads is part of the security model, so it lives in one place that
// can be tested, instead of being spelled out at each call site and drifting.
type Prompt struct {
	Kind      Kind
	ViewerUID string
	Files     []FileOffer
	Folder    string
	// Clipboard is true when this session will also share clipboard text. The
	// operator has to be told: it is the one part of a session that reads
	// something of theirs while they are using their own machine, rather than
	// only doing what they can see on screen.
	Clipboard bool
}

// ConnectRequest is the question asked before a session starts. clipboard says
// whether this agent will also share clipboard text, so the wording can be
// honest about it.
func ConnectRequest(viewerUID string, clipboard bool) Prompt {
	return Prompt{Kind: KindConnect, ViewerUID: viewerUID, Clipboard: clipboard}
}

// IncomingFiles is the question asked before anything is written to disk.
func IncomingFiles(viewerUID string, files []FileOffer, folder string) Prompt {
	return Prompt{Kind: KindFiles, ViewerUID: viewerUID, Files: files, Folder: folder}
}

// TotalSize is the number of bytes the batch claims to be.
func (p Prompt) TotalSize() int64 {
	var total int64
	for _, f := range p.Files {
		total += f.Size
	}
	return total
}

// Title is the window title. It differs per kind on purpose: the operator must
// never mistake a file prompt for the connection prompt they were expecting.
// The sequence number makes the title unique so exactly this box can be found
// and dismissed if the request is withdrawn.
func (p Prompt) Title(seq uint64) string {
	name := "Connection request"
	if p.Kind == KindFiles {
		name = "Incoming files"
	}
	return fmt.Sprintf("FreeDesk - %s (#%d)", name, seq)
}

// Text is the body of the dialog.
func (p Prompt) Text(timeout time.Duration) string {
	var b strings.Builder
	if p.Kind == KindConnect {
		b.WriteString("Someone entered this computer's code and wants to connect.\n\n")
		// Files are named because a person weighing this deserves the list of
		// what a session can do, not a summary of it. The clipboard is named
		// because it is the one part that reads something of the operator's
		// while they use their own machine, rather than only doing what they
		// can watch happen on screen.
		if p.Clipboard {
			b.WriteString("Allow them to see your screen, control this computer,\n" +
				"exchange files with it, and share copied text?\n\n")
		} else {
			b.WriteString("Allow them to see your screen, control this computer,\n" +
				"and exchange files with it?\n\n")
		}
	} else {
		fmt.Fprintf(&b, "The person connected to this computer wants to send %s.\n\n",
			describeBatch(p.Files))
		b.WriteString(p.fileList("    "))
		fmt.Fprintf(&b, "\nThey will be saved to:\n%s\n\n", p.Folder)
		// Worth saying out loud: it is true, and it lowers the stakes of Yes.
		b.WriteString("Existing files are never replaced. Accept these files?\n\n")
	}
	fmt.Fprintf(&b, "This request is rejected automatically in %.0f seconds.\n(viewer %s)",
		timeout.Seconds(), shortID(p.ViewerUID))
	return b.String()
}

// ConsoleHeader describes the request on the console, without asking for an
// answer. The dialog approver prints this on its own — the operator may be
// looking at the console, and it leaves a record of what was asked.
func (p Prompt) ConsoleHeader() string {
	var b strings.Builder
	if p.Kind == KindConnect {
		fmt.Fprintf(&b, "\n>>> INCOMING CONNECTION REQUEST (viewer=%s)\n", p.ViewerUID)
		return b.String()
	}
	fmt.Fprintf(&b, "\n>>> INCOMING FILES (viewer=%s): %s\n", p.ViewerUID, describeBatch(p.Files))
	b.WriteString(p.fileList(">>>   "))
	fmt.Fprintf(&b, ">>> Saved to %s\n", p.Folder)
	return b.String()
}

// ConsoleLine is the whole console prompt: the description plus the question.
func (p Prompt) ConsoleLine(timeout time.Duration) string {
	return fmt.Sprintf("%s>>> Type 'y' and press Enter to accept (rejected if there is no answer within %.0f s): ",
		p.ConsoleHeader(), timeout.Seconds())
}

// shortID trims a uid to something a person can compare at a glance.
func shortID(uid string) string {
	if len(uid) > 8 {
		return uid[:8] + "…"
	}
	return uid
}

// fileList renders the names, each on its own line. Names are never truncated
// or elided: the extension is the operator's only signal about what they are
// accepting, so "setup.exe" has to read as "setup.exe".
func (p Prompt) fileList(indent string) string {
	var b strings.Builder
	for i, f := range p.Files {
		if i == maxListedFiles && len(p.Files) > maxListedFiles+1 {
			fmt.Fprintf(&b, "%s… and %d more files\n", indent, len(p.Files)-maxListedFiles)
			break
		}
		fmt.Fprintf(&b, "%s%s  (%s)\n", indent, f.Name, humanSize(f.Size))
	}
	return b.String()
}

func describeBatch(files []FileOffer) string {
	var total int64
	for _, f := range files {
		total += f.Size
	}
	if len(files) == 1 {
		return fmt.Sprintf("1 file (%s)", humanSize(total))
	}
	return fmt.Sprintf("%d files (%s)", len(files), humanSize(total))
}

// humanSize renders a byte count the way the operator would say it.
func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}
