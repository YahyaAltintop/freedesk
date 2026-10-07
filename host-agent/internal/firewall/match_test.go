package firewall

import (
	"reflect"
	"strings"
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
		{
			name: "another product runs the firewall: Windows still reads as on, with no rule",
			out:  "owner|Norton 360\nenabled|3\nnetwork|Private,Private\n",
			want: Status{Owner: "Norton 360", Enabled: true, Network: "Private"},
		},
		{
			name: "Windows runs its own firewall",
			out:  "owner|\nenabled|3\nnetwork|Private\nrule|Allow|Any|" + exe + "\n",
			want: Status{Enabled: true, Allowed: true, Network: "Private"},
		},
		{
			name: "a product name is kept whole, trimmed, and the first one wins",
			out:  "owner|  Acme | Shield\x07  \nowner|Other\nenabled|3\nnetwork|Public\n",
			want: Status{Owner: "Acme | Shield", Enabled: true, Network: "Public"},
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
		// Windows' rules say nothing when another product runs the firewall.
		"owned elsewhere": Status{Owner: "Norton 360", Enabled: true}.Reachable(),
	}
	for name, ok := range reach {
		if !ok {
			t.Errorf("Reachable is wrong for %s", name)
		}
	}
}

func TestCleanName(t *testing.T) {
	long := strings.Repeat("x", maxOwnerLen+10)
	cases := map[string]string{
		"Norton 360":          "Norton 360",
		"  Kaspersky\r\n":     "Kaspersky",
		"ESET\x00 Security":   "ESET Security",
		long:                  strings.Repeat("x", maxOwnerLen),
		"Ağ Güvenliği Duvarı": "Ağ Güvenliği Duvarı", // not ASCII, still one line
		"":                    "",
	}
	for in, want := range cases {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
}
