// Package auth authenticates the host agent against Firebase using the Identity
// Toolkit + Secure Token REST APIs (no Admin SDK, no service account), so the
// agent runs as a real user and remains subject to Security Rules.
//
// The agent signs in ANONYMOUSLY and every launch is a brand-new identity:
// nothing is persisted on disk. On graceful shutdown the account deletes
// itself so the project's user table does not accumulate dead identities.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Token holds the credentials returned by the Firebase auth endpoints.
type Token struct {
	IDToken      string
	RefreshToken string
	UID          string
	ExpiresAt    time.Time
}

// Client talks to the Firebase Identity Toolkit + Secure Token REST APIs.
type Client struct {
	apiKey       string
	authBaseURL  string
	tokenBaseURL string
	httpClient   *http.Client
}

// NewClient builds an auth client for the given API key and base URLs.
func NewClient(apiKey, authBaseURL, tokenBaseURL string) *Client {
	return &Client{
		apiKey:       apiKey,
		authBaseURL:  authBaseURL,
		tokenBaseURL: tokenBaseURL,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

type signUpResponse struct {
	IDToken      string `json:"idToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    string `json:"expiresIn"`
	LocalID      string `json:"localId"`
}

// SignUpAnonymous creates a brand-new anonymous Firebase user (Identity Toolkit
// signUp with no email/password). Requires the Anonymous provider to be enabled
// on the Firebase project.
func (c *Client) SignUpAnonymous(ctx context.Context) (*Token, error) {
	url := fmt.Sprintf("%s/v1/accounts:signUp?key=%s", c.authBaseURL, c.apiKey)
	payload := map[string]any{"returnSecureToken": true}
	var out signUpResponse
	if err := c.postJSON(ctx, url, payload, &out); err != nil {
		return nil, err
	}
	return &Token{
		IDToken:      out.IDToken,
		RefreshToken: out.RefreshToken,
		UID:          out.LocalID,
		ExpiresAt:    expiryFromSeconds(out.ExpiresIn),
	}, nil
}

type refreshResponse struct {
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    string `json:"expires_in"`
	UserID       string `json:"user_id"`
}

// Refresh exchanges a refresh token for a fresh ID token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	url := fmt.Sprintf("%s/v1/token?key=%s", c.tokenBaseURL, c.apiKey)
	payload := map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
	}
	var out refreshResponse
	if err := c.postJSON(ctx, url, payload, &out); err != nil {
		return nil, err
	}
	return &Token{
		IDToken:      out.IDToken,
		RefreshToken: out.RefreshToken,
		UID:          out.UserID,
		ExpiresAt:    expiryFromSeconds(out.ExpiresIn),
	}, nil
}

// DeleteAccount permanently deletes the user that owns idToken. A user may
// always delete itself; no admin credentials are involved.
func (c *Client) DeleteAccount(ctx context.Context, idToken string) error {
	url := fmt.Sprintf("%s/v1/accounts:delete?key=%s", c.authBaseURL, c.apiKey)
	return c.postJSON(ctx, url, map[string]any{"idToken": idToken}, nil)
}

func (c *Client) postJSON(ctx context.Context, url string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newError(resp.StatusCode, data)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func expiryFromSeconds(seconds string) time.Time {
	if d, err := time.ParseDuration(seconds + "s"); err == nil {
		return time.Now().Add(d)
	}
	// Firebase ID tokens last one hour; fall back conservatively.
	return time.Now().Add(50 * time.Minute)
}

// Manager keeps a valid ID token available, refreshing it as needed. It is safe
// for concurrent use by the heartbeat, inbox and session goroutines.
type Manager struct {
	client *Client
	mu     sync.Mutex
	token  *Token
}

// NewManager signs up a fresh anonymous user for this run of the agent.
func NewManager(ctx context.Context, client *Client) (*Manager, error) {
	tok, err := client.SignUpAnonymous(ctx)
	if err != nil {
		return nil, err
	}
	return &Manager{client: client, token: tok}, nil
}

// UID returns the authenticated user's id.
func (m *Manager) UID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.token.UID
}

// IDToken returns a valid ID token, refreshing it when close to expiry.
func (m *Manager) IDToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if time.Now().After(m.token.ExpiresAt.Add(-1 * time.Minute)) {
		tok, err := m.client.Refresh(ctx, m.token.RefreshToken)
		if err != nil {
			return "", err
		}
		if tok.UID == "" {
			tok.UID = m.token.UID // secure-token endpoint may omit the uid
		}
		m.token = tok
	}
	return m.token.IDToken, nil
}

// DeleteAccount removes this run's anonymous user from the project. Call it
// last during shutdown: every database write made by this identity must be
// finished first.
func (m *Manager) DeleteAccount(ctx context.Context) error {
	token, err := m.IDToken(ctx)
	if err != nil {
		return err
	}
	return m.client.DeleteAccount(ctx, token)
}
