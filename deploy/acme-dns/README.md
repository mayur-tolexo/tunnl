# Wildcard certs for shoplit.in via acme-dns

GoDaddy restricted its Domains API in 2024, so the relay's default GoDaddy
DNS-01 provider can't write the `_acme-challenge` TXT record (it fails with
`UNABLE_TO_AUTHENTICATE`). This guide gets real `*.shoplit.in` certificates
anyway by delegating **only the ACME challenge** to a self-hosted
[acme-dns](https://github.com/joohoi/acme-dns) server.

You keep GoDaddy as your registrar. The only GoDaddy edits are **one-time** and
done in the web UI (no API): one delegation for the acme-dns zone, and one CNAME
for the challenge. After that, certificate issuance and renewal need no GoDaddy
access at all.

## How it works

```
Let's Encrypt ──TXT? _acme-challenge.shoplit.in──▶ GoDaddy
       │                                              │ CNAME
       │                                              ▼
       └──TXT? <uuid>.auth.shoplit.in──▶ acme-dns (authoritative for auth.shoplit.in)
                                              ▲
                            relay writes the TXT via the acme-dns REST API
```

The relay (`tunnld`) tells acme-dns the challenge token over its local REST API;
acme-dns serves it as a TXT record; Let's Encrypt follows the CNAME and reads it.

## Prerequisites

- A public host (the relay VPS is fine) with **UDP+TCP port 53 open** to the
  internet and Docker installed.
- Its public IP — referred to below as `RELAY_PUBLIC_IP`.
- On Ubuntu/Debian, free port 53 first: `systemd-resolved` usually binds it.
  Set `DNSStubListener=no` in `/etc/systemd/resolved.conf` and
  `sudo systemctl restart systemd-resolved`.

## 1. Configure and start acme-dns

```sh
cd deploy/acme-dns
sed -i 's/RELAY_PUBLIC_IP/203.0.113.10/' config.cfg   # your real IP
docker compose up -d
docker compose logs --no-log-prefix acme-dns | tail
```

## 2. Delegate the acme-dns zone at GoDaddy (web UI)

In the GoDaddy DNS manager for `shoplit.in`, add **two** records:

| Type | Name | Value |
|------|------|-------|
| A | `auth` | `RELAY_PUBLIC_IP` |
| NS | `auth` | `auth.shoplit.in` |

This makes `auth.shoplit.in` resolve to your acme-dns server and delegates the
zone to it. Verify (allow a few minutes for propagation):

```sh
dig +short NS auth.shoplit.in            # -> auth.shoplit.in.
dig +short @RELAY_PUBLIC_IP SOA auth.shoplit.in   # acme-dns answers
```

## 3. Register an acme-dns account

Run on the host (the API is bound to localhost):

```sh
curl -sS -X POST http://127.0.0.1:8081/register | tee account.json
```

You get back `username`, `password`, `subdomain`, and `fulldomain`
(e.g. `d420c923-...auth.shoplit.in`). Keep `account.json` safe.

Optional hardening: set `disable_registration = true` in `config.cfg` and
`docker compose up -d` again so no one else can register.

## 4. Point the challenge CNAME at GoDaddy (web UI)

Add the one-time CNAME using the `fulldomain` from step 3:

| Type | Name | Value |
|------|------|-------|
| CNAME | `_acme-challenge` | `<fulldomain>.` (e.g. `d420c923-...auth.shoplit.in`) |

Verify:

```sh
dig +short CNAME _acme-challenge.shoplit.in   # -> <fulldomain>.
```

## 5. Run the relay against acme-dns

Use the account values from step 3:

```sh
export TUNNL_TOKEN=$(openssl rand -hex 16)
export TUNNL_DOMAIN=shoplit.in
export TUNNL_ACME_EMAIL=you@shoplit.in

export TUNNL_DNS_PROVIDER=acmedns
export TUNNL_ACMEDNS_SERVER=http://127.0.0.1:8081
export TUNNL_ACMEDNS_USERNAME=<username>
export TUNNL_ACMEDNS_PASSWORD=<password>
export TUNNL_ACMEDNS_SUBDOMAIN=<subdomain>

export TUNNL_ACME_STAGING=1     # validate against staging first
make run-relay
```

Watch for a solved `dns-01` challenge and `:443 listening`. Once staging
succeeds, switch to production:

```sh
unset TUNNL_ACME_STAGING
make run-relay
```

Verify the certificate: `curl -sSf https://tunnl.shoplit.in -o /dev/null` (a 404
with a valid TLS handshake is fine).

## 6. Public tunnel DNS (separate from the above)

The acme-dns delegation only handles certificates. For tunnels to resolve, also
point the tunnel hostnames at the relay (as in the top-level README):

| Type | Name | Value |
|------|------|-------|
| A | `tunnl` | `RELAY_PUBLIC_IP` |
| A | `*` | `RELAY_PUBLIC_IP` |

## Notes

- Renewals: fully automatic. certmagic re-runs DNS-01 against acme-dns; no
  GoDaddy access is ever needed again.
- The acme-dns REST API stays on `127.0.0.1`; only port 53 is public.
- `data/` holds the acme-dns SQLite database (registered accounts) — back it up.
