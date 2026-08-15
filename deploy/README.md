# Deploying the coverage dashboard (`sccov`)

A self-hosted, always-current version of the capture-coverage dashboard, served
by `sccov` — a single static Go binary (pure standard library, no dependencies).
It reads the aggregate coverage data and renders the dashboard live; it **never**
serves capture contents (no pcapng, no session tokens, no credentials).

Target: **Vultr VPS, Ubuntu 26**, TLS for **starkonflict.com**, deployed by
**GitHub Actions** on every push to `main`.

```
 push to main ─▶ Actions: go build (static) ─▶ scp binary+data ─▶ ssh install.sh ─▶ systemctl restart sccov
                                                                                          │
 browser ─▶ :443 Caddy (auto Let's Encrypt) ─▶ 127.0.0.1:8080 sccov ◀── /var/lib/sccov/*.json
```

## What's in this directory

| File | Role |
|---|---|
| `sccov.service` | systemd unit — runs `sccov` as an unprivileged user on `127.0.0.1:8080`, hardened |
| `Caddyfile` | reverse proxy that terminates TLS for the domain and forwards to `sccov` |
| `install.sh` | server-side installer run by CI over SSH (idempotent) |
| `data/coverage.json` | the coverage snapshot the server serves (non-sensitive) |
| `data/bundles-summary.json` | sanitized per-session stats for the table (non-sensitive) |
| `../.github/workflows/deploy.yml` | the CI/CD pipeline |

## One-time server setup

On a fresh Ubuntu 26 Vultr instance:

1. **Create a deploy user with passwordless sudo** (or deploy as root and skip the SSH-key step's sudo).
   ```bash
   adduser --disabled-password deploy
   usermod -aG sudo deploy
   echo 'deploy ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/deploy
   ```
2. **Add the CI public key** to `~deploy/.ssh/authorized_keys`.
3. **Install Caddy** (handles TLS + renewal for the domain):
   ```bash
   sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
   curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
   curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
   sudo apt update && sudo apt install -y caddy
   sudo cp deploy/Caddyfile /etc/caddy/Caddyfile   # (copy the file's contents up to the box)
   sudo systemctl reload caddy
   ```
4. **DNS + firewall**: point `starkonflict.com` (and `www`) A/AAAA records at the server; allow ports **80** and **443** (Vultr firewall / `ufw`). Caddy needs 80 for the ACME challenge.

## GitHub repository secrets

Set these in **Settings → Secrets and variables → Actions**:

| Secret | Value |
|---|---|
| `DEPLOY_HOST` | server IP or hostname |
| `DEPLOY_USER` | `deploy` (the sudo-capable user above) |
| `DEPLOY_SSH_KEY` | the **private** key whose public half is in `authorized_keys` |

That's it — push to `main` and the workflow builds and deploys. Trigger a deploy
by hand anytime with **Actions → Deploy coverage dashboard → Run workflow**.

## Updating the data

The dashboard shows whatever is in `deploy/data/`. Refresh it from your capture
machine (all pure Go, no scripts), then commit and push — CI redeploys:

```bash
cd sc-capture
go build -o out/sccov ./cmd/sccov

cp ~/.local/share/sccap/coverage.json ../deploy/data/coverage.json
./out/sccov -emit-summary ../packet-caps > ../deploy/data/bundles-summary.json

git add ../deploy/data && git commit -m "Refresh coverage snapshot" && git push
```

`-emit-summary` reads the bundles and prints **only** scenario, region, frame
count, and observed/novel tallies — no capture contents — so the file is safe to
commit and serve publicly.

> Prefer live updates without a commit? `scp` a fresh `coverage.json` (and
> `bundles-summary.json`) straight to `/var/lib/sccov/` on the server; `sccov`
> re-reads them within 5 seconds. Keep the raw `packet-caps/` bundles off the
> public server — they contain credentials.

## Running it locally

```bash
cd sc-capture && go build -o out/sccov ./cmd/sccov
./out/sccov -addr :8080 -bundles ../packet-caps    # reads the raw dir directly
# open http://localhost:8080
```

`-bundles` reads the raw `packet-caps/` directory (local convenience);
`-bundles-summary` reads the sanitized file (what the server uses). `-tls-cert`
/ `-tls-key` enable HTTPS directly if you ever run without a proxy.
