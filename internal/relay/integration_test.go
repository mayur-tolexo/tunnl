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

	// 4. Wait for the tunnel to register, then learn its subdomain.
	sub := waitForTunnel(t, reg)

	// 5. Make a public request through the relay, spoofing the tunnel Host.
	req, _ := http.NewRequest("GET", relaySrv.URL+"/world", nil)
	req.Host = sub + "." + base
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("public request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if string(body) != "hello from /world" {
		t.Fatalf("body = %q, want %q", body, "hello from /world")
	}
}

func waitForTunnel(t *testing.T, reg *relay.Registry) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if reg.Count() == 1 {
			return reg.AnySubdomain()
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("tunnel did not register in time")
	return ""
}

func portFromURL(t *testing.T, rawURL string) int {
	t.Helper()
	h := strings.TrimPrefix(rawURL, "http://")
	_, p, _ := strings.Cut(h, ":")
	n, _ := strconv.Atoi(p)
	return n
}
