# Wiring claude.ai to Coggo

This guide gets claude.ai (web and mobile) talking to your Coggo over the public internet via Cloudflare Tunnel + an OAuth gateway. End state: ask "what projects do I have in business?" in claude.ai mobile and Claude calls Coggo's MCP tools to answer.

## Why this is more than a single command

claude.ai's Custom Connector UI requires **OAuth 2.1**, not a static bearer token. (Claude Code's MCP config accepts bearer tokens — that path is documented separately in [claude-code-setup.md](claude-code-setup.md).)

Coggo's substrate stays sovereign: it speaks bearer tokens only. To bridge to claude.ai we run a small companion binary, `coggo-oauth-gateway`, which exposes an OAuth surface to the public, validates incoming tokens against a real IdP (Google in v0.1), and reverse-proxies the validated request to Coggo on localhost. Coggo never trusts Google or anyone else for identity — the gateway is a swappable transport that can be replaced or removed without changing the substrate.

```
claude.ai mobile  --(OAuth bearer)-->  Cloudflare Tunnel
                                            |
                                            v
                                  coggo-oauth-gateway:8080
                                            |
                                            | validates token via Google OIDC
                                            | injects Coggo bearer token
                                            v
                                       coggo:6177/mcp
```

## 1. Configure Cloudflare Tunnel

Follow [cloudflare-tunnel.md](cloudflare-tunnel.md) end-to-end. By the end you should have `cloudflared` installed, a named tunnel created, DNS routed to your domain, and `~/.cloudflared/config.yml` pointing at the OAuth gateway on `localhost:8080`.

## 2. Initialize Coggo and mint a bearer token

If you haven't already:

```
coggo init
```

Mint a token the gateway will inject on every upstream call:

```
coggo token create --all --label claude-ai-mobile
```

Copy the printed `secret` value. You will paste it into an environment variable in step 5.

## 3. Create a Google OAuth client

claude.ai will sign you into Coggo via Google. You need to register Coggo as an OAuth app in Google's console.

1. Go to <https://console.cloud.google.com> and select (or create) a project. Any project name is fine — this is a Google administrative grouping, not visible to claude.ai.
2. Open **APIs & Services → OAuth consent screen**.
   - **User type:** External (unless you have a Google Workspace account, in which case Internal works too).
   - **App name:** Coggo (or whatever you like).
   - **User support email:** your email.
   - **Developer contact:** your email.
   - **Scopes:** `openid`, `email`, `profile` — these are non-sensitive, no Google verification required.
   - Save and continue.
3. Open **APIs & Services → Credentials → Create credentials → OAuth client ID**.
   - **Application type:** Web application.
   - **Name:** Coggo gateway.
   - **Authorized JavaScript origins:** your Cloudflare Tunnel origin, for example `https://coggo.example.com`.
   - **Authorized redirect URIs:** your gateway callback URL, for example `https://coggo.example.com/oauth/callback`.
   - Click Create.
4. Copy the **Client ID** (`...apps.googleusercontent.com`) and **Client Secret** (`GOCSPX-...`). You'll need both in the next step.

You will see a "Google hasn't verified this app" warning the first time you sign in. That is expected for unverified personal apps. Click **Advanced → Go to Coggo (unsafe)** to proceed. The warning only appears once per Google account; verification is only needed if you plan to onboard real third-party users (a v0.8+ concern).

## 4. Build the binaries

```
make install-all
```

This installs both `coggo` and `coggo-oauth-gateway` to your `$GOBIN`.

## 5. Run coggo + gateway + Cloudflare Tunnel

On Termux, `scripts/termux-deploy.sh` installs a boot launcher at `~/.termux/boot/30-coggo`. The launcher starts Coggo on localhost, starts the OAuth gateway, and runs `cloudflared tunnel run "$CLOUDFLARE_TUNNEL_NAME"`.

Set the required environment variables (the script will refuse to start without them):

```
export COGGO_TOKEN='<the secret you copied in step 2>'
export GOOGLE_CLIENT_ID='<id>.apps.googleusercontent.com'
export GOOGLE_CLIENT_SECRET='GOCSPX-...'
export GATEWAY_PUBLIC_URL='https://coggo.example.com'
export OAUTH_STATE_SECRET='<stable random secret>'
export CLOUDFLARE_TUNNEL_NAME='coggo'
export OAUTH_ALLOWED_EMAILS='you@example.com'
```

`OAUTH_STATE_SECRET` signs OAuth proxy state. Keep it stable across gateway restarts and updates; the Termux deploy script generates it automatically in `~/.coggo/env`.

Then start the boot launcher:

```
~/.termux/boot/30-coggo
```

The connector URL is:

```
https://coggo.example.com/mcp
```

For local desktop testing, `make serve-public` can still run the same gateway path when `CLOUDFLARE_TUNNEL_NAME` and `GATEWAY_PUBLIC_URL` are set.

## 6. Confirm Google OAuth settings

Return to **Google Cloud Console → Credentials → your OAuth client** and confirm both lists use the Cloudflare Tunnel hostname:

- **Authorized JavaScript origins:** `https://coggo.example.com`
- **Authorized redirect URIs:** `https://coggo.example.com/oauth/callback`

Save. Google's changes propagate within a minute.

## 7. Add the Custom Connector in claude.ai

In claude.ai (web), open **Settings → Connectors → Add custom connector**. Mobile follows the same path through the settings menu.

Fill in:

- **Name:** Coggo
- **URL:** the connector URL from step 5 (`https://coggo.example.com/mcp`)
- **OAuth Client ID:** your Google client ID
- **OAuth Client Secret:** your Google client secret

claude.ai will discover the OAuth endpoints automatically via the gateway's `/.well-known/oauth-authorization-server` document and walk you through the Google sign-in flow. After you approve, claude.ai will list the 12 Coggo tools (see [api.md](api.md)).

## 8. Install the Coggo skill

Without the skill, Claude won't know *when* to call Coggo's tools. Two install paths:

- **As a Project skill (recommended).** Open or create a Project in claude.ai. Open the Project's instructions / skills section and either upload `skills/coggo/SKILL.md` from this repo or paste its contents directly into the Project's system prompt.
- **As a global instruction.** Paste the contents of `skills/coggo/SKILL.md` into your overall claude.ai user preferences. Global = applies to every conversation.

If you also use Coggo from Claude Code, see [claude-code-setup.md](claude-code-setup.md) for the CLAUDE.md template — same skill content adapted for repo-level use.

## 9. Test

Start a new chat in claude.ai (within the Project where you installed the skill, if you used that path). Ask:

> What projects do I have in business?

Claude should call `coggo_entity_query` with `peer="business"` and `type="Project"`, then summarize the results. If you have not yet created any projects, ask Claude to create one:

> Log a new project called "Test from claude.ai" in business.

Verify it landed locally:

```
coggo entity list Project --peer business
```

You should see "Test from claude.ai" in the output. End-to-end working.

## Troubleshooting

**`make serve-public` fails with "missing required env".**
Set `COGGO_TOKEN`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` before running. The error message echoes the exact `export` commands you need.

**claude.ai says it cannot connect to the connector.**
1. From your laptop, test the connector URL: `curl -i https://coggo.example.com/mcp`. Expected response: `HTTP 401` with a `WWW-Authenticate` header pointing at `/.well-known/oauth-protected-resource`. If you get a connection error, check `~/.coggo/logs/cloudflared.log` and `~/.coggo/logs/gateway.log`.
2. Check the connector URL has `/mcp` on the end in the connector config.

**Google sign-in says "redirect_uri_mismatch".**
The Cloudflare Tunnel URL in step 5 doesn't exactly match what's registered in Google (step 6). Common gotchas: trailing slash, missing `/oauth/callback`, http vs https. Update the Google client credentials and try again.

**claude.ai says "Invalid redirect_uri" before reaching Google.**
The gateway rejected claude.ai's callback URL because its domain isn't in the allowlist. The gateway ships with `claude.ai,claude.com` allowlisted by default. If you're using a non-default Claude domain (e.g. a custom Anthropic deployment) or a different MCP client entirely, set `OAUTH_ALLOWED_CLIENT_DOMAINS` before running `make serve-public`:
```
export OAUTH_ALLOWED_CLIENT_DOMAINS="claude.ai,claude.com,your-other-client.example.com"
```
Localhost is always allowed regardless of this setting.

**Google sign-in says "Access blocked: Authorization Error" / "Access blocked: This app's request is invalid".**
Usually the OAuth consent screen is incomplete. In Google Cloud Console open **APIs & Services → OAuth consent screen** and confirm: app name, user support email, developer contact email, and the three scopes (`openid`, `email`, `profile`) are all set.

**"Google hasn't verified this app" warning.**
Expected for unverified personal apps. Click **Advanced → Go to Coggo (unsafe)** once. Verification is only needed for distribution to real third parties.

**Tools call but return "unauthorized" from Coggo.**
The gateway forwarded the request, but Coggo rejected the injected `COGGO_TOKEN`. Mint a fresh wildcard token and restart `make serve-public`:
```
coggo token create --all --label claude-ai-mobile
```

**claude.ai shows "Authorization with the MCP server failed".**
This usually means token validation failed at the gateway. The gateway log will show why. The most common causes:
- `audience mismatch` — `GOOGLE_CLIENT_ID` doesn't match the client_id Google issued the token for. Re-check both env vars.
- `token expired` — the user-agent took longer than the token's lifetime to complete the flow. Retry the connection in claude.ai.
- `google rejected token: invalid_token` — the token was revoked or never valid. Re-add the connector in claude.ai.
- `compact JWS format must have three parts` — should not occur after v0.1's gateway fix; means the gateway is using oauth-mcp-proxy's OIDC validator instead of Google's tokeninfo endpoint. Rebuild with `make build-gateway`.

**Claude doesn't call any Coggo tools.**
Skill not installed in this conversation. If you used the Project path, confirm you're in that Project. If global, start a fresh chat. Quick test: prompt directly "Call `coggo_type_list` for peer `business`." If that works, the connector is fine and the skill is the issue.

**Claude calls Coggo but with the wrong peer.**
Edit the "Choosing a peer" section of your installed skill to match your usage. The shipping default distinguishes `business` (Coggo as product, work, codebase, engineering) from `coggo` (Coggo's identity, personality, behavioral preferences) — see `skills/coggo/SKILL.md` for the full text.

**I want to stop using OAuth / I want to use Claude Code instead.**
Switch to `make serve` (bearer-token only, no gateway) for Claude Code or curl access. The OAuth path is only for claude.ai. Both can run; they're independent.
