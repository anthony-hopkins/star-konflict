#!/usr/bin/env bash
# Server-side installer for the sccov coverage dashboard.
#
# Invoked by CI as:  ssh <host> 'sudo bash -s' < deploy/install.sh
# It expects these files already uploaded to /tmp by the workflow:
#   /tmp/sccov  /tmp/sccov.service  /tmp/coverage.json  /tmp/bundles-summary.json
#
# Idempotent: safe to run on every deploy.
set -euo pipefail

# Dedicated unprivileged service account (no shell, no home).
id sccov >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin sccov

# Binary + unit.
install -m 0755 /tmp/sccov         /usr/local/bin/sccov
install -m 0644 /tmp/sccov.service /etc/systemd/system/sccov.service

# Data (non-sensitive aggregate coverage only). World-readable so the sccov
# user can read it; the directory is root-owned and read-only to the service.
install -d -m 0755 /var/lib/sccov
install -m 0644 /tmp/coverage.json /var/lib/sccov/coverage.json
[ -f /tmp/bundles-summary.json ] && install -m 0644 /tmp/bundles-summary.json /var/lib/sccov/bundles-summary.json

rm -f /tmp/sccov /tmp/sccov.service /tmp/coverage.json /tmp/bundles-summary.json

systemctl daemon-reload
systemctl enable sccov >/dev/null 2>&1 || true
systemctl restart sccov

# Report health, fail the deploy if it didn't come up.
sleep 1
if systemctl is-active --quiet sccov; then
	echo "sccov is active on 127.0.0.1:8080"
else
	echo "sccov failed to start:" >&2
	journalctl -u sccov -n 30 --no-pager >&2 || true
	exit 1
fi
