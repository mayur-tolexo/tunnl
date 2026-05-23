# tunnl

Expose a website running on your `localhost` at a public HTTPS URL.

## Client

    export TUNNL_RELAY=wss://tunnl.<domain>/tunnel
    export TUNNL_TOKEN=<shared-token>
    tunnl http 3000

Prints a URL like `https://happy-fox-0042.<domain>` that forwards to
`http://localhost:3000`.

## Relay (`tunnld`)

Runs on a public host with `*.<domain>` and `<domain>` DNS pointed at it.

Required environment:

| Variable | Purpose |
|----------|---------|
| `TUNNL_TOKEN` | shared auth token clients must present |
| `TUNNL_DOMAIN` | base domain, e.g. `example.com` |
| `TUNNL_ACME_EMAIL` | Let's Encrypt account email |
| `TUNNL_GODADDY_KEY` / `TUNNL_GODADDY_SECRET` | GoDaddy API credentials for DNS-01 |
| `TUNNL_MAX_TUNNELS` | optional, default 100 |
| `TUNNL_ACME_STAGING` | set to `1` to use the Let's Encrypt staging CA |

    sudo -E go run ./cmd/tunnld   # binds :80 and :443

The relay obtains a single wildcard certificate for `*.<domain>`; every
subdomain is served from it.

## Architecture

See `docs/design/2026-05-23-tunnl-mvp-design.md`.
