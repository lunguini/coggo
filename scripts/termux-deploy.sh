#!/data/data/com.termux/files/usr/bin/bash
# termux-deploy.sh — bootstrap Coggo on Android via Termux.
#
# What this does (idempotent — safe to re-run):
#   1. Verifies we're running inside Termux.
#   2. Installs required Termux packages (golang, git, clang, tailscale,
#      termux-services, termux-api, openssh).
#   3. Builds ./coggo (CGO, sqlite needs clang) and ./coggo-oauth-gateway.
#   4. Installs both binaries to $PREFIX/bin.
#   5. Drops an env-file template at ~/.coggo/gateway.env you must fill in.
#   6. Installs a Termux:Boot launcher at ~/.termux/boot/30-coggo so the
#      whole stack comes back up after reboot.
#   7. Prints next steps (tailscale up, fill env, reboot).
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

# --- 2. packages -------------------------------------------------------------

echo "==> installing Termux packages..."
pkg update -y >/dev/null
# clang: required for CGO sqlite. tailscale: userspace-mode daemon.
# termux-services: sv-style supervisor (not strictly needed since we use
# Termux:Boot, but useful for `sv restart coggo` etc).
pkg install -y \
    golang git clang make \
    tailscale \
    termux-services \
    termux-api \
    openssh \
    jq curl

# --- 3. build ----------------------------------------------------------------

echo
echo "==> building binaries (this takes ~1-2 min on a phone)..."
# Use the Makefile so we pick up VERSION + LDFLAGS consistently.
make build build-gateway

echo
echo "==> installing to \$PREFIX/bin..."
install -m 0755 ./coggo                "$PREFIX/bin/coggo"
install -m 0755 ./coggo-oauth-gateway  "$PREFIX/bin/coggo-oauth-gateway"

# --- 4. config + env template -----------------------------------------------

mkdir -p "$HOME/.coggo"
ENV_FILE="$HOME/.coggo/gateway.env"
if [ ! -f "$ENV_FILE" ]; then
    echo
    echo "==> writing env template at $ENV_FILE"
    cat > "$ENV_FILE" <<'EOF'
# Coggo OAuth gateway environment.
#
# Fill these in, then run the boot launcher (or reboot the phone if Termux:Boot
# is installed). Lines starting with # are ignored.
#
# Required:

# Bearer token coggo trusts. Mint with:
#   coggo token create --all --label termux-gateway
COGGO_TOKEN=

# Google OAuth client (Web application) from console.cloud.google.com.
# Authorized redirect URI must be: $GATEWAY_PUBLIC_URL/oauth/callback
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=

# Public URL the gateway is reachable at. With Tailscale Funnel this is
# your phone's tailnet hostname, e.g. https://pixel-8.tail3b1f7.ts.net
GATEWAY_PUBLIC_URL=

# FAIL-CLOSED EMAIL ALLOWLIST. Comma-separated Google account emails
# permitted to call /mcp. If empty, EVERY request is rejected — so set this
# before exposing the gateway. Example:
#   OAUTH_ALLOWED_EMAILS=you@gmail.com,partner@example.com
OAUTH_ALLOWED_EMAILS=

# Optional:
# OAUTH_ALLOWED_CLIENT_DOMAINS=claude.ai,claude.com,chatgpt.com,openai.com
# COGGO_LOG_LEVEL=info
# GATEWAY_LISTEN=:8080
# COGGO_UPSTREAM=http://localhost:6177

# Rate limits (defaults shown — tune if you see 429s in normal use):
# RATE_GLOBAL_RPS=50         # global token-bucket across all endpoints
# RATE_GLOBAL_BURST=100
# RATE_PER_EMAIL_RPM=10      # per authenticated email on /mcp
# RATE_PER_EMAIL_BURST=30
EOF
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
#   - tailscaled in userspace mode (Funnel needs to drive this from CLI)
#   - coggo serve
#   - coggo-oauth-gateway
#   - tailscale funnel pointing at the gateway port
#
# Re-running is safe: each step checks for an existing PID first.

set -u

PREFIX="/data/data/com.termux/files/usr"
HOME_DIR="/data/data/com.termux/files/home"
LOG_DIR="$HOME_DIR/.coggo/logs"
RUN_DIR="$HOME_DIR/.coggo/run"
ENV_FILE="$HOME_DIR/.coggo/gateway.env"

mkdir -p "$LOG_DIR" "$RUN_DIR"

ts() { date '+%Y-%m-%dT%H:%M:%S'; }
log() { echo "[$(ts)] $*" >> "$LOG_DIR/boot.log"; }

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
    nohup "$@" >> "$LOG_DIR/$name.log" 2>&1 &
    echo $! > "$pidfile"
    sleep 1
    if ! kill -0 "$(cat "$pidfile")" 2>/dev/null; then
        log "$name failed to stay up — see $LOG_DIR/$name.log"
        return 1
    fi
}

# 1. tailscaled in userspace mode. Required so Funnel CLI works from Termux —
#    the Android Tailscale app is a system VPN and is not driveable from here.
start_if_down tailscaled \
    "$PREFIX/bin/tailscaled" --tun=userspace-networking \
        --state="$HOME_DIR/.coggo/tailscaled.state" \
        --socket="$RUN_DIR/tailscaled.sock"

# Tailscale CLI needs to know which socket to talk to.
export TS_SOCKET="$RUN_DIR/tailscaled.sock"
TAILSCALE="$PREFIX/bin/tailscale --socket=$TS_SOCKET"

# Wait for tailscaled to be ready (up to 15s).
for _ in $(seq 1 15); do
    if $TAILSCALE status >/dev/null 2>&1; then break; fi
    sleep 1
done

# 2. Coggo + gateway. Source env file so $COGGO_TOKEN etc are exported.
if [ ! -f "$ENV_FILE" ]; then
    log "missing $ENV_FILE — refusing to start gateway"
    exit 1
fi
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

start_if_down coggo "$PREFIX/bin/coggo" serve

# Wait for coggo's MCP port before launching the gateway.
COGGO_PORT="${COGGO_PORT:-6177}"
for _ in $(seq 1 15); do
    if "$PREFIX/bin/curl" -s -o /dev/null -w '%{http_code}' \
        "http://localhost:$COGGO_PORT/mcp" 2>/dev/null | grep -qE '^[0-9]+$'; then
        break
    fi
    sleep 1
done

start_if_down gateway "$PREFIX/bin/coggo-oauth-gateway"

# 3. Funnel — exposes the gateway port publicly via your tailnet hostname.
GATEWAY_PORT="${GATEWAY_PORT:-8080}"
# Strip any leading colon so 'tailscale funnel' gets a bare port.
GATEWAY_PORT="${GATEWAY_PORT#:}"
log "enabling Tailscale Funnel on port $GATEWAY_PORT"
$TAILSCALE funnel --bg "$GATEWAY_PORT" >> "$LOG_DIR/funnel.log" 2>&1 || \
    log "funnel failed — run '$TAILSCALE up' interactively first?"

log "boot sequence complete"
BOOT_EOF
chmod 700 "$BOOT_SCRIPT"

# --- 6. next steps -----------------------------------------------------------

cat <<EOF

==> done.

Next steps (do these once, in order):

1. Authenticate Tailscale (interactive, one-time):
     tailscaled --tun=userspace-networking --state=\$HOME/.coggo/tailscaled.state &
     tailscale --socket=\$HOME/.coggo/run/tailscaled.sock up
   Follow the URL it prints, log in, then 'kill %1' when done.

2. Mint a Coggo bearer token:
     coggo token create --all --label termux-gateway

3. Edit $ENV_FILE and fill in:
     COGGO_TOKEN, GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET,
     GATEWAY_PUBLIC_URL (your tailnet HTTPS hostname),
     OAUTH_ALLOWED_EMAILS  <-- REQUIRED. Empty = everyone is rejected.

4. Run the boot script once to bring everything up now (or reboot):
     ~/.termux/boot/30-coggo
     tail -f ~/.coggo/logs/*.log

5. In claude.ai (or ChatGPT) custom connector, point at:
     \$GATEWAY_PUBLIC_URL/mcp

Logs: ~/.coggo/logs/   PIDs: ~/.coggo/run/

To stop everything:
   for f in ~/.coggo/run/*.pid; do kill "\$(cat \$f)" 2>/dev/null; done
   tailscale --socket=\$HOME/.coggo/run/tailscaled.sock funnel reset
EOF
