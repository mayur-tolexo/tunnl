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
