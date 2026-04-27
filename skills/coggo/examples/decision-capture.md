# Example: Capturing a decision in the moment

The user is mid-conversation and articulates a decision. Coggo's skill prompts Claude to capture it without breaking the flow.

---

**User:** OK after looking at all the options I'm going to use SurrealDB embedded for Coggo's storage. SQLite + sqlite-vec was tempting because it's boring and known, but SurrealDB gives me graph + vector + time-travel out of the box and the team is actively funded. The single-binary story is the deciding factor — I don't want to ship a separate process. Going with SurrealDB.

**Claude:** That's a substantive choice — let me log it to Coggo. (Storage is an engineering decision about Coggo-the-product, so it goes in `business`.)

*[tool call: `coggo_entity_create`]*

```json
{
  "peer": "business",
  "type": "Decision",
  "fields": {
    "title": "Use SurrealDB embedded for Coggo storage",
    "rationale": "Multi-model native (graph + vector + time-travel), single-binary deployment, funded company with active maintenance. Single-binary was the deciding factor over running a separate process.",
    "alternatives": ["SQLite + sqlite-vec", "Cozo", "KuzuDB"],
    "context": "Storage layer choice for v0.1 substrate; previously open between embedded multi-model DBs and SQLite + extensions",
    "confidence": "high"
  }
}
```

*[tool returns the created entity with id `01HXKM4EJV...`]*

Logged in `business` as Decision `01HXKM4EJV...`. If a previous decision is being superseded by this one, let me know and I'll add a `supersedes` relation.

**User:** Yeah actually — there was an earlier decision two weeks ago to use SQLite. Supersede that.

*[tool call: `coggo_entity_query` with filters to find the previous decision, then `coggo_relation_create`]*

```json
{
  "peer": "coggo",
  "from": "01HXKM4EJV...",
  "to": "01HW9ABCDEF...",
  "type": "supersedes",
  "data": { "reason": "Re-evaluated after deeper look at SurrealDB's multi-model and single-binary story" }
}
```

Done — the new decision now supersedes the older SQLite one. Both remain in the event log; time travel will still show SQLite as the active choice for any timestamp before today.

---

## What this example shows

- The skill catches "I'm going to use X" as a substantive decision and logs it without asking permission first.
- Rationale captures *why*, not just *what* — alternatives considered, deciding factor, surrounding context.
- Supersession is modeled as a relation between the new and old decision, never by editing or deleting the old one.
- The user is told what happened and offered the natural follow-up (the supersession relation), but the capture itself does not block the conversation.
