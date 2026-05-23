package relay_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mayur-tolexo/tunnl/internal/client"
	"github.com/mayur-tolexo/tunnl/internal/relay"
)

func TestEndToEndTunnel(t *testing.T) {
	const base = "example.com"

	// 1. Fake local service the client will proxy to.
	localSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s", r.URL.Path)
	}))
	defer localSrv.Close()
	localPort := portFromURL(t, localSrv.URL)

	// 2. Relay: control on tunnl.example.com, forwarder for everything else.
	reg := relay.NewRegistry()
	ctrl := relay.NewControl(relay.Config{Token: "secret", BaseDomain: base, MaxTunnels: 100}, reg)
	fwd := relay.NewForwarder(reg, base)
	// In-process the test routes control by request path; production routes by
	// Host (covered by cmd/tunnld TestRouterDispatchesByHost). The websocket
	// client cannot easily spoof the Host header, so path routing keeps this
	// test simple. Public requests use other paths and set req.Host below.
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tunnel" {
			ctrl.ServeHTTP(w, r)
			return
		}
		fwd.ServeHTTP(w, r)
	})
	relaySrv := httptest.NewServer(mux)
	defer relaySrv.Close()

	// 3. Start the client against the relay's control endpoint.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(relaySrv.URL, "http") + "/tunnel"
	go func() {
		_ = client.Run(ctx, client.Config{RelayURL: wsURL, Token: "secret", LocalPort: localPort})
	}()

	// 4. Wait until the tunnel is live by polling the actual public endpoint.
	// This avoids the 502 window that exists while the registry holds only the
	// placeholder reservation (Count==1 but yamux is not yet ready).
	body := waitForTunnel(t, relaySrv.URL, base, reg)

	// 5. Assert the final body.
	if body != "hello from /world" {
		t.Fatalf("body = %q, want %q", body, "hello from /world")
	}
}

// waitForTunnel polls the relay with real HTTP requests until the tunnel is
// fully live (non-502 response) or the deadline elapses. It returns the
// response body of the first successful request (to /world).
func waitForTunnel(t *testing.T, relayURL, base string, reg *relay.Registry) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)

	// First wait until a subdomain appears in the registry so we know what
	// Host header to spoof.
	var sub string
	for time.Now().Before(deadline) {
		if s := reg.AnySubdomain(); s != "" {
			sub = s
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if sub == "" {
		t.Fatal("tunnel did not register in time")
	}

	// Now poll the actual endpoint until it returns something other than 502,
	// meaning the yamux session is live and the reservation has been replaced.
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", relayURL+"/world", nil)
		req.Host = sub + "." + base
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadGateway {
			return string(body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("tunnel never became ready (still 502 at deadline)")
	return ""
}

func portFromURL(t *testing.T, rawURL string) int {
	t.Helper()
	h := strings.TrimPrefix(rawURL, "http://")
	_, p, _ := strings.Cut(h, ":")
	n, _ := strconv.Atoi(p)
	return n
}
