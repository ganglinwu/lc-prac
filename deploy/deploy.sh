#!/usr/bin/env bash
# Build lcpracd for the VPS and install it as a systemd service behind Caddy.
#
#   ./deploy/deploy.sh ubuntu@54.169.114.237 lcprac.guenyanghae.com
#
# Re-running is safe: the env file (which holds the account tokens) is only
# written when it does not already exist, so a redeploy never rotates a token.
set -euo pipefail

HOST=${1:?usage: deploy.sh <ssh-host> [domain]}
DOMAIN=${2:-lcprac.guenyanghae.com}
REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

echo "==> building lcpracd for linux/amd64"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -C "$REPO" -o /tmp/lcpracd.deploy ./cmd/lcpracd

echo "==> uploading"
scp -q /tmp/lcpracd.deploy "$HOST":/tmp/lcpracd.new
scp -q "$REPO/deploy/lcpracd.service" "$HOST":/tmp/lcpracd.service

echo "==> installing on $HOST"
ssh "$HOST" DOMAIN="$DOMAIN" bash -s <<'REMOTE'
set -euo pipefail

id -u lcpracd >/dev/null 2>&1 || sudo useradd --system --no-create-home --shell /usr/sbin/nologin lcpracd
sudo install -m 0755 /tmp/lcpracd.new /usr/local/bin/lcpracd
sudo install -m 0644 /tmp/lcpracd.service /etc/systemd/system/lcpracd.service
rm -f /tmp/lcpracd.new /tmp/lcpracd.service

sudo install -d -m 0750 -o root -g lcpracd /etc/lcpracd
if ! sudo test -f /etc/lcpracd/lcpracd.env; then
	token=$(head -c 32 /dev/urandom | base64 | tr -d '=+/' | cut -c1-32)
	printf 'LCPRAC_SYNC_ADDR=127.0.0.1:8090\nLCPRAC_SYNC_DATA=/var/lib/lcpracd\nLCPRAC_SYNC_TOKENS=ganglin:%s\n' "$token" \
		| sudo tee /etc/lcpracd/lcpracd.env >/dev/null
	echo "GENERATED TOKEN ganglin: $token"
	echo "  save it now: lcprac sync -set-url https://$DOMAIN -set-token $token"
fi
sudo chown root:lcpracd /etc/lcpracd/lcpracd.env
sudo chmod 0640 /etc/lcpracd/lcpracd.env

sudo systemctl daemon-reload
sudo systemctl enable --now lcpracd
sudo systemctl restart lcpracd

# Add the vhost only once; Caddy fetches the certificate on reload.
if ! grep -q "^$DOMAIN" /etc/caddy/Caddyfile; then
	printf '\n%s {\n\treverse_proxy 127.0.0.1:8090\n}\n' "$DOMAIN" | sudo tee -a /etc/caddy/Caddyfile >/dev/null
	sudo systemctl reload caddy
fi

sleep 2
systemctl is-active lcpracd
curl -fsS http://127.0.0.1:8090/healthz && echo
REMOTE

# The first request after a new vhost can beat Caddy's ACME run, so retry
# rather than reporting a deploy failure that fixes itself in ten seconds.
echo "==> checking $DOMAIN through Caddy"
for _ in $(seq 1 12); do
	if curl -fsS "https://$DOMAIN/healthz"; then
		echo
		echo "==> done"
		exit 0
	fi
	sleep 5
done
echo "FAILED: https://$DOMAIN/healthz never answered" >&2
exit 1
