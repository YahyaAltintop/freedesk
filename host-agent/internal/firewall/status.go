package firewall

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Status is what the computer's firewall would do with a packet for this
// program arriving from another computer: Windows Defender Firewall on
// Windows, the firewall of System Settings > Network on macOS.
type Status struct {
	// Owner names the security product that has taken the firewall over from
	// Windows (Norton 360, Kaspersky, …), or is empty when Windows Defender
	// Firewall does the job itself. With an owner, Windows' profiles still
	// read as on but its rules are not enforced and it never asks about a
	// program, so nothing below says what reaches this one. Windows only.
	Owner string
	// Enabled is whether the firewall is on (on Windows, any of its
	// profiles). Off, nothing below matters and the program is reachable.
	Enabled bool
	// BlockAll is whether the firewall refuses every incoming connection but
	// the system's own, whatever it says about programs: macOS's "Block all
	// incoming connections".
	BlockAll bool
	// Blocked is whether the firewall blocks the program by name: on Windows
	// an enabled inbound block rule names it, and wins over every allow
	// rule; on macOS it is set to block incoming connections.
	Blocked bool
	// Allowed is whether the firewall lets the program in: on Windows an
	// enabled inbound allow rule names it for the kind of network the
	// computer is on now; on macOS nothing blocks it (see Check there).
	Allowed bool
	// Elsewhere lists the network kinds allow rules for the program do name,
	// when none of them covers the current one: the operator ticked Public
	// and the computer is on a Private network, typically. Windows only.
	Elsewhere []string
	// Network is the kind of network the computer is on (Private, Public,
	// Domain), or empty when Windows could not say. Windows only.
	Network string
}

// Reachable reports whether packets from other computers get through. When
// another product owns the firewall its rules cannot be read from here, and
// silence is better than a false alarm.
func (s Status) Reachable() bool {
	return s.Owner != "" || !s.Enabled || (!s.BlockAll && !s.Blocked && s.Allowed)
}

// maxOwnerLen bounds a product name before it reaches the operator's window.
const maxOwnerLen = 64

// parse reads the query's output: an "owner|Name" line (the name empty when
// Windows runs its own firewall), one "enabled|N" line, one "network|A,B"
// line and a "rule|Action|Profiles|Program" line per inbound rule that names
// a program. Anything else is ignored, so a stray warning on stdout cannot
// turn into a wrong answer.
func parse(out, exe string) Status {
	var st Status
	var networks []string
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "|")
		switch fields[0] {
		case "owner":
			if st.Owner == "" && len(fields) >= 2 {
				st.Owner = cleanName(strings.Join(fields[1:], "|"))
			}
		case "enabled":
			if len(fields) == 2 {
				n, _ := strconv.Atoi(strings.TrimSpace(fields[1]))
				st.Enabled = n > 0
			}
		case "network":
			if len(fields) == 2 {
				for cat := range strings.SplitSeq(fields[1], ",") {
					if cat = normalizeProfile(cat); cat != "" {
						networks = append(networks, cat)
					}
				}
			}
		case "rule":
			if len(fields) != 4 || !sameProgram(fields[3], exe) {
				continue
			}
			action, profiles := strings.TrimSpace(fields[1]), fields[2]
			switch {
			case strings.EqualFold(action, "Block"):
				st.Blocked = true
			case strings.EqualFold(action, "Allow"):
				if covers(profiles, networks) {
					st.Allowed = true
				} else {
					for p := range strings.SplitSeq(profiles, ",") {
						if p = normalizeProfile(p); p != "" {
							st.Elsewhere = append(st.Elsewhere, p)
						}
					}
				}
			}
		}
	}
	if len(networks) > 0 {
		st.Network = networks[0]
	}
	if st.Allowed {
		st.Elsewhere = nil
	}
	return st
}

// cleanName makes a product's self-chosen display name fit for one line of
// the operator's window: no control characters, no surrounding space, and a
// sane length.
func cleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxOwnerLen {
		s = strings.TrimSpace(string(r[:maxOwnerLen]))
	}
	return s
}

// covers reports whether a rule's profile list ("Any", "Private, Public",
// "Domain") includes one of the networks the computer is on. With no known
// network any allow rule has to count: better silence than a false alarm.
func covers(profiles string, networks []string) bool {
	if len(networks) == 0 {
		return true
	}
	for p := range strings.SplitSeq(profiles, ",") {
		p = normalizeProfile(p)
		if p == "Any" || slices.Contains(networks, p) {
			return true
		}
	}
	return false
}

// normalizeProfile maps the names Windows uses for a connection's category
// and for a rule's profile onto one vocabulary: Private, Public, Domain, Any.
func normalizeProfile(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "private":
		return "Private"
	case "public":
		return "Public"
	case "domain", "domainauthenticated":
		return "Domain"
	case "any":
		return "Any"
	}
	return ""
}
