package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
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
	req.Host = ""       // clear so local service receives localhost:<port>, not the public subdomain
	req.RequestURI = "" // must be cleared for client-side requests

	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		writeBadGateway(stream)
		return
	}
	defer resp.Body.Close()
	if err := resp.Write(stream); err != nil {
		log.Printf("tunnl: write response to relay stream: %v", err)
	}
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
