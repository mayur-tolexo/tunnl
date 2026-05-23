# Deploying the tunnl relay on a VPS (shoplit.in)

Goal: run `tunnld` on a cheap public host so `https://<sub>.shoplit.in` URLs
forward to your laptop. Roughly 20–30 minutes.

## 0. Pick a host (cheapest options)

The relay is tiny — the smallest instance anywhere is plenty (≥512 MB RAM).

| Provider | Plan | Price | Notes |
|----------|------|-------|-------|
| **Oracle Cloud** | Always Free (Ampere ARM or AMD micro) | **$0** | Genuinely free forever; sign-up is fussy and ARM stock varies. Pick `arm64` when building. |
| **Hetzner** | CX22 (amd64) / CAX11 (arm64) | ~€3.8/mo | Best value + reliability. Recommended if you'd rather just pay. |
| **DigitalOcean / Vultr / Linode** | basic droplet | $4–6/mo | Easiest dashboards; lots of regions. |

Use **Ubuntu 24.04 LTS**. Note the architecture (amd64 vs arm64) — you need it
when cross-compiling in step 3. Add your SSH key during creation.

## 1. Open the firewall

The relay needs 80, 443, and (for acme-dns) 53. On the host:

```sh
ssh root@X.X.X.X
ufw allow 22,80,443/tcp
ufw allow 53
ufw --force enable
```

Also open those ports in any provider-side cloud firewall (DigitalOcean,
Oracle, Vultr have one; Hetzner's is optional and off by default).

Free port 53 from systemd-resolved (Ubuntu binds it by default):

```sh
sed -i 's/^#\?DNSStubListener=.*/DNSStubListener=no/' /etc/systemd/resolved.conf
systemctl restart systemd-resolved
```

## 2. Point shoplit.in at the host — GoDaddy DNS (web UI)

Replace `X.X.X.X` with the VPS public IP.

| Type | Name | Value | For |
|------|------|-------|-----|
| A | `tunnl` | `X.X.X.X` | control host the client dials |
| A | `*` | `X.X.X.X` | every tunnel subdomain |
| A | `auth` | `X.X.X.X` | acme-dns server |
| NS | `auth` | `auth.shoplit.in` | delegate the acme-dns zone |

(The `_acme-challenge` CNAME comes later, in step 4.)

## 3. Build the relay and copy it up (from your Mac)

No need for Go on the VPS — cross-compile a static binary. Match the VPS arch:

```sh
cd ~/Documents/projects/go/src/tunnl
# amd64 host (Hetzner CX22, most DO/Vultr):
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o tunnld-linux ./cmd/tunnld
# arm64 host (Oracle Ampere, Hetzner CAX11): GOARCH=arm64

scp tunnld-linux root@X.X.X.X:/usr/local/bin/tunnld
scp -r deploy root@X.X.X.X:/root/tunnl-deploy
ssh root@X.X.X.X 'chmod +x /usr/local/bin/tunnld'
```

## 4. Set up acme-dns + the challenge CNAME

acme-dns gives you real `*.shoplit.in` certs without GoDaddy API access. Follow
**[acme-dns/README.md](acme-dns/README.md)** on the VPS — it covers: edit
`config.cfg` with your IP, `docker compose up -d`, `POST /register`, and adding
the one-time `_acme-challenge` CNAME at GoDaddy. Install Docker first if needed:
`curl -fsSL https://get.docker.com | sh`.

Keep the `username` / `password` / `subdomain` from registration for step 5.

## 5. Configure and start the relay (systemd)

```sh
ssh root@X.X.X.X
mkdir -p /etc/tunnl
cp /root/tunnl-deploy/tunnld.env.example /etc/tunnl/tunnld.env
nano /etc/tunnl/tunnld.env      # fill TUNNL_TOKEN + the TUNNL_ACMEDNS_* values
chmod 600 /etc/tunnl/tunnld.env

cp /root/tunnl-deploy/tunnld.service /etc/systemd/system/tunnld.service
systemctl daemon-reload
systemctl enable --now tunnld
journalctl -u tunnld -f         # watch: dns-01 solved, ":443 listening"
```

`tunnld.env` ships with `TUNNL_ACME_STAGING=1` — confirm a **staging** cert is
obtained first. Then comment that line out and `systemctl restart tunnld` to get
a real Let's Encrypt cert.

Verify: `curl -sSf https://tunnl.shoplit.in -o /dev/null` (a 404 with a valid TLS
handshake means the cert works).

## 6. Connect from your Mac

```sh
export TUNNL_RELAY=wss://tunnl.shoplit.in/tunnel
export TUNNL_TOKEN=<the same TUNNL_TOKEN from tunnld.env>
tunnl http 8080        # -> https://happy-fox-1234.shoplit.in
```

## Updating later

Rebuild and copy the binary, then restart:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o tunnld-linux ./cmd/tunnld
scp tunnld-linux root@X.X.X.X:/usr/local/bin/tunnld
ssh root@X.X.X.X 'systemctl restart tunnld'
```
