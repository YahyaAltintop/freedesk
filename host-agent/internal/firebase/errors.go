// Package firebase provides a minimal Realtime Database REST/SSE client scoped
// to what the host agent needs: host registration, the connection-request
// inbox and WebRTC signaling. No media or input ever passes through here.
package firebase

import (
	"errors"
	"fmt"
	"net/http"
)

// StatusError reports a non-2xx Realtime Database response, exposing the HTTP
// status so callers can react to specific outcomes. Security-rule denials
// arrive as 401 (unlike Firestore's 403).
type StatusError struct {
	Code   int
	Method string
	Path   string
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("rtdb %s %s failed (%d): %s", e.Method, e.Path, e.Code, e.Body)
}

// IsPermissionDenied reports whether err is a security-rule denial.
func IsPermissionDenied(err error) bool {
	var statusErr *StatusError
	return errors.As(err, &statusErr) && statusErr.Code == http.StatusUnauthorized
}
