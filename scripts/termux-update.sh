#!/data/data/com.termux/files/usr/bin/bash
# termux-update.sh — pull latest code, rebuild, restart on Termux.
#
# Designed to be safe to run remotely over any shell you already have into
# the phone:
#   ssh phone bash ~/coggo/scripts/termux-update.sh
#
# What this does:
#   1. git pull (fast-forward only — refuses to merge or rebase)
#   2. Re-runs termux-deploy.sh (idempotent: rebuilds, reinstalls via GOBIN)
#   3. Re-runs the Termux:Boot launcher to enable configured runit services
#   4. Restarts enabled runit services with sv
#   5. Tails the last 20 lines of each runit log so you can see it landed
#
# Failure modes are deliberate:
#   - Local uncommitted changes => abort (don't silently lose work)
#   - Pull is non-fast-forward  => abort (manual reconciliation needed)
#   - Build fails               => stops before touching running services

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOME_DIR="${HOME:-/data/data/com.termux/files/home}"
LOG_DIR="$HOME_DIR/.coggo/logs"
BOOT_SCRIPT="$HOME_DIR/.termux/boot/30-coggo"
SERVICE_DIR="${PREFIX:-/data/data/com.termux/files/usr}/var/service"
SERVICE_LOG_DIR="${PREFIX:-/data/data/com.termux/files/usr}/var/log/sv"
SERVICE_NAMES="coggo coggo-gateway coggo-litestream coggo-cloudflared"

cd "$REPO_ROOT"

# --- 1. pull ----------------------------------------------------------------

if [ -n "$(git status --porcelain)" ]; then
    echo "refusing to update: working tree has uncommitted changes" >&2
    git status --short >&2
    exit 1
fi

echo "==> git pull (fast-forward only)..."
git pull --ff-only

# --- 2. rebuild + reinstall (idempotent) ------------------------------------

echo
echo "==> rebuilding via termux-deploy.sh..."
bash "$REPO_ROOT/scripts/termux-deploy.sh"

# --- 3. enable configured services ------------------------------------------

echo
if [ ! -x "$BOOT_SCRIPT" ]; then
    echo "boot launcher missing at $BOOT_SCRIPT — did termux-deploy.sh succeed?" >&2
    exit 1
fi

echo "==> applying service enablement via $BOOT_SCRIPT..."
"$BOOT_SCRIPT"

# --- 4. restart enabled runit services --------------------------------------

if [ -f "${PREFIX:-/data/data/com.termux/files/usr}/etc/profile.d/start-services.sh" ]; then
    # shellcheck disable=SC1090
    . "${PREFIX:-/data/data/com.termux/files/usr}/etc/profile.d/start-services.sh"
fi

echo
echo "==> restarting enabled services with sv..."
for svc in $SERVICE_NAMES; do
    if [ ! -d "$SERVICE_DIR/$svc" ]; then
        echo "    $svc: missing service directory"
        continue
    fi
    if [ -f "$SERVICE_DIR/$svc/down" ]; then
        echo "    $svc: disabled"
        continue
    fi
    echo "    sv restart $svc"
    sv restart "$svc" >/dev/null 2>&1 || sv up "$svc" >/dev/null 2>&1 || true
done

sleep 2

# --- 5. show recent logs so the operator can see it worked ------------------

echo
echo "==> recent logs:"
for svc in $SERVICE_NAMES; do
    if [ -f "$SERVICE_LOG_DIR/$svc/current" ]; then
        echo
        echo "--- $svc (last 20 lines) ---"
        tail -20 "$SERVICE_LOG_DIR/$svc/current"
    fi
done
if [ -f "$LOG_DIR/boot.log" ]; then
    echo
    echo "--- boot.log (last 20 lines) ---"
    tail -20 "$LOG_DIR/boot.log"
fi

echo
echo "==> update complete. running services:"
for svc in $SERVICE_NAMES; do
    if [ ! -d "$SERVICE_DIR/$svc" ]; then
        continue
    fi
    sv status "$svc" 2>/dev/null || true
done
