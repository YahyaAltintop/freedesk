//go:build darwin

package firewall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestCheckAsksTheFirewall asks this Mac's firewall, which is off unless
// someone turned it on, so only that it was asked and answered is checked;
// the answers themselves are TestCheckReadsTheAnswers'.
func TestCheckAsksTheFirewall(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	start := time.Now()
	st, err := Check(ctx, exe)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	t.Logf("in %v: enabled=%v blockall=%v blocked=%v allowed=%v", time.Since(start), st.Enabled, st.BlockAll, st.Blocked, st.Allowed)
	if !st.Enabled && !st.Reachable() {
		t.Error("with the firewall off the program is not reachable")
	}
}

// fakeAsk answers the questions Check asks with the answers given, by the
// question's first word, and records every question.
func fakeAsk(t *testing.T, answers map[string]string) *[]string {
	t.Helper()
	var asked []string
	orig := ask
	ask = func(_ context.Context, args ...string) (string, error) {
		asked = append(asked, strings.Join(args, " "))
		if args[0] == "--getappblocked" {
			if answer, ok := answers[args[0]+" "+args[1]]; ok {
				return answer, nil
			}
			return "Incoming connection to " + args[1] + " is permitted.\n", nil
		}
		answer, ok := answers[args[0]]
		if !ok {
			return "", errors.New("not answered")
		}
		return answer, nil
	}
	t.Cleanup(func() { ask = orig })
	return &asked
}

func TestCheckReadsTheAnswers(t *testing.T) {
	const (
		exe = "/Applications/FreeDesk.app/Contents/MacOS/freedesk"
		app = "/Applications/FreeDesk.app"
		on  = "Firewall is enabled. (State = 1)\n"
		off = "Firewall has block all state set to disabled.\n"
	)
	cases := []struct {
		name    string
		answers map[string]string
		want    Status
		reach   bool
	}{
		{
			name:    "the firewall is off",
			answers: map[string]string{"--getglobalstate": "Firewall is disabled. (State = 0)\n"},
			want:    Status{},
			reach:   true,
		},
		{
			name:    "on, and nothing blocks the program",
			answers: map[string]string{"--getglobalstate": on, "--getblockall": off},
			want:    Status{Enabled: true, Allowed: true},
			reach:   true,
		},
		{
			name: "on, set to block all incoming connections",
			answers: map[string]string{"--getglobalstate": on,
				"--getblockall": "Firewall is set to block all non-essential incoming connections\n"},
			want: Status{Enabled: true, BlockAll: true, Allowed: true},
		},
		{
			name:    "block all, said the older way",
			answers: map[string]string{"--getglobalstate": "Firewall is enabled. (State = 2)\n"},
			want:    Status{Enabled: true, BlockAll: true, Allowed: true},
		},
		{
			// The firewall lists the app, not the program inside it.
			name: "on, and the app is blocked",
			answers: map[string]string{"--getglobalstate": on, "--getblockall": off,
				"--getappblocked " + app: "Incoming connection to " + app + " is blocked.\n"},
			want: Status{Enabled: true, Blocked: true},
		},
		{
			name: "on, and the program itself is blocked",
			answers: map[string]string{"--getglobalstate": on, "--getblockall": off,
				"--getappblocked " + exe: "Incoming connection to " + exe + " is blocked\n"},
			want: Status{Enabled: true, Blocked: true},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAsk(t, c.answers)
			got, err := Check(t.Context(), exe)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Check = %+v, want %+v", got, c.want)
			}
			if got.Reachable() != c.reach {
				t.Errorf("Reachable = %v, want %v", got.Reachable(), c.reach)
			}
		})
	}
}

// A path can say anything; only how the answer ends tells a block.
func TestAPathThatSaysBlockedIsNotABlock(t *testing.T) {
	const exe = "/Users/someone/is blocked/freedesk"
	fakeAsk(t, map[string]string{"--getglobalstate": "Firewall is enabled. (State = 1)\n", "--getblockall": "Block all DISABLED! \n"})
	st, err := Check(t.Context(), exe)
	if err != nil {
		t.Fatal(err)
	}
	if st.Blocked || !st.Reachable() {
		t.Errorf("a permitted program in a folder named \"is blocked\" read as %+v", st)
	}
}

// An answer that cannot be read says nothing: no warning, and the reason goes
// to the developer's channel.
func TestCheckFailsOnAnswersItCannotRead(t *testing.T) {
	fakeAsk(t, map[string]string{"--getglobalstate": "something else entirely\n"})
	if _, err := Check(t.Context(), "/usr/local/bin/freedesk"); err == nil {
		t.Error("an unreadable state was taken for an answer")
	}
	fakeAsk(t, map[string]string{})
	if _, err := Check(t.Context(), "/usr/local/bin/freedesk"); err == nil {
		t.Error("a question that failed was taken for an answer")
	}
}

func TestListedAsNamesTheAppAndTheProgram(t *testing.T) {
	got := listedAs("/Applications/FreeDesk.app/Contents/MacOS/freedesk")
	want := []string{"/Applications/FreeDesk.app/Contents/MacOS/freedesk", "/Applications/FreeDesk.app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("in an app: %q, want %q", got, want)
	}
	if got := listedAs("/usr/local/bin/freedesk"); !reflect.DeepEqual(got, []string{"/usr/local/bin/freedesk"}) {
		t.Errorf("a bare program: %q", got)
	}

	// The firewall keeps a path as it was handed one, links resolved or not.
	dir := t.TempDir()
	real := filepath.Join(dir, "freedesk")
	if err := os.WriteFile(real, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	got = listedAs(link)
	if len(got) != 2 || got[0] != link || got[1] != resolved {
		t.Errorf("through a link: %q, want %q and %q", got, link, resolved)
	}
}

// Every way the firewall keeps viewers out is said, then where to click: the
// setting that is in the way, named as System Settings names it.
func TestAdviceSaysWhereToClick(t *testing.T) {
	for _, c := range []struct {
		st      Status
		setting string
	}{
		{Status{Enabled: true, BlockAll: true}, "Block all incoming connections"},
		{Status{Enabled: true, Blocked: true}, "Allow incoming connections"},
		{Status{Enabled: true}, "click Allow"},
	} {
		st, setting := c.st, c.setting
		advice := st.Advice()
		if len(advice) < 2 || !strings.Contains(advice[0], "nobody can connect") || !strings.HasPrefix(advice[1], "  ") {
			t.Errorf("%+v: %q", st, advice)
		}
		if !strings.Contains(strings.Join(advice, "\n"), setting) {
			t.Errorf("%+v: %q does not name %q", st, advice, setting)
		}
	}
}
