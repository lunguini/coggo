# Security Policy

Coggo v0.1 is intended for single-user deployments. It stores personal state, bearer-token hashes, peer identity metadata, and optional OAuth gateway configuration. Treat a running Coggo instance as sensitive infrastructure.

## Supported Versions

Security fixes are accepted for the current `main` branch until tagged releases exist. After public releases start, only the latest minor release line will receive security fixes unless stated otherwise.

## Reporting a Vulnerability

Do not open a public issue for suspected credential leaks, authentication bypasses, private-key exposure, or data disclosure bugs.

Report security issues privately by emailing the maintainer listed on the GitHub profile for this repository. Include:

- affected commit or version
- deployment mode: local, Termux, Cloudflare Tunnel, Tailscale, or other
- impact and reproduction steps
- whether any token, peer identity, tunnel credential, or database content may have been exposed

Expect an initial response within 7 days for public v0.1.

## Deployment Safety Notes

- `coggo serve` should bind to localhost by default. It is bearer-token protected, but it does not provide an OAuth login flow, an email allowlist, or public-surface rate limiting.
- Do not expose raw Coggo directly to the public internet unless you put it behind a trusted transport/auth layer and understand that access is controlled only by Coggo bearer tokens.
- OAuth-only or browser/mobile clients should use `coggo-oauth-gateway`, with Google OAuth and `OAUTH_ALLOWED_EMAILS` configured. The gateway keeps the internal `COGGO_TOKEN` server-side and adds OAuth validation, an email allowlist, and rate limits.
- `COGGO_TOKEN` is a bearer secret. Anyone with it can use the peers it is scoped for.
- `~/.local/share/coggo/peers.json` contains hosted peer private keys. Keep identity exports encrypted.
- `~/.coggo/env` may contain R2 credentials, Google OAuth secrets, and Coggo bearer tokens. Keep it mode `0600` and out of git.
- `~/.cloudflared/<tunnel-uuid>.json` is a tunnel credential. Treat it like a private key.
- Litestream backs up the SQLite database only. It does not back up peer identities, env secrets, OAuth credentials, or Cloudflare tunnel credentials.
- Cloudflare Tunnel terminates TLS at Cloudflare's edge. Cloudflare can see decrypted HTTP requests before they are forwarded through the tunnel.

## Current v0.1 Limitations

- No multi-user authorization model.
- No encrypted database at rest by default.
- No signed event enforcement yet.
- No network federation between independent Coggo instances yet.
- Loose schema validation by design; required fields are enforced, but unknown fields are accepted.
