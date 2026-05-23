package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/libdns/acmedns"
	"github.com/libdns/godaddy"
)

func TestRouterDispatchesByHost(t *testing.T) {
	control := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("control")) })
	forwarder := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("forward")) })
	router := newRouter(control, forwarder, "tunnl.example.com")

	cases := []struct{ host, want string }{
		{"tunnl.example.com", "control"},
		{"tunnl.example.com:443", "control"},
		{"happy-fox-0001.example.com", "forward"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = c.host
		router.ServeHTTP(rec, req)
		if rec.Body.String() != c.want {
			t.Errorf("host %q -> %q, want %q", c.host, rec.Body.String(), c.want)
		}
	}
}

func TestPortSuffix(t *testing.T) {
	cases := map[string]string{
		":8080":          ":8080",
		"127.0.0.1:8080": ":8080",
		"8080":           "",
		"":               "",
	}
	for in, want := range cases {
		if got := portSuffix(in); got != want {
			t.Errorf("portSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDNSProviderFromEnv(t *testing.T) {
	// Clear any inherited config so the cases are deterministic.
	for _, k := range []string{
		"TUNNL_DNS_PROVIDER", "TUNNL_GODADDY_KEY", "TUNNL_GODADDY_SECRET",
		"TUNNL_ACMEDNS_SERVER", "TUNNL_ACMEDNS_USERNAME", "TUNNL_ACMEDNS_PASSWORD", "TUNNL_ACMEDNS_SUBDOMAIN",
	} {
		t.Setenv(k, "")
	}

	// Default (godaddy) with credentials.
	t.Setenv("TUNNL_GODADDY_KEY", "k")
	t.Setenv("TUNNL_GODADDY_SECRET", "s")
	p, err := dnsProviderFromEnv()
	if err != nil {
		t.Fatalf("godaddy: %v", err)
	}
	gd, ok := p.(*godaddy.Provider)
	if !ok {
		t.Fatalf("want *godaddy.Provider, got %T", p)
	}
	if gd.APIToken != "k:s" {
		t.Fatalf("APIToken = %q, want k:s", gd.APIToken)
	}

	// godaddy with missing credentials -> error.
	t.Setenv("TUNNL_GODADDY_SECRET", "")
	if _, err := dnsProviderFromEnv(); err == nil {
		t.Fatal("expected error for missing godaddy credentials")
	}

	// acmedns with all fields.
	t.Setenv("TUNNL_DNS_PROVIDER", "acmedns")
	t.Setenv("TUNNL_ACMEDNS_SERVER", "http://127.0.0.1:8081")
	t.Setenv("TUNNL_ACMEDNS_USERNAME", "u")
	t.Setenv("TUNNL_ACMEDNS_PASSWORD", "pw")
	t.Setenv("TUNNL_ACMEDNS_SUBDOMAIN", "sub")
	p, err = dnsProviderFromEnv()
	if err != nil {
		t.Fatalf("acmedns: %v", err)
	}
	if _, ok := p.(*acmedns.Provider); !ok {
		t.Fatalf("want *acmedns.Provider, got %T", p)
	}

	// acmedns missing a field -> error.
	t.Setenv("TUNNL_ACMEDNS_PASSWORD", "")
	if _, err := dnsProviderFromEnv(); err == nil {
		t.Fatal("expected error for missing acmedns field")
	}

	// Unknown provider -> error.
	t.Setenv("TUNNL_DNS_PROVIDER", "route53")
	if _, err := dnsProviderFromEnv(); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
