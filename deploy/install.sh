#!/usr/bin/env bash
# Installs the Fugitive 3D server repository on hydra: site user, Go toolchain,
# checkout, systemd unit, nginx vhost, and the webhook-driven redeploy pipeline.
#
# Run with sudo. Safe to re-run; the webhook secret is preserved across runs.
#
# Differs from the document-root sites on this box in two ways. There is no
# docroot and no database: nginx reverse-proxies to a long-running Go binary,
# and a deploy rebuilds that binary and restarts its unit rather than syncing
# files. This is the first app service actually running on hydra; the other
# proxy vhosts here all forward to charon.
set -euo pipefail

SITE_USER="fugitive-repo"
DOMAIN="repository.fugitivethegame.online"
REPO_URL="https://github.com/FugitiveTheGame/Fugitive3dServerRepository.git"
BRANCH="master"
HOME_DIR="/home/$SITE_USER"
DEPLOY_DIR="$HOME_DIR/deploy"
BIN_DIR="$HOME_DIR/bin"
CONF_DIR="/etc/fugitive-repo-webhook"
SECRET_FILE="$CONF_DIR/secret"
HOOKS_FILE="$CONF_DIR/hooks.yaml"
NGINX_SITE="/etc/nginx/sites-available/$DOMAIN"
NGINX_SNIPPET="/etc/nginx/snippets/fugitive-repo-deploy.conf"
SERVICE="fugitive-repo.service"
WEBHOOK_SERVICE="fugitive-repo-webhook.service"
SUDOERS="/etc/sudoers.d/fugitive-repo-deploy"
GO_DIR="/usr/local/go"
LISTEN_PORT=8080
SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ $EUID -ne 0 ]]; then
	echo "error: must run as root (use sudo)" >&2
	exit 1
fi

step() { printf '\n==> %s\n' "$*"; }

step "Creating the $SITE_USER user"
if id "$SITE_USER" >/dev/null 2>&1; then
	echo "user already exists"
else
	useradd --create-home --shell /bin/bash "$SITE_USER"
	echo "created $SITE_USER"
fi

step "Installing packages"
missing=()
command -v git >/dev/null || missing+=(git)
command -v webhook >/dev/null || missing+=(webhook)
command -v curl >/dev/null || missing+=(curl)
if ((${#missing[@]})); then
	apt-get update -qq
	apt-get install -y "${missing[@]}"
else
	echo "git, webhook and curl all present"
fi

# The Debian webhook package ships its own always-on instance bound to :9000.
# We run our own unit instead, so make sure the stock one cannot collide.
if systemctl list-unit-files webhook.service >/dev/null 2>&1; then
	if systemctl is-enabled --quiet webhook.service 2>/dev/null; then
		step "Disabling the stock webhook.service"
		systemctl disable --now webhook.service || true
	fi
fi

step "Installing the Go toolchain"
# Deliberately not apt's golang-go. Its candidate is exactly 1.26.0, the oldest
# patch of that minor version, which carries standard library advisories that
# later patches fix. go.mod's directive is a floor, not the version to build
# with, so take the current upstream release instead.
want="$(curl -fsSL https://go.dev/VERSION?m=text | head -1)"
if [[ -z "$want" ]]; then
	echo "error: could not determine the current Go version" >&2
	exit 1
fi
have=""
if [[ -x "$GO_DIR/bin/go" ]]; then
	have="$("$GO_DIR/bin/go" env GOVERSION 2>/dev/null || true)"
fi
if [[ "$have" == "$want" ]]; then
	echo "already at $have"
else
	echo "installing $want (was ${have:-none})"
	tmp="$(mktemp -d)"
	tarball="$want.linux-amd64.tar.gz"
	curl -fsSL -o "$tmp/$tarball" "https://dl.google.com/go/$tarball"
	curl -fsSL -o "$tmp/$tarball.sha256" "https://dl.google.com/go/$tarball.sha256"
	echo "$(cat "$tmp/$tarball.sha256")  $tmp/$tarball" | sha256sum -c -
	rm -rf "$GO_DIR"
	tar -C /usr/local -xzf "$tmp/$tarball"
	rm -rf "$tmp"
	echo "installed $("$GO_DIR/bin/go" version)"
fi

step "Creating $DEPLOY_DIR and $BIN_DIR"
install -d -o "$SITE_USER" -g "$SITE_USER" -m 0755 "$DEPLOY_DIR"
install -d -o "$SITE_USER" -g "$SITE_USER" -m 0755 "$BIN_DIR"
install -o "$SITE_USER" -g "$SITE_USER" -m 0750 "$SRC_DIR/redeploy.sh" "$DEPLOY_DIR/redeploy.sh"
if [[ -f "$SRC_DIR/server-README.md" ]]; then
	install -o "$SITE_USER" -g "$SITE_USER" -m 0644 "$SRC_DIR/server-README.md" "$HOME_DIR/README.md"
fi

step "Ensuring the git checkout exists"
if [[ ! -d "$DEPLOY_DIR/repo/.git" ]]; then
	sudo -u "$SITE_USER" -H git clone --branch "$BRANCH" "$REPO_URL" "$DEPLOY_DIR/repo"
else
	echo "checkout already present at $DEPLOY_DIR/repo"
	sudo -u "$SITE_USER" git -C "$DEPLOY_DIR/repo" remote set-url origin "$REPO_URL"
fi

step "Allowing $SITE_USER to restart the service"
# Scoped to exactly the three subcommands redeploy.sh needs on one unit. This
# is the only privilege the deploy pipeline has.
cat >"$SUDOERS" <<EOF
$SITE_USER ALL=(root) NOPASSWD: /usr/bin/systemctl restart $SERVICE
$SITE_USER ALL=(root) NOPASSWD: /usr/bin/systemctl is-active --quiet $SERVICE
$SITE_USER ALL=(root) NOPASSWD: /usr/bin/systemctl status $SERVICE *
EOF
chmod 0440 "$SUDOERS"
visudo -cf "$SUDOERS"

step "Installing $SERVICE"
install -m 0644 "$SRC_DIR/fugitive-repo.service" "/etc/systemd/system/$SERVICE"

step "Setting up the webhook"
install -d -m 0755 "$CONF_DIR"
if [[ -f "$SECRET_FILE" ]]; then
	SECRET="$(cat "$SECRET_FILE")"
	echo "reusing the existing webhook secret"
else
	SECRET="$(openssl rand -hex 32)"
	printf '%s' "$SECRET" >"$SECRET_FILE"
	echo "generated a new webhook secret"
fi
chmod 0600 "$SECRET_FILE"

# The secret is substituted in rather than committed. Readable by the webhook
# user only.
sed "s|__WEBHOOK_SECRET__|$SECRET|" "$SRC_DIR/hooks.yaml" >"$HOOKS_FILE"
chown "root:$SITE_USER" "$HOOKS_FILE"
chmod 0640 "$HOOKS_FILE"

install -m 0644 "$SRC_DIR/fugitive-repo-webhook.service" "/etc/systemd/system/$WEBHOOK_SERVICE"
install -m 0644 "$SRC_DIR/nginx-deploy-snippet.conf" "$NGINX_SNIPPET"

step "Installing the nginx vhost"
# Never overwritten once present: the vhost is the file certbot would rewrite,
# and on this box that has to stay hand-managed. Patch it in place instead.
if [[ -f "$NGINX_SITE" ]]; then
	echo "vhost already present, leaving it alone"
else
	install -m 0644 "$SRC_DIR/nginx-site.conf" "$NGINX_SITE"
	echo "installed $NGINX_SITE"
fi
ln -sf "$NGINX_SITE" "/etc/nginx/sites-enabled/$DOMAIN"

step "Reloading systemd"
systemctl daemon-reload

step "Running the first deploy"
# Builds the binary and restarts the unit. Enable first so the restart in
# redeploy.sh has something to act on.
systemctl enable "$SERVICE" >/dev/null
sudo -u "$SITE_USER" -H "$DEPLOY_DIR/redeploy.sh" --force

step "Starting the webhook"
systemctl enable --now "$WEBHOOK_SERVICE" >/dev/null
systemctl is-active "$WEBHOOK_SERVICE"

step "Validating and reloading nginx"
nginx -t
systemctl reload nginx

step "Smoke test"
probe() {
	curl -sS -o /dev/null -w '%{http_code}' --max-time 10 "$@" 2>/dev/null || echo ERR
}

printf '  %-34s %s\n' "direct  /servers" \
	"$(probe "http://127.0.0.1:$LISTEN_PORT/servers")"
printf '  %-34s %s\n' "nginx   /servers" \
	"$(probe --resolve "$DOMAIN:80:127.0.0.1" "http://$DOMAIN/servers")"

# nginx appends the caller to any inbound X-Forwarded-For, so sending one from
# loopback produces the chain "203.0.113.4, 127.0.0.1". The service walks that
# right to left, skips the trusted loopback hop and should report the first
# untrusted address. If this echoes 127.0.0.1 the forwarding header is not
# reaching the service and every registration would 403.
forwarded="$(curl -sS --max-time 10 --resolve "$DOMAIN:80:127.0.0.1" \
	-H 'X-Forwarded-For: 203.0.113.4' "http://$DOMAIN/reflection/ip" 2>/dev/null || echo ERR)"
printf '  %-34s %s\n' "nginx   /reflection/ip (forwarded)" "$forwarded"
if [[ "$forwarded" != *'"ip":"203.0.113.4"'* ]]; then
	echo
	echo "  WARNING: the forwarded address did not survive the proxy." >&2
	echo "  Game servers would be unable to register. Check that the vhost sets" >&2
	echo "  X-Forwarded-For and that the service trusts 127.0.0.1." >&2
fi

cat <<EOF

============================================================
 Server side is ready.

 Service:      http://$DOMAIN  (plain HTTP, deliberately)
 Unit:         systemctl status $SERVICE
 Logs:         journalctl -u fugitive-repo -f
 Deploy log:   journalctl -u fugitive-repo-webhook -f
 Manual sync:  sudo -u $SITE_USER $DEPLOY_DIR/redeploy.sh --force

 DNS still has to exist for any of this to be reachable:
 an A record for $DOMAIN pointing at this box.

 Register the webhook from a workstation with gh authenticated. Kept to one
 line with double quotes so it pastes unchanged into bash, PowerShell or cmd:

gh api repos/FugitiveTheGame/Fugitive3dServerRepository/hooks -f name=web -F active=true -f "events[]=push" -f "config[url]=http://$DOMAIN/_deploy" -f "config[content_type]=json" -f "config[secret]=$SECRET"

 The secret is kept at $SECRET_FILE if this needs printing again.

 Then verify from OFF this box:

   ./deploy/verify.sh
============================================================
EOF
