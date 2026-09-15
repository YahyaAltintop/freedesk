// Package localapi exposes this agent's pairing code to the web UI running on
// the SAME machine, over a loopback-only HTTP endpoint. The browser cannot read
// local files or processes, so this is the bridge that lets the home page show
// "this computer's code". It binds to 127.0.0.1 exclusively — nothing is
// reachable from the network.
package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Identity is the payload served to the local web UI.
type Identity struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Server is a running loopback identity server.
type Server struct {
	srv *http.Server
}

// Start listens on 127.0.0.1:port and serves GET /identity to the given web
// origins only. It fails fast if the port is taken (e.g. a second agent
// instance).
//
// The origin allow-list matters even on loopback: any website open in the
// operator's browser could otherwise read this machine's code and name. Only
// the FreeDesk web app (and a local dev server) may ask.
func Start(port int, id Identity, allowedOrigins []string) (*Server, error) {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = true
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/identity", func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !allowed[origin] {
			// No CORS headers: the browser refuses to hand the response to
			// the page. Non-browser callers on this machine are not a concern
			// (they can already see the console).
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		// Chrome's private-network-access preflight for public → loopback.
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(id)
	})

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}

	s := &Server{srv: &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}}
	go func() { _ = s.srv.Serve(listener) }()
	return s, nil
}

// Close shuts the server down gracefully.
func (s *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
}
