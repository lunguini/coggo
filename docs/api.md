# Coggo MCP API reference

**Status:** v0.1
**Companion to:** [SCHEMA.md](SCHEMA.md), [claude-ai-setup.md](claude-ai-setup.md), [claude-code-setup.md](claude-code-setup.md)

This document is the reference for the 12 MCP tools Coggo exposes in v0.1. All tools are peer-addressed and parametric over types where applicable. AI clients should call `coggo_type_list` first to discover the vocabulary, then operate on it.

Authentication is via bearer token in the `Authorization: Bearer <token>` header on every request. Tokens are issued via `coggo token create --peer <name>`. A request without a token returns HTTP 401; a request with a token that lacks authority for the target peer returns HTTP 403.

The default endpoint is `http://localhost:6177/mcp`. JSON-RPC 2.0 per the MCP spec.

---

## Read tools

### `coggo_entity_get`

Fetch a single entity by ID.

**Args:**

| Name | Type | Required | Description |
|---|---|---|---|
| `peer` | string | yes | Peer name or DID |
| `id` | string | yes | Entity ID (ULID) |

**Returns:** Entity object — type, fields, provenance, archival status.

**Use when:** You need a specific entity by ID, e.g. to read the current state of a project, decision, or goal that was previously identified by a query.

**Example:**

```json
// request
{
  "tool": "coggo_entity_get",
  "args": { "peer": "business", "id": "01HXKM4EJV8R2K9PQRSTUVWXYZ" }
}

// response
{
  "id": "01HXKM4EJV8R2K9PQRSTUVWXYZ",
  "type": "Project",
  "peer_did": "did:key:z6MkpTHR8...",
  "created_at": "2026-04-26T14:23:11Z",
  "created_by_client": "claude-ai",
  "updated_at": "2026-04-26T14:23:11Z",
  "archived_at": null,
  "data": {
    "title": "Coggo",
    "status": "active",
    "completion_estimate": 35,
    "tags": ["substrate", "personal-ai"]
  }
}
```

---

### `coggo_entity_query`

List entities of a type matching filters.

**Args:**

| Name | Type | Required | Description |
|---|---|---|---|
| `peer` | string | yes | |
| `type` | string | yes | Entity type to query |
| `filters` | object | no | Field-value filters (exact match in v0.1) |
| `limit` | number | no | Max results (default 50) |
| `include_archived` | boolean | no | Default false |

**Returns:** Array of entity objects.

**Use when:** You need a list of entities matching some criteria — "all active projects", "decisions tagged with Y".

**Example:**

```json
// request
{
  "tool": "coggo_entity_query",
  "args": {
    "peer": "business",
    "type": "Project",
    "filters": { "status": "active" },
    "limit": 20
  }
}

// response
[
  { "id": "01HXKM4EJV...", "type": "Project", "data": { "title": "Coggo", "status": "active" } },
  { "id": "01HXKM5GQX...", "type": "Project", "data": { "title": "Loreweaver", "status": "active" } }
]
```

---

### `coggo_relation_query`

Find relationships matching filters.

**Args:**

| Name | Type | Required | Description |
|---|---|---|---|
| `peer` | string | yes | |
| `from` | string | no | Source entity ID |
| `to` | string | no | Target entity ID |
| `type` | string | no | Relationship type |
| `limit` | number | no | Default 50 |

**Returns:** Array of relation objects (id, from, to, type, data, provenance).

**Use when:** You need to explore the graph — what does this depend on, what affects what, what was superseded by what.

**Example:**

```json
// request
{
  "tool": "coggo_relation_query",
  "args": {
    "peer": "business",
    "from": "01HXKM4EJV8R2K9PQRSTUVWXYZ",
    "type": "depends_on"
  }
}

// response
[
  {
    "id": "01HXKM6PQR...",
    "from": "01HXKM4EJV...",
    "to": "01HXKL9ABC...",
    "type": "depends_on",
    "data": { "kind": "blocking" }
  }
]
```

---

### `coggo_semantic_search`

Vector similarity search across entities in a peer.

**Args:**

| Name | Type | Required | Description |
|---|---|---|---|
| `peer` | string | yes | |
| `query` | string | yes | Natural language query |
| `type_filter` | string | no | Restrict to entity type |
| `limit` | number | no | Default 10 |

**Returns:** Array of entities ranked by similarity, each with a `similarity` score.

**Use when:** You don't know the exact entity name or ID but want to find things related to a concept. Falls back to substring search if vector search returns weak matches or if the embedding provider is configured as `none`.

**Example:**

```json
// request
{
  "tool": "coggo_semantic_search",
  "args": {
    "peer": "business",
    "query": "decisions about storage layer choice",
    "type_filter": "Decision",
    "limit": 5
  }
}

// response
[
  {
    "id": "01HXKM4EJV...",
    "type": "Decision",
    "data": { "title": "Use SurrealDB embedded for Coggo storage" },
    "similarity": 0.91
  }
]
```

---

### `coggo_time_travel`

Same as `coggo_entity_query`, but returns state as it existed at a historical point.

**Args:**

| Name | Type | Required | Description |
|---|---|---|---|
| `peer` | string | yes | |
| `query` | object | yes | Same shape as `coggo_entity_query` args |
| `as_of` | timestamp | yes | Historical point in time (RFC3339) |

**Returns:** Array of entities as they existed at `as_of`.

**Use when:** You need historical state — "what did this look like a month ago", "what decisions were active when X happened". Implementation replays events up to `as_of`; slower than current-state queries, so use sparingly.

**Example:**

```json
// request
{
  "tool": "coggo_time_travel",
  "args": {
    "peer": "business",
    "query": { "type": "Project", "filters": { "status": "active" } },
    "as_of": "2026-03-01T00:00:00Z"
  }
}

// response
[
  { "id": "01HW...", "type": "Project", "data": { "title": "Loreweaver", "status": "active" } }
]
```

---

### `coggo_type_list`

List entity and relationship types defined in a peer.

**Args:**

| Name | Type | Required | Description |
|---|---|---|---|
| `peer` | string | yes | |

**Returns:** Object with `entity_types` and `relationship_types`, each an array of `{ name, description }`.

**Use when:** Starting to work with a peer. Call this first before creating entities so you know what types exist.

**Example:**

```json
// response
{
  "entity_types": [
    { "name": "Project", "description": "Work or efforts with goals and trajectories" },
    { "name": "Decision", "description": "Discrete reasoned choices with rationale" }
  ],
  "relationship_types": [
    { "name": "depends_on", "description": "Directional dependency" },
    { "name": "supersedes", "description": "Replacement (decisions, goals)" }
  ]
}
```

---

### `coggo_type_describe`

Show the full definition of a single type.

**Args:**

| Name | Type | Required | Description |
|---|---|---|---|
| `peer` | string | yes | |
| `type_name` | string | yes | |

**Returns:** Full type definition including fields, types, required flags, defaults, descriptions.

**Use when:** You need to know what fields a type accepts before creating an entity of that type.

**Example:**

```json
// response
{
  "name": "Decision",
  "description": "Discrete reasoned choices with rationale",
  "fields": [
    { "name": "title", "type": "string", "required": true },
    { "name": "rationale", "type": "string", "required": true },
    { "name": "alternatives", "type": "list_of:string", "required": false },
    { "name": "confidence", "type": "string", "required": false, "description": "low | medium | high" }
  ]
}
```

---

## Write tools

### `coggo_entity_create`

Create a new entity, validated against its type definition.

**Args:**

| Name | Type | Required | Description |
|---|---|---|---|
| `peer` | string | yes | |
| `type` | string | yes | Must reference a defined type in this peer |
| `fields` | object | yes | Field values per the type definition |

**Returns:** The created entity, with assigned ID and provenance.

**Use when:** Capturing new state — a new project, a decision being made, a goal being set, an observation being logged. AI clients should call this proactively when the user makes a substantive decision or notes something worth remembering. Do not ask permission first; the user has already configured Coggo with a capture confirmation policy.

In v0.1: missing required fields returns an error; type mismatches and unknown fields are warned about and accepted.

**Example:**

```json
// request
{
  "tool": "coggo_entity_create",
  "args": {
    "peer": "business",
    "type": "Decision",
    "fields": {
      "title": "Use SurrealDB embedded for Coggo storage",
      "rationale": "Multi-model native, funded company, single binary, time travel built in",
      "alternatives": ["SQLite + sqlite-vec", "Cozo", "KuzuDB"],
      "confidence": "high"
    }
  }
}

// response
{
  "id": "01HXKM4EJV8R2K9PQRSTUVWXYZ",
  "type": "Decision",
  "peer_did": "did:key:z6MkpTHR8...",
  "created_at": "2026-04-26T14:23:11Z",
  "created_by_client": "claude-ai",
  "data": { "title": "Use SurrealDB embedded for Coggo storage", "...": "..." }
}
```

---

### `coggo_entity_update`

Update fields on an existing entity.

**Args:**

| Name | Type | Required | Description |
|---|---|---|---|
| `peer` | string | yes | |
| `id` | string | yes | |
| `fields` | object | yes | Fields to update |

**Returns:** The updated entity.

**Use when:** An existing entity's state changes — a project moves from `active` to `paused`, a goal's target date shifts. Generates an `EntityUpdated` event; the original state remains in the event log for time travel.

**Example:**

```json
// request
{
  "tool": "coggo_entity_update",
  "args": {
    "peer": "business",
    "id": "01HXKM4EJV...",
    "fields": { "status": "paused", "completion_estimate": 40 }
  }
}
```

---

### `coggo_relation_create`

Establish a relationship between two entities.

**Args:**

| Name | Type | Required | Description |
|---|---|---|---|
| `peer` | string | yes | |
| `from` | string | yes | Source entity ID |
| `to` | string | yes | Target entity ID |
| `type` | string | yes | Must reference a defined relationship type |
| `data` | object | no | Type-specific fields |

**Returns:** The created relation.

**Use when:** Connecting entities — a project depends on another, a decision supersedes a previous one, an observation affects a goal.

**Example:**

```json
// request
{
  "tool": "coggo_relation_create",
  "args": {
    "peer": "business",
    "from": "01HXKM4EJV...",
    "to": "01HXKL9ABC...",
    "type": "supersedes",
    "data": { "reason": "Original decision was made before SurrealDB had embedded mode" }
  }
}
```

---

### `coggo_type_define`

Define a new entity or relationship type at runtime.

**Args:**

| Name | Type | Required | Description |
|---|---|---|---|
| `peer` | string | yes | Where this type is registered (typically `coggo`) |
| `name` | string | yes | New type name |
| `fields` | array | yes | Array of field definitions |
| `description` | string | yes | |
| `is_relationship` | boolean | no | True to define a relationship type (default false) |

Each field definition: `{ name, type, required, default?, description? }`. Field types: `string`, `number`, `boolean`, `timestamp`, `reference`, `list_of:<type>`.

**Returns:** The created type definition.

**Use when:** The user's life or work introduces a structure that doesn't fit existing types. Coggo's type system is open; new types are added through this tool.

**Example:**

```json
// request
{
  "tool": "coggo_type_define",
  "args": {
    "peer": "personal",
    "name": "Workout",
    "description": "A single training session",
    "fields": [
      { "name": "date", "type": "timestamp", "required": true },
      { "name": "kind", "type": "string", "required": true, "description": "strength | cardio | mobility" },
      { "name": "duration_minutes", "type": "number", "required": false },
      { "name": "notes", "type": "string", "required": false }
    ]
  }
}
```

---

## Cross-peer

### `coggo_cross_peer_query`

Run a query across multiple peers; results are tagged by peer.

**Args:**

| Name | Type | Required | Description |
|---|---|---|---|
| `query` | object | yes | A query object with `type` and `filters` (same shape as `coggo_entity_query`) |
| `peers` | array of string | yes | Peer names or DIDs to query |
| `limit_per_peer` | number | no | Default 50 |

**Returns:** Array of `{ peer, entities }` — results grouped by peer.

**Use when:** A question spans peers — "show me open work across personal and business", "what decisions are active anywhere".

Implementation note: routes a `Query` federation message to each peer and aggregates responses. The token used must have authority for each peer being queried; peers it cannot access are returned as errors in the response rather than failing the whole call.

**Example:**

```json
// request
{
  "tool": "coggo_cross_peer_query",
  "args": {
    "query": { "type": "Goal", "filters": { "status": "open" } },
    "peers": ["personal", "business"]
  }
}

// response
[
  {
    "peer": "personal",
    "entities": [
      { "id": "01HW...", "type": "Goal", "data": { "title": "Reach 90kg by 2026-08" } }
    ]
  },
  {
    "peer": "business",
    "entities": [
      { "id": "01HX...", "type": "Goal", "data": { "title": "Ship Coggo v0.1" } }
    ]
  }
]
```
