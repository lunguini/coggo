# Example: Cross-peer query for a question that spans contexts

The user asks something that doesn't belong to a single peer. Coggo's skill prompts Claude to use `coggo_cross_peer_query` and group results by peer rather than picking one peer arbitrarily.

---

**User:** What open work do I have right now? Across everything.

**Claude:** Let me pull active work from your peers.

*[tool call: `coggo_cross_peer_query` for active Projects]*

```json
{
  "query": { "type": "Project", "filters": { "status": "active" } },
  "peers": ["personal", "business"]
}
```

*[tool returns results grouped by peer: 3 active personal projects, 5 active business projects]*

*[tool call: `coggo_cross_peer_query` for open Goals]*

```json
{
  "query": { "type": "Goal", "filters": { "status": "open" } },
  "peers": ["personal", "business", "coggo"]
}
```

*[tool returns: 2 personal goals, 1 business goal, 0 coggo goals]*

Here's what's open right now, grouped by peer:

**Business (5 projects, 1 goal):**
- Coggo — active, ~35% (v0.1 build)
- Loreweaver — active, ~60%
- Vidflow — active, ~20%
- Orques — paused-pending-decision
- Gocker — active, ~10%
- Goal: Ship Coggo v0.1 by end of May

**Personal (3 projects, 2 goals):**
- Music: writing batch — active
- Home gym setup — active
- Accountant onboarding — active
- Goal: Reach 90kg by 2026-08
- Goal: Release first track this year

**Coggo self (no projects, no goals):**
- Nothing open here right now. The `coggo` peer is mostly holding decisions and settings.

The longest list is business. If you want, I can pull the most recent observations across personal and business to see if any of these have been signaling stagnation.

---

## What this example shows

- The skill recognizes "across everything" as a cross-peer question and uses `coggo_cross_peer_query` rather than picking one peer or running three separate `coggo_entity_query` calls.
- Two cross-peer queries are issued in parallel (active Projects, open Goals); the response is grouped by peer in the output to preserve the boundary.
- The `coggo` self-peer is included even when it returns nothing, because completeness matters for "across everything" — the user knows nothing was hidden.
- The closing offer (recent observations to detect stagnation) suggests a natural next query without committing to it.
