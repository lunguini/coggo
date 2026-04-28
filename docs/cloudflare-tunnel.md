# Custom domain via Cloudflare Tunnel

Tailscale Funnel is the zero-config default and works fine — but it pins your public URL to `*.ts.net`. If you'd rather expose Coggo on a domain you own (e.g. `coggo.example.com`), use Cloudflare Tunnel instead.

The OAuth gateway, the rate limiter, the email allowlist — none of it changes. Only the layer that brings public traffic to localhost:8080 swaps out.

## Why this works

`cloudflared` opens an outbound connection from the phone to Cloudflare's edge. Cloudflare terminates TLS at the edge and forwards plaintext over that tunnel to `localhost:$GATEWAY_PORT`. No inbound port, no router config, no static IP. Cloudflare DNS handles the hostname — you just point a CNAME at the tunnel.

Tradeoffs vs Tailscale Funnel:

- ✅ Stable hostname on a domain you control. Survives Tailscale account changes.
- ✅ Cloudflare's edge sits in front (DDoS protection, optional WAF rules, request analytics).
- ✅ No Funnel quotas or `*.ts.net` quirks.
- ⚠️ One more daemon on the phone (`cloudflared`).
- ⚠️ Cloudflare sees decrypted requests at their edge. The gateway still validates Google OAuth tokens server-side, so there's no auth bypass — but Cloudflare's edge has visibility into request bodies. Worth knowing.
- ⚠️ Tunnel credentials (`~/.cloudflared/<uuid>.json`) are a secret. Litestream replicates the SQLite DB only — back this file up separately.

Tailscale itself stays running for SSH (`tailscale up --ssh`). You only stop using its Funnel feature.

## One-time setup

Do this from the phone (Termux). All `cloudflared` state lives in `~/.cloudflared/` and is per-device.

### 1. Install cloudflared

`scripts/termux-deploy.sh` already installs it via `pkg install cloudflared`. If you're setting up by hand:

```bash
pkg install -y cloudflared
```

### 2. Authenticate

```bash
cloudflared tunnel login
```

Opens a URL — log in to Cloudflare, pick the zone (`example.com`), and confirm. This drops a `cert.pem` in `~/.cloudflared/`.

### 3. Create a named tunnel

```bash
cloudflared tunnel create coggo
```

This writes `~/.cloudflared/<tunnel-uuid>.json` containing the tunnel's credentials. **Treat this file like a private key** — anyone with it can answer for your tunnel. Back it up to your password manager alongside `.env`.

### 4. Route DNS

```bash
cloudflared tunnel route dns coggo coggo.example.com
```

Cloudflare creates a proxied CNAME `coggo.example.com` → `<tunnel-uuid>.cfargotunnel.com` in your zone. No manual DNS editing needed.

### 5. Write the config

`~/.cloudflared/config.yml`:

```yaml
tunnel: coggo
credentials-file: /data/data/com.termux/files/home/.cloudflared/<tunnel-uuid>.json

ingress:
  - hostname: coggo.example.com
    service: http://localhost:8080
  - service: http_status:404
```

The `service:` URL points at the **gateway** port, not Coggo's MCP port. The gateway is what speaks OAuth.

### 6. Update `.env`

```bash
GATEWAY_PUBLIC_URL=https://coggo.example.com
CLOUDFLARE_TUNNEL_NAME=coggo
```

The boot launcher reads `CLOUDFLARE_TUNNEL_NAME` — when set, it starts `cloudflared tunnel run` and skips Tailscale Funnel.

### 7. Update Google OAuth

In Google Cloud Console → your OAuth client → **Authorized redirect URIs**, add:

```
https://coggo.example.com/oauth/callback
```

You can leave the old `*.ts.net` redirect in place during cutover; remove it once the new path is verified.

### 8. Restart

```bash
~/.termux/boot/30-coggo
tail -f ~/.coggo/logs/cloudflared.log
```

You should see `Registered tunnel connection` lines. Visit `https://coggo.example.com/healthz` (or whatever you have wired up) to confirm.

## Operations

**Logs**: `~/.coggo/logs/cloudflared.log`.

**Restart cloudflared only**:
```bash
kill "$(cat ~/.coggo/run/cloudflared.pid)"
~/.termux/boot/30-coggo
```

**Switch back to Tailscale Funnel**: blank out `CLOUDFLARE_TUNNEL_NAME` in `.env`, kill the cloudflared PID, re-run the boot script. The launcher's branch logic reverts cleanly.

**Migrating the tunnel to another machine**: copy `~/.cloudflared/cert.pem` and `~/.cloudflared/<uuid>.json` to the new host, then run `cloudflared tunnel run coggo` there. Don't run the same tunnel from two machines simultaneously — Cloudflare load-balances across them, which will confuse the gateway's OAuth state.

## What's NOT covered by Litestream

The `~/.cloudflared/` directory. Add `cert.pem` and the tunnel credentials JSON to your password manager / encrypted backup. They are not derivable from the SQLite DB.
