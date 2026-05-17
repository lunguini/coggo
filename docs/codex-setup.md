# Wiring Codex to Coggo

This guide adds Coggo as a local MCP server in Codex. End state: Codex can query and write durable project context through Coggo while `coggo serve` runs on your machine.

Prerequisite: a running Coggo on `localhost:6177`.

## 1. Create a token

For day-to-day code work, start with the `business` peer:

```bash
coggo token create --peer business --label codex-local
```

If you want Codex to access multiple peers, scope the token explicitly:

```bash
coggo token create --peer business --peer coggo --label codex-local
```

Use `--all` only when you intentionally want Codex to cross every peer boundary.

## 2. Export the token

Codex can read bearer tokens for streamable HTTP MCP servers from an environment variable:

```bash
export COGGO_TOKEN='paste-token-here'
```

Keep this token out of shell history, dotfiles you commit, and shared logs.

## 3. Start Coggo

In another terminal:

```bash
coggo serve
```

The default MCP endpoint is:

```text
http://localhost:6177/mcp
```

## 4. Add Coggo to Codex

```bash
codex mcp add coggo-local \
  --url http://localhost:6177/mcp \
  --bearer-token-env-var COGGO_TOKEN
```

Restart Codex if it was already running so the MCP server list is reloaded.

For a remote Coggo that accepts direct bearer-token MCP, use the remote URL instead:

```bash
codex mcp add coggo-remote \
  --url https://coggo.example.com/mcp \
  --bearer-token-env-var COGGO_TOKEN
```

See [remote-bearer-mcp.md](remote-bearer-mcp.md) for the remote deployment shape and security notes.

## 5. Smoke test

Ask Codex to call:

```text
coggo_type_list(peer="coggo")
```

On a freshly initialized install, the response should include seed entity types such as `Project`, `Decision`, `Goal`, `Observation`, and `Setting`.

For a more useful project-level test, ask Codex to query the `business` peer:

```text
coggo_entity_query(peer="business", type="Project", limit=5)
```

If no projects exist yet, ask Codex to create one with `coggo_entity_create` after it has called `coggo_type_describe` for `Project`.

## Troubleshooting

**Codex reports that the MCP server is unauthorized.**

Confirm `COGGO_TOKEN` is exported in the environment where Codex starts. Then create a fresh token and update the environment variable. A request without a token returns HTTP 401; a token without access to the target peer returns HTTP 403.

**Codex can connect but cannot read a peer.**

The token is probably scoped to a different peer. Create a token with the needed `--peer` values and restart Codex.

**Codex cannot connect to `localhost:6177`.**

Coggo is not running or is bound to a different address. Start it with `coggo serve`. You can confirm the HTTP server is reachable with:

```bash
curl -i http://localhost:6177/mcp
```

An HTTP 401 response is expected without a bearer token and proves the request reached Coggo.

**The `codex mcp add` command is missing or has different flags.**

Check your local Codex version:

```bash
codex mcp add --help
```

The command used in this guide expects support for `--url` and `--bearer-token-env-var` on streamable HTTP MCP servers.
