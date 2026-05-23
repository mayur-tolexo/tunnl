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
