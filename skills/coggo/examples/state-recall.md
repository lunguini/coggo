# Example: Recalling state before answering

The user asks a question about their own work. Coggo's skill prompts Claude to query first, then ground the answer in what Coggo returns rather than what Claude might recall from earlier in the conversation.

---

**User:** What's the current state of the Coggo project? I want to make sure I'm focused on the right thing this weekend.

**Claude:** Let me check Coggo first.

*[tool call: `coggo_entity_query`]*

```json
{
  "peer": "business",
  "type": "Project",
  "filters": { "title": "Coggo" }
}
```

*[tool returns the Project entity with `status: "active"`, `completion_estimate: 35`, and a description noting current focus is weekend 4 of v0.1]*

*[tool call: `coggo_relation_query` to find what depends on or affects this project]*

```json
{
  "peer": "business",
  "from": "01HXKM4EJV...",
  "type": "depends_on"
}
```

*[tool returns one relation: depends_on a Decision about Tailscale Funnel exposure]*

*[tool call: `coggo_entity_query` for recent Decisions]*

```json
{
  "peer": "business",
  "type": "Decision",
  "limit": 5
}
```

*[tool returns the five most recent Decision entities about Coggo's design — engineering decisions live in `business` alongside the Project itself]*

Here's where Coggo says you are:

- **Project status:** active, ~35% complete, currently on weekend 4 of v0.1 (CLI and setup).
- **Most recent design decisions:** Use SurrealDB embedded; CLI framework urfave/cli/v3; MCP library mark3labs/mcp-go.
- **Open dependency:** the project depends on a decision about Tailscale Funnel vs Cloudflare Tunnel for remote exposure — that decision is still open.
- **Suggested focus:** weekend 4 work is CLI and `coggo init`. The Tailscale dependency only blocks weekend 5 (integration), so it's not on the critical path this weekend.

Want me to dig into any of those, or pull up the full decision history?

---

## What this example shows

- The skill triggers a query *before* answering, even though Claude could have guessed at a plausible answer from earlier conversation.
- Multiple tools chain together: get the project, find what it depends on, find recent related decisions. Each is a small focused query.
- The answer is grounded in what Coggo returned, with concrete IDs and entities behind every claim. If the user disagrees, they can ask Claude to show the raw entity.
- The closing offer (dig deeper, full history) keeps Coggo's substrate available without overwhelming the user with everything at once.
