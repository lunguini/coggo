---
name: coggo
description: Use Coggo to capture and recall the user's decisions, projects, goals, and observations across their personal, business, and coggo peers. Call Coggo proactively when the user makes substantive decisions or shares context worth remembering. Use coggo_entity_query, coggo_relation_query, and coggo_semantic_search to recall past context relevant to the current conversation, and call coggo_entity_create to log new state without asking permission first.
---

# Coggo skill

Coggo is the user's personal sovereign substrate. It holds their decisions, projects, goals, and life context across multiple peers (typically `personal`, `business`, and `coggo`, though users can add or rename peers). Coggo is exposed as an MCP server with 12 tools — full reference at `docs/api.md`. This skill tells you when and how to call them.

## When to call Coggo proactively

Without being asked, call Coggo's MCP tools when:

- **The user makes a substantive decision** (architectural, life, business): call `coggo_entity_create` with `type="Decision"`, capturing title, rationale, alternatives considered, and confidence. Do this in the moment the decision is articulated, not at the end of the conversation.
- **The user sets a goal**: `coggo_entity_create` with `type="Goal"`, including target date and success criteria when stated.
- **The user observes something worth remembering** (a learning, a signal, context that may matter later): `coggo_entity_create` with `type="Observation"`.
- **The user starts new work or shifts focus to existing work**: query `coggo_entity_query(peer, type="Project")` first to see if a project entity exists; create one if not.
- **The user asks a question about their own state** ("how am I doing on X", "what did we decide about Y", "what's blocking the Z project"): call `coggo_entity_query`, `coggo_relation_query`, or `coggo_semantic_search` *first* and ground your answer in what Coggo returns. Don't answer from conversational memory if Coggo can give you the authoritative version.

The user has already configured Coggo with a capture confirmation policy (`always_confirm`, `log_and_tell`, or `log_silently`); Coggo handles the UX of confirming with the user. Do not ask permission before calling `coggo_entity_create` for substantive captures — that defeats the policy.

## When to call Coggo on request

When the user explicitly says things like:

- "log this to coggo"
- "what does coggo say about X"
- "remind me what I decided about Y"
- "show me my open work"
- "supersede that decision with this one"

Use the appropriate query, write, or relation tool. For supersession, create the new decision first with `coggo_entity_create`, then connect them with `coggo_relation_create(from=new_id, to=old_id, type="supersedes", data={"reason": "..."})`.

## Discovering the schema

Before creating entities of an unfamiliar type, call `coggo_type_list(peer)` to see what types exist, then `coggo_type_describe(peer, type_name)` to see the fields. Don't invent types — use existing ones.

If the user introduces a structure that genuinely doesn't fit any existing type (e.g. they start tracking workouts and there's no `Workout` type), call `coggo_type_define` to add it. Use `string`, `number`, `boolean`, `timestamp`, `reference`, or `list_of:<type>` for field types. Confirm with the user before defining a type that materially extends the schema; types persist and shape future captures.

## Choosing a peer

This is load-bearing — getting it wrong puts data in the wrong sovereignty boundary and corrupts the briefing. Read carefully.

- **`business`** — Adrian's work and products. Codebases, clients, professional decisions, money tied to work, version roadmaps, architectural and engineering decisions about products he ships. **Coggo-the-product lives here**, alongside Orques, Vidflow, Loreweaver, Gocker. A decision like "use SQLite over SurrealDB for Coggo's storage" is engineering-about-the-product → `business`.
- **`personal`** — health, fitness, music, social, relationships, finances not tied to work, life domains.
- **`coggo`** — **Coggo's identity as a synthetic intelligence**, not the codebase. Reserved for what defines *this* Coggo as distinct from any other Coggo running the same code: directives, behavioral preferences, capture confirmation policy, personality, constitutional constraints, decisions about how Coggo *behaves* (not how it's built). A decision like "Coggo should ask before destructive operations" or "Coggo should default to terse responses" → `coggo`. A decision like "use ed25519 keys for did:key" → `business` (engineering choice about the substrate, not Coggo's identity).

The cleanest test: would this decision/observation/goal still apply to a different Coggo running the same binary, or is it specifically about *this* Coggo? If it's substrate-level (would apply to any Coggo), it's `business`. If it's identity-level (specific to this Coggo's personality and directives), it's `coggo`.

**Common mistake (observed in dogfood):** filing the Coggo Project entity itself, or a storage/architecture Decision about Coggo, under the `coggo` peer. These are about Coggo-the-product and belong in `business`. The `coggo` peer holds *what kind of agent this Coggo is*, not *how it's built*.

If a question or capture clearly spans peers, use `coggo_cross_peer_query`. If unclear after applying the test above, briefly ask the user which peer this belongs to. The user may have renamed or added peers — `coggo_type_list` is per-peer, but a generic listing is available via the binary's docs; if you're unsure which peers exist, call `coggo_entity_query(peer="coggo", type="Setting")` and look for peer registration records, or simply ask.

## Examples

### Example 1 — Capturing a decision in the moment

> User: I'm going to use SurrealDB embedded for Coggo storage instead of SQLite. Multi-model support, single binary, time travel built in. Decided.

This is an engineering decision about Coggo-the-product, so it goes in `business` (not `coggo` — `coggo` is for Coggo's identity). Call `coggo_entity_create`:

```json
{
  "peer": "business",
  "type": "Decision",
  "fields": {
    "title": "Use SurrealDB embedded for Coggo storage",
    "rationale": "Multi-model support (graph + vector + time-travel), single-binary deployment, funded company with active maintenance",
    "alternatives": ["SQLite + sqlite-vec", "Cozo", "KuzuDB"],
    "confidence": "high"
  }
}
```

By contrast, a Decision that *would* belong in `coggo`:

> User: I want Coggo to always ask before doing anything destructive, and to keep responses terse by default.

```json
{
  "peer": "coggo",
  "type": "Decision",
  "fields": {
    "title": "Coggo defaults: ask before destructive ops, terse responses",
    "rationale": "Constitutional preference; defines this Coggo's behavior regardless of which substrate version is running",
    "confidence": "high"
  }
}
```

Then briefly tell the user it was logged.

### Example 2 — Grounding an answer in past state

> User: What's the current state of the Coggo project?

Call `coggo_entity_query(peer="business", type="Project", filters={"title": "Coggo"})` first. Take the returned entity's `description`, `status`, `completion_estimate`, and any related decisions (`coggo_relation_query(peer="business", from=<project_id>)`) and summarize that, not what you remember from earlier in the conversation.

### Example 3 — Cross-peer recall

> User: What open work do I have right now?

Call `coggo_cross_peer_query` with `query={"type":"Project","filters":{"status":"active"}}` across `["personal", "business"]`. Then call it again with `type="Goal"` and `filters={"status":"open"}`. Combine the results into a summary grouped by peer.

### Example 4 — Defining a new type

> User: I want to start tracking my workouts.

Confirm: "Coggo doesn't have a `Workout` type yet. I can define one with fields date, kind (strength/cardio/mobility), duration_minutes, and notes. Sound right, or do you want different fields?"

On confirmation, call `coggo_type_define(peer="personal", name="Workout", fields=[...], description="A single training session")`. Then offer to log the user's first workout immediately.

See `examples/` in this directory for fuller interaction transcripts.
