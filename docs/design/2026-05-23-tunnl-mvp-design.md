# tunnl — Localhost Tunnel with SSL (MVP Design)

**Date:** 2026-05-23
**Status:** Approved design, pending implementation plan
**Repo:** git@github.com:mayur-tolexo/tunnl.git
**Scope:** Core tunnel MVP only. Open-public-service features (accounts, scaled
rate-limiting, abuse handling) are deliberately deferred to later specs.

## 1. Summary

`tunnl` exposes a website running on a developer's `localhost` to the public
internet over HTTPS, using a reverse-tunnel model (like ngrok / Cloudflare
Tunnel). The site keeps running on the user's machine; a client opens a single
persistent outbound connection to a public relay, and the relay forwards
inbound HTTPS requests down that connection to `localhost`.

Public URLs look like `https://happy-fox-1234.example.com`. TLS is served from a
single wildcard certificate, so every random subdomain works instantly.

## 2. Goals and non-goals

### Goals (MVP)
- Expose `http://localhost:<port>` at a public `https://<slug>.<domain>` URL.
- Work behind NAT/firewalls using only outbound 443.
- Automatic, hands-off TLS via a wildcard Let's Encrypt certificate.
- Single shared static auth token gates who may create tunnels.
- Random, ephemeral subdomains.
- Single relay instance.
- HTTP(S) websites only (including WebSocket passthrough for tunneled apps,
  best-effort).

### Non-goals (deferred to future specs)
- User accounts / multi-user tokens / dashboard.
- Scaled rate-limiting and quota management.
- Abuse / phishing / malware scanning.
- Custom or reserved subdomains.
- Tunnel persistence across relay restarts.
- Raw TCP tunnels (SSH, databases).
- Multi-region or multiple relays.
- Metrics / billing integration.

## 3. Architecture

A single Go module producing two binaries and one shared package.

```
tunnl/
├── cmd/
│   ├── tunnld/             # relay server (public VPS)
│   └── tunnl/              # client CLI (user machine)
├── internal/
│   ├── relay/             # relay logic: TLS frontend, registry, forwarder
│   ├── client/            # client logic: dial, auth, local proxy, reconnect
│   └── protocol/          # shared control messages + framing
└── docs/design/           # this spec and future design docs
```

### Component responsibilities

| Unit | Does | Depends on |
|------|------|------------|
| `tunnld` (relay) | Terminate public TLS, authenticate clients, assign subdomains, forward inbound requests over tunnels | `internal/relay`, `internal/protocol`, certmagic |
| `tunnl` (client) | Connect to relay, authenticate, accept forwarded requests, proxy to localhost | `internal/client`, `internal/protocol`, yamux |
| `internal/protocol` | Define `Register`/`Registered`/error control messages and their encoding | none |
| `internal/relay` | Registry, subdomain generation, request forwarding, lifecycle | `internal/protocol`, yamux |
| `internal/client` | Session setup, local proxying, reconnect/backoff | `internal/protocol`, yamux |

## 4. Relay (`tunnld`) detail

- **Listeners:** :443 (TLS) and :80 (redirect to https). `tunnld` runs on a
  public VPS with the domain's wildcard DNS pointed at it.
- **Traffic split by hostname** on :443:
  - **Control traffic** to a reserved host `tunnl.<domain>`: HTTP→WebSocket
    upgrade at `wss://tunnl.<domain>/tunnel`. Used by clients to register.
  - **Public traffic**: any other `*.<domain>` host is matched to a registered
    tunnel and forwarded.
- **Registry:** in-memory `map[string]*session` (subdomain → session),
  mutex-guarded. Ephemeral; cleared on restart.
- **Subdomain generator:** random friendly slug `adjective-noun-NNNN`,
  collision-checked against the registry; retry on collision.
- **Forwarder:** for each inbound public request, open a new yamux stream to the
  owning client, write the HTTP request, and stream the response back. Supports
  HTTP/1.1 keep-alive and WebSocket upgrade passthrough (best-effort for MVP).

## 5. Client (`tunnl`) detail

- **CLI:** `tunnl http <port>` (e.g. `tunnl http 3000`). Relay URL and token from
  flags (`--relay`, `--token`), environment, or a small config file.
- **Connect:** dial `wss://tunnl.<domain>/tunnel`, send `Register{token}`.
- **On success:** print the assigned `https://<slug>.<domain>` URL.
- **Serve:** wrap the WebSocket connection in a yamux session as the *accepting*
  side. For each stream the relay opens, read the forwarded HTTP request, dial
  `http://localhost:<port>`, and copy the response back over the stream.
- **Resilience:** on disconnect, reconnect with exponential backoff. (Subdomain
  may change on reconnect in MVP, since the registry is ephemeral.)

## 6. Protocol and data flow

Control messages (in `internal/protocol`) sent as JSON frames over the WS
connection during handshake, before the connection is handed to yamux:

- `Register{ token string, target string }` — client → relay.
- `Registered{ url string, subdomain string }` — relay → client.
- `Error{ code string, message string }` — relay → client (e.g. bad token).

End-to-end request flow:

1. Client dials control WSS, sends `Register{token}`.
2. Relay validates the token, generates a subdomain, replies `Registered{url}`.
3. Both sides wrap the WS connection in a **yamux** session — relay opens
   streams, client accepts them.
4. A visitor requests `https://slug.<domain>/path`.
5. Relay terminates TLS, matches `slug` in the registry, opens a yamux stream,
   writes the HTTP request.
6. Client reads the request, proxies it to `http://localhost:<port>`, and writes
   the response back over the stream; relay streams it to the visitor.
7. Periodic ping/heartbeat detects dead tunnels; the registry entry is removed on
   disconnect.

## 7. SSL / certificate management

- **Strategy:** one **wildcard certificate** `*.<domain>` (+ apex) from Let's
  Encrypt via the **DNS-01** challenge. A single cert serves every subdomain,
  with zero per-tunnel ACME calls.
- **Library:** **certmagic** handles issuance, on-disk storage, and automatic
  renewal. The relay loads the cert into `tls.Config` via `GetCertificate`.
- **DNS provider:** GoDaddy.

### GoDaddy API risk (must address during implementation)

GoDaddy **restricted its Domains API in 2024**: programmatic DNS access is now
limited to accounts meeting domain-count / spend thresholds. The libdns/lego
`godaddy` provider may therefore fail to write the `_acme-challenge` TXT record.

**Mitigations, in order of preference:**
1. **`acme-dns` delegation (recommended):** keep GoDaddy as registrar; add a
   one-time `_acme-challenge.<domain>` CNAME pointing to a small self-hosted
   `acme-dns` server. certmagic/lego solve DNS-01 against `acme-dns`, which has a
   simple, reliable API. GoDaddy's restricted API is never touched.
2. **Delegate the zone to Cloudflare:** point the domain's nameservers (or a
   delegated subdomain) at Cloudflare and use the well-supported Cloudflare
   libdns plugin.
3. **Verify the GoDaddy API token works** for this specific account before
   committing to the direct GoDaddy provider.

The implementation plan must validate which path works **before** building cert
automation, since it gates the whole HTTPS story.

## 8. Authentication (MVP)

A single shared **static token**: configured on the relay via env var, supplied
by the client via `--token` / env / config. Mismatch → `Error{code:"unauthorized"}`
and the connection is closed. No accounts, no per-user tokens in MVP.

## 9. Error handling

**Client**
- Local port unreachable → relay returns a friendly 502 to the visitor.
- Token rejected → exit with a clear message.
- Disconnect → reconnect with exponential backoff.

**Relay**
- Unknown subdomain → 404 page.
- Owning client gone mid-request → 502.
- Certificate load/issuance failure → fail fast at startup with a clear error.
- Graceful shutdown drains active sessions.

**Light guardrails (still in MVP, to prevent trivial abuse)**
- Max concurrent tunnels per token.
- Max request body size.
- Idle-tunnel timeout.

## 10. Testing strategy

- **Unit:** protocol encode/decode round-trips; subdomain generator (format +
  collision retry); registry concurrency; request-forwarding logic over an
  in-memory yamux pipe.
- **Integration:** relay + client wired in-process over loopback; hit a fake
  local HTTP server through the tunnel and assert the response. TLS is skipped or
  self-signed in tests; cert automation is not exercised in unit/integration
  tests.
- **Discipline:** TDD throughout implementation (red → green → refactor).

## 11. Open questions / decisions to confirm during planning

- Which GoDaddy mitigation path (Section 7) — confirm before building cert
  automation.
- Config file format/location for the client (e.g. `~/.config/tunnl/config`).
- The `<domain>` to use for the relay's wildcard DNS.

## 12. Future specs (out of scope here)

Each becomes its own brainstorm → spec → plan cycle:
1. Accounts, signup, per-user tokens, dashboard.
2. Scaled rate-limiting and quotas.
3. Abuse / phishing / malware scanning.
4. Custom / reserved subdomains + persistence across restarts.
5. Raw TCP tunnels.
6. Multi-region relays, metrics, billing.
