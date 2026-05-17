# Contributing

Coggo is early v0.1 software. Contributions are welcome, but changes should preserve the single-user, local-first trust model unless a proposal explicitly changes it.

## Development Setup

```bash
git clone https://github.com/lunguini/coggo.git
cd coggo
go test ./...
make install
coggo init
```

The project is expected to build and test with `CGO_ENABLED=0`.

## Before Opening a PR

Run:

```bash
gofmt -w .
go vet ./...
CGO_ENABLED=0 go test ./...
go test ./scripts -count=1
```

Optionally:

```bash
bash -n scripts/termux-deploy.sh
bash -n scripts/termux-update.sh
```

Keep changes scoped. If a patch touches public deployment behavior, update the relevant docs in `README.md` or `docs/`.

## Security and Secrets

Never commit:

- `.env` or `~/.coggo/env`
- Coggo bearer tokens
- R2 access keys
- Google OAuth client secrets
- `peers.json` identity exports
- Cloudflare tunnel credentials
- local SQLite databases or WAL files

If you find a vulnerability, follow [SECURITY.md](SECURITY.md) instead of opening a public issue.

## Design Constraints

- Coggo v0.1 is single-user and local-first.
- Raw `coggo serve` should stay localhost-bound by default.
- OAuth-only public access should go through `coggo-oauth-gateway`.
- Direct remote MCP is acceptable for trusted clients and transports that can send Coggo bearer tokens.
- Peer boundaries are authorization boundaries. Do not bypass peer-scoped token checks.
- `peers.json` is deliberately outside the Litestream DB backup because it contains peer private keys.
- Avoid adding cgo dependencies; Termux support depends on cgo-free builds.

## Commit Style

Use [Conventional Commits](https://www.conventionalcommits.org/) so release notes and `CHANGELOG.md` can be generated from commits between release tags.

```text
feat(termux): add runit services
fix(gateway): persist OAuth state secret
docs(security): clarify public exposure model
```
