package relay

import (
	"bufio"
	"io"
	"net/http"
	"strings"
)

// Forwarder is the public-facing HTTP handler. It maps the request's Host to a
// registered tunnel and proxies the request over a fresh stream.
type Forwarder struct {
	reg        *Registry
	baseDomain string
	// MaxRequestBody caps the forwarded request body in bytes (light abuse
	// guardrail). Defaults to 32 MiB; <= 0 disables the cap.
	MaxRequestBody int64
}

// NewForwarder returns a Forwarder serving subdomains of baseDomain.
func NewForwarder(reg *Registry, baseDomain string) *Forwarder {
	return &Forwarder{reg: reg, baseDomain: baseDomain, MaxRequestBody: 32 << 20}
}

func (f *Forwarder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sub, ok := subdomainFromHost(r.Host, f.baseDomain)
	if !ok {
		http.Error(w, "tunnl: no tunnel for this host", http.StatusNotFound)
		return
	}
	tun, ok := f.reg.Get(sub)
	if !ok {
		http.Error(w, "tunnl: tunnel not found", http.StatusNotFound)
		return
	}
	stream, err := tun.OpenStream()
	if err != nil {
		http.Error(w, "tunnl: tunnel unavailable", http.StatusBadGateway)
		return
	}
	defer stream.Close()

	if f.MaxRequestBody > 0 && r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, f.MaxRequestBody)
	}
	if err := r.Write(stream); err != nil {
		http.Error(w, "tunnl: failed to forward request", http.StatusBadGateway)
		return
	}
	resp, err := http.ReadResponse(bufio.NewReader(stream), r)
	if err != nil {
		http.Error(w, "tunnl: no response from local service", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// subdomainFromHost extracts the leftmost label of host when host is a direct
// subdomain of baseDomain. It strips any port. Returns ok=false for the apex
// domain or hosts outside baseDomain.
func subdomainFromHost(host, baseDomain string) (string, bool) {
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	suffix := "." + baseDomain
	if !strings.HasSuffix(host, suffix) {
		return "", false
	}
	sub := strings.TrimSuffix(host, suffix)
	if sub == "" || strings.Contains(sub, ".") {
		return "", false
	}
	return sub, true
}
