# Backup and replication

Coggo has two backup surfaces:

- **Data DB:** `~/.local/share/coggo/coggo.db` stores events, entities, relationships, embeddings, type definitions, and bearer token hashes.
- **Hosted peer identities:** `~/.local/share/coggo/peers.json` stores peer names, DIDs, settings, and private keys.

The DB is safe to replicate continuously with Litestream. The identity file is more sensitive: if someone gets `peers.json`, they can host/sign as those peers in future versions. Back it up separately, store it encrypted, and restore it alongside the DB when migrating machines.

The recommended DB setup is **continuous replication via [Litestream](https://litestream.io) to [Cloudflare R2](https://developers.cloudflare.com/r2/)**: 10 GB free, no egress charges, S3-compatible API. Litestream snapshots the DB and streams WAL frames as they're written; DB recovery is one command from anywhere.

## Why this stack

- **R2 free tier is genuinely free** — 10 GB storage forever, zero egress (most providers charge per GB out, which makes restore expensive). A v0.1 Coggo DB is in the MB range, so 10 GB is effectively unbounded.
- **Litestream's continuous mode means RPO is seconds, not hours.** A snapshot+WAL stream keeps the remote copy within ~1 second of live.
- **DB restore is the same command as migration.** `litestream restore -o new.db s3://coggo-replica/coggo` works whether you're recovering from corruption or moving the DB to a new machine.
- **Identity backup stays deliberate.** `peers.json` contains private keys, so Coggo does not silently upload it to R2 with the DB.

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

### 3. Export hosted peer identities

On the machine that currently hosts your peers:

```bash
coggo backup identity export ~/coggo-peers.json
```

The export is a direct `peers.json` backup written with mode `0600`. It contains peer private keys. Store it in a password manager, encrypted archive, or another secret storage system. Do not put it in git and do not upload it next to the Litestream DB unless that storage is encrypted separately.

Use `--force` if you intentionally want to overwrite an existing export:

```bash
coggo backup identity export --force ~/coggo-peers.json
```

### 4. Restart

```bash
~/.termux/boot/30-coggo
tail -f ~/.coggo/logs/litestream.log
```

You should see `replication started` and periodic `wrote snapshot` / `wrote wal segment` entries. The remote bucket starts populating immediately.

## Restoring from backup

Restore both parts before first boot on a new host:

1. Restore `peers.json` from your identity backup.
2. Restore `coggo.db` from Litestream/R2.

If you restore only the DB, `coggo peer list` will show no peers because the hosted identities are missing.

### Restore identities

Put the identity export back in the same data directory as the DB:

```bash
coggo backup identity import ~/coggo-peers.json
```

The import writes `peers.json` with mode `0600` and refuses to overwrite an existing local registry unless you pass `--force`. If you use a non-default config path or data dir, pass the same `--config` value you use for `coggo serve`.

```bash
coggo backup identity import --force ~/coggo-peers.json
```

### Restore the DB

From any machine with `litestream` installed and the same R2 credentials:

```bash
cd /path/to/coggo
source .env       # variables are declared with `export`, so this is enough

litestream restore \
  -config scripts/litestream.yml \
  -o /tmp/coggo-restored.db \
  "$COGGO_DB_PATH"
```

This pulls the latest snapshot + replays WAL frames to reconstruct the DB at the most recent state Litestream had time to ship. Point-in-time recovery (`-timestamp`) is also supported — useful if you need to roll back to before a bad write.
The final `"$COGGO_DB_PATH"` argument selects the database entry from `scripts/litestream.yml`; `-o` only controls where the restored file is written.

To restore in place on a running deployment:

```bash
# Stop coggo first.
for f in ~/.coggo/run/*.pid; do kill "$(cat $f)" 2>/dev/null; done

# Restore on top of the current path.
litestream restore -config ~/coggo/scripts/litestream.yml -o "$COGGO_DB_PATH" "$COGGO_DB_PATH"

# Bring everything back up.
~/.termux/boot/30-coggo
```

## What's NOT covered by this

- **The `peers.json` identity file.** Use `coggo backup identity export` and store the result encrypted. Litestream intentionally does not replicate peer private keys.
- **The `.env` file itself.** Put the R2 keys, Google OAuth client ID/secret, and `COGGO_TOKEN` in your password manager. They're not derived from the DB and Litestream doesn't replicate them.
- **The Tailscale state.** Each device has its own tailnet identity — this is by design, not something to back up.
- **New peers not yet exported.** If you `coggo peer add`, run `coggo backup identity export --force <path>` again. Litestream will capture the peer's DB data, but it will not capture the new private key in `peers.json`.

## Cost reality check

A v0.1 Coggo DB grows by maybe a few MB per month under normal use. Litestream's snapshots + WAL deltas are well under that. You will sit comfortably inside R2's 10 GB free tier indefinitely. If you ever exceed it, R2 storage is $0.015/GB/month — moving to a 100 GB DB would cost ~$1.50/month with no egress charges.
