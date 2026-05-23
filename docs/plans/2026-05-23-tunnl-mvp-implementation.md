# tunnl MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `tunnl`, a reverse-tunnel tool that exposes `http://localhost:<port>` at a public `https://<slug>.<domain>` URL with automatic wildcard TLS.

**Architecture:** Two Go binaries plus a shared package. The relay (`tunnld`) runs on a public VPS, terminates TLS with a wildcard cert, and routes inbound requests by `Host` header to the right client. The client (`tunnl`) opens one outbound WebSocket to the relay, which both sides wrap in a yamux session; the relay opens one yamux stream per inbound HTTP request and the client proxies each to localhost.

**Tech Stack:** Go 1.26, `github.com/coder/websocket` (WebSocket + `net.Conn` adapter), `github.com/hashicorp/yamux` (stream multiplexing), `github.com/caddyserver/certmagic` + `github.com/libdns/godaddy` (wildcard DNS-01 TLS).

**Spec:** `docs/design/2026-05-23-tunnl-mvp-design.md`

---

## File structure

```
tunnl/
├── go.mod                          # module github.com/mayur-tolexo/tunnl
├── cmd/
│   ├── tunnld/main.go              # relay entrypoint: config, routing, TLS, shutdown
│   └── tunnl/main.go               # client CLI: `tunnl http <port>`
├── internal/
│   ├── protocol/
│   │   ├── protocol.go             # Message type + Encode/Decode
│   │   └── protocol_test.go
│   ├── relay/
│   │   ├── subdomain.go            # random slug generator
│   │   ├── subdomain_test.go
│   │   ├── registry.go             # subdomain -> Tunnel map (concurrency-safe)
│   │   ├── registry_test.go
│   │   ├── forwarder.go            # public HTTP handler: lookup + forward
│   │   ├── forwarder_test.go
│   │   ├── control.go              # WS control handler: auth, register, yamux
│   │   ├── tls.go                  # certmagic + godaddy TLS config
│   │   └── integration_test.go     # end-to-end relay+client over loopback
│   └── client/
│       ├── client.go               # dial, register, accept streams, proxy, reconnect
│       └── client_test.go
└── docs/
    ├── design/2026-05-23-tunnl-mvp-design.md
    └── plans/2026-05-23-tunnl-mvp-implementation.md
```

Each file has one responsibility. `protocol` is pure (encoding only, no transport deps). `relay` owns server-side logic; `client` owns client-side logic. The yamux-over-websocket wiring is exercised for real by `integration_test.go`, which is the primary safety net for the transport.

---

## Task 1: Scaffold the module

**Files:**
- Create: `go.mod`
- Create: `internal/protocol/protocol.go` (empty package stub)

- [ ] **Step 1: Initialize the module**

Run from the repo root:
```bash
go mod init github.com/mayur-tolexo/tunnl
```

- [ ] **Step 2: Add dependencies**

```bash
go get github.com/coder/websocket@latest
go get github.com/hashicorp/yamux@latest
go get github.com/caddyserver/certmagic@latest
go get github.com/libdns/godaddy@latest
```

- [ ] **Step 3: Create a package stub so the module builds**

Create `internal/protocol/protocol.go`:
```go
// Package protocol defines the control messages exchanged between the tunnl
// client and the tunnld relay during the connection handshake.
package protocol
```

- [ ] **Step 4: Verify it builds**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/protocol/protocol.go
git commit -m "chore: scaffold go module and dependencies"
```

---

## Task 2: Protocol messages

**Files:**
- Modify: `internal/protocol/protocol.go`
- Test: `internal/protocol/protocol_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/protocol/protocol_test.go`:
```go
package protocol

import "testing"

func TestEncodeDecodeRegister(t *testing.T) {
	in := Message{Type: TypeRegister, Register: &Register{Token: "secret", Target: "http://localhost:3000"}}
	data, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out.Type != TypeRegister {
		t.Fatalf("type = %q, want %q", out.Type, TypeRegister)
	}
	if out.Register == nil || out.Register.Token != "secret" || out.Register.Target != "http://localhost:3000" {
		t.Fatalf("register = %+v, want token=secret target=http://localhost:3000", out.Register)
	}
}

func TestEncodeDecodeRegistered(t *testing.T) {
	in := Message{Type: TypeRegistered, Registered: &Registered{URL: "https://happy-fox-0001.example.com", Subdomain: "happy-fox-0001"}}
	data, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out.Registered == nil || out.Registered.Subdomain != "happy-fox-0001" {
		t.Fatalf("registered = %+v", out.Registered)
	}
}

func TestEncodeDecodeError(t *testing.T) {
	in := Message{Type: TypeError, Error: &Error{Code: "unauthorized", Message: "invalid token"}}
	data, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out.Error == nil || out.Error.Code != "unauthorized" {
		t.Fatalf("error = %+v", out.Error)
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	if _, err := Decode([]byte("not json")); err == nil {
		t.Fatal("expected error decoding invalid JSON")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/protocol/`
Expected: FAIL — undefined `Message`, `TypeRegister`, etc.

- [ ] **Step 3: Implement the messages**

Replace the body of `internal/protocol/protocol.go` with:
```go
// Package protocol defines the control messages exchanged between the tunnl
// client and the tunnld relay during the connection handshake.
package protocol

import "encoding/json"

// MessageType identifies a control message.
type MessageType string

const (
	// TypeRegister is sent by the client to request a tunnel.
	TypeRegister MessageType = "register"
	// TypeRegistered is the relay's success reply.
	TypeRegistered MessageType = "registered"
	// TypeError is the relay's failure reply.
	TypeError MessageType = "error"
)

// Register asks the relay to open a tunnel.
type Register struct {
	Token  string `json:"token"`
	Target string `json:"target"` // informational, e.g. "http://localhost:3000"
}

// Registered tells the client which public URL was assigned.
type Registered struct {
	URL       string `json:"url"`
	Subdomain string `json:"subdomain"`
}

// Error reports why a request failed.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Message is the single envelope sent over the control connection. Exactly one
// of the pointer fields is set, matching Type.
type Message struct {
	Type       MessageType `json:"type"`
	Register   *Register   `json:"register,omitempty"`
	Registered *Registered `json:"registered,omitempty"`
	Error      *Error      `json:"error,omitempty"`
}

// Encode marshals a Message to its wire form.
func Encode(m Message) ([]byte, error) {
	return json.Marshal(m)
}

// Decode parses a Message from its wire form.
func Decode(data []byte) (Message, error) {
	var m Message
	err := json.Unmarshal(data, &m)
	return m, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/protocol/`
Expected: PASS (ok).

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/
git commit -m "feat: add control protocol messages"
```

---

## Task 3: Subdomain generator

**Files:**
- Create: `internal/relay/subdomain.go`
- Test: `internal/relay/subdomain_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/relay/subdomain_test.go`:
```go
package relay

import (
	"regexp"
	"testing"
)

var subdomainRe = regexp.MustCompile(`^[a-z]+-[a-z]+-\d{4}$`)

func TestGenerateSubdomainFormat(t *testing.T) {
	s, err := GenerateSubdomain()
	if err != nil {
		t.Fatalf("GenerateSubdomain: %v", err)
	}
	if !subdomainRe.MatchString(s) {
		t.Fatalf("subdomain %q does not match %s", s, subdomainRe)
	}
}

func TestGenerateSubdomainVariety(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		s, err := GenerateSubdomain()
		if err != nil {
			t.Fatalf("GenerateSubdomain: %v", err)
		}
		seen[s] = true
	}
	// With ~tens of thousands of combinations, 100 draws should yield many
	// distinct values. Allow generous slack to avoid flakiness.
	if len(seen) < 90 {
		t.Fatalf("expected high variety, got %d distinct of 100", len(seen))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestGenerateSubdomain`
Expected: FAIL — undefined `GenerateSubdomain`.

- [ ] **Step 3: Implement the generator**

Create `internal/relay/subdomain.go`:
```go
package relay

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

var adjectives = []string{
	"happy", "brave", "calm", "clever", "eager", "fancy", "gentle", "jolly",
	"keen", "lively", "merry", "nimble", "proud", "quiet", "rapid", "shiny",
	"swift", "tidy", "witty", "zesty",
}

var nouns = []string{
	"fox", "otter", "panda", "hawk", "lynx", "moose", "newt", "owl",
	"quail", "raven", "seal", "tiger", "viper", "wolf", "yak", "zebra",
	"bison", "crane", "dingo", "egret",
}

// GenerateSubdomain returns a random, URL-safe slug like "happy-fox-0042".
func GenerateSubdomain() (string, error) {
	adj, err := pick(adjectives)
	if err != nil {
		return "", err
	}
	noun, err := pick(nouns)
	if err != nil {
		return "", err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%04d", adj, noun, n.Int64()), nil
}

func pick(list []string) (string, error) {
	idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		return "", err
	}
	return list[idx.Int64()], nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/ -run TestGenerateSubdomain`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/subdomain.go internal/relay/subdomain_test.go
git commit -m "feat: add random subdomain generator"
```

---

## Task 4: Tunnel registry

**Files:**
- Create: `internal/relay/registry.go`
- Test: `internal/relay/registry_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/relay/registry_test.go`:
```go
package relay

import (
	"net"
	"sync"
	"testing"
)

// fakeTunnel is a no-op Tunnel for registry tests.
type fakeTunnel struct{ closed bool }

func (f *fakeTunnel) OpenStream() (net.Conn, error) { return nil, nil }
func (f *fakeTunnel) Close() error                  { f.closed = true; return nil }

func TestRegistryTryAddGetRemove(t *testing.T) {
	r := NewRegistry()
	tun := &fakeTunnel{}

	if !r.TryAdd("happy-fox-0001", tun) {
		t.Fatal("first TryAdd should succeed")
	}
	if r.TryAdd("happy-fox-0001", &fakeTunnel{}) {
		t.Fatal("duplicate TryAdd should fail")
	}

	got, ok := r.Get("happy-fox-0001")
	if !ok || got != tun {
		t.Fatalf("Get = %v, %v; want the original tunnel", got, ok)
	}
	if r.Count() != 1 {
		t.Fatalf("Count = %d, want 1", r.Count())
	}

	r.Remove("happy-fox-0001")
	if _, ok := r.Get("happy-fox-0001"); ok {
		t.Fatal("Get should fail after Remove")
	}
	if r.Count() != 0 {
		t.Fatalf("Count = %d, want 0", r.Count())
	}
}

func TestRegistryConcurrentAdd(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.TryAdd(GenerateName(n), &fakeTunnel{})
		}(i)
	}
	wg.Wait()
	if r.Count() != 50 {
		t.Fatalf("Count = %d, want 50", r.Count())
	}
}

// GenerateName is a deterministic helper for the concurrency test.
func GenerateName(n int) string { return "sub-" + string(rune('a'+n%26)) + "-" + itoa(n) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestRegistry`
Expected: FAIL — undefined `NewRegistry`, `Tunnel`.

- [ ] **Step 3: Implement the registry**

Create `internal/relay/registry.go`:
```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/ -run TestRegistry`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/registry.go internal/relay/registry_test.go
git commit -m "feat: add concurrency-safe tunnel registry"
```

---

## Task 5: Host parsing + request forwarder

**Files:**
- Create: `internal/relay/forwarder.go`
- Test: `internal/relay/forwarder_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/relay/forwarder_test.go`:
```go
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
		{"example.com", "example.com", "", false},          // apex, no subdomain
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestForwarder`
Expected: FAIL — undefined `NewForwarder`, `subdomainFromHost`.

- [ ] **Step 3: Implement the forwarder**

Create `internal/relay/forwarder.go`:
```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/ -run TestForwarder`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/forwarder.go internal/relay/forwarder_test.go
git commit -m "feat: add public request forwarder"
```

---

## Task 6: Control handler (auth + yamux session)

**Files:**
- Create: `internal/relay/control.go`

This task wires the WebSocket control endpoint to yamux. The success path is exercised end-to-end in Task 8's integration test; here we add the handler plus a focused unit test for token rejection.

- [ ] **Step 1: Write the failing test**

Append to `internal/relay/control_test.go` (create the file):
```go
package relay

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mayur-tolexo/tunnl/internal/protocol"
)

func TestControlRejectsBadToken(t *testing.T) {
	reg := NewRegistry()
	ctrl := NewControl(Config{Token: "right", BaseDomain: "example.com", MaxTunnels: 10}, reg)
	srv := httptest.NewServer(ctrl)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusInternalError, "test cleanup")

	out, _ := protocol.Encode(protocol.Message{Type: protocol.TypeRegister, Register: &protocol.Register{Token: "wrong"}})
	if err := conn.Write(ctx, websocket.MessageBinary, out); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	msg, err := protocol.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if msg.Type != protocol.TypeError || msg.Error == nil || msg.Error.Code != "unauthorized" {
		t.Fatalf("got %+v, want unauthorized error", msg)
	}
	if reg.Count() != 0 {
		t.Fatalf("registry should be empty, got %d", reg.Count())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestControl`
Expected: FAIL — undefined `NewControl`, `Config`.

- [ ] **Step 3: Implement the control handler**

Create `internal/relay/control.go`:
```go
package relay

import (
	"context"
	"net"
	"net/http"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"github.com/mayur-tolexo/tunnl/internal/protocol"
)

// Config holds relay settings shared by the control handler and entrypoint.
type Config struct {
	Token      string // shared static auth token
	BaseDomain string // e.g. "example.com"
	MaxTunnels int    // global cap on concurrent tunnels (0 = unlimited)
}

// Control is the WebSocket control handler. Clients connect here to register a
// tunnel; the connection is then promoted to a yamux session.
type Control struct {
	cfg Config
	reg *Registry
}

// NewControl returns a Control handler.
func NewControl(cfg Config, reg *Registry) *Control {
	return &Control{cfg: cfg, reg: reg}
}

func (c *Control) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	// yamux frames can exceed the default 32KiB message read limit.
	conn.SetReadLimit(1 << 30)

	ctx := context.Background()
	_, data, err := conn.Read(ctx)
	if err != nil {
		conn.Close(websocket.StatusProtocolError, "expected register")
		return
	}
	msg, err := protocol.Decode(data)
	if err != nil || msg.Type != protocol.TypeRegister || msg.Register == nil {
		c.writeError(ctx, conn, "bad_request", "expected register message")
		return
	}
	if msg.Register.Token != c.cfg.Token {
		c.writeError(ctx, conn, "unauthorized", "invalid token")
		return
	}
	if c.cfg.MaxTunnels > 0 && c.reg.Count() >= c.cfg.MaxTunnels {
		c.writeError(ctx, conn, "capacity", "tunnel capacity reached")
		return
	}

	sub, ok := c.assignSubdomain()
	if !ok {
		c.writeError(ctx, conn, "internal", "could not assign subdomain")
		return
	}

	out, _ := protocol.Encode(protocol.Message{
		Type:       protocol.TypeRegistered,
		Registered: &protocol.Registered{URL: "https://" + sub + "." + c.cfg.BaseDomain, Subdomain: sub},
	})
	if err := conn.Write(ctx, websocket.MessageBinary, out); err != nil {
		c.reg.Remove(sub)
		return
	}

	netConn := websocket.NetConn(ctx, conn, websocket.MessageBinary)
	sess, err := yamux.Client(netConn, yamuxConfig())
	if err != nil {
		c.reg.Remove(sub)
		conn.Close(websocket.StatusInternalError, "yamux setup failed")
		return
	}
	// Replace the placeholder reservation with the real yamux-backed tunnel.
	c.reg.Remove(sub)
	if !c.reg.TryAdd(sub, &yamuxTunnel{sess: sess}) {
		sess.Close()
		return
	}
	defer c.reg.Remove(sub)

	<-sess.CloseChan() // blocks until the session dies (yamux keepalive detects this)
}

// assignSubdomain reserves a free subdomain, retrying on collision.
func (c *Control) assignSubdomain() (string, bool) {
	for i := 0; i < 10; i++ {
		s, err := GenerateSubdomain()
		if err != nil {
			return "", false
		}
		if c.reg.TryAdd(s, reservation{}) {
			return s, true
		}
	}
	return "", false
}

func (c *Control) writeError(ctx context.Context, conn *websocket.Conn, code, message string) {
	out, _ := protocol.Encode(protocol.Message{Type: protocol.TypeError, Error: &protocol.Error{Code: code, Message: message}})
	_ = conn.Write(ctx, websocket.MessageBinary, out)
	conn.Close(websocket.StatusPolicyViolation, message)
}

// reservation is a placeholder Tunnel used to atomically claim a subdomain
// before the real yamux session is ready.
type reservation struct{}

func (reservation) OpenStream() (net.Conn, error) { return nil, net.ErrClosed }
func (reservation) Close() error                  { return nil }

// yamuxTunnel adapts a yamux session to the Tunnel interface.
type yamuxTunnel struct{ sess *yamux.Session }

func (y *yamuxTunnel) OpenStream() (net.Conn, error) { return y.sess.Open() }
func (y *yamuxTunnel) Close() error                  { return y.sess.Close() }

func yamuxConfig() *yamux.Config {
	// DefaultConfig enables keepalive, which is how the relay detects a dead
	// client and triggers registry cleanup (via Session.CloseChan).
	return yamux.DefaultConfig()
}
```

Note: the `yamuxConfig` body intentionally returns `yamux.DefaultConfig()` (keepalive enabled). Keeping the function lets later tasks customize logging without touching callers.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/ -run TestControl`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/control.go internal/relay/control_test.go
git commit -m "feat: add relay control handler with token auth"
```

---

## Task 7: Client (dial, proxy, reconnect)

**Files:**
- Create: `internal/client/client.go`
- Test: `internal/client/client_test.go`

- [ ] **Step 1: Write the failing test for stream handling**

Create `internal/client/client_test.go`:
```go
package client

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestHandleStreamProxiesToLocal(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "echo %s", r.URL.Path)
	}))
	defer local.Close()

	port := portOf(t, local.URL)

	relayEnd, clientEnd := net.Pipe()
	go handleStream(clientEnd, port)

	// Play the relay: write a request, read the response.
	req, _ := http.NewRequest("GET", "http://placeholder/widgets", nil)
	if err := req.Write(relayEnd); err != nil {
		t.Fatalf("write request: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(relayEnd), req)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "echo /widgets" {
		t.Fatalf("body = %q, want %q", body, "echo /widgets")
	}
}

func TestHandleStreamLocalUnreachableReturns502(t *testing.T) {
	relayEnd, clientEnd := net.Pipe()
	go handleStream(clientEnd, 1) // port 1: nothing listening

	req, _ := http.NewRequest("GET", "http://placeholder/", nil)
	_ = req.Write(relayEnd)
	resp, err := http.ReadResponse(bufio.NewReader(relayEnd), req)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}

func portOf(t *testing.T, rawURL string) int {
	t.Helper()
	h := strings.TrimPrefix(rawURL, "http://")
	_, p, err := net.SplitHostPort(h)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	n, _ := strconv.Atoi(p)
	return n
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run TestHandleStream`
Expected: FAIL — undefined `handleStream`.

- [ ] **Step 3: Implement the client**

Create `internal/client/client.go`:
```go
package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"github.com/mayur-tolexo/tunnl/internal/protocol"
)

// ErrUnauthorized is returned when the relay rejects the token. It is fatal:
// the reconnect loop stops.
var ErrUnauthorized = errors.New("relay rejected token")

// Config configures a client run.
type Config struct {
	RelayURL  string // e.g. "wss://tunnl.example.com/tunnel"
	Token     string
	LocalPort int
}

// Run connects to the relay and serves forwarded requests until ctx is
// cancelled, reconnecting with backoff on transient failures.
func Run(ctx context.Context, cfg Config) error {
	backoff := time.Second
	for {
		err := connectOnce(ctx, cfg)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, ErrUnauthorized) {
			return err
		}
		fmt.Printf("tunnl: disconnected (%v); reconnecting in %s\n", err, backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func connectOnce(ctx context.Context, cfg Config) error {
	conn, _, err := websocket.Dial(ctx, cfg.RelayURL, nil)
	if err != nil {
		return fmt.Errorf("dial relay: %w", err)
	}
	conn.SetReadLimit(1 << 30)
	defer conn.Close(websocket.StatusNormalClosure, "")

	reg, _ := protocol.Encode(protocol.Message{
		Type:     protocol.TypeRegister,
		Register: &protocol.Register{Token: cfg.Token, Target: fmt.Sprintf("http://localhost:%d", cfg.LocalPort)},
	})
	if err := conn.Write(ctx, websocket.MessageBinary, reg); err != nil {
		return fmt.Errorf("send register: %w", err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("read reply: %w", err)
	}
	msg, err := protocol.Decode(data)
	if err != nil {
		return fmt.Errorf("decode reply: %w", err)
	}
	switch msg.Type {
	case protocol.TypeError:
		if msg.Error != nil && msg.Error.Code == "unauthorized" {
			return ErrUnauthorized
		}
		return fmt.Errorf("relay error: %v", msg.Error)
	case protocol.TypeRegistered:
		fmt.Printf("tunnl: %s -> http://localhost:%d\n", msg.Registered.URL, cfg.LocalPort)
	default:
		return fmt.Errorf("unexpected reply type %q", msg.Type)
	}

	netConn := websocket.NetConn(ctx, conn, websocket.MessageBinary)
	sess, err := yamux.Server(netConn, yamux.DefaultConfig())
	if err != nil {
		return fmt.Errorf("yamux setup: %w", err)
	}
	defer sess.Close()

	for {
		stream, err := sess.Accept()
		if err != nil {
			return fmt.Errorf("accept stream: %w", err)
		}
		go handleStream(stream, cfg.LocalPort)
	}
}

// handleStream reads one forwarded HTTP request, proxies it to the local
// service, and writes the response back over the stream.
func handleStream(stream net.Conn, localPort int) {
	defer stream.Close()
	req, err := http.ReadRequest(bufio.NewReader(stream))
	if err != nil {
		return
	}
	req.URL.Scheme = "http"
	req.URL.Host = fmt.Sprintf("localhost:%d", localPort)
	req.RequestURI = "" // must be cleared for client-side requests

	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		writeBadGateway(stream)
		return
	}
	defer resp.Body.Close()
	_ = resp.Write(stream)
}

func writeBadGateway(w io.Writer) {
	resp := &http.Response{
		StatusCode:    http.StatusBadGateway,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": {"text/plain"}},
		Body:          io.NopCloser(strings.NewReader("tunnl: local service unreachable\n")),
		ContentLength: -1,
	}
	_ = resp.Write(w)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/ -run TestHandleStream`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/
git commit -m "feat: add tunnel client with reconnect"
```

---

## Task 8: End-to-end integration test

This is the primary safety net for the WebSocket+yamux transport. It wires a real relay control handler and forwarder behind one `httptest` server, runs the real client against it, and asserts a request reaches a fake local service.

**Files:**
- Create: `internal/relay/integration_test.go`

- [ ] **Step 1: Write the integration test**

Create `internal/relay/integration_test.go`:
```go
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
```

The test needs a way to read the assigned subdomain from the registry. Add a small test-support method.

- [ ] **Step 2: Add the `AnySubdomain` helper to the registry**

In `internal/relay/registry.go`, add:
```go
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
```

- [ ] **Step 3: Run the integration test**

Run: `go test ./internal/relay/ -run TestEndToEndTunnel -v`
Expected: PASS. If it hangs or fails on framing, the likely cause is the WebSocket read limit (must be raised before wrapping in yamux — already done in `control.go` and `client.go`).

- [ ] **Step 4: Run the full suite**

Run: `go test ./...`
Expected: all packages PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/integration_test.go internal/relay/registry.go
git commit -m "test: add end-to-end tunnel integration test"
```

---

## Task 9: Relay entrypoint (`cmd/tunnld`)

**Files:**
- Create: `cmd/tunnld/main.go`
- Create: `cmd/tunnld/main_test.go`

- [ ] **Step 1: Write the failing test for host routing**

Create `cmd/tunnld/main_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tunnld/`
Expected: FAIL — undefined `newRouter`.

- [ ] **Step 3: Implement the entrypoint**

Create `cmd/tunnld/main.go`:
```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mayur-tolexo/tunnl/internal/relay"
)

func main() {
	cfg := relay.Config{
		Token:      mustEnv("TUNNL_TOKEN"),
		BaseDomain: mustEnv("TUNNL_DOMAIN"),
		MaxTunnels: envInt("TUNNL_MAX_TUNNELS", 100),
	}
	email := mustEnv("TUNNL_ACME_EMAIL")
	gdKey := mustEnv("TUNNL_GODADDY_KEY")
	gdSecret := mustEnv("TUNNL_GODADDY_SECRET")
	staging := os.Getenv("TUNNL_ACME_STAGING") == "1"

	reg := relay.NewRegistry()
	control := relay.NewControl(cfg, reg)
	forwarder := relay.NewForwarder(reg, cfg.BaseDomain)
	router := newRouter(control, forwarder, "tunnl."+cfg.BaseDomain)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tlsCfg, err := relay.TLSConfig(ctx, cfg.BaseDomain, email, gdKey, gdSecret, staging)
	if err != nil {
		log.Fatalf("tunnld: TLS setup failed: %v", err)
	}

	httpsSrv := &http.Server{Addr: ":443", Handler: router, TLSConfig: tlsCfg}
	httpSrv := &http.Server{Addr: ":80", Handler: http.HandlerFunc(redirectToHTTPS)}

	go func() {
		log.Println("tunnld: :80 redirect listening")
		_ = httpSrv.ListenAndServe()
	}()
	go func() {
		log.Println("tunnld: :443 listening")
		if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Fatalf("tunnld: https server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("tunnld: shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpsSrv.Shutdown(shutCtx)
	_ = httpSrv.Shutdown(shutCtx)
}

// newRouter dispatches control-host traffic to control and everything else to
// the forwarder.
func newRouter(control, forwarder http.Handler, controlHost string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if i := strings.IndexByte(host, ':'); i >= 0 {
			host = host[:i]
		}
		if host == controlHost {
			control.ServeHTTP(w, r)
			return
		}
		forwarder.ServeHTTP(w, r)
	})
}

func redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusMovedPermanently)
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("tunnld: required env %s is not set", key)
	}
	return v
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
```

This references `relay.TLSConfig`, implemented in Task 10. It will not compile until then.

- [ ] **Step 4: Defer running until Task 10**

Run: `go vet ./cmd/tunnld/` — expect a compile error about `relay.TLSConfig`. That is expected; Task 10 adds it. Do not commit a broken build.

- [ ] **Step 5: Commit after Task 10**

(Commit instructions are at the end of Task 10, once the build is green.)

---

## Task 10: Wildcard TLS via certmagic + GoDaddy

This is the gated, externally-dependent task. Per the spec (Section 7), validate the GoDaddy API token **first**; only fall back to acme-dns/Cloudflare if it is blocked. Because issuance hits Let's Encrypt, this task is verified manually against the **staging** CA, not by unit tests.

**Files:**
- Create: `internal/relay/tls.go`

- [ ] **Step 1: Implement the TLS config**

Create `internal/relay/tls.go`:
```go
package relay

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/godaddy"
)

// TLSConfig obtains (or loads from cache) a wildcard certificate for
// *.baseDomain and baseDomain via the Let's Encrypt DNS-01 challenge, solved
// through GoDaddy. The returned *tls.Config serves every subdomain from the one
// wildcard cert.
func TLSConfig(ctx context.Context, baseDomain, email, gdKey, gdSecret string, staging bool) (*tls.Config, error) {
	certmagic.DefaultACME.Agreed = true
	certmagic.DefaultACME.Email = email
	if staging {
		certmagic.DefaultACME.CA = certmagic.LetsEncryptStagingCA
	} else {
		certmagic.DefaultACME.CA = certmagic.LetsEncryptProductionCA
	}
	certmagic.DefaultACME.DNS01Solver = &certmagic.DNS01Solver{
		DNSManager: certmagic.DNSManager{
			DNSProvider: &godaddy.Provider{APIToken: gdKey + ":" + gdSecret},
		},
	}

	magic := certmagic.NewDefault()
	domains := []string{"*." + baseDomain, baseDomain}
	if err := magic.ManageSync(ctx, domains); err != nil {
		return nil, fmt.Errorf("obtain wildcard cert for %v: %w", domains, err)
	}

	tlsCfg := magic.TLSConfig()
	// Ensure plain HTTPS negotiates alongside ACME-TLS.
	tlsCfg.NextProtos = append(tlsCfg.NextProtos, "h2", "http/1.1")
	return tlsCfg, nil
}
```

> **API verification (required before relying on this):** the certmagic and
> libdns/godaddy APIs change between versions. After `go get`, confirm the field
> names against the installed version:
> ```bash
> go doc github.com/caddyserver/certmagic.DNS01Solver
> go doc github.com/caddyserver/certmagic.DNSManager
> go doc github.com/libdns/godaddy.Provider
> ```
> If `DNSManager` does not exist in the installed certmagic version, the older
> API used `DNS01Solver{DNSProvider: ...}` directly — adjust accordingly. If
> `godaddy.Provider` uses separate `APIKey`/`APISecret` fields instead of a
> combined `APIToken`, set those instead.

- [ ] **Step 2: Build the whole module**

Run: `go build ./...`
Expected: success (this unblocks `cmd/tunnld` from Task 9).

- [ ] **Step 3: Validate the GoDaddy token against ACME staging**

Set real values and run the relay against the **staging** CA so a failed GoDaddy
API does not burn production rate limits:
```bash
export TUNNL_TOKEN=devtoken
export TUNNL_DOMAIN=<your-domain>
export TUNNL_ACME_EMAIL=<you@example.com>
export TUNNL_GODADDY_KEY=<key>
export TUNNL_GODADDY_SECRET=<secret>
export TUNNL_ACME_STAGING=1
sudo -E go run ./cmd/tunnld
```
Expected: logs show a DNS-01 challenge being solved and a staging certificate
obtained, then ":443 listening".

**If GoDaddy returns 403/authorization errors** (the 2024 API restriction):
stop here and switch to a spec fallback (acme-dns delegation, recommended) before
continuing. Re-run this step until a staging cert is obtained.

- [ ] **Step 4: Switch to production and confirm**

Unset staging and run once more to obtain a real cert:
```bash
unset TUNNL_ACME_STAGING
sudo -E go run ./cmd/tunnld
```
Expected: ":443 listening" with a valid Let's Encrypt certificate (verify with
`curl -sSf https://<your-domain> -o /dev/null` or a browser).

- [ ] **Step 5: Commit**

```bash
git add internal/relay/tls.go cmd/tunnld/
git commit -m "feat: add relay entrypoint and wildcard DNS-01 TLS"
```

---

## Task 11: Client CLI (`cmd/tunnl`)

**Files:**
- Create: `cmd/tunnl/main.go`
- Create: `cmd/tunnl/main_test.go`

- [ ] **Step 1: Write the failing test for argument parsing**

Create `cmd/tunnl/main_test.go`:
```go
package main

import "testing"

func TestParseArgs(t *testing.T) {
	cfg, err := parseArgs([]string{"http", "3000"}, "wss://tunnl.example.com/tunnel", "secret")
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if cfg.LocalPort != 3000 {
		t.Fatalf("LocalPort = %d, want 3000", cfg.LocalPort)
	}
	if cfg.RelayURL != "wss://tunnl.example.com/tunnel" || cfg.Token != "secret" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestParseArgsRejectsBadPort(t *testing.T) {
	if _, err := parseArgs([]string{"http", "notaport"}, "wss://x/tunnel", "t"); err == nil {
		t.Fatal("expected error for non-numeric port")
	}
}

func TestParseArgsRequiresHTTPSubcommand(t *testing.T) {
	if _, err := parseArgs([]string{"ftp", "21"}, "wss://x/tunnel", "t"); err == nil {
		t.Fatal("expected error for unsupported subcommand")
	}
}

func TestParseArgsRequiresRelayAndToken(t *testing.T) {
	if _, err := parseArgs([]string{"http", "3000"}, "", "secret"); err == nil {
		t.Fatal("expected error for missing relay URL")
	}
	if _, err := parseArgs([]string{"http", "3000"}, "wss://x/tunnel", ""); err == nil {
		t.Fatal("expected error for missing token")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tunnl/`
Expected: FAIL — undefined `parseArgs`.

- [ ] **Step 3: Implement the CLI**

Create `cmd/tunnl/main.go`:
```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/mayur-tolexo/tunnl/internal/client"
)

func main() {
	relayURL := os.Getenv("TUNNL_RELAY")
	token := os.Getenv("TUNNL_TOKEN")

	cfg, err := parseArgs(os.Args[1:], relayURL, token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := client.Run(ctx, cfg); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "tunnl:", err)
		os.Exit(1)
	}
}

const usage = `usage: tunnl http <port>

environment:
  TUNNL_RELAY   relay control URL, e.g. wss://tunnl.example.com/tunnel
  TUNNL_TOKEN   shared auth token`

// parseArgs builds a client.Config from CLI args and environment-sourced relay
// URL and token.
func parseArgs(args []string, relayURL, token string) (client.Config, error) {
	if len(args) != 2 || args[0] != "http" {
		return client.Config{}, errors.New("expected: tunnl http <port>")
	}
	port, err := strconv.Atoi(args[1])
	if err != nil || port < 1 || port > 65535 {
		return client.Config{}, fmt.Errorf("invalid port %q", args[1])
	}
	if relayURL == "" {
		return client.Config{}, errors.New("relay URL not set (TUNNL_RELAY)")
	}
	if token == "" {
		return client.Config{}, errors.New("token not set (TUNNL_TOKEN)")
	}
	return client.Config{RelayURL: relayURL, Token: token, LocalPort: port}, nil
}
```

- [ ] **Step 4: Run tests and full build**

Run: `go test ./cmd/tunnl/ && go build ./...`
Expected: PASS and a clean build.

- [ ] **Step 5: Commit**

```bash
git add cmd/tunnl/
git commit -m "feat: add tunnl client CLI"
```

---

## Task 12 (optional / best-effort): WebSocket passthrough

The spec lists tunneled-app WebSocket passthrough as best-effort. The HTTP path
above does not handle `Upgrade` requests. Implement this only after Tasks 1–11
are solid; it can also be deferred to a follow-up. It requires hijacking the
visitor connection on the relay and bidirectional copying on both ends.

**Files:**
- Modify: `internal/relay/forwarder.go`
- Modify: `internal/client/client.go`

- [ ] **Step 1: Detect upgrade requests in the forwarder and hijack**

In `Forwarder.ServeHTTP`, before the normal response path, branch on
`r.Header.Get("Upgrade") != ""`:
```go
if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
	f.forwardUpgrade(w, r, stream)
	return
}
```
Add:
```go
func (f *Forwarder) forwardUpgrade(w http.ResponseWriter, r *http.Request, stream net.Conn) {
	if err := r.Write(stream); err != nil {
		http.Error(w, "tunnl: forward failed", http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "tunnl: cannot hijack", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	defer clientConn.Close()
	// Bidirectional copy: visitor <-> stream.
	go func() { _, _ = io.Copy(stream, clientConn) }()
	_, _ = io.Copy(clientConn, stream)
}
```
Add `"net"` to the imports.

- [ ] **Step 2: Handle the upgrade response on the client**

In `handleStream`, after reading the request, if it is an upgrade request, dial
the local service raw and copy bidirectionally instead of using `RoundTrip`:
```go
if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
	local, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", localPort))
	if err != nil {
		writeBadGateway(stream)
		return
	}
	defer local.Close()
	if err := req.Write(local); err != nil {
		return
	}
	go func() { _, _ = io.Copy(local, stream) }()
	_, _ = io.Copy(stream, local)
	return
}
```

- [ ] **Step 3: Manual verification**

Run a local WebSocket echo server (e.g. `websocat -s 3000`) behind the tunnel
and connect a WebSocket client to the public URL. Confirm messages echo. There
is no automated test for this best-effort path in the MVP.

- [ ] **Step 4: Commit**

```bash
git add internal/relay/forwarder.go internal/client/client.go
git commit -m "feat: best-effort websocket passthrough"
```

---

## Task 13: README and deployment notes

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Rewrite the README**

Replace `README.md` with concise usage and deployment instructions:
```markdown
# tunnl

Expose a website running on your `localhost` at a public HTTPS URL.

## Client

    export TUNNL_RELAY=wss://tunnl.<domain>/tunnel
    export TUNNL_TOKEN=<shared-token>
    tunnl http 3000

Prints a URL like `https://happy-fox-0042.<domain>` that forwards to
`http://localhost:3000`.

## Relay (`tunnld`)

Runs on a public host with `*.<domain>` and `<domain>` DNS pointed at it.

Required environment:

| Variable | Purpose |
|----------|---------|
| `TUNNL_TOKEN` | shared auth token clients must present |
| `TUNNL_DOMAIN` | base domain, e.g. `example.com` |
| `TUNNL_ACME_EMAIL` | Let's Encrypt account email |
| `TUNNL_GODADDY_KEY` / `TUNNL_GODADDY_SECRET` | GoDaddy API credentials for DNS-01 |
| `TUNNL_MAX_TUNNELS` | optional, default 100 |
| `TUNNL_ACME_STAGING` | set to `1` to use the Let's Encrypt staging CA |

    sudo -E go run ./cmd/tunnld   # binds :80 and :443

The relay obtains a single wildcard certificate for `*.<domain>`; every
subdomain is served from it.

## Architecture

See `docs/design/2026-05-23-tunnl-mvp-design.md`.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: usage and deployment instructions"
```

---

## Final verification

- [ ] Run the full suite: `go test ./...` — all PASS.
- [ ] Run `go vet ./...` — clean.
- [ ] Manually confirm an end-to-end tunnel against the real relay (Task 10 cert + a client) serves a localhost site over HTTPS.

---

## Deferred (future specs, per design Section 12)

Accounts/multi-user tokens, dashboard, scaled rate-limiting, abuse/phishing
scanning, custom/reserved subdomains, persistence across restarts, raw TCP
tunnels, multi-region relays, metrics/billing.
