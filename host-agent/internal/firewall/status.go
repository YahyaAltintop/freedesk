package firewall

import (
	"slices"
	"strconv"
	"strings"
)

// Status is what Windows Defender Firewall would do with a packet for this
// program arriving from another computer.
type Status struct {
	// Enabled is whether any firewall profile is on. Off, nothing below
	// matters and the program is reachable.
	Enabled bool
	// Blocked is whether an enabled inbound block rule names the program. A
	// block rule wins over every allow rule.
	Blocked bool
	// Allowed is whether an enabled inbound allow rule names the program for
	// the kind of network the computer is on now.
	Allowed bool
	// Elsewhere lists the network kinds allow rules for the program do name,
	// when none of them covers the current one: the operator ticked Public
	// and the computer is on a Private network, typically.
	Elsewhere []string
	// Network is the kind of network the computer is on (Private, Public,
	// Domain), or empty when Windows could not say.
	Network string
}

// Reachable reports whether packets from other computers get through.
func (s Status) Reachable() bool {
	return !s.Enabled || (!s.Blocked && s.Allowed)
}

// parse reads the query's output: one "enabled|N" line, one "network|A,B"
// line and a "rule|Action|Profiles|Program" line per inbound rule that names
// a program. Anything else is ignored, so a stray warning on stdout cannot
// turn into a wrong answer.
func parse(out, exe string) Status {
	var st Status
	var networks []string
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "|")
		switch fields[0] {
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
