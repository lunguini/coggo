# Tailscale setup for Coggo

Coggo binds to `localhost:6177` and does not expose itself to the public internet on its own. To make Coggo reachable by claude.ai (so the mobile app and web can use Coggo as a Custom Connector), you use Tailscale and Tailscale Funnel: Tailscale puts Coggo on your tailnet, and Funnel exposes that tailnet endpoint over public TLS, gated by Tailscale's auth.

This is the path for v0.1. Cloudflare Tunnel and other exposure patterns work, but Tailscale Funnel is what the setup flow assumes.

## 1. Install Tailscale

macOS:

```
brew install tailscale
```

Or download the app from <https://tailscale.com/download>. Linux and Windows installers are at the same link.

Start the Tailscale daemon (the brew install will print platform-specific instructions; on Linux it is typically `sudo systemctl enable --now tailscaled`).

## 2. Sign in to your tailnet

```
tailscale up
```

This will print a URL. Open it, sign in (Google, Microsoft, GitHub, or email-based identity provider — your choice during initial tailnet creation), and authorize the device.

Verify:

```
tailscale status
```

You should see your machine listed with a `100.x.y.z` tailnet IP and a hostname (something like `your-machine.tail-scale.ts.net`).

## 3. Run Coggo bound to localhost

Coggo binds to `localhost:6177` by default. No change needed. If you have customized `[server].listen_address` in `~/.config/coggo/config.toml`, make sure it stays on localhost — Funnel will be the only public exposure.

```
coggo serve
```

## 4. Enable Funnel for port 6177

```
tailscale funnel 6177
```

This tells Tailscale to expose port 6177 of this machine on the public Funnel endpoint. The first time you run this, Tailscale may prompt you to enable Funnel on your tailnet (a one-click toggle in the Tailscale admin console).

To check the current state:

```
tailscale funnel status
```

Output looks roughly like:

```
https://your-machine.tail-scale.ts.net (Funnel on)
|-- proxy http://127.0.0.1:6177
```

The URL printed there is your Funnel URL. Note it down — this is what you give to claude.ai.

## 5. Test reachability

From a machine that is **not** on your tailnet (a phone on cellular works well):

```
curl https://your-machine.tail-scale.ts.net/mcp
```

You should get an HTTP 401 response from Coggo (because no token is provided). A 401 means the request reached Coggo. A timeout, DNS failure, or 5xx from Tailscale means something earlier in the chain is wrong.

A 200 with a token attached:

```
curl -H "Authorization: Bearer <your-coggo-token>" \
     https://your-machine.tail-scale.ts.net/mcp
```

## 6. Wire claude.ai

See [claude-ai-setup.md](claude-ai-setup.md) for the Custom Connector configuration that points claude.ai at the Funnel URL.

## Troubleshooting

**`tailscale funnel` says Funnel is not enabled for this tailnet.**
Open the Tailscale admin console at <https://login.tailscale.com/admin/dns>, find Funnel under "Settings", enable it for the tailnet, and re-run `tailscale funnel 6177`.

**Funnel is enabled but the URL is unreachable.**
Check `tailscale funnel status` — the proxy should point at `http://127.0.0.1:6177`. If Coggo isn't running, the URL will respond with a 502 or connection refused. Start Coggo with `coggo serve`.

**Funnel quota exceeded.**
Tailscale Funnel has bandwidth quotas on the free tier. Check the admin console for your current usage. For typical Coggo traffic (low volume, mostly small JSON requests) this is rarely an issue, but it is worth being aware of.

**MagicDNS shows the wrong hostname.**
The Funnel URL is your machine's MagicDNS name plus `.ts.net`. If you renamed the machine after creating the tailnet, check `tailscale status` for the current name. You can rename a machine in the admin console under Machines.

**ACLs are blocking Funnel access.**
By default Tailscale ACLs allow Funnel. If you have customized your ACL JSON, ensure your machine is in the `funnel` node attribute or that an ACL rule explicitly permits Funnel for it. See <https://tailscale.com/kb/1223/funnel> for the current syntax.

**Phone shows "not secure" or refuses to load.**
Funnel terminates TLS using a Tailscale-managed certificate. If you bypassed certificate validation in `curl` testing, claude.ai will not bypass it; the cert must validate cleanly. Check that `tailscale funnel status` reports the Funnel as on, and that the URL hostname matches the certificate (it should, because Tailscale issues the cert for the MagicDNS hostname).
