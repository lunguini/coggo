package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lunguini/coggo/internal/schema"
	"github.com/lunguini/coggo/internal/types"
	"github.com/oklog/ulid/v2"
)

// SchemaResolver is the minimum surface StoreHandler needs from a schema
// registry. internal/schema.Resolver satisfies it.
type SchemaResolver interface {
	EntityType(peerDID, name string) (*types.EntityTypeDefinition, error)
	RelationType(peerDID, name string) (*types.RelationshipTypeDefinition, error)
}

// StoreHandler implements types.PeerHandler by translating federation
// messages into Store operations.
//
// Every write path appends an Event before mutating the projection so the
// event log is the durable source of truth and time travel can rebuild
// state from it.
type StoreHandler struct {
	PeerDID  string
	Store    types.Store
	Resolver *schema.Resolver
}

// NewStoreHandler returns a handler bound to a peer's DID and Store.
func NewStoreHandler(peerDID string, store types.Store, resolver *schema.Resolver) *StoreHandler {
	return &StoreHandler{PeerDID: peerDID, Store: store, Resolver: resolver}
}

// envelope is the discriminated request payload carried inside a
// FederationMessage. The "op" field selects the operation; "args" carries the
// op-specific arguments.
type envelope struct {
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
}

// HandleQuery dispatches read operations.
func (h *StoreHandler) HandleQuery(ctx context.Context, msg types.FederationMessage) (types.FederationMessage, error) {
	return h.dispatch(ctx, msg, false)
}

// HandleWrite dispatches mutation operations.
func (h *StoreHandler) HandleWrite(ctx context.Context, msg types.FederationMessage) (types.FederationMessage, error) {
	return h.dispatch(ctx, msg, true)
}

// HandlePing returns a Response echo with a small status payload.
func (h *StoreHandler) HandlePing(ctx context.Context, msg types.FederationMessage) (types.FederationMessage, error) {
	return h.respond(msg, map[string]any{"ok": true, "peer_did": h.PeerDID})
}

func (h *StoreHandler) dispatch(ctx context.Context, msg types.FederationMessage, write bool) (types.FederationMessage, error) {
	var env envelope
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		return h.errorResp(msg, fmt.Errorf("federation: bad payload envelope: %w", err))
	}
	switch env.Op {
	case "entity.get":
		return h.opEntityGet(ctx, msg, env.Args)
	case "entity.query":
		return h.opEntityQuery(ctx, msg, env.Args)
	case "entity.create":
		return h.opEntityCreate(ctx, msg, env.Args)
	case "entity.update":
		return h.opEntityUpdate(ctx, msg, env.Args)
	case "entity.archive":
		return h.opEntityArchive(ctx, msg, env.Args)
	case "relation.create":
		return h.opRelationCreate(ctx, msg, env.Args)
	case "relation.query":
		return h.opRelationQuery(ctx, msg, env.Args)
	case "relation.dissolve":
		return h.opRelationDissolve(ctx, msg, env.Args)
	case "type.list":
		return h.opTypeList(ctx, msg, env.Args)
	case "type.describe":
		return h.opTypeDescribe(ctx, msg, env.Args)
	case "type.define":
		return h.opTypeDefine(ctx, msg, env.Args)
	case "semantic.search":
		return h.opSemanticSearch(ctx, msg, env.Args)
	case "time.travel":
		return h.opTimeTravel(ctx, msg, env.Args)
	default:
		return h.errorResp(msg, fmt.Errorf("federation: unknown op %q (write=%v)", env.Op, write))
	}
}

// ---- request shapes ----

type entityGetArgs struct {
	ID string `json:"id"`
}

type entityCreateArgs struct {
	Type   string         `json:"type"`
	Fields map[string]any `json:"fields"`
	Author string         `json:"author_did,omitempty"`
	Client string         `json:"client_id,omitempty"`
}

type entityUpdateArgs struct {
	ID     string         `json:"id"`
	Fields map[string]any `json:"fields"`
	Author string         `json:"author_did,omitempty"`
	Client string         `json:"client_id,omitempty"`
}

type entityArchiveArgs struct {
	ID     string `json:"id"`
	Author string `json:"author_did,omitempty"`
	Client string `json:"client_id,omitempty"`
}

type relationCreateArgs struct {
	From   string         `json:"from"`
	To     string         `json:"to"`
	Type   string         `json:"type"`
	Data   map[string]any `json:"data,omitempty"`
	Author string         `json:"author_did,omitempty"`
	Client string         `json:"client_id,omitempty"`
}

type relationQueryArgs struct {
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
	Type  string `json:"type,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type relationDissolveArgs struct {
	ID     string `json:"id"`
	Author string `json:"author_did,omitempty"`
	Client string `json:"client_id,omitempty"`
}

type typeDescribeArgs struct {
	Name           string `json:"name"`
	IsRelationship bool   `json:"is_relationship,omitempty"`
}

type typeDefineArgs struct {
	Name           string           `json:"name"`
	Description    string           `json:"description"`
	Fields         []types.FieldDef `json:"fields"`
	IsRelationship bool             `json:"is_relationship,omitempty"`
	Directional    bool             `json:"directional,omitempty"`
	Author         string           `json:"author_did,omitempty"`
	Client         string           `json:"client_id,omitempty"`
}

type semanticArgs struct {
	Query      string    `json:"query"`
	Vector     []float32 `json:"vector,omitempty"`
	TypeFilter string    `json:"type_filter,omitempty"`
	Limit      int       `json:"limit,omitempty"`
}

type timeTravelArgs struct {
	Query types.EntityQuery `json:"query"`
	AsOf  time.Time         `json:"as_of"`
}

// ---- entity ops ----

func (h *StoreHandler) opEntityGet(ctx context.Context, msg types.FederationMessage, raw json.RawMessage) (types.FederationMessage, error) {
	var a entityGetArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.get: %w", err))
	}
	e, err := h.Store.GetEntity(ctx, h.PeerDID, a.ID)
	if err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.get: %w", err))
	}
	return h.respond(msg, e)
}

func (h *StoreHandler) opEntityQuery(ctx context.Context, msg types.FederationMessage, raw json.RawMessage) (types.FederationMessage, error) {
	var q types.EntityQuery
	if err := json.Unmarshal(raw, &q); err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.query: %w", err))
	}
	es, err := h.Store.QueryEntities(ctx, h.PeerDID, q)
	if err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.query: %w", err))
	}
	return h.respond(msg, es)
}

func (h *StoreHandler) opEntityCreate(ctx context.Context, msg types.FederationMessage, raw json.RawMessage) (types.FederationMessage, error) {
	var a entityCreateArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.create: %w", err))
	}
	if h.Resolver != nil {
		def, err := h.Resolver.EntityType(h.PeerDID, a.Type)
		if err != nil {
			return h.errorResp(msg, fmt.Errorf("entity.create: %w", err))
		}
		issues := schema.ValidateEntity(def, a.Fields)
		for _, i := range issues {
			if i.Severity == "warning" {
				slog.Warn("federation: validation warning", "field", i.Field, "msg", i.Message, "type", a.Type)
			}
		}
		if schema.HasErrors(issues) {
			return h.errorResp(msg, fmt.Errorf("entity.create: validation failed: %+v", issues))
		}
	}
	now := time.Now().UTC()
	id := newID()
	e := &types.Entity{
		ID:              id,
		Type:            a.Type,
		PeerDID:         h.PeerDID,
		CreatedAt:       now,
		CreatedByDID:    fallback(a.Author, msg.SourceDID, h.PeerDID),
		CreatedByClient: fallback(a.Client, "federation", ""),
		UpdatedAt:       now,
		Data:            a.Fields,
	}
	payload, _ := json.Marshal(e)
	ev := &types.Event{
		ID:        newID(),
		PeerDID:   h.PeerDID,
		Type:      types.EventEntityCreated,
		Payload:   payload,
		Timestamp: now,
		AuthorDID: e.CreatedByDID,
		ClientID:  e.CreatedByClient,
	}
	if err := h.Store.AppendEvent(ctx, ev); err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.create: append event: %w", err))
	}
	if err := h.Store.UpsertEntity(ctx, e); err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.create: upsert: %w", err))
	}
	return h.respond(msg, e)
}

func (h *StoreHandler) opEntityUpdate(ctx context.Context, msg types.FederationMessage, raw json.RawMessage) (types.FederationMessage, error) {
	var a entityUpdateArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.update: %w", err))
	}
	cur, err := h.Store.GetEntity(ctx, h.PeerDID, a.ID)
	if err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.update: %w", err))
	}
	if cur.Data == nil {
		cur.Data = map[string]any{}
	}
	for k, v := range a.Fields {
		cur.Data[k] = v
	}
	now := time.Now().UTC()
	cur.UpdatedAt = now
	payload, _ := json.Marshal(cur)
	ev := &types.Event{
		ID:        newID(),
		PeerDID:   h.PeerDID,
		Type:      types.EventEntityUpdated,
		Payload:   payload,
		Timestamp: now,
		AuthorDID: fallback(a.Author, msg.SourceDID, h.PeerDID),
		ClientID:  fallback(a.Client, "federation", ""),
	}
	if err := h.Store.AppendEvent(ctx, ev); err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.update: append: %w", err))
	}
	if err := h.Store.UpsertEntity(ctx, cur); err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.update: upsert: %w", err))
	}
	return h.respond(msg, cur)
}

func (h *StoreHandler) opEntityArchive(ctx context.Context, msg types.FederationMessage, raw json.RawMessage) (types.FederationMessage, error) {
	var a entityArchiveArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.archive: %w", err))
	}
	now := time.Now().UTC()
	payload, _ := json.Marshal(map[string]any{"id": a.ID, "archived_at": now})
	ev := &types.Event{
		ID:        newID(),
		PeerDID:   h.PeerDID,
		Type:      types.EventEntityArchived,
		Payload:   payload,
		Timestamp: now,
		AuthorDID: fallback(a.Author, msg.SourceDID, h.PeerDID),
		ClientID:  fallback(a.Client, "federation", ""),
	}
	if err := h.Store.AppendEvent(ctx, ev); err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.archive: append: %w", err))
	}
	if err := h.Store.ArchiveEntity(ctx, h.PeerDID, a.ID, now); err != nil {
		return h.errorResp(msg, fmt.Errorf("entity.archive: %w", err))
	}
	return h.respond(msg, map[string]any{"id": a.ID, "archived_at": now})
}

// ---- relation ops ----

func (h *StoreHandler) opRelationCreate(ctx context.Context, msg types.FederationMessage, raw json.RawMessage) (types.FederationMessage, error) {
	var a relationCreateArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return h.errorResp(msg, fmt.Errorf("relation.create: %w", err))
	}
	if h.Resolver != nil {
		if _, err := h.Resolver.RelationType(h.PeerDID, a.Type); err != nil {
			return h.errorResp(msg, fmt.Errorf("relation.create: %w", err))
		}
	}
	now := time.Now().UTC()
	r := &types.Relation{
		ID:              newID(),
		From:            a.From,
		To:              a.To,
		Type:            a.Type,
		PeerDID:         h.PeerDID,
		CreatedAt:       now,
		CreatedByDID:    fallback(a.Author, msg.SourceDID, h.PeerDID),
		CreatedByClient: fallback(a.Client, "federation", ""),
		Data:            a.Data,
	}
	payload, _ := json.Marshal(r)
	ev := &types.Event{
		ID:        newID(),
		PeerDID:   h.PeerDID,
		Type:      types.EventRelationCreated,
		Payload:   payload,
		Timestamp: now,
		AuthorDID: r.CreatedByDID,
		ClientID:  r.CreatedByClient,
	}
	if err := h.Store.AppendEvent(ctx, ev); err != nil {
		return h.errorResp(msg, fmt.Errorf("relation.create: append: %w", err))
	}
	if err := h.Store.UpsertRelation(ctx, r); err != nil {
		return h.errorResp(msg, fmt.Errorf("relation.create: %w", err))
	}
	return h.respond(msg, r)
}

func (h *StoreHandler) opRelationQuery(ctx context.Context, msg types.FederationMessage, raw json.RawMessage) (types.FederationMessage, error) {
	var a relationQueryArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return h.errorResp(msg, fmt.Errorf("relation.query: %w", err))
	}
	rs, err := h.Store.QueryRelations(ctx, h.PeerDID, a.From, a.To, a.Type, a.Limit)
	if err != nil {
		return h.errorResp(msg, fmt.Errorf("relation.query: %w", err))
	}
	return h.respond(msg, rs)
}

func (h *StoreHandler) opRelationDissolve(ctx context.Context, msg types.FederationMessage, raw json.RawMessage) (types.FederationMessage, error) {
	var a relationDissolveArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return h.errorResp(msg, fmt.Errorf("relation.dissolve: %w", err))
	}
	now := time.Now().UTC()
	payload, _ := json.Marshal(map[string]any{"id": a.ID, "dissolved_at": now})
	ev := &types.Event{
		ID:        newID(),
		PeerDID:   h.PeerDID,
		Type:      types.EventRelationDissolved,
		Payload:   payload,
		Timestamp: now,
		AuthorDID: fallback(a.Author, msg.SourceDID, h.PeerDID),
		ClientID:  fallback(a.Client, "federation", ""),
	}
	if err := h.Store.AppendEvent(ctx, ev); err != nil {
		return h.errorResp(msg, fmt.Errorf("relation.dissolve: append: %w", err))
	}
	if err := h.Store.DissolveRelation(ctx, h.PeerDID, a.ID); err != nil {
		return h.errorResp(msg, fmt.Errorf("relation.dissolve: %w", err))
	}
	return h.respond(msg, map[string]any{"id": a.ID, "dissolved_at": now})
}

// ---- type ops ----

func (h *StoreHandler) opTypeList(ctx context.Context, msg types.FederationMessage, _ json.RawMessage) (types.FederationMessage, error) {
	out := map[string]any{}
	if h.Resolver != nil {
		out["entity_types"] = h.Resolver.EntityTypes(h.PeerDID)
		out["relationship_types"] = h.Resolver.RelationTypes(h.PeerDID)
	}
	return h.respond(msg, out)
}

func (h *StoreHandler) opTypeDescribe(ctx context.Context, msg types.FederationMessage, raw json.RawMessage) (types.FederationMessage, error) {
	var a typeDescribeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return h.errorResp(msg, fmt.Errorf("type.describe: %w", err))
	}
	if h.Resolver == nil {
		return h.errorResp(msg, fmt.Errorf("type.describe: no resolver"))
	}
	if a.IsRelationship {
		def, err := h.Resolver.RelationType(h.PeerDID, a.Name)
		if err != nil {
			return h.errorResp(msg, err)
		}
		return h.respond(msg, def)
	}
	def, err := h.Resolver.EntityType(h.PeerDID, a.Name)
	if err != nil {
		return h.errorResp(msg, err)
	}
	return h.respond(msg, def)
}

func (h *StoreHandler) opTypeDefine(ctx context.Context, msg types.FederationMessage, raw json.RawMessage) (types.FederationMessage, error) {
	var a typeDefineArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return h.errorResp(msg, fmt.Errorf("type.define: %w", err))
	}
	now := time.Now().UTC()

	// Type definitions are stored as Entities in this peer's own store with
	// type "EntityTypeDefinition" / "RelationshipTypeDefinition", and also
	// registered into the in-memory resolver for fast lookup.
	var (
		evType   types.EventType
		entType  string
		stored   any
	)
	if a.IsRelationship {
		def := &types.RelationshipTypeDefinition{
			Name: a.Name, PeerDID: h.PeerDID, Description: a.Description,
			Fields: a.Fields, Directional: a.Directional,
		}
		if h.Resolver != nil {
			h.Resolver.RegisterRelationType(h.PeerDID, def)
		}
		stored = def
		evType = types.EventRelationshipTypeDefined
		entType = "RelationshipTypeDefinition"
	} else {
		def := &types.EntityTypeDefinition{
			Name: a.Name, PeerDID: h.PeerDID, Description: a.Description, Fields: a.Fields,
		}
		if h.Resolver != nil {
			h.Resolver.RegisterEntityType(h.PeerDID, def)
		}
		stored = def
		evType = types.EventEntityTypeDefined
		entType = "EntityTypeDefinition"
	}

	defJSON, _ := json.Marshal(stored)
	var defMap map[string]any
	_ = json.Unmarshal(defJSON, &defMap)

	id := newID()
	e := &types.Entity{
		ID:              id,
		Type:            entType,
		PeerDID:         h.PeerDID,
		CreatedAt:       now,
		CreatedByDID:    fallback(a.Author, msg.SourceDID, h.PeerDID),
		CreatedByClient: fallback(a.Client, "federation", ""),
		UpdatedAt:       now,
		Data:            defMap,
	}
	ev := &types.Event{
		ID:        newID(),
		PeerDID:   h.PeerDID,
		Type:      evType,
		Payload:   defJSON,
		Timestamp: now,
		AuthorDID: e.CreatedByDID,
		ClientID:  e.CreatedByClient,
	}
	if err := h.Store.AppendEvent(ctx, ev); err != nil {
		return h.errorResp(msg, fmt.Errorf("type.define: append: %w", err))
	}
	if err := h.Store.UpsertEntity(ctx, e); err != nil {
		return h.errorResp(msg, fmt.Errorf("type.define: upsert: %w", err))
	}
	return h.respond(msg, stored)
}

// ---- search / time travel ----

func (h *StoreHandler) opSemanticSearch(ctx context.Context, msg types.FederationMessage, raw json.RawMessage) (types.FederationMessage, error) {
	var a semanticArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return h.errorResp(msg, fmt.Errorf("semantic.search: %w", err))
	}
	if len(a.Vector) > 0 {
		hits, err := h.Store.SemanticSearch(ctx, h.PeerDID, a.Vector, a.TypeFilter, a.Limit)
		if err != nil {
			return h.errorResp(msg, fmt.Errorf("semantic.search: %w", err))
		}
		return h.respond(msg, hits)
	}
	hits, err := h.Store.SubstringSearch(ctx, h.PeerDID, a.Query, a.TypeFilter, a.Limit)
	if err != nil {
		return h.errorResp(msg, fmt.Errorf("semantic.search: %w", err))
	}
	return h.respond(msg, hits)
}

func (h *StoreHandler) opTimeTravel(ctx context.Context, msg types.FederationMessage, raw json.RawMessage) (types.FederationMessage, error) {
	var a timeTravelArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return h.errorResp(msg, fmt.Errorf("time.travel: %w", err))
	}
	es, err := h.Store.TimeTravelEntities(ctx, h.PeerDID, a.Query, a.AsOf)
	if err != nil {
		return h.errorResp(msg, fmt.Errorf("time.travel: %w", err))
	}
	return h.respond(msg, es)
}

// ---- helpers ----

func (h *StoreHandler) respond(in types.FederationMessage, body any) (types.FederationMessage, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return h.errorResp(in, err)
	}
	return types.FederationMessage{
		Version:   in.Version,
		SourceDID: h.PeerDID,
		TargetDID: in.SourceDID,
		Type:      types.FedMsgResponse,
		Payload:   payload,
		MessageID: newMessageID(),
		Timestamp: time.Now().UTC(),
	}, nil
}

func (h *StoreHandler) errorResp(in types.FederationMessage, err error) (types.FederationMessage, error) {
	payload, _ := json.Marshal(map[string]string{"error": err.Error()})
	return types.FederationMessage{
		Version:   in.Version,
		SourceDID: h.PeerDID,
		TargetDID: in.SourceDID,
		Type:      types.FedMsgError,
		Payload:   payload,
		MessageID: newMessageID(),
		Timestamp: time.Now().UTC(),
	}, err
}

func fallback(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// newID returns a fresh ULID string.
func newID() string {
	idMu.Lock()
	defer idMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), idEntropy).String()
}

// (idMu and idEntropy are declared in router.go.)
var _ = sync.Mutex{}
