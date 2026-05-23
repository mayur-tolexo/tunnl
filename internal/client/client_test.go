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
