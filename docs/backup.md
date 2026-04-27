# Backup and replication

Coggo's substrate is a single SQLite file at `~/.local/share/coggo/coggo.db`. Everything — peer DIDs, private keys, entities, relationships, events, embeddings, hashed bearer tokens — lives there. Lose it, lose the substrate.

The recommended setup is **continuous replication via [Litestream](https://litestream.io) to [Cloudflare R2](https://developers.cloudflare.com/r2/)**: 10 GB free, no egress charges, S3-compatible API. Litestream snapshots the DB and streams WAL frames as they're written; recovery is one command from anywhere.

## Why this stack

- **R2 free tier is genuinely free** — 10 GB storage forever, zero egress (most providers charge per GB out, which makes restore expensive). A v0.1 Coggo DB is in the MB range, so 10 GB is effectively unbounded.
- **Litestream's continuous mode means RPO is seconds, not hours.** A snapshot+WAL stream keeps the remote copy within ~1 second of live.
- **Restore is the same command as migration.** `litestream restore -o new.db s3://coggo-replica/coggo` works whether you're recovering from corruption or moving the substrate to a new machine.

## One-time setup

### 1. Create an R2 bucket

In the Cloudflare dashboard:

1. **R2 → Create bucket** → name it `coggo-replica` (or anything; you'll put the name in `.env`).
2. **R2 → Manage R2 API Tokens → Create API Token**:
   - Permission: **Object Read & Write**
   - Specify bucket: the one you just created (least privilege)
3. Copy the **Access Key ID**, **Secret Access Key**, and your **Account ID** (top-right of the R2 dashboard, or in the token-creation panel).

### 2. Fill in `.env`

In the repo root `.env` (the same file the gateway reads from):

```bash
COGGO_DB_PATH=/data/data/com.termux/files/home/.local/share/coggo/coggo.db  # phone
# COGGO_DB_PATH=/Users/<you>/.local/share/coggo/coggo.db                    # laptop
R2_ACCOUNT_ID=<your-cloudflare-account-id>
R2_BUCKET=coggo-replica
R2_ACCESS_KEY_ID=<the-access-key>
R2_SECRET_ACCESS_KEY=<the-secret>
```

The boot launcher only starts Litestream if all four `R2_*` variables are non-empty. Leave any blank to disable replication cleanly.

### 3. Restart

```bash
~/.termux/boot/30-coggo
tail -f ~/.coggo/logs/litestream.log
```

You should see `replication started` and periodic `wrote snapshot` / `wrote wal segment` entries. The remote bucket starts populating immediately.

## Restoring from backup

From any machine with `litestream` installed and the same R2 credentials:

```bash
cd /path/to/coggo
source .env       # variables are declared with `export`, so this is enough

litestream restore \
  -o /tmp/coggo-restored.db \
  -config scripts/litestream.yml
```

This pulls the latest snapshot + replays WAL frames to reconstruct the DB at the most recent state Litestream had time to ship. Point-in-time recovery (`-timestamp`) is also supported — useful if you need to roll back to before a bad write.

To restore in place on a running deployment:

```bash
# Stop coggo first.
for f in ~/.coggo/run/*.pid; do kill "$(cat $f)" 2>/dev/null; done

# Restore on top of the current path.
litestream restore -o "$COGGO_DB_PATH" -config ~/coggo/scripts/litestream.yml

# Bring everything back up.
~/.termux/boot/30-coggo
```

## What's NOT covered by this

- **The `.env` file itself.** Put the R2 keys, Google OAuth client ID/secret, and `COGGO_TOKEN` in your password manager. They're not derived from the DB and Litestream doesn't replicate them.
- **The Tailscale state.** Each device has its own tailnet identity — this is by design, not something to back up.
- **Keys for peers you've added but haven't yet checkpointed.** If you `coggo peer add` and the WAL hasn't shipped yet, that peer is at risk for the next ~1 second. For practical purposes this is fine; for paranoid setups, force a checkpoint with `litestream snapshots` after sensitive operations.

## Cost reality check

A v0.1 Coggo DB grows by maybe a few MB per month under normal use. Litestream's snapshots + WAL deltas are well under that. You will sit comfortably inside R2's 10 GB free tier indefinitely. If you ever exceed it, R2 storage is $0.015/GB/month — moving to a 100 GB DB would cost ~$1.50/month with no egress charges.
