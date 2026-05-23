package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
