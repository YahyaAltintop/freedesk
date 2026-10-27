//go:build darwin

package firewall

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// socketfilterfw is the command line of the macOS firewall, the one in
// System Settings > Network > Firewall. Only its questions are asked; no
// setting is ever changed.
const socketfilterfw = "/usr/libexec/ApplicationFirewall/socketfilterfw"

// ask runs socketfilterfw with args and returns what it printed. A variable,
// so a test can answer in its place.
var ask = func(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, socketfilterfw, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("socketfilterfw %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// Check asks the macOS firewall what it does with incoming connections for
// exe. Each question is a run of socketfilterfw: a few milliseconds.
//
// macOS keeps a list of programs, each allowed or blocked. One that is not on
// the list is either let in without a question (signed software, when the
// firewall is set to allow it, as it is unless the operator changed that) or
// asked about the first time it waits for connections, in a dialog whose
// buttons put it on the list. Nothing here can tell those two apart, and
// neither needs a warning: the operator is looking at the dialog. What keeps
// viewers out unnoticed is a block, and that is what is looked for: the
// operator's Deny in that dialog or in the firewall's options, or the
// firewall refusing every incoming connection.
func Check(ctx context.Context, exe string) (Status, error) {
	out, err := ask(ctx, "--getglobalstate")
	if err != nil {
		return Status{}, err
	}
	state, ok := globalState(out)
	if !ok {
		return Status{}, fmt.Errorf("socketfilterfw answered %q", strings.TrimSpace(out))
	}
	st := Status{Enabled: state > 0}
	if !st.Enabled {
		return st, nil
	}
	// macOS has also said "block all" as state 2.
	st.BlockAll = state == 2
	if !st.BlockAll {
		if out, err = ask(ctx, "--getblockall"); err != nil {
			return Status{}, err
		}
		st.BlockAll = blocksAll(out)
	}
	for _, p := range listedAs(exe) {
		if out, err = ask(ctx, "--getappblocked", p); err != nil {
			return Status{}, err
		}
		st.Blocked = st.Blocked || appBlocked(out)
	}
	st.Allowed = !st.Blocked
	return st, nil
}

var stateNumber = regexp.MustCompile(`State = (\d+)`)

// globalState reads "Firewall is enabled. (State = 1)": 0 off, 1 on, 2 on
// and refusing everything.
func globalState(out string) (int, bool) {
	m := stateNumber.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// blocksAll reads the answer to --getblockall. macOS has put it in more than
// one way ("Firewall is set to block all non-essential incoming
// connections", "Firewall has block all state set to disabled.", "Block all
// DISABLED!"); an answer it does not recognise counts as no, since silence is
// better than a false alarm.
func blocksAll(out string) bool {
	s := strings.ToLower(out)
	switch {
	case strings.Contains(s, "disabled"):
		return false
	case strings.Contains(s, "non-essential"), strings.Contains(s, "enabled"):
		return true
	}
	return false
}

// appBlocked reads the answer to --getappblocked: "Incoming connection to
// <path> is blocked." for a program on the list as blocked, "... is
// permitted." for every other one, and the path can say anything, so only
// the end of the answer counts.
func appBlocked(out string) bool {
	return strings.HasSuffix(strings.TrimRight(strings.TrimSpace(out), "."), " is blocked")
}

// listedAs is every path the firewall may list the program at exe under:
// the program itself, and the app it is the program of, since the firewall
// lists an app and not the program inside it. Each comes as given and with
// its links resolved, as the firewall keeps whichever it was handed.
func listedAs(exe string) []string {
	var paths []string
	add := func(p string) {
		if p != "" && !slices.Contains(paths, p) {
			paths = append(paths, p)
		}
	}
	candidates := []string{exe}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		candidates = append(candidates, resolved)
	}
	for _, p := range candidates {
		p = filepath.Clean(p)
		add(p)
		add(appOf(p))
	}
	return paths
}

// appOf returns the app bundle exe is the program of
// (<name>.app/Contents/MacOS/<exe>), or "".
func appOf(exe string) string {
	macOS := filepath.Dir(exe)
	contents := filepath.Dir(macOS)
	app := filepath.Dir(contents)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || !strings.HasSuffix(app, ".app") {
		return ""
	}
	return app
}
