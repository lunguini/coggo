#!/usr/bin/env bash
# Run coggo serve and expose it via Tailscale Funnel. Cleans up the funnel on
# any exit (Ctrl-C, SIGTERM, normal). Owns its own process group so signal
# forwarding doesn't get tangled when invoked from `make`.

set -euo pipefail

PORT="${1:-6177}"
COGGO="${COGGO:-./coggo}"

if ! command -v tailscale >/dev/null 2>&1; then
    echo "tailscale not found — install it (https://tailscale.com/download) or use 'make dev' for local-only" >&2
    exit 1
fi

if [[ ! -x "$COGGO" ]]; then
    echo "coggo binary not found at $COGGO — run 'make build' first" >&2
    exit 1
fi

echo "starting Tailscale Funnel for port $PORT..."
if ! tailscale funnel --bg "$PORT"; then
    echo "funnel failed — check 'tailscale status' and ACLs" >&2
    exit 1
fi

URL="$(tailscale funnel status 2>/dev/null | sed -n 's|^\(https://[^ ]*\).*|\1|p' | head -1 || true)"
if [[ -n "$URL" ]]; then
    echo "funnel up. URL: $URL"
else
    echo "funnel up (could not auto-detect URL; run 'tailscale funnel status')"
fi

cleanup() {
    trap - EXIT INT TERM
    echo
    if [[ -n "${COGGO_PID:-}" ]] && kill -0 "$COGGO_PID" 2>/dev/null; then
        echo "stopping coggo..."
        kill -TERM "$COGGO_PID" 2>/dev/null || true
        wait "$COGGO_PID" 2>/dev/null || true
    fi
    echo "resetting funnel..."
    tailscale funnel reset 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Start coggo in the background so this script keeps the foreground and
# can run `cleanup` on Ctrl-C without depending on tty signal forwarding.
"$COGGO" serve &
COGGO_PID=$!

wait "$COGGO_PID"
