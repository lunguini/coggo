# Coggo schema reference

**Status:** v0.1
**Companion to:** [BCPC.md](BCPC.md), [api.md](api.md)

## 1. Overview

Coggo's schema is split in two:

- **The type system is open.** Entity types and relationship types are themselves data, stored as `EntityTypeDefinition` and `RelationshipTypeDefinition` entities in the `coggo` self-peer. New types can be defined at runtime via MCP (`coggo_type_define`) or CLI (`coggo type add`) without modifying source.
- **The event vocabulary is closed.** The set of event types is fixed and small (10 types, listed in §4). This keeps the event log replayable across versions: a future-Coggo replaying a v0.1 event log knows exactly what each event means.

A new entity type does not require a new event type. Defining a `Project` and creating one both happen through the same `EntityTypeDefined` and `EntityCreated` events with type-specific payloads. The vocabulary stays small while the structure stays open.

## 2. Common entity fields

Every entity, regardless of type:

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string (ULID) | yes | Sortable, locally generated, no UUID dependency |
| `type` | string | yes | References an `EntityTypeDefinition` by name |
| `peer_did` | string | yes | The peer this entity belongs to |
| `created_at` | timestamp | yes | RFC3339 UTC |
| `created_by_did` | string | yes | The DID that authored the creation event |
| `created_by_client` | string | yes | Client identifier (e.g. `claude-ai`, `claude-code`, `cli`) |
| `updated_at` | timestamp | yes | Updated on every `EntityUpdated` event |
| `archived_at` | timestamp | nullable | Set when archived; entities are never hard-deleted |
| `data` | object | yes | Type-specific fields, validated against the type definition |
| `embedding_id` | string | nullable | Reference to an entry in the vector index, if one exists |

## 3. Common relation fields

Every relationship:

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string (ULID) | yes | |
| `from` | string (entity ID) | yes | Source entity |
| `to` | string (entity ID) | yes | Target entity |
| `type` | string | yes | References a `RelationshipTypeDefinition` by name |
| `peer_did` | string | yes | |
| `created_at` | timestamp | yes | |
| `created_by_did` | string | yes | |
| `created_by_client` | string | yes | |
| `data` | object | yes | Type-specific fields, may be empty |

## 4. Event vocabulary

The closed set of event types in v0.1. Every state change in Coggo goes through one of these.

### `EntityCreated`

Payload: full entity record (id, type, peer_did, data, provenance fields). Emitted whenever a new entity is added to a peer. The materialized `entities` row is a projection of this event.

### `EntityUpdated`

Payload: entity id, peer_did, the fields being changed, and the new values. Original field values remain in earlier events; time travel reconstructs them by replay.

### `EntityArchived`

Payload: entity id, peer_did, archive timestamp, optional reason string. Sets `archived_at` on the materialized entity. No hard delete — the entity remains queryable with `include_archived=true`.

### `RelationCreated`

Payload: relation id, from, to, type, peer_did, optional data fields. Adds an edge to the graph projection.

### `RelationDissolved`

Payload: relation id, peer_did, optional reason string. Removes the edge from the active graph projection. The original creation event remains for time travel.

### `EntityTypeDefined`

Payload: type name, peer_did, full field definitions, description. Creates an `EntityTypeDefinition` entity in the target peer (typically `coggo`). New entity types become available immediately for `coggo_entity_create`.

### `EntityTypeUpdated`

Payload: type name, peer_did, the field changes (added, removed, modified). v0.1 does not migrate existing entities when a type changes; existing data continues to live under whatever fields were present at creation. Strict migration semantics are a v0.5+ concern.

### `RelationshipTypeDefined`

Payload: type name, peer_did, optional fields, description, directional flag. Same shape as `EntityTypeDefined`, applied to the relationship vocabulary.

### `RelationshipTypeUpdated`

Payload: same shape as `EntityTypeUpdated` for relationships.

### `SettingChanged`

Payload: setting key, scope (`global` or peer-specific), old value, new value. Used for the `coggo` self-peer's behavioral settings (default peer, capture confirmation policy, briefing schedule, etc.). Setting changes are events so that the audit trail covers Coggo's own configuration.

### Common event fields

In addition to the payload, every event carries:

| Field | Type | Description |
|---|---|---|
| `id` | string (ULID) | Sortable; chronological order is derivable |
| `peer_did` | string | The peer this event belongs to |
| `type` | string | One of the vocabulary above |
| `payload` | object | Type-specific |
| `timestamp` | timestamp | RFC3339 UTC |
| `author_did` | string | Who authored — usually = `peer_did` |
| `client_id` | string | Which client issued the write |
| `signature` | bytes | **Nullable in v0.1**; reserved for v0.3 |

Events are append-only. The SurrealDB `events` table has `update NONE` and `delete NONE` permissions; no path through the application writes to existing event rows.

## 5. Field types

The type system in v0.1 supports the following field types. Sufficient for most useful structures without inventing a query language.

| Type | Encoding | Notes |
|---|---|---|
| `string` | UTF-8 | |
| `number` | float64 | Use this for both integers and decimals in v0.1 |
| `boolean` | bool | |
| `timestamp` | RFC3339 UTC | |
| `reference` | string (entity ID) | A pointer to another entity in the same peer |
| `list_of:<type>` | array | The element type follows the colon (e.g. `list_of:string`) |

Nested objects are not first-class in v0.1. If a type needs structured sub-values, model them as separate entities and connect them with relationships.

## 6. Validation discipline (v0.1: loose)

Validation is intentionally permissive in v0.1.

- **Missing required fields → error.** `coggo_entity_create` rejects the call.
- **Type mismatches → warning, accepted.** A `number` field given a string is logged at `warn` level and stored as-is.
- **Unknown fields → accepted.** Fields not declared in the type definition are stored on the entity's `data` object.

Strict validation (rejecting writes that violate type definitions) is a v0.7 concern, when federation introduces cross-peer type compatibility issues. v0.1 is permissive on purpose: discovering type definition gaps during use shapes them better than upfront rigor.

## 7. Seed entity types

These ship with v0.1 and are created during `coggo init`. Users can keep, modify, extend, or supplement them; they have no special status beyond being preinstalled.

### Project

Work or efforts with goals and trajectories.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `title` | string | yes | | |
| `status` | string | no | `active` | `active` \| `paused` \| `completed` \| `abandoned` |
| `description` | string | no | | |
| `completion_estimate` | number | no | | 0–100 |
| `tags` | list_of:string | no | | |
| `external_url` | string | no | | GitHub URL, project page, etc. |

### Domain

Life areas (health, finance, social, music, etc.).

| Field | Type | Required | Description |
|---|---|---|---|
| `title` | string | yes | |
| `description` | string | no | |
| `tags` | list_of:string | no | |

### Decision

Discrete reasoned choices with rationale.

| Field | Type | Required | Description |
|---|---|---|---|
| `title` | string | yes | One-line summary |
| `rationale` | string | yes | Why this was decided |
| `alternatives` | list_of:string | no | Alternatives considered and rejected |
| `context` | string | no | Background that led to this decision |
| `confidence` | string | no | `low` \| `medium` \| `high` |

Decision-specific behavior like supersession is captured via relationships, not fields. A decision that supersedes another creates a `supersedes` relation between the two.

### Goal

Desired states with time horizons.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `title` | string | yes | | |
| `description` | string | no | | |
| `target_date` | timestamp | no | | |
| `status` | string | no | `open` | `open` \| `achieved` \| `abandoned` \| `paused` |
| `success_criteria` | string | no | | What it looks like when achieved |

### Observation

Context, learnings, signals that don't fit elsewhere.

| Field | Type | Required | Description |
|---|---|---|---|
| `text` | string | yes | |
| `tags` | list_of:string | no | |
| `source` | string | no | Where this observation came from |

### Setting

Used by the `coggo` self-peer to store behavioral settings as data, so settings changes are auditable events.

| Field | Type | Required | Description |
|---|---|---|---|
| `key` | string | yes | |
| `value` | string | yes | |
| `scope` | string | no | `global` \| peer-specific |

## 8. Seed relationship types

### depends_on

Directional dependency.

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | string | no | `blocking` \| `soft` \| `informational` |

### supersedes

Replacement (typically for decisions or goals).

| Field | Type | Required | Description |
|---|---|---|---|
| `reason` | string | no | |

### affects

Entity A affects entity B's state or trajectory.

| Field | Type | Required | Description |
|---|---|---|---|
| `nature` | string | no | `positive` \| `negative` \| `mixed` \| `neutral` |

## 9. Defining new types at runtime

Two paths, both produce `EntityTypeDefined` (or `RelationshipTypeDefined`) events:

- **Via MCP:** `coggo_type_define(peer, name, fields, description, is_relationship?)`. See [api.md](api.md#coggo_type_define) for the full tool signature. AI clients can extend the schema without leaving the conversation.
- **Via CLI:** `coggo type add` walks through field definitions interactively.

New types are immediately available to `coggo_entity_create` and the `coggo entity new <type>` CLI. There is no schema migration step; the type definition is itself just an entity, and the runtime reads it on demand.
