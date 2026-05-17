// Package types holds the core data model shared across Coggo packages:
// peers, events, entities, relations, type definitions, and federation messages.
//
// These types are the load-bearing contract for v0.1. The event vocabulary is
// closed and stable; entity and relationship types are open and stored as data.
package types

import (
	"context"
	"encoding/json"
	"time"
)

// EventType is the closed vocabulary of events that can appear in the log.
type EventType string

const (
	EventEntityCreated           EventType = "EntityCreated"
	EventEntityUpdated           EventType = "EntityUpdated"
	EventEntityArchived          EventType = "EntityArchived"
	EventRelationCreated         EventType = "RelationCreated"
	EventRelationDissolved       EventType = "RelationDissolved"
	EventEntityTypeDefined       EventType = "EntityTypeDefined"
	EventEntityTypeUpdated       EventType = "EntityTypeUpdated"
	EventRelationshipTypeDefined EventType = "RelationshipTypeDefined"
	EventRelationshipTypeUpdated EventType = "RelationshipTypeUpdated"
	EventSettingChanged          EventType = "SettingChanged"
)

// Event is the source-of-truth record. Append-only.
type Event struct {
	ID        string          `json:"id"`
	PeerDID   string          `json:"peer_did"`
	Type      EventType       `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp time.Time       `json:"timestamp"`
	AuthorDID string          `json:"author_did"`
	ClientID  string          `json:"client_id"`
	Signature []byte          `json:"signature,omitempty"` // reserved for v0.3
}

// Entity is materialized state for a single object.
type Entity struct {
	ID              string         `json:"id"`
	Type            string         `json:"type"`
	PeerDID         string         `json:"peer_did"`
	CreatedAt       time.Time      `json:"created_at"`
	CreatedByDID    string         `json:"created_by_did"`
	CreatedByClient string         `json:"created_by_client"`
	UpdatedAt       time.Time      `json:"updated_at"`
	ArchivedAt      *time.Time     `json:"archived_at,omitempty"`
	Data            map[string]any `json:"data"`
	EmbeddingID     *string        `json:"embedding_id,omitempty"`
}

// Relation is materialized state for a single edge.
type Relation struct {
	ID              string         `json:"id"`
	From            string         `json:"from"`
	To              string         `json:"to"`
	Type            string         `json:"type"`
	PeerDID         string         `json:"peer_did"`
	CreatedAt       time.Time      `json:"created_at"`
	CreatedByDID    string         `json:"created_by_did"`
	CreatedByClient string         `json:"created_by_client"`
	Data            map[string]any `json:"data"`
}

// FieldType is the closed list of supported field types in v0.1.
type FieldType string

const (
	FieldString    FieldType = "string"
	FieldNumber    FieldType = "number"
	FieldBoolean   FieldType = "boolean"
	FieldTimestamp FieldType = "timestamp"
	FieldReference FieldType = "reference"
	FieldListOf    FieldType = "list_of" // element type in ElementType
)

// FieldDef defines a single field on an entity or relationship type.
type FieldDef struct {
	Name        string    `json:"name"`
	Type        FieldType `json:"type"`
	ElementType FieldType `json:"element_type,omitempty"` // for list_of
	Required    bool      `json:"required"`
	Default     any       `json:"default,omitempty"`
	Validation  string    `json:"validation,omitempty"`
	Description string    `json:"description,omitempty"`
}

// EntityTypeDefinition declares a user-extensible entity type.
type EntityTypeDefinition struct {
	Name        string     `json:"name"`
	PeerDID     string     `json:"peer_did"`
	Fields      []FieldDef `json:"fields"`
	Description string     `json:"description,omitempty"`
}

// RelationshipTypeDefinition declares a user-extensible relationship type.
type RelationshipTypeDefinition struct {
	Name        string     `json:"name"`
	PeerDID     string     `json:"peer_did"`
	Fields      []FieldDef `json:"fields,omitempty"`
	Description string     `json:"description,omitempty"`
	Directional bool       `json:"directional"`
}

// PeerSettings holds the per-peer behavioral configuration.
type PeerSettings struct {
	DefaultClarificationThreshold string `json:"default_clarification_threshold,omitempty"`
	BriefingFrequency             string `json:"briefing_frequency,omitempty"`
	BriefingTime                  string `json:"briefing_time,omitempty"`
	CaptureConfirmation           string `json:"capture_confirmation,omitempty"`
}

// Peer is a unit of identity and sovereignty hosted by the binary.
//
// PrivateKey is never returned over MCP and never written to logs.
type Peer struct {
	DID         string       `json:"did"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	PrivateKey  []byte       `json:"-"`
	PublicKey   []byte       `json:"public_key"`
	CreatedAt   time.Time    `json:"created_at"`
	Settings    PeerSettings `json:"settings"`
}

// Token is a peer-scoped bearer token.
type Token struct {
	ID         string    `json:"id"`
	SecretHash string    `json:"secret_hash"`
	Peers      []string  `json:"peers"` // peer names
	Label      string    `json:"label,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at,omitzero"`
}

// EntityQuery is the parameters for listing entities.
type EntityQuery struct {
	Type            string         `json:"type"`
	Filters         map[string]any `json:"filters,omitempty"`
	Limit           int            `json:"limit,omitempty"`
	IncludeArchived bool           `json:"include_archived,omitempty"`
}

// SemanticQuery is the parameters for semantic search.
type SemanticQuery struct {
	Query      string `json:"query"`
	TypeFilter string `json:"type_filter,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

// SemanticHit pairs an entity with its similarity score.
type SemanticHit struct {
	Entity *Entity `json:"entity"`
	Score  float64 `json:"score"`
}

// FederationMessageType is the closed list of message types in v0.1.
type FederationMessageType string

const (
	FedMsgQuery    FederationMessageType = "Query"
	FedMsgWrite    FederationMessageType = "Write"
	FedMsgResponse FederationMessageType = "Response"
	FedMsgError    FederationMessageType = "Error"
	FedMsgPing     FederationMessageType = "Ping"
)

// FederationMessage is the unit of cross-peer communication. Same shape
// regardless of transport (in-process in v0.1; network in v0.4).
type FederationMessage struct {
	Version   string                `json:"version"`
	SourceDID string                `json:"source_did"`
	TargetDID string                `json:"target_did"`
	Type      FederationMessageType `json:"type"`
	Payload   json.RawMessage       `json:"payload"`
	AuthToken string                `json:"auth_token,omitempty"`
	MessageID string                `json:"message_id"`
	Timestamp time.Time             `json:"timestamp"`
}

// Store is the persistence contract. One Store backs the whole binary; per-peer
// isolation is achieved by tagging every row with PeerDID.
type Store interface {
	// Lifecycle
	Init(ctx context.Context) error
	Close() error

	// Events
	AppendEvent(ctx context.Context, ev *Event) error
	ListEvents(ctx context.Context, peerDID string, since time.Time, until time.Time) ([]*Event, error)

	// Entities (projections)
	UpsertEntity(ctx context.Context, e *Entity) error
	ArchiveEntity(ctx context.Context, peerDID, id string, at time.Time) error
	GetEntity(ctx context.Context, peerDID, id string) (*Entity, error)
	QueryEntities(ctx context.Context, peerDID string, q EntityQuery) ([]*Entity, error)

	// Relations
	UpsertRelation(ctx context.Context, r *Relation) error
	DissolveRelation(ctx context.Context, peerDID, id string) error
	QueryRelations(ctx context.Context, peerDID string, from, to, relType string, limit int) ([]*Relation, error)

	// Embeddings
	UpsertEmbedding(ctx context.Context, peerDID, entityID string, vector []float32, model string) error
	SemanticSearch(ctx context.Context, peerDID string, vector []float32, typeFilter string, limit int) ([]SemanticHit, error)
	SubstringSearch(ctx context.Context, peerDID, query, typeFilter string, limit int) ([]SemanticHit, error)

	// Time travel: rebuild entity state at a point in time.
	TimeTravelEntities(ctx context.Context, peerDID string, q EntityQuery, asOf time.Time) ([]*Entity, error)
}

// PeerHandler is the per-peer side of the federation protocol. Every peer
// hosted in the binary registers a PeerHandler with the Router.
type PeerHandler interface {
	HandleQuery(ctx context.Context, msg FederationMessage) (FederationMessage, error)
	HandleWrite(ctx context.Context, msg FederationMessage) (FederationMessage, error)
	HandlePing(ctx context.Context, msg FederationMessage) (FederationMessage, error)
}

// Router selects a transport (in-process in v0.1) and delivers federation
// messages to the target peer's handler.
type Router interface {
	RegisterPeer(did string, handler PeerHandler) error
	Route(ctx context.Context, msg FederationMessage) (FederationMessage, error)
	ListPeers() []string
}

// Authority is the bearer-token authorization contract.
type Authority interface {
	Issue(ctx context.Context, peers []string, label string) (tokenID, secret string, err error)
	Verify(ctx context.Context, secret, peerName string) (*Token, error)
	List(ctx context.Context) ([]*Token, error)
	Revoke(ctx context.Context, tokenID string) error
}

// Embedder produces vectors for text. v0.1 supports voyage/openai/local/none.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	// Dimension is the vector size produced by this embedder.
	Dimension() int
	// Name is the model identifier stored alongside vectors.
	Name() string
	// Enabled reports whether this embedder produces real vectors. When false,
	// callers should fall back to substring search.
	Enabled() bool
}
