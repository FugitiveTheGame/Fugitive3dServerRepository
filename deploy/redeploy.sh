#!/usr/bin/env bash
# Pulls the latest master, rebuilds the binary and restarts the service.
# Runs as the fugitive-repo user, invoked by the webhook service.
#
# Unlike the document-root sites on this box there is nothing to rsync: the
# deployable artifact is a single binary. It is built to a temporary path and
# only moved into place once the build succeeds, so a broken commit leaves the
# running service untouched.
set -euo pipefail

REPO_URL="https://github.com/FugitiveTheGame/Fugitive3dServerRepository.git"
BRANCH="master"
DEPLOY_DIR="/home/fugitive-repo/deploy"
REPO_DIR="$DEPLOY_DIR/repo"
BIN_DIR="/home/fugitive-repo/bin"
BIN="$BIN_DIR/fugitive-repo"
SERVICE="fugitive-repo.service"
LOCK_FILE="$DEPLOY_DIR/.deploy.lock"
# Records the commit the installed binary was built from. Deliberately not the
# checkout's HEAD: a fresh clone is already at origin/master while no binary
# has been built yet, which would skip the first deploy.
STAMP_FILE="$DEPLOY_DIR/.deployed-sha"

export PATH="/usr/local/go/bin:$PATH"
# The build needs a writable module cache and GOPATH inside the site user's home.
export HOME="/home/fugitive-repo"
export GOPATH="$HOME/go"
export GOCACHE="$HOME/.cache/go-build"

FORCE=0
if [[ "${1:-}" == "--force" ]]; then
	FORCE=1
fi

log() { echo "[$(date -Is)] $*"; }

exec 9>"$LOCK_FILE"
if ! flock -n 9; then
	log "another deploy is in progress, skipping this trigger"
	exit 0
fi

if [[ ! -d "$REPO_DIR/.git" ]]; then
	log "no checkout yet, cloning $REPO_URL"
	git clone --branch "$BRANCH" "$REPO_URL" "$REPO_DIR"
fi

git -C "$REPO_DIR" fetch --prune origin "$BRANCH"

target="$(git -C "$REPO_DIR" rev-parse "origin/$BRANCH")"
deployed="$(cat "$STAMP_FILE" 2>/dev/null || echo none)"

if [[ $FORCE -eq 0 && "$deployed" == "$target" ]]; then
	log "already running ${target:0:8}, nothing to deploy"
	exit 0
fi

log "deploying ${deployed:0:8} -> ${target:0:8}"
git -C "$REPO_DIR" reset --hard "origin/$BRANCH"
git -C "$REPO_DIR" clean -fd

# Refuse to build something that is not this service. Deliberately structural
# rather than a named file, so a rename cannot silently freeze deploys.
test -f "$REPO_DIR/go.mod"
test -f "$REPO_DIR/main.go"
test -d "$REPO_DIR/internal/httpapi"

log "go version: $(go version)"

# Vet, then build to a temp path. The test suite is not run here: CI runs it on
# every push, including under -race, which this box cannot do without cgo.
log "vetting"
go -C "$REPO_DIR" vet ./...

staged="$(mktemp "$BIN_DIR/.fugitive-repo.XXXXXX")"
trap 'rm -f "$staged"' EXIT

log "building"
go -C "$REPO_DIR" build -o "$staged" .
chmod 0755 "$staged"

# Same filesystem, so this replaces the binary atomically. A running process
# keeps its open inode until it is restarted.
mv -f "$staged" "$BIN"
trap - EXIT
log "installed $BIN"

log "restarting $SERVICE"
sudo -n /usr/bin/systemctl restart "$SERVICE"

# Give it a moment to bind before reporting, so a failure to start shows up
# here rather than silently in the journal.
sleep 2
if ! sudo -n /usr/bin/systemctl is-active --quiet "$SERVICE"; then
	log "ERROR: $SERVICE did not come back up"
	sudo -n /usr/bin/systemctl status "$SERVICE" --no-pager --lines=20 || true
	exit 1
fi

echo "$target" >"$STAMP_FILE"
log "deployed ${target:0:8}"
