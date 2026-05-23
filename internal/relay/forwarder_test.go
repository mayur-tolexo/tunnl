package relay

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubdomainFromHost(t *testing.T) {
	cases := []struct {
		host, base, want string
		ok               bool
	}{
		{"happy-fox-0001.example.com", "example.com", "happy-fox-0001", true},
		{"happy-fox-0001.example.com:443", "example.com", "happy-fox-0001", true},
		{"example.com", "example.com", "", false},           // apex, no subdomain
		{"tunnl.example.com", "example.com", "tunnl", true}, // reserved host handled by caller
		{"evil.com", "example.com", "", false},              // wrong base domain
	}
	for _, c := range cases {
		got, ok := subdomainFromHost(c.host, c.base)
		if got != c.want || ok != c.ok {
			t.Errorf("subdomainFromHost(%q, %q) = (%q, %v); want (%q, %v)", c.host, c.base, got, ok, c.want, c.ok)
		}
	}
}

// pipeTunnel is a Tunnel whose OpenStream returns one end of an in-memory pipe.
// A goroutine plays the role of the client on the other end.
type pipeTunnel struct {
	handler func(stream net.Conn)
}

func (p *pipeTunnel) OpenStream() (net.Conn, error) {
	relayEnd, clientEnd := net.Pipe()
	go p.handler(clientEnd)
	return relayEnd, nil
}
func (p *pipeTunnel) Close() error { return nil }

func TestForwarderForwardsRequestAndResponse(t *testing.T) {
	reg := NewRegistry()
	// The fake client reads the forwarded request and writes a canned response.
	reg.TryAdd("happy-fox-0001", &pipeTunnel{handler: func(stream net.Conn) {
		defer stream.Close()
		req, err := http.ReadRequest(bufio.NewReader(stream))
		if err != nil {
			return
		}
		if req.URL.Path != "/hello" {
			return
		}
		resp := &http.Response{
			StatusCode:    http.StatusOK,
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        http.Header{"Content-Type": {"text/plain"}},
			Body:          io.NopCloser(stringReader("pong")),
			ContentLength: 4,
		}
		_ = resp.Write(stream)
	}})

	fwd := NewForwarder(reg, "example.com")
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest("GET", "http://happy-fox-0001.example.com/hello", nil)
	fwd.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "pong" {
		t.Fatalf("body = %q, want pong", rec.Body.String())
	}
}

func TestForwarderUnknownSubdomain404(t *testing.T) {
	fwd := NewForwarder(NewRegistry(), "example.com")
	rec := httptest.NewRecorder()
	fwd.ServeHTTP(rec, httptest.NewRequest("GET", "http://nope-nope-0000.example.com/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func stringReader(s string) io.Reader { return io.NopCloser(io.Reader(&sr{s: s})) }

type sr struct {
	s string
	i int
}

func (r *sr) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}
