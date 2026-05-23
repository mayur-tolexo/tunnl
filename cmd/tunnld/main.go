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
	reg.CloseAll()
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
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("tunnld: %s=%q is not an integer, using default %d", key, v, def)
			return def
		}
		return n
	}
	return def
}
