//go:build windows

package firewall

import (
	"context"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"
)

// TestCheckAsksTheFirewall asks this machine's firewall. What it answers
// depends on the machine — Windows' own firewall on a CI runner, a security
// suite's on many desktops — so only that it was asked and answered is
// checked here; TestQueryReadsTheRules looks at what came back.
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
	t.Logf("in %v: owner=%q enabled=%v blocked=%v allowed=%v elsewhere=%v network=%q",
		time.Since(start), st.Owner, st.Enabled, st.Blocked, st.Allowed, st.Elsewhere, st.Network)
}

// TestQueryReadsTheRules reads the rules even where another product runs the
// firewall (Check leaves them out there) and checks every line is one parse
// takes. Every Windows ships enabled inbound rules that name a program
// (Core Networking's, for svchost.exe and System), so none at all means the
// rules were not read: a wrong vtable slot reads nothing, not an error.
func TestQueryReadsTheRules(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	report, err := ask(ctx, true)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	var owners, enabled, networks, rules int
	for line := range strings.SplitSeq(strings.TrimSuffix(report, "\n"), "\n") {
		fields := strings.Split(line, "|")
		switch fields[0] {
		case "owner":
			owners++
			t.Logf("%s", line)
		case "enabled":
			enabled++
			if n, err := strconv.Atoi(fields[1]); err != nil || n < 0 || n > 3 {
				t.Errorf("not a count of profiles: %q", line)
			}
		case "network":
			networks++
			t.Logf("%s", line)
			for name := range strings.SplitSeq(fields[1], ",") {
				if name != "" && !slices.Contains([]string{"Public", "Private", "DomainAuthenticated"}, name) {
					t.Errorf("unknown network category in %q", line)
				}
			}
		case "rule":
			rules++
			if len(fields) != 4 || (fields[1] != "Allow" && fields[1] != "Block") || fields[2] == "" || strings.TrimSpace(fields[3]) == "" {
				t.Errorf("malformed rule line %q", line)
			}
		default:
			t.Errorf("unexpected line %q", line)
		}
	}
	if owners != 1 || enabled != 1 || networks != 1 {
		t.Errorf("want one owner, enabled and network line each, got %d, %d, %d:\n%s", owners, enabled, networks, report)
	}
	if rules == 0 {
		t.Errorf("no enabled inbound rule that names a program was read:\n%s", report)
	}
	t.Logf("%d rules", rules)
}

func TestNames(t *testing.T) {
	profiles := map[int32]string{
		profileAll:                     "Any",
		profileDomain:                  "Domain",
		profilePrivate | profilePublic: "Private, Public",
		profileDomain | profilePrivate | profilePublic: "Domain, Private, Public",
	}
	for bits, want := range profiles {
		if got := profileNames(bits); got != want {
			t.Errorf("profileNames(%#x) = %q, want %q", bits, got, want)
		}
	}
	categories := map[int32]string{0: "Public", 1: "Private", 2: "DomainAuthenticated", 7: ""}
	for c, want := range categories {
		if got := categoryName(c); got != want {
			t.Errorf("categoryName(%d) = %q, want %q", c, got, want)
		}
	}
	// What query writes, read back by parse.
	exe := `C:\Program Files\FreeDesk\freedesk.exe`
	report := "owner|\nenabled|3\nnetwork|" + categoryName(2) + "\n" +
		"rule|Allow|" + profileNames(profileDomain|profilePrivate) + "|" + exe + "\n"
	if got, want := parse(report, exe), (Status{Enabled: true, Allowed: true, Network: "Domain"}); !reflect.DeepEqual(got, want) {
		t.Errorf("parse(query's lines) = %+v, want %+v", got, want)
	}
}

// IEnumVARIANT::Next and VariantClear write and read whole VARIANTs: a
// mirror of the wrong size would corrupt memory.
func TestVariantLayout(t *testing.T) {
	if got, want := unsafe.Sizeof(variant{}), 8+2*unsafe.Sizeof(uintptr(0)); got != want {
		t.Fatalf("variant is %d bytes, VARIANT is %d", got, want)
	}
	if off := unsafe.Offsetof(variant{}.val); off != 8 {
		t.Fatalf("the VARIANT union starts at byte 8, the mirror's at %d", off)
	}
}
