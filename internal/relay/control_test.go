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

func TestControlPublicURL(t *testing.T) {
	dev := NewControl(Config{BaseDomain: "localhost", PublicScheme: "http", PublicHostSuffix: ":8080"}, NewRegistry())
	if got := dev.publicURL("happy-fox-0001"); got != "http://happy-fox-0001.localhost:8080" {
		t.Fatalf("dev publicURL = %q", got)
	}
	// Empty PublicScheme defaults to https with no port suffix (production).
	prod := NewControl(Config{BaseDomain: "shoplit.in"}, NewRegistry())
	if got := prod.publicURL("happy-fox-0001"); got != "https://happy-fox-0001.shoplit.in" {
		t.Fatalf("prod publicURL = %q", got)
	}
}
