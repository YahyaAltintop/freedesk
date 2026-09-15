package firebase

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// TokenProvider supplies a valid Firebase ID token for authorization.
type TokenProvider interface {
	IDToken(ctx context.Context) (string, error)
}

// Server-Sent Event names emitted by the Realtime Database REST streaming API.
const (
	EventPut         = "put"
	EventPatch       = "patch"
	EventKeepAlive   = "keep-alive"
	EventCancel      = "cancel"       // rules no longer allow reading the location
	EventAuthRevoked = "auth_revoked" // the auth token expired; reconnect with a fresh one
)

// ServerTimestamp is the placeholder the database replaces with its own clock
// (milliseconds since the epoch) when written. Rules validate timestamps
// against server time, so clients must never send their local clock.
func ServerTimestamp() map[string]string {
	return map[string]string{".sv": "timestamp"}
}

// RTDB is a minimal Realtime Database REST client: one-shot reads/writes plus
// Server-Sent Events streaming for live updates.
type RTDB struct {
	baseURL    string
	namespace  string
	tokens     TokenProvider
	httpClient *http.Client
}

// NewRTDB builds a client for the given database base URL. namespace is empty
// for production (encoded in the URL) and set to the database name for the
// emulator.
func NewRTDB(baseURL, namespace string, tokens TokenProvider) *RTDB {
	return &RTDB{
		baseURL:   strings.TrimRight(baseURL, "/"),
		namespace: namespace,
		tokens:    tokens,
		// No global timeout: streaming connections are long-lived (cancelled via ctx).
		httpClient: &http.Client{},
	}
}

func (r *RTDB) endpoint(ctx context.Context, path string, silent bool) (string, error) {
	token, err := r.tokens.IDToken(ctx)
	if err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("%s/%s.json?auth=%s", r.baseURL, strings.Trim(path, "/"), url.QueryEscape(token))
	if r.namespace != "" {
		endpoint += "&ns=" + url.QueryEscape(r.namespace)
	}
	if silent {
		// 204 with no body: the server does not echo the written data back.
		endpoint += "&print=silent"
	}
	return endpoint, nil
}

// Put writes value at path (overwriting any existing data).
func (r *RTDB) Put(ctx context.Context, path string, value any) error {
	return r.write(ctx, http.MethodPut, path, value, nil, true)
}

// Patch updates the named children of path without touching its siblings.
func (r *RTDB) Patch(ctx context.Context, path string, value any) error {
	return r.write(ctx, http.MethodPatch, path, value, nil, true)
}

// Push appends value under path and returns the server-generated key.
func (r *RTDB) Push(ctx context.Context, path string, value any) (string, error) {
	var out struct {
		Name string `json:"name"`
	}
	if err := r.write(ctx, http.MethodPost, path, value, &out, false); err != nil {
		return "", err
	}
	return out.Name, nil
}

// Delete removes the data at path.
func (r *RTDB) Delete(ctx context.Context, path string) error {
	return r.write(ctx, http.MethodDelete, path, nil, nil, true)
}

// Get reads the data at path into out. The second result is false when the
// location holds no data (the database answers `null`).
func (r *RTDB) Get(ctx context.Context, path string, out any) (bool, error) {
	endpoint, err := r.endpoint(ctx, path, false)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	data, err := r.do(req, http.MethodGet, path)
	if err != nil {
		return false, err
	}
	if trimmed := strings.TrimSpace(string(data)); trimmed == "" || trimmed == "null" {
		return false, nil
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (r *RTDB) write(ctx context.Context, method, path string, value, out any, silent bool) error {
	endpoint, err := r.endpoint(ctx, path, silent)
	if err != nil {
		return err
	}
	var body io.Reader
	if value != nil {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if value != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	data, err := r.do(req, method, path)
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (r *RTDB) do(req *http.Request, method, path string) ([]byte, error) {
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{Code: resp.StatusCode, Method: method, Path: path, Body: strings.TrimSpace(string(data))}
	}
	return data, nil
}

// StreamEvent is one Server-Sent Event emitted from a streamed path. For put
// and patch events Path is relative to the streamed location ("/" on initial
// sync) and Data is the JSON payload. For cancel/auth_revoked events Data
// carries the server's reason text and Path is empty.
type StreamEvent struct {
	Event string
	Path  string
	Data  json.RawMessage
}

// Stream opens a Server-Sent Events stream at path and emits events until ctx
// is cancelled or the connection ends (the channel is then closed). The auth
// token is fixed at open time: after roughly an hour the server emits
// auth_revoked and the caller must reopen the stream.
func (r *RTDB) Stream(ctx context.Context, path string) (<-chan StreamEvent, error) {
	endpoint, err := r.endpoint(ctx, path, false)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &StatusError{Code: resp.StatusCode, Method: "STREAM", Path: path, Body: strings.TrimSpace(string(data))}
	}

	events := make(chan StreamEvent)
	go func() {
		defer close(events)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

		var eventType string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event:"):
				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				ev, ok := parseEvent(eventType, payload)
				if !ok {
					continue
				}
				select {
				case events <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
		// A read error simply ends the stream; callers reconnect on close.
		_ = scanner.Err()
	}()
	return events, nil
}

// parseEvent turns one SSE data line into a StreamEvent. put/patch carry a
// {path, data} object; the control events carry a bare JSON string.
func parseEvent(eventType, payload string) (StreamEvent, bool) {
	switch eventType {
	case EventPut, EventPatch:
		var parsed struct {
			Path string          `json:"path"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
			return StreamEvent{}, false
		}
		return StreamEvent{Event: eventType, Path: parsed.Path, Data: parsed.Data}, true
	case EventCancel, EventAuthRevoked:
		return StreamEvent{Event: eventType, Data: json.RawMessage(payload)}, true
	default:
		return StreamEvent{}, false
	}
}
