# Changelog

## v0.1.2

- fix(store): bump sqlite vec bindings
- ci: target main branch for release updates

## v0.1.1

- docs: point install docs at v0.1.1
- fix(cli): make status disk probe portable

## v0.1.0

- chore: ignore local agent files
- ci(release): add tag-based GoReleaser flow
- docs: refresh public README and client setup
- docs(deploy): clarify public tunnel paths
- chore: normalize gofmt alignment
- Persist OAuth gateway state secret
- Capture Termux service stderr logs
- Add gateway token debug logs
- Use runit for Termux services
- Add identity backup import and export
- Read status env from coggo state
- Fix Termux tunnel and restore setup
- Move Termux env file under coggo state
- Use GOBIN for Termux installs
- Add gateway checks to status
- Fix Termux status memory probe
- Add coggo status command
- Auto-generate cloudflared config.yml and DNS route in deploy script
- Add curl --max-time 2 to prevent boot script hanging on port check
- Use setsid to detach boot processes from terminal process group
- Document Termux DB restore
- Use cgo-free sqlite driver
- Use Cloudflare-only Termux deploy
- Fix Termux Tailscale install
- Add Cloudflare Tunnel support for custom-domain exposure
- Add .env.example and use export prefix for direct sourcing
- Add Litestream replication to Cloudflare R2
- Unify env file convention across laptop and phone
- Add Termux update script and enable Tailscale SSH at deploy time
- Add OAuth gateway and Termux deploy

