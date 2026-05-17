@/Users/adrian/.codex/RTK.md

# Coggo Agent Notes

## Project Shape

Coggo v0.1 is a single-user, local-first knowledge substrate with MCP access.
Keep changes aligned with the current trust model unless the task explicitly
changes it.

- Peer boundaries are authorization boundaries.
- Raw `coggo serve` is bearer-token protected and should stay localhost-bound by
  default.
- Public browser/mobile/OAuth-only access should go through
  `coggo-oauth-gateway`.
- Direct remote MCP is acceptable for trusted clients and private transports
  that can send Coggo bearer tokens directly; OAuth is not required for those
  clients.
- `peers.json` is deliberately outside Litestream DB backup because it contains
  hosted peer private keys.
- Termux support matters; avoid adding cgo dependencies.

## Commit Style

Use Conventional Commits for all commits so release notes and `CHANGELOG.md`
can be generated from commits between release tags.

Examples:

```text
feat(termux): add runit services
fix(gateway): persist OAuth state secret
docs(security): clarify public exposure model
```

## Validation

Before claiming work is complete, run the checks relevant to the change.
For broad code changes, prefer:

```bash
gofmt -w .
go vet ./...
go test ./...
CGO_ENABLED=0 go test ./...
```

For Termux, deployment, Cloudflare Tunnel, or script changes, also run:

```bash
bash -n scripts/termux-deploy.sh
bash -n scripts/termux-update.sh
go test ./scripts -count=1
```

`internal/mcp` tests bind localhost sockets. If sandboxing blocks them with
`bind: operation not permitted`, rerun the Go test command with the required
approval instead of treating it as a code failure.

## Public Release Docs

If a change touches install, deployment, OAuth, Cloudflare Tunnel, Termux,
backup/restore, or token behavior, update the relevant docs in `README.md` or
`docs/`.

Keep the Cloudflare Tunnel path as the supported public path for Termux and
OAuth-only clients. Tailscale Funnel docs are legacy/bearer-token-client notes.

## Release Workflow

Releases follow the Gocker-style tag flow, not semantic-release:

- Push only the intended release tag, for example `git push origin v0.1.0`.
- GitHub Actions runs release readiness checks on `v*` tags.
- GoReleaser builds release archives, attaches checksums to the GitHub release,
  and updates `lunguini/homebrew-tap`.
- The Homebrew token is expected to be provided as `HOMEBREW_TAP_TOKEN` at the
  org/repo secret level.
- `CHANGELOG.md` is generated from Conventional Commit subjects between tags
  and committed back to `master` by the release workflow.

For public install docs, prefer stable tagged installs such as
`go install github.com/lunguini/coggo/cmd/coggo@v0.1.0`; use `@latest` for the
newest tagged release and `@main` only for development builds. Homebrew install
uses `brew install --cask lunguini/tap/coggo`.

## Secrets

Never commit:

- `.env` or `~/.coggo/env`
- Coggo bearer tokens
- R2 access keys
- Google OAuth client secrets
- `peers.json` identity exports
- Cloudflare tunnel credentials
- local SQLite databases or WAL files

## Tooling

- Find files with `fd`.
- Find text with `rg`.
- Search code structure with `ast-grep`.
- Use `jq` for JSON.
- Use `yq` for YAML/XML.
