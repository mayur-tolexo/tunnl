package relay

import (
	"net"
	"sync"
)

// Tunnel is a live connection to a client capable of opening request streams.
type Tunnel interface {
	OpenStream() (net.Conn, error)
	Close() error
}

// Registry maps assigned subdomains to their live tunnels. It is safe for
// concurrent use and holds no state across process restarts.
type Registry struct {
	mu      sync.RWMutex
	tunnels map[string]Tunnel
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tunnels: make(map[string]Tunnel)}
}

// TryAdd registers t under subdomain. It returns false if the subdomain is
// already taken, leaving the existing entry untouched.
func (r *Registry) TryAdd(subdomain string, t Tunnel) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tunnels[subdomain]; exists {
		return false
	}
	r.tunnels[subdomain] = t
	return true
}

// Get returns the tunnel for subdomain, if any.
func (r *Registry) Get(subdomain string) (Tunnel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tunnels[subdomain]
	return t, ok
}

// Remove deletes the entry for subdomain if present.
func (r *Registry) Remove(subdomain string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tunnels, subdomain)
}

// Count returns the number of live tunnels.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tunnels)
}

// AnySubdomain returns one registered subdomain, or "" if none. Intended for
// tests and diagnostics.
func (r *Registry) AnySubdomain() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for s := range r.tunnels {
		return s
	}
	return ""
}
