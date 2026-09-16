// Package host publishes this machine under its pairing code in the Realtime
// Database and keeps the record fresh while the agent runs.
package host

import (
	"context"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/firebase"
)

const pathHosts = "hosts"

// Registrar creates, refreshes and removes this machine's host record.
type Registrar struct {
	rtdb     *firebase.RTDB
	ownerUID string
	version  string
}

// NewRegistrar builds a registrar bound to the authenticated owner.
func NewRegistrar(rtdb *firebase.RTDB, ownerUID, version string) *Registrar {
	return &Registrar{rtdb: rtdb, ownerUID: ownerUID, version: version}
}

type hostRecord struct {
	OwnerUID string            `json:"ownerUid"`
	Name     string            `json:"name"`
	Version  string            `json:"version"`
	LastSeen map[string]string `json:"lastSeen"`
}

// Register publishes /hosts/{code}. The rules only allow this when the code
// is free, already ours, or abandoned (no heartbeat for 5 minutes); a live
// record owned by someone else yields a permission-denied error and the
// caller picks another code.
func (r *Registrar) Register(ctx context.Context, code, name string) error {
	return r.rtdb.Put(ctx, pathHosts+"/"+code, hostRecord{
		OwnerUID: r.ownerUID,
		Name:     name,
		Version:  r.version,
		LastSeen: firebase.ServerTimestamp(),
	})
}

// Heartbeat refreshes lastSeen so viewers can tell a live host from a
// crashed one (the record cannot be removed server-side when the process
// dies, so freshness is the presence signal).
func (r *Registrar) Heartbeat(ctx context.Context, code string) error {
	return r.rtdb.Patch(ctx, pathHosts+"/"+code, map[string]any{
		"lastSeen": firebase.ServerTimestamp(),
	})
}

// Unregister removes the host record on graceful shutdown.
func (r *Registrar) Unregister(ctx context.Context, code string) error {
	return r.rtdb.Delete(ctx, pathHosts+"/"+code)
}
