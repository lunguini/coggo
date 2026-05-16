#!/data/data/com.termux/files/usr/bin/bash
# termux-deploy.sh — bootstrap Coggo on Android via Termux.
#
# What this does (idempotent — safe to re-run):
#   1. Verifies we're running inside Termux.
#   2. Installs required Termux packages (golang, git,
#      termux-services, termux-api, openssh, cloudflared).
#   3. Builds and installs coggo + coggo-oauth-gateway via make install-all.
#   4. Installs Go-built binaries to $GOBIN (or go env GOPATH/bin).
#   5. Drops an env-file template at ~/.coggo/env you must fill in.
#   6. Installs runit services via termux-services and a Termux:Boot launcher
#      that enables the right services after reboot.
#   7. Prints next steps (Cloudflare Tunnel setup, fill env, service control).
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

detect_shell_rc() {
    case "$(basename "${SHELL:-bash}")" in
        zsh) printf '%s\n' "$HOME/.zshrc" ;;
        bash|sh) printf '%s\n' "$HOME/.bashrc" ;;
        *) printf '%s\n' "$HOME/.profile" ;;
    esac
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
# Termux runtime env lives beside logs/run state, outside the source tree.
# Single source of truth for the template remains the committed .env.example.
ENV_FILE="$HOME/.coggo/env"
ENV_EXAMPLE="$REPO_ROOT/.env.example"
SHELL_RC="$(detect_shell_rc)"
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

# --- 5. runit services + boot launcher ---------------------------------------

SERVICE_DIR="$PREFIX/var/service"
SERVICE_LOG_DIR="$PREFIX/var/log/sv"

mkdir -p "$SERVICE_DIR" "$SERVICE_LOG_DIR"

install_runit_service() {
    local name="$1"
    local command_block="$2"
    local svc_dir="$SERVICE_DIR/$name"
    mkdir -p "$svc_dir/log" "$SERVICE_LOG_DIR/$name"
    cat > "$svc_dir/run" <<SERVICE_EOF
#!/data/data/com.termux/files/usr/bin/bash
set -euo pipefail

PREFIX="/data/data/com.termux/files/usr"
HOME_DIR="/data/data/com.termux/files/home"
ENV_FILE="\$HOME_DIR/.coggo/env"

resolve_app_bin_dir() {
    local dir
    dir="\${GOBIN:-}"
    if [ -z "\$dir" ] && [ -x "\$PREFIX/bin/go" ]; then
        dir="\$("\$PREFIX/bin/go" env GOBIN 2>/dev/null || true)"
    fi
    if [ -z "\$dir" ] && [ -x "\$PREFIX/bin/go" ]; then
        dir="\$("\$PREFIX/bin/go" env GOPATH)/bin"
    fi
    if [ -z "\$dir" ]; then
        dir="\$HOME_DIR/go/bin"
    fi
    printf '%s\n' "\$dir"
}

if [ ! -f "\$ENV_FILE" ]; then
    echo "missing \$ENV_FILE"
    sleep 30
    exit 1
fi

# shellcheck disable=SC1090
. "\$ENV_FILE"

APP_BIN_DIR="\$(resolve_app_bin_dir)"
export PATH="\$APP_BIN_DIR:\$PREFIX/bin:\${PATH:-}"

exec 2>&1
$command_block
SERVICE_EOF
    chmod 700 "$svc_dir/run"
    cat > "$svc_dir/log/run" <<LOG_EOF
#!/data/data/com.termux/files/usr/bin/sh
exec svlogd -tt "$SERVICE_LOG_DIR/$name"
LOG_EOF
    chmod 700 "$svc_dir/log/run"
    # Services stay disabled until Termux:Boot or the operator enables them
    # after env/identity/DB restore is complete.
    touch "$svc_dir/down"
}

echo "==> installing runit services under $SERVICE_DIR"
install_runit_service "coggo" 'exec "$APP_BIN_DIR/coggo" serve'
install_runit_service "coggo-gateway" 'exec "$APP_BIN_DIR/coggo-oauth-gateway"'
install_runit_service "coggo-litestream" 'exec "$APP_BIN_DIR/litestream" replicate -config "$HOME_DIR/coggo/scripts/litestream.yml"'
install_runit_service "coggo-cloudflared" 'exec "$PREFIX/bin/cloudflared" tunnel run "$CLOUDFLARE_TUNNEL_NAME"'

BOOT_DIR="$HOME/.termux/boot"
mkdir -p "$BOOT_DIR"
BOOT_SCRIPT="$BOOT_DIR/30-coggo"

echo "==> installing boot launcher at $BOOT_SCRIPT"
# Uses absolute path inside the script (Termux:Boot runs without a real
# shell environment). Logs go to ~/.coggo/logs/.
cat > "$BOOT_SCRIPT" <<'BOOT_EOF'
#!/data/data/com.termux/files/usr/bin/bash
# Termux:Boot launcher for Coggo. Starts termux-services' runit supervisor and
# enables the Coggo service set whose env prerequisites are present.

set -u

PREFIX="/data/data/com.termux/files/usr"
HOME_DIR="/data/data/com.termux/files/home"
LOG_DIR="$HOME_DIR/.coggo/logs"
ENV_FILE="$HOME_DIR/.coggo/env"
SERVICE_DIR="$PREFIX/var/service"

mkdir -p "$LOG_DIR"

ts() { date '+%Y-%m-%dT%H:%M:%S'; }
log() { echo "[$(ts)] $*" >> "$LOG_DIR/boot.log"; }

export PATH="$PREFIX/bin:${PATH:-}"

"$PREFIX/bin/termux-wake-lock" || log "termux-wake-lock failed (Termux:API not installed?)"

if [ -f "$PREFIX/etc/profile.d/start-services.sh" ]; then
    # shellcheck disable=SC1090
    . "$PREFIX/etc/profile.d/start-services.sh"
else
    log "missing start-services.sh — pkg install termux-services"
fi

if [ ! -f "$ENV_FILE" ]; then
    log "missing $ENV_FILE — refusing to enable Coggo services"
    exit 1
fi
# shellcheck disable=SC1090
. "$ENV_FILE"

enable_service() {
    local name="$1"
    if [ ! -d "$SERVICE_DIR/$name" ]; then
        log "service $name is missing under $SERVICE_DIR"
        return 1
    fi
    log "enabling $name"
    if command -v sv-enable >/dev/null 2>&1; then
        sv-enable "$name" >> "$LOG_DIR/boot.log" 2>&1 || sv up "$name" >> "$LOG_DIR/boot.log" 2>&1 || true
    else
        rm -f "$SERVICE_DIR/$name/down"
        sv up "$name" >> "$LOG_DIR/boot.log" 2>&1 || true
    fi
}

disable_service() {
    local name="$1"
    if [ ! -d "$SERVICE_DIR/$name" ]; then
        return 0
    fi
    log "disabling $name"
    if command -v sv-disable >/dev/null 2>&1; then
        sv-disable "$name" >> "$LOG_DIR/boot.log" 2>&1 || true
    else
        touch "$SERVICE_DIR/$name/down"
        sv down "$name" >> "$LOG_DIR/boot.log" 2>&1 || true
    fi
}

enable_service coggo

if [ -n "${COGGO_TOKEN:-}" ] && [ -n "${GOOGLE_CLIENT_ID:-}" ] \
   && [ -n "${GOOGLE_CLIENT_SECRET:-}" ] && [ -n "${GATEWAY_PUBLIC_URL:-}" ] \
   && [ -n "${OAUTH_ALLOWED_EMAILS:-}" ]; then
    enable_service coggo-gateway
else
    disable_service coggo-gateway
    log "coggo-gateway disabled — gateway env vars are incomplete"
fi

if [ -n "${R2_ACCESS_KEY_ID:-}" ] && [ -n "${R2_SECRET_ACCESS_KEY:-}" ] \
   && [ -n "${R2_ACCOUNT_ID:-}" ] && [ -n "${R2_BUCKET:-}" ]; then
    enable_service coggo-litestream
else
    disable_service coggo-litestream
    log "coggo-litestream disabled — R2_* vars are incomplete"
fi

if [ -n "${CLOUDFLARE_TUNNEL_NAME:-}" ]; then
    enable_service coggo-cloudflared
else
    disable_service coggo-cloudflared
    log "coggo-cloudflared disabled — CLOUDFLARE_TUNNEL_NAME is not set"
fi

log "service enablement complete"
BOOT_EOF
chmod 700 "$BOOT_SCRIPT"

# --- 6. cloudflared config ---------------------------------------------------

CF_CONFIG="$HOME/.cloudflared/config.yml"
CF_CREDS=$(ls "$HOME/.cloudflared/"*.json 2>/dev/null | head -1)

write_cloudflared_config() {
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
}

if [ -n "$CF_CREDS" ]; then
    TUNNEL_UUID="$(basename "$CF_CREDS" .json)"
    # Read CLOUDFLARE_TUNNEL_NAME + GATEWAY_PORT from the Termux env file if set.
    # shellcheck disable=SC1090
    [ -f "$ENV_FILE" ] && . "$ENV_FILE"
    TUNNEL_NAME="${CLOUDFLARE_TUNNEL_NAME:-coggo}"
    GW_PORT="${GATEWAY_PORT:-8080}"

    if [ ! -f "$CF_CONFIG" ]; then
        echo
        echo "==> generating $CF_CONFIG"
        # GATEWAY_PUBLIC_URL must already be set in the env file — prompt if missing.
        if [ -z "${GATEWAY_PUBLIC_URL:-}" ]; then
            echo "    WARNING: GATEWAY_PUBLIC_URL is not set in $ENV_FILE"
            echo "    Edit $CF_CONFIG after filling that in."
            CF_HOSTNAME="<your-hostname>"
        else
            # Strip the scheme for the hostname field.
            CF_HOSTNAME="${GATEWAY_PUBLIC_URL#https://}"
            CF_HOSTNAME="${CF_HOSTNAME#http://}"
        fi
        write_cloudflared_config
    elif [ -n "${GATEWAY_PUBLIC_URL:-}" ]; then
        CF_HOSTNAME="${GATEWAY_PUBLIC_URL#https://}"
        CF_HOSTNAME="${CF_HOSTNAME#http://}"
        if ! grep -Fq "hostname: $CF_HOSTNAME" "$CF_CONFIG" \
           || ! grep -Fq "service: http://localhost:$GW_PORT" "$CF_CONFIG"; then
            echo
            echo "==> updating $CF_CONFIG for $CF_HOSTNAME -> localhost:$GW_PORT"
            cp "$CF_CONFIG" "$CF_CONFIG.bak.$(date +%Y%m%d%H%M%S)"
            write_cloudflared_config
        else
            echo "==> cloudflared config already matches $CF_HOSTNAME -> localhost:$GW_PORT"
        fi
    else
        echo "==> cloudflared config already exists at $CF_CONFIG (leaving it alone)"
    fi

    # Register the DNS route (idempotent).
    if [ -n "${GATEWAY_PUBLIC_URL:-}" ]; then
        CF_HOSTNAME="${GATEWAY_PUBLIC_URL#https://}"
        CF_HOSTNAME="${CF_HOSTNAME#http://}"
        echo "==> registering DNS route: $CF_HOSTNAME -> tunnel $TUNNEL_NAME"
        if ! route_output="$(cloudflared tunnel route dns "$TUNNEL_NAME" "$CF_HOSTNAME" 2>&1)"; then
            if printf '%s\n' "$route_output" | grep -qi "already exists"; then
                echo "==> DNS route already exists for $CF_HOSTNAME"
            else
                printf '%s\n' "$route_output" >&2
                exit 1
            fi
        elif [ -n "$route_output" ]; then
            printf '%s\n' "$route_output"
        fi
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
   Optional: make these values available in your interactive shell:
     printf '\n# Coggo\n[ -f "$ENV_FILE" ] && . "$ENV_FILE"\n' >> "$SHELL_RC"
     . "$SHELL_RC"

3. Restore hosted peer identities before first boot (skip for a fresh Coggo):
     # On the old host, first run:
     #   coggo backup identity export ~/coggo-peers.json
     # Transfer the file securely, then on Termux:
     source $ENV_FILE
     $APP_BIN_DIR/coggo backup identity import /path/to/coggo-peers.json
   This file contains peer private keys. Keep the exported copy encrypted.

4. Restore an existing Coggo DB from R2 before first boot (skip for a fresh DB):
     source $ENV_FILE
     mkdir -p "\$(dirname "\$COGGO_DB_PATH")"
     litestream restore -config scripts/litestream.yml -o "\$COGGO_DB_PATH" "\$COGGO_DB_PATH"
   Do this before starting the boot script so Coggo does not create an empty DB.

5. Mint a Coggo bearer token, then add the printed secret to COGGO_TOKEN in $ENV_FILE:
     $APP_BIN_DIR/coggo token create --all --label termux-gateway

6. Run the boot script once to enable configured services now (or reboot):
     ~/.termux/boot/30-coggo
     sv status coggo
     sv status coggo-gateway
     sv status coggo-litestream
     sv status coggo-cloudflared

7. In claude.ai (or ChatGPT) custom connector, point at:
     \$GATEWAY_PUBLIC_URL/mcp

Logs:
   ~/.coggo/logs/boot.log
   $PREFIX/var/log/sv/<service>/current

Service controls:
   sv status coggo
   sv restart coggo
   sv restart coggo-gateway
   sv restart coggo-litestream
   sv restart coggo-cloudflared
   sv down coggo-gateway
EOF
