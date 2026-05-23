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
