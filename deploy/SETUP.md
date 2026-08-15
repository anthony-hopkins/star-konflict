# Server setup runbook — deploying `sccov` to Vultr

End-to-end setup for the coverage dashboard. You work in three places:
**🌐 web** (Vultr portal, DNS, GitHub), **💻 your machine**, and **🖥️ the server**.

Once this is done, every push to `main` that touches the dashboard or
`deploy/data/` redeploys automatically. See [README.md](README.md) for the
architecture and data-refresh workflow.

## Prerequisite — the deploy SSH key

A dedicated, single-purpose key pair for CI (generated on the capture machine):

```bash
ssh-keygen -t ed25519 -f ~/.ssh/starkonflict_deploy -N "" -C "starkonflict-deploy-ci"
```

- **Public** half → the server (Step 3).
- **Private** half → the GitHub secret (Step 4), via `cat ~/.ssh/starkonflict_deploy`.

The public key for this deployment:

```
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJx3sl+T26f3Pqa/eLeRv1XUgaTPbLyy6EtsnqwIBYA4 starkonflict-deploy-ci
```

## Step 1 — 🌐 Create the Vultr instance

Vultr portal → **Deploy → Cloud Compute**.

- **OS:** Ubuntu 26 (x64).
- **Plan:** any Regular / High-Frequency plan — must be **x86_64 / amd64**, because the
  pipeline ships an amd64 binary. For an ARM instance, change the build step in
  `deploy.yml` to `GOARCH=arm64 go build …` (it already compiles there in CI).
- Note the **public IP** and the **root password**.

## Step 2 — 🌐 Point DNS at it

Where `starkonflict.com`'s DNS lives (registrar, or Vultr → Products → DNS):

| Type | Name | Value |
|---|---|---|
| A | `@` | server IP |
| A | `www` | server IP |

Add matching `AAAA` records if the instance has IPv6. Do this **before** Step 3 —
Caddy needs the name resolving to obtain a certificate. Confirm with
`dig +short starkonflict.com`.

## Step 3 — 🖥️ Bootstrap the server (run as root, one paste)

`ssh root@YOUR_SERVER_IP`, then:

```bash
set -e

# deploy user with passwordless sudo (CI runs install.sh via sudo)
adduser --disabled-password --gecos "" deploy
usermod -aG sudo deploy
echo 'deploy ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/deploy && chmod 440 /etc/sudoers.d/deploy

# install the CI deploy public key
install -d -m 700 -o deploy -g deploy /home/deploy/.ssh
echo 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJx3sl+T26f3Pqa/eLeRv1XUgaTPbLyy6EtsnqwIBYA4 starkonflict-deploy-ci' \
  > /home/deploy/.ssh/authorized_keys
chown deploy:deploy /home/deploy/.ssh/authorized_keys && chmod 600 /home/deploy/.ssh/authorized_keys

# Caddy (auto Let's Encrypt reverse proxy)
apt-get update
apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' > /etc/apt/sources.list.d/caddy-stable.list
apt-get update && apt-get install -y caddy

cat > /etc/caddy/Caddyfile <<'EOF'
starkonflict.com, www.starkonflict.com {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8080
	header {
		Referrer-Policy strict-origin-when-cross-origin
		X-Content-Type-Options nosniff
	}
}
EOF
systemctl reload caddy

# firewall
ufw allow OpenSSH && ufw allow 80/tcp && ufw allow 443/tcp && ufw --force enable

echo "server ready; caddy is $(systemctl is-active caddy)"
```

If a Vultr **cloud firewall** is attached to the instance, also allow **22, 80,
443** there. Until Step 5 deploys the app, `https://starkonflict.com` returns a
Caddy **502** — expected.

## Step 4 — 🌐 Add the GitHub secrets

Repo → **Settings → Secrets and variables → Actions → New repository secret**:

| Name | Value |
|---|---|
| `DEPLOY_HOST` | server IP |
| `DEPLOY_USER` | `deploy` |
| `DEPLOY_SSH_KEY` | full private key, incl. the `-----BEGIN/END-----` lines (`cat ~/.ssh/starkonflict_deploy`) |

## Step 5 — 🌐 Deploy and verify

Repo → **Actions → "Deploy coverage dashboard" → Run workflow → main**. Every
step should pass; *Install and restart* ends by confirming `sccov is active`.

```bash
curl -sI https://starkonflict.com | head -1     # HTTP/2 200
# on the server, if you want a closer look:
systemctl status sccov --no-pager
curl -s 127.0.0.1:8080/healthz                  # ok
```

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| Deploy fails at **Configure SSH** | Secrets missing or mistyped. |
| **`sudo: a password is required`** | The `/etc/sudoers.d/deploy` NOPASSWD line didn't apply. |
| **`Permission denied (publickey)`** | Public key not in `deploy`'s `authorized_keys`, or wrong private key in the secret. |
| Caddy **502** | `sccov` not running → `journalctl -u sccov -n 30`. |
| Caddy **no cert / TLS error** | DNS not resolving yet, or port 80 blocked (Vultr firewall). Caddy retries. |
