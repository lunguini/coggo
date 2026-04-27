# Wiring Claude Code to Coggo

This guide adds Coggo as an MCP server in Claude Code, so when you start a session in any repo, Claude Code can query and write to Coggo. End state: at the start of a session, Claude Code calls `coggo_entity_query` to fetch the current project state, and during the session it logs decisions back as `Decision` entities.

Prerequisite: a running Coggo on `localhost:6177` (run `coggo serve` if it isn't already).

## 1. Generate a peer-scoped bearer token

Most repos belong to one peer (typically `business`). Generate a token for that peer:

```
coggo token create --peer business --label claude-code
```

Copy the secret it prints. Coggo only stores the hash; you cannot retrieve it later.

If you work across multiple peers from the same Claude Code config, scope the token to all of them:

```
coggo token create --peer business --peer personal --peer coggo --label claude-code-all
```

## 2. Add Coggo to Claude Code's MCP config

Claude Code reads MCP server configuration from one of two paths depending on platform and version:

- `~/.claude/mcp.json`
- `~/.config/claude-code/mcp.json` (XDG-style)

Use whichever your installation expects (check Claude Code's documentation for your version; if both exist, `~/.claude/mcp.json` typically wins). The file is JSON; create it if it doesn't exist.

```json
{
  "mcpServers": {
    "coggo": {
      "url": "http://localhost:6177/mcp",
      "headers": {
        "Authorization": "Bearer cgg_live_aB3cD4eF5gH6iJ7kL8mN9oP0qR1sT2uV3wX4yZ5"
      }
    }
  }
}
```

Replace the token with the secret from step 1. Save the file. Restart Claude Code if it was already running, so it picks up the new MCP server.

## 3. Drop the CLAUDE.md template into your repo

In each repo where you want Claude Code to use Coggo, copy `templates/CLAUDE.md.template` from this repository to a `CLAUDE.md` at the repo root (or merge its Coggo section into an existing CLAUDE.md). Replace the `<PROJECT_NAME>` placeholder with the actual project name as it appears in Coggo.

The template instructs Claude Code to:

- Query Coggo at session start for the project's current state.
- Log meaningful decisions made during the session as `Decision` entities.
- Update the project entity when state materially changes.

You can adjust the template per-repo to point at different peers or filter by different fields.

## 4. Test

Open a Claude Code session in a repo that has the CLAUDE.md template installed. Ask:

> What's the current state of this project from Coggo's view?

Claude Code should call `coggo_entity_query` with the project name and respond with the current entity. If no project entity exists yet, ask:

> Create a project entity in Coggo for this repo.

Claude Code should call `coggo_entity_create` with `peer="business"` and `type="Project"`. Verify with:

```
coggo entity list Project --peer business
```

You should see the new project. End-to-end working.

## Troubleshooting

**Claude Code doesn't show Coggo in its MCP server list.**
The MCP config file is in the wrong location, malformed JSON, or the file mode is wrong. Validate the JSON with `cat ~/.claude/mcp.json | jq .` (or whichever path you used). Restart Claude Code after edits. Some versions of Claude Code log MCP load errors to stderr; check the terminal where Claude Code was started.

**Connection refused.**
Coggo isn't running. Start it with `coggo serve`. Confirm it bound to the expected port: `curl http://localhost:6177/mcp` should return HTTP 401.

**HTTP 401 unauthorized.**
The token is missing or malformed in the `headers` block. Re-check the JSON structure — `headers` is an object whose keys are header names. The value of `Authorization` must start with `Bearer ` (with a space) and have no surrounding quotes inside the string.

**HTTP 403 forbidden.**
The token is valid but lacks authority for the peer the tool call targets. Issue a token scoped to the target peer or expand the existing token's scope by reissuing it with multiple `--peer` flags.

**Claude Code calls Coggo with the wrong peer.**
The CLAUDE.md template specifies which peer this repo belongs to. Edit the template in the repo to make the peer explicit and unambiguous.

**MCP tools work but Claude Code never calls them proactively.**
The CLAUDE.md template's instructions on when to call Coggo aren't being followed. Make the instructions more explicit. Direct prompting always works (e.g. "log this decision to Coggo: ...") even if proactive calling is unreliable.
