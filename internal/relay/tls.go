package relay

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/caddyserver/certmagic"
)

// TLSConfig obtains (or loads from cache) a wildcard certificate for
// *.baseDomain and baseDomain via the Let's Encrypt DNS-01 challenge, solved
// through the supplied libdns DNS provider (e.g. GoDaddy or acme-dns). The
// returned *tls.Config serves every subdomain from the one wildcard cert.
func TLSConfig(ctx context.Context, baseDomain, email string, staging bool, dns certmagic.DNSProvider) (*tls.Config, error) {
	certmagic.DefaultACME.Agreed = true
	certmagic.DefaultACME.Email = email
	if staging {
		certmagic.DefaultACME.CA = certmagic.LetsEncryptStagingCA
	} else {
		certmagic.DefaultACME.CA = certmagic.LetsEncryptProductionCA
	}
	certmagic.DefaultACME.DNS01Solver = &certmagic.DNS01Solver{
		DNSManager: certmagic.DNSManager{DNSProvider: dns},
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
