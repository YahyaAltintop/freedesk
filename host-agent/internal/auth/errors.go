package auth

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Error is a rejected Identity Toolkit / Secure Token request. Google's
// machine-readable reason is kept so the console can say what to fix instead
// of dumping the raw JSON envelope on a person who just double-clicked the exe.
type Error struct {
	Status  int    // HTTP status code
	Reason  string // e.g. API_KEY_HTTP_REFERRER_BLOCKED, ADMIN_ONLY_OPERATION
	Message string // Google's human-readable message
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Firebase sign-in failed (HTTP %d", e.Status)
	if e.Reason != "" {
		fmt.Fprintf(&b, ", %s", e.Reason)
	}
	b.WriteString(")")
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if hint := e.Hint(); hint != "" {
		b.WriteString("\n  -> ")
		b.WriteString(hint)
	}
	return b.String()
}

// Hint explains the likely cause and the fix for the well-known reasons; it is
// empty for anything else.
func (e *Error) Hint() string {
	switch e.Reason {
	case "API_KEY_HTTP_REFERRER_BLOCKED", "API_KEY_IP_ADDRESS_BLOCKED",
		"API_KEY_ANDROID_APP_BLOCKED", "API_KEY_IOS_APP_BLOCKED":
		return "The Firebase API key built into this program has application restrictions (websites, IP addresses or apps). " +
			"The host agent is a desktop program: it sends no website referrer, so Google rejects it. " +
			"In Google Cloud Console -> APIs & Services -> Credentials, give the agent a key with Application restrictions = None " +
			"(release builds: repository variable FIREBASE_AGENT_API_KEY)."
	case "API_KEY_SERVICE_BLOCKED":
		return "The Firebase API key's API restrictions do not allow the Identity Toolkit API / Token Service API. " +
			"In Google Cloud Console -> APIs & Services -> Credentials, edit the key: choose \"Don't restrict key\" " +
			"or select both \"Identity Toolkit API\" and \"Token Service API\"."
	case "API_KEY_INVALID":
		return "The Firebase API key is not valid for this project. Check the key; a key created a moment ago can take a few minutes to become active."
	case "ADMIN_ONLY_OPERATION", "OPERATION_NOT_ALLOWED":
		return "Anonymous sign-in is disabled. Firebase Console -> Authentication -> Sign-in method -> Anonymous -> Enable."
	case "CONFIGURATION_NOT_FOUND":
		return "Firebase Authentication is not set up for this project. Open Authentication in the Firebase Console and enable the Anonymous provider."
	case "TOO_MANY_ATTEMPTS_TRY_LATER", "QUOTA_EXCEEDED":
		return "Too many sign-ins from this network for now; wait a while and start the agent again."
	}
	return ""
}

// googleError mirrors the error envelope of Google APIs. API-key rejections
// carry the reason in details[].reason (google.rpc.ErrorInfo); Identity
// Toolkit's own errors use the reason as the message ("ADMIN_ONLY_OPERATION",
// "TOO_MANY_ATTEMPTS_TRY_LATER : ...").
type googleError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Reason string `json:"reason"`
		} `json:"details"`
	} `json:"error"`
}

func newError(status int, body []byte) *Error {
	e := &Error{Status: status}
	var g googleError
	if err := json.Unmarshal(body, &g); err != nil || g.Error.Message == "" {
		e.Message = compact(string(body), 300)
		return e
	}
	e.Message = g.Error.Message
	for _, d := range g.Error.Details {
		if d.Reason != "" {
			e.Reason = d.Reason
			break
		}
	}
	if e.Reason == "" {
		e.Reason = reasonFromMessage(g.Error.Message)
	}
	return e
}

// reasonFromMessage extracts the leading UPPER_SNAKE code that Identity
// Toolkit uses as its message; prose ("API key not valid. ...") yields "".
func reasonFromMessage(msg string) string {
	code, _, _ := strings.Cut(strings.TrimSpace(msg), " ")
	code = strings.TrimSuffix(code, ":")
	if !strings.Contains(code, "_") {
		return ""
	}
	for _, r := range code {
		if r != '_' && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return code
}

// compact collapses whitespace and truncates s so a non-JSON error page does
// not flood the console.
func compact(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
