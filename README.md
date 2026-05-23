# tunnl

Expose a website running on your `localhost` at a public HTTPS URL — like ngrok,
self-hosted on your own domain.

This guide uses the domain **`shoplit.in`**. Public tunnel URLs will look like
`https://happy-fox-0042.shoplit.in`.

## How it works

A small client on your machine opens one outbound WebSocket to a relay you run
on a public host. The relay terminates HTTPS with a wildcard certificate and
forwards each inbound request down the tunnel to your `localhost`. No inbound
ports, works behind NAT/firewalls.

```
visitor ──HTTPS──▶ tunnl.shoplit.in relay ──WSS/yamux──▶ tunnl client ──HTTP──▶ localhost:3000
```

## Build

```sh
make build          # compiles ./bin/tunnl (client) and ./bin/tunnld (relay)
```

Or put both binaries on your `PATH`:

```sh
make install        # go install into $GOBIN (e.g. ~/go/bin)
```

Run `make help` to list all targets.

## Client usage

```sh
export TUNNL_RELAY=wss://tunnl.shoplit.in/tunnel
export TUNNL_TOKEN=<shared-token>      # must match the relay's TUNNL_TOKEN

./bin/tunnl http 3000                  # or `tunnl http 3000` after `make install`
```

Or via the Makefile (reads the same env vars):

```sh
make run-client PORT=3000
```

Output:

```
tunnl: https://happy-fox-0042.shoplit.in -> http://localhost:3000
```

Share that URL — requests to it are served by whatever is running on
`http://localhost:3000`. The subdomain is random and lasts for the life of the
client connection.

## Running the relay (`tunnld`)

The relay runs on a public host (a small VPS is plenty) that owns the
`shoplit.in` wildcard.

### 1. DNS

Point these records at the relay's public IP (e.g. `203.0.113.10`):

| Type | Name | Value |
|------|------|-------|
| A | `tunnl.shoplit.in` | `203.0.113.10` |
| A | `*.shoplit.in` | `203.0.113.10` |

`tunnl.shoplit.in` is the reserved control host clients connect to; `*.shoplit.in`
makes every random tunnel subdomain resolve to the relay.

### 2. Environment

| Variable | Purpose |
|----------|---------|
| `TUNNL_TOKEN` | shared auth token clients must present |
| `TUNNL_DOMAIN` | base domain — `shoplit.in` |
| `TUNNL_ACME_EMAIL` | Let's Encrypt account email |
| `TUNNL_GODADDY_KEY` / `TUNNL_GODADDY_SECRET` | GoDaddy API key/secret for the DNS-01 challenge |
| `TUNNL_MAX_TUNNELS` | optional, max concurrent tunnels (default 100) |
| `TUNNL_ACME_STAGING` | set to `1` to use the Let's Encrypt **staging** CA |

### 3. First run — validate certs against staging

The relay obtains one wildcard certificate for `*.shoplit.in` via the Let's
Encrypt DNS-01 challenge, solved through the GoDaddy API. **Always test against
the staging CA first** so a misconfiguration doesn't burn production rate limits:

```sh
export TUNNL_TOKEN=$(openssl rand -hex 16)
export TUNNL_DOMAIN=shoplit.in
export TUNNL_ACME_EMAIL=you@shoplit.in
export TUNNL_GODADDY_KEY=<godaddy-key>
export TUNNL_GODADDY_SECRET=<godaddy-secret>
export TUNNL_ACME_STAGING=1

make run-relay            # builds ./bin/tunnld and runs `sudo -E ./bin/tunnld`
```

`make run-relay` binds :80 (redirect) and :443, so it runs under `sudo` and
inherits the exported env (`sudo -E`).

Watch the logs for a solved DNS-01 challenge and `:443 listening`. Once staging
works, drop `TUNNL_ACME_STAGING` and run again to obtain a real certificate:

```sh
unset TUNNL_ACME_STAGING
make run-relay
```

Verify: `curl -sSf https://tunnl.shoplit.in -o /dev/null` (a 404 with a valid TLS
handshake is fine — it means the cert works).

> **GoDaddy API note:** GoDaddy restricted its Domains API in 2024 (programmatic
> DNS access requires accounts meeting a domain-count/spend threshold). If the
> DNS-01 challenge fails with a 403/authorization error, your key can't write
> records. Fallback: keep GoDaddy as registrar but add a one-time
> `_acme-challenge.shoplit.in` CNAME pointing at a self-hosted
> [acme-dns](https://github.com/joohoi/acme-dns) server, or delegate the zone to
> a provider with an open API. See `docs/design/2026-05-23-tunnl-mvp-design.md` §7.

`sudo` is required to bind ports 80/443 (or grant the binary
`CAP_NET_BIND_SERVICE` / front it with a reverse proxy).

## Architecture

See `docs/design/2026-05-23-tunnl-mvp-design.md` and the implementation plan in
`docs/plans/`.
