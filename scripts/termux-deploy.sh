#!/data/data/com.termux/files/usr/bin/bash
# termux-deploy.sh — bootstrap Coggo on Android via Termux.
#
# What this does (idempotent — safe to re-run):
#   1. Verifies we're running inside Termux.
#   2. Installs required Termux packages (golang, git,
#      termux-services, termux-api, openssh, cloudflared).
#   3. Builds and installs coggo + coggo-oauth-gateway via make install-all.
#   4. Installs Go-built binaries to $GOBIN (or go env GOPATH/bin).
#   5. Drops an env-file template at <repo>/.env you must fill in. Same
#      filename and shape as the laptop's repo-root .env, so secrets follow
#      one convention across machines.
#   6. Installs a Termux:Boot launcher at ~/.termux/boot/30-coggo so the
#      whole stack comes back up after reboot.
#   7. Prints next steps (Cloudflare Tunnel setup, fill env, reboot).
#
# Prereqs you handle manually (one-time):
#   - Install Termux from F-Droid (NOT Play Store — Play version is stale).
#   - Install Termux:Boot from F-Droid (so boot script auto-runs on reboot).
#   - Disable battery optimization for Termux + Termux:Boot in Android settings.
#   - Open Termux:Boot once so Android grants it permission to run on boot.
#
# Re-run this script after pulling new commits to rebuild + redeploy.

set -euo pipefail

# --- 1. sanity ---------------------------------------------------------------

if [ -z "${PREFIX:-}" ] || [ ! -d "/data/data/com.termux" ]; then
    echo "this script must run inside Termux on Android" >&2
    exit 1
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

echo "==> Coggo Termux deploy"
echo "    repo:    $REPO_ROOT"
echo "    HOME:    $HOME"
echo "    PREFIX:  $PREFIX"
echo

resolve_go_bin_dir() {
    local dir
    dir="${GOBIN:-}"
    if [ -z "$dir" ]; then
        dir="$(go env GOBIN 2>/dev/null || true)"
    fi
    if [ -z "$dir" ]; then
        dir="$(go env GOPATH)/bin"
    fi
    printf '%s\n' "$dir"
}

# --- 2. packages -------------------------------------------------------------

echo "==> installing Termux packages..."
pkg update -y >/dev/null
# termux-services: sv-style supervisor (not strictly needed since we use
# Termux:Boot, but useful for `sv restart coggo` etc).
pkg install -y \
    golang git make \
    termux-services \
    termux-api \
    openssh \
    jq curl

# cloudflared opens an outbound tunnel from the phone to Cloudflare. This is
# the supported public exposure path for Termux; no Tailscale daemon is needed.
pkg install -y cloudflared

# --- 3. build ----------------------------------------------------------------

APP_BIN_DIR="$(resolve_go_bin_dir)"
mkdir -p "$APP_BIN_DIR"
export GOBIN="$APP_BIN_DIR"
export PATH="$APP_BIN_DIR:$PATH"

echo
echo "==> installing Go binaries to $APP_BIN_DIR (this takes ~1-2 min on a phone)..."
# Use the Makefile so we pick up VERSION + LDFLAGS consistently and install
# into GOBIN instead of Termux's package binary directory.
make install-all

# Litestream — continuous DB replication to Cloudflare R2. Termux's pkg
# repo doesn't ship it, so we go install. Idempotent: re-running upgrades
# in place. Installed next to the other Go-built binaries.
if [ ! -x "$APP_BIN_DIR/litestream" ]; then
    echo
    echo "==> installing litestream (one-time, ~30s)..."
    GOBIN="$APP_BIN_DIR" go install github.com/benbjohnson/litestream/cmd/litestream@latest
fi

# --- 4. config + env template -----------------------------------------------

mkdir -p "$HOME/.coggo"
# .env lives at the repo root — same convention as the laptop. Gitignored,
# chmod 600. termux-update.sh and `make serve-public` both read from here.
# Single source of truth for the template: the committed .env.example.
ENV_FILE="$REPO_ROOT/.env"
ENV_EXAMPLE="$REPO_ROOT/.env.example"
if [ ! -f "$ENV_FILE" ]; then
    if [ ! -f "$ENV_EXAMPLE" ]; then
        echo "missing $ENV_EXAMPLE — repo is incomplete" >&2
        exit 1
    fi
    echo
    echo "==> seeding $ENV_FILE from .env.example"
    cp "$ENV_EXAMPLE" "$ENV_FILE"
    chmod 600 "$ENV_FILE"
else
    echo "==> env file already exists at $ENV_FILE (leaving it alone)"
fi

# --- 5. boot launcher --------------------------------------------------------

BOOT_DIR="$HOME/.termux/boot"
mkdir -p "$BOOT_DIR"
BOOT_SCRIPT="$BOOT_DIR/30-coggo"

echo "==> installing boot launcher at $BOOT_SCRIPT"
# Uses absolute path inside the script (Termux:Boot runs without a real
# shell environment). Logs go to ~/.coggo/logs/.
cat > "$BOOT_SCRIPT" <<'BOOT_EOF'
#!/data/data/com.termux/files/usr/bin/bash
# Termux:Boot launcher for Coggo. Brings up:
#   - termux-wake-lock (prevents Android from killing the process tree)
#   - coggo serve
#   - coggo-oauth-gateway
#   - cloudflared tunnel pointing at the gateway port
#
# Re-running is safe: each step checks for an existing PID first.

set -u

PREFIX="/data/data/com.termux/files/usr"
HOME_DIR="/data/data/com.termux/files/home"
LOG_DIR="$HOME_DIR/.coggo/logs"
RUN_DIR="$HOME_DIR/.coggo/run"
# .env at the repo root — same convention the laptop uses.
ENV_FILE="$HOME_DIR/coggo/.env"

mkdir -p "$LOG_DIR" "$RUN_DIR"

ts() { date '+%Y-%m-%dT%H:%M:%S'; }
log() { echo "[$(ts)] $*" >> "$LOG_DIR/boot.log"; }

resolve_app_bin_dir() {
    local dir
    dir="${GOBIN:-}"
    if [ -z "$dir" ] && [ -x "$PREFIX/bin/go" ]; then
        dir="$("$PREFIX/bin/go" env GOBIN 2>/dev/null || true)"
    fi
    if [ -z "$dir" ] && [ -x "$PREFIX/bin/go" ]; then
        dir="$("$PREFIX/bin/go" env GOPATH)/bin"
    fi
    if [ -z "$dir" ]; then
        dir="$HOME_DIR/go/bin"
    fi
    printf '%s\n' "$dir"
}

APP_BIN_DIR="$(resolve_app_bin_dir)"
export PATH="$APP_BIN_DIR:$PREFIX/bin:${PATH:-}"
log "app bin dir: $APP_BIN_DIR"

# 0. Wake lock so Android doesn't suspend us.
"$PREFIX/bin/termux-wake-lock" || log "termux-wake-lock failed (Termux:API not installed?)"

# Helper: start $1 with cmd "$2 ..." if not already running. Writes pidfile.
start_if_down() {
    local name="$1"; shift
    local pidfile="$RUN_DIR/$name.pid"
    if [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
        log "$name already running (pid $(cat "$pidfile"))"
        return 0
    fi
    log "starting $name: $*"
    setsid nohup "$@" >> "$LOG_DIR/$name.log" 2>&1 &
    echo $! > "$pidfile"
    sleep 1
    if ! kill -0 "$(cat "$pidfile")" 2>/dev/null; then
        log "$name failed to stay up — see $LOG_DIR/$name.log"
        return 1
    fi
}

# 1. Coggo + gateway. Sourcing .env is enough — every variable in it is
#    declared with `export`, so children inherit them without `set -a`.
if [ ! -f "$ENV_FILE" ]; then
    log "missing $ENV_FILE — refusing to start gateway"
    exit 1
fi
# shellcheck disable=SC1090
. "$ENV_FILE"

APP_BIN_DIR="$(resolve_app_bin_dir)"
export PATH="$APP_BIN_DIR:$PREFIX/bin:${PATH:-}"

start_if_down coggo "$APP_BIN_DIR/coggo" serve

# Litestream — only if all four R2_* values are set in .env.
if [ -n "${R2_ACCESS_KEY_ID:-}" ] && [ -n "${R2_SECRET_ACCESS_KEY:-}" ] \
   && [ -n "${R2_ACCOUNT_ID:-}" ] && [ -n "${R2_BUCKET:-}" ]; then
    start_if_down litestream "$APP_BIN_DIR/litestream" replicate \
        -config "$HOME_DIR/coggo/scripts/litestream.yml"
else
    log "litestream skipped — R2_* vars not all set in .env (coggo will run without replication)"
fi

# Wait for coggo's MCP port before launching the gateway.
COGGO_PORT="${COGGO_PORT:-6177}"
for _ in $(seq 1 15); do
    if "$PREFIX/bin/curl" -s --max-time 2 -o /dev/null -w '%{http_code}' \
        "http://localhost:$COGGO_PORT/mcp" 2>/dev/null | grep -qE '^[0-9]+$'; then
        break
    fi
    sleep 1
done

start_if_down gateway "$APP_BIN_DIR/coggo-oauth-gateway"

# 2. Public exposure via Cloudflare Tunnel.
GATEWAY_PORT="${GATEWAY_PORT:-8080}"
GATEWAY_PORT="${GATEWAY_PORT#:}"

if [ -z "${CLOUDFLARE_TUNNEL_NAME:-}" ]; then
    log "CLOUDFLARE_TUNNEL_NAME is not set — refusing to start public tunnel"
    exit 1
fi
if [ ! -x "$PREFIX/bin/cloudflared" ]; then
    log "cloudflared not installed — run pkg install cloudflared"
    exit 1
fi
log "starting cloudflared tunnel '$CLOUDFLARE_TUNNEL_NAME'"
start_if_down cloudflared "$PREFIX/bin/cloudflared" tunnel run "$CLOUDFLARE_TUNNEL_NAME"

log "boot sequence complete"
BOOT_EOF
chmod 700 "$BOOT_SCRIPT"

# --- 6. cloudflared config ---------------------------------------------------

CF_CONFIG="$HOME/.cloudflared/config.yml"
CF_CREDS=$(ls "$HOME/.cloudflared/"*.json 2>/dev/null | head -1)

if [ -n "$CF_CREDS" ]; then
    TUNNEL_UUID="$(basename "$CF_CREDS" .json)"
    # Read CLOUDFLARE_TUNNEL_NAME + GATEWAY_PORT from .env if set.
    # shellcheck disable=SC1090
    [ -f "$REPO_ROOT/.env" ] && . "$REPO_ROOT/.env"
    TUNNEL_NAME="${CLOUDFLARE_TUNNEL_NAME:-coggo}"
    GW_PORT="${GATEWAY_PORT:-8080}"

    if [ ! -f "$CF_CONFIG" ]; then
        echo
        echo "==> generating $CF_CONFIG"
        # GATEWAY_PUBLIC_URL must already be set in .env — prompt if missing.
        if [ -z "${GATEWAY_PUBLIC_URL:-}" ]; then
            echo "    WARNING: GATEWAY_PUBLIC_URL is not set in .env"
            echo "    Edit $CF_CONFIG after filling that in."
            CF_HOSTNAME="<your-hostname>"
        else
            # Strip the scheme for the hostname field.
            CF_HOSTNAME="${GATEWAY_PUBLIC_URL#https://}"
            CF_HOSTNAME="${CF_HOSTNAME#http://}"
        fi
        mkdir -p "$HOME/.cloudflared"
        cat > "$CF_CONFIG" <<CFEOF
tunnel: $TUNNEL_UUID
credentials-file: $CF_CREDS

ingress:
  - hostname: $CF_HOSTNAME
    service: http://localhost:$GW_PORT
  - service: http_status:404
CFEOF
        chmod 600 "$CF_CONFIG"
    else
        echo "==> cloudflared config already exists at $CF_CONFIG (leaving it alone)"
    fi

    # Register the DNS route (idempotent).
    if [ -n "${GATEWAY_PUBLIC_URL:-}" ]; then
        CF_HOSTNAME="${GATEWAY_PUBLIC_URL#https://}"
        CF_HOSTNAME="${CF_HOSTNAME#http://}"
        echo "==> registering DNS route: $CF_HOSTNAME -> tunnel $TUNNEL_NAME"
        cloudflared tunnel route dns "$TUNNEL_NAME" "$CF_HOSTNAME" || true
    fi
else
    echo
    echo "==> cloudflared tunnel not yet created — skipping config generation."
    echo "    Run these once, then re-run this deploy script:"
    echo "      cloudflared tunnel login"
    echo "      cloudflared tunnel create coggo"
fi

# --- 7. next steps -----------------------------------------------------------

cat <<EOF

==> done.

Next steps (do these once, in order):

1. If not done yet, create the Cloudflare Tunnel then re-run this script:
     cloudflared tunnel login
     cloudflared tunnel create coggo
   This script auto-generates ~/.cloudflared/config.yml and registers the
   DNS route once the tunnel credentials exist.

2. Edit $ENV_FILE and fill in:
     GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET,
     GATEWAY_PUBLIC_URL (your Cloudflare Tunnel hostname),
     CLOUDFLARE_TUNNEL_NAME, OAUTH_ALLOWED_EMAILS,
     COGGO_DB_PATH, R2_ACCOUNT_ID, R2_BUCKET,
     R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY
   OAUTH_ALLOWED_EMAILS is REQUIRED. Empty = everyone is rejected.

3. Restore an existing Coggo DB from R2 before first boot (skip for a fresh DB):
     source $ENV_FILE
     mkdir -p "\$(dirname "\$COGGO_DB_PATH")"
     litestream restore -o "\$COGGO_DB_PATH" -config scripts/litestream.yml
   Do this before starting the boot script so Coggo does not create an empty DB.

4. Mint a Coggo bearer token, then add the printed secret to COGGO_TOKEN in $ENV_FILE:
     $APP_BIN_DIR/coggo token create --all --label termux-gateway

5. Run the boot script once to bring everything up now (or reboot):
     ~/.termux/boot/30-coggo
     tail -f ~/.coggo/logs/*.log

6. In claude.ai (or ChatGPT) custom connector, point at:
     \$GATEWAY_PUBLIC_URL/mcp

Logs: ~/.coggo/logs/   PIDs: ~/.coggo/run/

To stop everything:
   for f in ~/.coggo/run/*.pid; do kill "\$(cat \$f)" 2>/dev/null; done
EOF
