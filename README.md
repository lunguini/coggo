# Coggo

Coggo is a federated synthetic intelligence — a software entity that holds state, reasons over it, and operates within explicit boundaries. It is designed for sovereignty: a user's Coggo serves the user, holds the user's data, acts on the user's authority, and produces evidence of its actions that the user can inspect and other parties can verify. The reasoning substrate is swappable; the identity, goals, decisions, and contracts are not.

**v0.1 ships the substrate.** It stores events, entities, and relationships across one or more peers. It exposes an MCP server so AI clients — claude.ai, Claude Code, anything that speaks MCP — can query and write to Coggo. It supports cross-peer queries through an in-process federation router. It generates a structured daily briefing aggregating state across peers. The schema is open: entity and relationship types are defined as data, and new types can be added at runtime.

**v0.1 does not reason, act autonomously, federate over the network, or sign events.** The reasoning loop is v0.2. Cryptographic identity is v0.3. Cross-machine federation is v0.4. Agent dispatch is v0.5. Autonomy is v0.6. Full BCPC contract enforcement is v0.7. Inter-user federation is v0.8. The substrate has the schema fields these later versions need (signatures, consent grants, contract IDs) but does not enforce them.

## Concepts

Coggo's mental model is small. Five building blocks; everything else is composition.

### Peer

**A peer is a unit of identity and sovereignty.** It has its own DID (decentralised identifier — a `did:key:...` derived from an ed25519 keypair), its own data, its own settings, its own authorization scope. A peer is what holds your stuff.

You typically have several. The default `coggo init` creates three:

- **`personal`** — life domains: health, finance, social, music, family.
- **`business`** — work, projects, clients, decisions about code.
- **`coggo`** — Coggo's own state: settings, directives, decisions about Coggo itself.

You can add more (`coggo peer add accountant`), rename (`coggo peer rename business work`), and define new ones for any context that warrants its own boundary — a side venture, a co-owned project, a household.

**Why peers and not just folders or tags?** Three reasons:

1. **Authorization is per-peer.** A bearer token can be scoped to one peer, several, or all (`--all`). When you give claude.ai access to your `business` peer, it cannot read `personal` data even if you ask it to. The boundary is enforced, not aspirational.
2. **The federation protocol is per-peer.** Even when peers are co-located in one binary today, every cross-peer query goes through the federation router using the same protocol that v0.4 will use over the network. When you eventually run your `business` peer on a VPS and your `personal` peer on your laptop, no code changes — just transport configuration.
3. **Sovereignty is per-peer.** When you log a `Decision` into `business`, the event is signed (eventually, in v0.3) by the `business` peer's key. That signature is portable evidence: another Coggo can verify it independently, regardless of where it came from. Identity travels with data.

A peer is the closest thing Coggo has to "an account," but it is yours, lives where you choose, and federates as a peer rather than reporting to a central platform.

### Entity

**An entity is a thing.** A project, a decision, a goal, an observation, a person, a recipe, a contract — whatever you want Coggo to remember. Every entity has:

- A **type** (`Project`, `Decision`, `Goal`, etc. — see seed types below)
- A set of **fields** defined by the type (a `Decision` has `title`, `rationale`, `alternatives`, `confidence`, ...)
- A **peer** it belongs to
- **Provenance** — who created it, with which client, when

Entities are mutable but never deleted. Updates produce new events on the log; archival sets a flag rather than dropping the row. You can ask "what did this look like a month ago" and get a real answer (`coggo_time_travel`).

### Relationship

**A relationship connects two entities.** A `Decision` *supersedes* an earlier `Decision`. A `Project` *depends_on* another `Project`. An `Observation` *affects* a `Goal`. Three seed relationship types ship: `depends_on`, `supersedes`, `affects`. Add your own with `coggo type add --relationship`.

### Event

**An event is what actually happened.** Every state change — entity created, entity updated, type defined, setting changed — is an immutable, ordered event in a per-peer log. The event log is the source of truth. Entities and relationships are *projections* of the event log; they can be rebuilt from it.

The event vocabulary is closed and stable (`EntityCreated`, `EntityUpdated`, `EntityArchived`, `RelationCreated`, `RelationDissolved`, `EntityTypeDefined`, `EntityTypeUpdated`, `RelationshipTypeDefined`, `RelationshipTypeUpdated`, `SettingChanged`). The entity types it carries are open — you define them.

### Type

**A type is a schema for entities.** Types are themselves data — defined as `EntityTypeDefinition` entities living in the peer where they apply. v0.1 ships seed types per peer (`Project`, `Domain`, `Decision`, `Goal`, `Observation`, `Setting`); you extend the vocabulary at runtime via `coggo type add` (CLI) or `coggo_type_define` (MCP). v0.1 validation is loose: required fields are enforced, type mismatches and unknown fields are accepted with a warning. Strict validation arrives in v0.7.

For a full reference of the seed schema and how to define your own, see [docs/SCHEMA.md](docs/SCHEMA.md).

## Status

v0.1 development. Single user, single machine. Apache 2.0 licensed.

## Install and first run

```bash
git clone https://github.com/lunguini/coggo.git
cd coggo
make install        # installs `coggo` to $GOPATH/bin
coggo init          # interactive setup: peers, settings, Tailscale check
coggo today         # structured daily briefing
make dev            # build + serve MCP locally on :6177
```

For remote access (claude.ai mobile, etc.) on a custom domain, use Cloudflare Tunnel:

```bash
make serve-public   # build coggo + OAuth gateway; expose with cloudflared
```

`make help` lists all targets. The phone deployment uses `cloudflared` and a named Cloudflare Tunnel — see [docs/cloudflare-tunnel.md](docs/cloudflare-tunnel.md).

## Day-to-day commands

```bash
coggo today                      # daily briefing across all peers
coggo today --peer business      # one peer

coggo decision new               # interactive: log a Decision (defaults to business peer)
coggo goal new                   # ditto, defaults to personal peer
coggo observation new            # ditto, defaults to personal peer

coggo entity new <Type> --peer <name>    # any type, any peer
coggo entity list <Type> --peer <name>
coggo entity show <id> --peer <name>

coggo type list --peer <name>    # see what types exist
coggo type add                   # interactive: define a new type

coggo peer add <name>            # add a new peer
coggo peer list                  # show all peers

coggo token create --all                 # one token for everything
coggo token create --peer business       # peer-scoped
coggo token create --peer business --peer personal  # multiple peers

coggo backup identity export ~/coggo-peers.json  # export hosted peer private keys
coggo backup identity import ~/coggo-peers.json  # restore hosted peer private keys
```

## Wiring AI clients

**Claude Code (local):**

```bash
coggo token create --all --label claude-code-local
coggo serve &
claude mcp add --transport http coggo http://localhost:6177/mcp \
  --header "Authorization: Bearer <secret>"
```

Drop [templates/CLAUDE.md.template](templates/CLAUDE.md.template) into your repo's `CLAUDE.md` so Claude Code knows when to query Coggo proactively.

**claude.ai (remote):** see [docs/claude-ai-setup.md](docs/claude-ai-setup.md). The Termux phone path uses Cloudflare Tunnel plus the OAuth gateway.

## Documentation

- [SCHEMA.md](docs/SCHEMA.md) — entity types, relationship types, event vocabulary
- [api.md](docs/api.md) — MCP tool reference (the 12 tools v0.1 exposes)
- [claude-ai-setup.md](docs/claude-ai-setup.md) — wiring claude.ai web and mobile
- [claude-code-setup.md](docs/claude-code-setup.md) — wiring Claude Code
- [cloudflare-tunnel.md](docs/cloudflare-tunnel.md) — exposing Coggo on a custom domain via Cloudflare Tunnel
- [tailscale-setup.md](docs/tailscale-setup.md) — legacy Tailscale + Funnel notes for non-Termux hosts
- [backup.md](docs/backup.md) — DB replication to Cloudflare R2 plus identity backup
- [skills/coggo/SKILL.md](skills/coggo/SKILL.md) — the Coggo skill for AI clients
- [templates/CLAUDE.md.template](templates/CLAUDE.md.template) — drop-in CLAUDE.md for Claude Code repos

## License

Apache 2.0. See [LICENSE](LICENSE).
