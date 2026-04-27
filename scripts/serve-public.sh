#!/usr/bin/env bash
# Run coggo + coggo-oauth-gateway + Tailscale Funnel together. Funnel exposes
# the gateway (which speaks OAuth) so claude.ai can reach it. Coggo proper
# stays on localhost behind the gateway.
#
# Cleanup on Ctrl-C: stop both processes, reset funnel.
#
# bash 3.2 compatible (macOS default) — no `wait -n`, no associative arrays.

set -euo pipefail

COGGO_PORT="${1:-6177}"
GATEWAY_PORT="${2:-8080}"
COGGO_BIN="${COGGO_BIN:-./coggo}"
GATEWAY_BIN="${GATEWAY_BIN:-./coggo-oauth-gateway}"

# Auto-load .env from the repo root if present — same convention as Termux.
# Variables in .env use `export` so plain sourcing is enough.
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
if [ -f "$REPO_ROOT/.env" ]; then
    # shellcheck disable=SC1091
    . "$REPO_ROOT/.env"
fi

# Required env
required="COGGO_TOKEN GOOGLE_CLIENT_ID GOOGLE_CLIENT_SECRET"
missing=""
for v in $required; do
    if [ -z "${!v:-}" ]; then
        missing="$missing $v"
    fi
done
if [ -n "$missing" ]; then
    echo "missing required env:$missing" >&2
    echo >&2
    echo "set these and re-run. e.g.:" >&2
    echo "  export COGGO_TOKEN=\$(coggo token create --all --label claude-ai-mobile | awk '/secret:/ {print \$2}')" >&2
    echo "  export GOOGLE_CLIENT_ID=...apps.googleusercontent.com" >&2
    echo "  export GOOGLE_CLIENT_SECRET=GOCSPX-..." >&2
    exit 1
fi

if ! command -v tailscale >/dev/null 2>&1; then
    echo "tailscale not found — install it (https://tailscale.com/download)" >&2
    exit 1
fi

if [ ! -x "$COGGO_BIN" ] || [ ! -x "$GATEWAY_BIN" ]; then
    echo "coggo or coggo-oauth-gateway binary missing — run 'make build build-gateway' first" >&2
    exit 1
fi

# Logs go here so we can show them if a process dies unexpectedly.
LOG_DIR="$(mktemp -d -t coggo-serve-public-XXXXXX)"
COGGO_LOG="$LOG_DIR/coggo.log"
GATEWAY_LOG="$LOG_DIR/gateway.log"
COGGO_PID=""
GATEWAY_PID=""

cleanup() {
    trap - EXIT INT TERM
    echo
    echo "shutting down..."
    if [ -n "$GATEWAY_PID" ] && kill -0 "$GATEWAY_PID" 2>/dev/null; then
        kill -TERM "$GATEWAY_PID" 2>/dev/null || true
        wait "$GATEWAY_PID" 2>/dev/null || true
    fi
    if [ -n "$COGGO_PID" ] && kill -0 "$COGGO_PID" 2>/dev/null; then
        kill -TERM "$COGGO_PID" 2>/dev/null || true
        wait "$COGGO_PID" 2>/dev/null || true
    fi
    echo "resetting funnel..."
    tailscale funnel reset 2>/dev/null || true
    echo "logs preserved at: $LOG_DIR"
}
trap cleanup EXIT INT TERM

dump_log_on_death() {
    local name="$1"
    local log="$2"
    echo
    echo "=== $name died unexpectedly. last 30 lines of its log: ==="
    tail -30 "$log" 2>/dev/null || echo "(no log)"
    echo "==="
}

# 1. Start coggo on localhost (no funnel — it's the upstream).
echo "[1/3] starting coggo on localhost:$COGGO_PORT (log: $COGGO_LOG)..."
"$COGGO_BIN" serve >"$COGGO_LOG" 2>&1 &
COGGO_PID=$!
echo "      coggo pid=$COGGO_PID"

# Wait for coggo to actually bind. Up to ~5 seconds.
ready=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
    if curl -fsS "http://localhost:$COGGO_PORT/mcp" >/dev/null 2>&1; then
        ready=1; break
    fi
    # Even a 401 from coggo means it's listening — accept any HTTP response.
    if curl -fsSI "http://localhost:$COGGO_PORT/mcp" >/dev/null 2>&1; then
        ready=1; break
    fi
    code="$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$COGGO_PORT/mcp" 2>/dev/null || echo 000)"
    if [ "$code" != "000" ]; then
        ready=1; break
    fi
    if ! kill -0 "$COGGO_PID" 2>/dev/null; then
        dump_log_on_death "coggo" "$COGGO_LOG"
        exit 1
    fi
    sleep 0.5
done
if [ "$ready" -ne 1 ]; then
    echo "coggo did not become reachable on localhost:$COGGO_PORT within 5s" >&2
    dump_log_on_death "coggo" "$COGGO_LOG"
    exit 1
fi
echo "      coggo ready"

# 2. Start funnel against the gateway port.
echo "[2/3] starting Tailscale Funnel for gateway port $GATEWAY_PORT..."
if ! tailscale funnel --bg "$GATEWAY_PORT"; then
    echo "funnel failed — check 'tailscale status' and ACLs" >&2
    exit 1
fi

PUBLIC_URL="$(tailscale funnel status 2>/dev/null | sed -n 's|^\(https://[^ ]*\).*|\1|p' | head -1 || true)"
if [ -z "$PUBLIC_URL" ]; then
    echo "could not auto-detect funnel URL; run 'tailscale funnel status' to see it" >&2
    exit 1
fi
PUBLIC_URL="${PUBLIC_URL%/}"
echo "      public URL: $PUBLIC_URL"

# 3. Start the gateway pointing at the public URL we just learned.
echo "[3/3] starting coggo-oauth-gateway on :$GATEWAY_PORT (log: $GATEWAY_LOG)..."
GATEWAY_PUBLIC_URL="$PUBLIC_URL" \
GATEWAY_LISTEN=":$GATEWAY_PORT" \
COGGO_UPSTREAM="http://localhost:$COGGO_PORT" \
    "$GATEWAY_BIN" >"$GATEWAY_LOG" 2>&1 &
GATEWAY_PID=$!
echo "      gateway pid=$GATEWAY_PID"

# Confirm the gateway came up before we tell the user it worked.
sleep 1
if ! kill -0 "$GATEWAY_PID" 2>/dev/null; then
    dump_log_on_death "coggo-oauth-gateway" "$GATEWAY_LOG"
    exit 1
fi

echo
echo "all up. claude.ai connector URL:"
echo "  $PUBLIC_URL/mcp"
echo
echo "watching coggo + gateway. Ctrl-C to stop both and reset funnel."

# Poll instead of `wait -n` (bash 3.2 compatible).
while kill -0 "$COGGO_PID" 2>/dev/null && kill -0 "$GATEWAY_PID" 2>/dev/null; do
    sleep 1
done

# One of them died on its own. Surface which and dump its log.
if ! kill -0 "$COGGO_PID" 2>/dev/null; then
    dump_log_on_death "coggo" "$COGGO_LOG"
fi
if ! kill -0 "$GATEWAY_PID" 2>/dev/null; then
    dump_log_on_death "coggo-oauth-gateway" "$GATEWAY_LOG"
fi
exit 1
