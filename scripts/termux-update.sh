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
#   3. Stops the running coggo / gateway / cloudflared processes
#   4. Re-runs the boot launcher to bring everything back up
#   5. Tails the last 20 lines of each log so you can see it landed
#
# Failure modes are deliberate:
#   - Local uncommitted changes => abort (don't silently lose work)
#   - Pull is non-fast-forward  => abort (manual reconciliation needed)
#   - Build fails               => stops before touching running services

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOME_DIR="${HOME:-/data/data/com.termux/files/home}"
RUN_DIR="$HOME_DIR/.coggo/run"
LOG_DIR="$HOME_DIR/.coggo/logs"
BOOT_SCRIPT="$HOME_DIR/.termux/boot/30-coggo"

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

# --- 3. stop running services ----------------------------------------------

echo
echo "==> stopping running services..."
if [ -d "$RUN_DIR" ]; then
    for f in "$RUN_DIR"/*.pid; do
        [ -e "$f" ] || continue
        pid="$(cat "$f" 2>/dev/null || true)"
        name="$(basename "$f" .pid)"
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            echo "    kill $name (pid $pid)"
            kill -TERM "$pid" 2>/dev/null || true
            # Give it 3s to exit cleanly before SIGKILL.
            for _ in 1 2 3; do
                kill -0 "$pid" 2>/dev/null || break
                sleep 1
            done
            kill -KILL "$pid" 2>/dev/null || true
        fi
        rm -f "$f"
    done
fi

# --- 4. restart via boot launcher ------------------------------------------

if [ ! -x "$BOOT_SCRIPT" ]; then
    echo "boot launcher missing at $BOOT_SCRIPT — did termux-deploy.sh succeed?" >&2
    exit 1
fi

echo
echo "==> restarting via $BOOT_SCRIPT..."
"$BOOT_SCRIPT"

# Give services a moment to bind before we tail logs.
sleep 2

# --- 5. show recent logs so the operator can see it worked ------------------

echo
echo "==> recent logs:"
for log in coggo.log gateway.log cloudflared.log boot.log; do
    if [ -f "$LOG_DIR/$log" ]; then
        echo
        echo "--- $log (last 20 lines) ---"
        tail -20 "$LOG_DIR/$log"
    fi
done

echo
echo "==> update complete. running services:"
for f in "$RUN_DIR"/*.pid; do
    [ -e "$f" ] || continue
    name="$(basename "$f" .pid)"
    pid="$(cat "$f")"
    if kill -0 "$pid" 2>/dev/null; then
        echo "    $name: pid $pid (running)"
    else
        echo "    $name: pid $pid (DEAD — check $LOG_DIR/$name.log)"
    fi
done
