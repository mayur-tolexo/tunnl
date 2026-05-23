package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/acmedns"
	"github.com/libdns/godaddy"
	"github.com/mayur-tolexo/tunnl/internal/relay"
)

func main() {
	cfg := relay.Config{
		Token:        mustEnv("TUNNL_TOKEN"),
		BaseDomain:   mustEnv("TUNNL_DOMAIN"),
		MaxTunnels:   envInt("TUNNL_MAX_TUNNELS", 100),
		PublicScheme: "https",
	}
	reg := relay.NewRegistry()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Local dev mode: serve plain HTTP and skip ACME/GoDaddy entirely.
	// Set TUNNL_HTTP_ADDR (e.g. ":8080") to enable.
	if devAddr := os.Getenv("TUNNL_HTTP_ADDR"); devAddr != "" {
		runDev(ctx, cfg, reg, devAddr)
		return
	}

	email := mustEnv("TUNNL_ACME_EMAIL")
	staging := os.Getenv("TUNNL_ACME_STAGING") == "1"
	dns, err := dnsProviderFromEnv()
	if err != nil {
		log.Fatalf("tunnld: DNS provider config: %v", err)
	}

	control := relay.NewControl(cfg, reg)
	forwarder := relay.NewForwarder(reg, cfg.BaseDomain)
	router := newRouter(control, forwarder, "tunnl."+cfg.BaseDomain)

	tlsCfg, err := relay.TLSConfig(ctx, cfg.BaseDomain, email, staging, dns)
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
	reg.CloseAll()
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpsSrv.Shutdown(shutCtx)
	_ = httpSrv.Shutdown(shutCtx)
}

// runDev serves the relay over plain HTTP for local testing, skipping ACME/TLS.
// Public URLs are advertised as http://<sub>.<domain><:port>.
func runDev(ctx context.Context, cfg relay.Config, reg *relay.Registry, addr string) {
	cfg.PublicScheme = "http"
	cfg.PublicHostSuffix = portSuffix(addr)

	control := relay.NewControl(cfg, reg)
	forwarder := relay.NewForwarder(reg, cfg.BaseDomain)
	router := newRouter(control, forwarder, "tunnl."+cfg.BaseDomain)

	srv := &http.Server{Addr: addr, Handler: router}
	go func() {
		log.Printf("tunnld: DEV mode (plain HTTP) on %s — public URLs http://<sub>.%s%s", addr, cfg.BaseDomain, cfg.PublicHostSuffix)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("tunnld: http server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("tunnld: shutting down")
	reg.CloseAll()
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}

// portSuffix returns ":port" from a listen address like ":8080" or
// "127.0.0.1:8080", or "" when no port is present.
func portSuffix(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return ""
	}
	return ":" + port
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

// dnsProviderFromEnv builds the libdns DNS provider used to solve the ACME
// DNS-01 challenge, selected by TUNNL_DNS_PROVIDER (default "godaddy"). Use
// "acmedns" to delegate the challenge to a self-hosted acme-dns server when the
// registrar's API cannot write records (e.g. GoDaddy's gated Domains API).
func dnsProviderFromEnv() (certmagic.DNSProvider, error) {
	switch kind := envOr("TUNNL_DNS_PROVIDER", "godaddy"); kind {
	case "godaddy":
		key, secret := os.Getenv("TUNNL_GODADDY_KEY"), os.Getenv("TUNNL_GODADDY_SECRET")
		if key == "" || secret == "" {
			return nil, fmt.Errorf("godaddy provider requires TUNNL_GODADDY_KEY and TUNNL_GODADDY_SECRET")
		}
		return &godaddy.Provider{APIToken: key + ":" + secret}, nil
	case "acmedns":
		p := &acmedns.Provider{
			ServerURL: os.Getenv("TUNNL_ACMEDNS_SERVER"),
			Username:  os.Getenv("TUNNL_ACMEDNS_USERNAME"),
			Password:  os.Getenv("TUNNL_ACMEDNS_PASSWORD"),
			Subdomain: os.Getenv("TUNNL_ACMEDNS_SUBDOMAIN"),
		}
		if p.ServerURL == "" || p.Username == "" || p.Password == "" || p.Subdomain == "" {
			return nil, fmt.Errorf("acmedns provider requires TUNNL_ACMEDNS_SERVER, TUNNL_ACMEDNS_USERNAME, TUNNL_ACMEDNS_PASSWORD and TUNNL_ACMEDNS_SUBDOMAIN")
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unknown TUNNL_DNS_PROVIDER %q (want \"godaddy\" or \"acmedns\")", kind)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("tunnld: %s=%q is not an integer, using default %d", key, v, def)
			return def
		}
		return n
	}
	return def
}
