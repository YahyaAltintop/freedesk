package firewall

import (
	"reflect"
	"testing"
)

func TestSameProgram(t *testing.T) {
	t.Setenv("FD_TEST_HOME", `C:\Users\someone`)
	exe := `C:\Users\someone\Downloads\freedesk-windows-x64\freedesk.exe`

	cases := []struct {
		rule string
		want bool
	}{
		{exe, true},
		{`c:\users\SOMEONE\downloads\freedesk-windows-x64\FREEDESK.EXE`, true}, // Windows paths ignore case
		{`%FD_TEST_HOME%\Downloads\freedesk-windows-x64\freedesk.exe`, true},   // as Windows writes some rules
		{`  ` + exe + `  `, true},
		{`C:\Users\someone\Downloads\other\freedesk.exe`, false},
		{"Any", false}, // a rule with no program filter
		{"", false},
	}
	for _, c := range cases {
		if got := sameProgram(c.rule, exe); got != c.want {
			t.Errorf("sameProgram(%q) = %v, want %v", c.rule, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	exe := `C:\Users\someone\Downloads\freedesk-windows-x64\freedesk.exe`
	other := `C:\Users\someone\old\freedesk-host.exe`

	cases := []struct {
		name string
		out  string
		want Status
	}{
		{
			name: "no rule at all: the question was never answered",
			out:  "enabled|3\nnetwork|Private\nrule|Allow|Any|" + other + "\n",
			want: Status{Enabled: true, Network: "Private"},
		},
		{
			name: "allowed on the network the computer is on",
			out:  "enabled|3\nnetwork|Private\nrule|Allow|Private, Public|" + exe + "\nrule|Allow|Private, Public|" + exe + "\n",
			want: Status{Enabled: true, Allowed: true, Network: "Private"},
		},
		{
			name: "allowed only for another kind of network",
			out:  "enabled|3\nnetwork|Private\nrule|Allow|Public|" + exe + "\n",
			want: Status{Enabled: true, Elsewhere: []string{"Public"}, Network: "Private"},
		},
		{
			name: "a block rule beats an allow rule",
			out:  "enabled|3\nnetwork|Public\nrule|Allow|Any|" + exe + "\nrule|Block|Any|" + exe + "\n",
			want: Status{Enabled: true, Blocked: true, Allowed: true, Network: "Public"},
		},
		{
			name: "domain network spelled the way Windows spells it",
			out:  "enabled|1\nnetwork|DomainAuthenticated\nrule|Allow|Domain|" + exe + "\n",
			want: Status{Enabled: true, Allowed: true, Network: "Domain"},
		},
		{
			name: "no known network: any allow rule counts",
			out:  "enabled|3\nnetwork|\nrule|Allow|Public|" + exe + "\n",
			want: Status{Enabled: true, Allowed: true},
		},
		{
			name: "firewall off",
			out:  "enabled|0\nnetwork|Private\n",
			want: Status{Network: "Private"},
		},
		{
			name: "noise on stdout is ignored",
			out:  "WARNING: something\nenabled|3\nnetwork|Private\nrule|Allow|Any|" + exe + "\nrule|garbage\n",
			want: Status{Enabled: true, Allowed: true, Network: "Private"},
		},
	}
	for _, c := range cases {
		if got := parse(c.out, exe); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: parse = %+v, want %+v", c.name, got, c.want)
		}
	}

	reach := map[string]bool{
		"off":            Status{}.Reachable(),
		"allowed":        Status{Enabled: true, Allowed: true}.Reachable(),
		"blocked":        !Status{Enabled: true, Allowed: true, Blocked: true}.Reachable(),
		"never answered": !Status{Enabled: true}.Reachable(),
	}
	for name, ok := range reach {
		if !ok {
			t.Errorf("Reachable is wrong for %s", name)
		}
	}
}
