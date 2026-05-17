// Package cli implements the Coggo command-line interface.
//
// All subcommands share a Runtime built by Setup, which opens the peer
// registry, the SQLite store, the auth store, the schema resolver, and the
// federation router, then registers a StoreHandler per peer. Type definitions
// previously stored as EntityTypeDefinition entities are rehydrated into the
// resolver so user-defined types survive restarts.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	cli "github.com/urfave/cli/v3"

	"github.com/lunguini/coggo/internal/auth"
	"github.com/lunguini/coggo/internal/config"
	"github.com/lunguini/coggo/internal/federation"
	"github.com/lunguini/coggo/internal/peer"
	"github.com/lunguini/coggo/internal/schema"
	"github.com/lunguini/coggo/internal/store"
	"github.com/lunguini/coggo/internal/types"
)

// Runtime bundles the substrate every CLI command needs.
type Runtime struct {
	Cfg      *config.Config
	Registry *peer.Registry
	Store    types.Store
	storeRaw *store.Store
	Auth     types.Authority
	Router   types.Router
	Resolver *schema.Resolver
}

// Setup opens all dependencies and wires them together. Idempotent in the
// sense that re-opening an existing data directory rehydrates state from disk.
func Setup(ctx context.Context, cfg *config.Config) (*Runtime, error) {
	dataDir := config.DataDir(cfg)

	reg, err := peer.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("cli setup: peer registry: %w", err)
	}

	dim := cfg.Storage.EmbeddingDimension
	if dim <= 0 {
		dim = 1024
	}
	st, err := store.New(config.ResolvedDBPath(cfg), dim)
	if err != nil {
		return nil, fmt.Errorf("cli setup: store: %w", err)
	}
	if err := st.Init(ctx); err != nil {
		return nil, fmt.Errorf("cli setup: store init: %w", err)
	}

	authStore, err := auth.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("cli setup: auth: %w", err)
	}

	resolver := schema.NewResolver()
	router := federation.New()

	for _, p := range reg.List() {
		// Register seed types in the resolver as a baseline. They may be
		// overridden by stored EntityTypeDefinition entities (rehydration
		// below).
		for _, def := range schema.SeedEntityTypes(p.DID) {
			resolver.RegisterEntityType(p.DID, def)
		}
		for _, def := range schema.SeedRelationshipTypes(p.DID) {
			resolver.RegisterRelationType(p.DID, def)
		}
		// Rehydrate user-defined entity types.
		if ents, err := st.QueryEntities(ctx, p.DID, types.EntityQuery{Type: "EntityTypeDefinition"}); err == nil {
			for _, e := range ents {
				def := entityToEntityTypeDef(e)
				if def != nil {
					resolver.RegisterEntityType(p.DID, def)
				}
			}
		}
		if ents, err := st.QueryEntities(ctx, p.DID, types.EntityQuery{Type: "RelationshipTypeDefinition"}); err == nil {
			for _, e := range ents {
				def := entityToRelTypeDef(e)
				if def != nil {
					resolver.RegisterRelationType(p.DID, def)
				}
			}
		}
		h := federation.NewStoreHandler(p.DID, st, resolver)
		if err := router.RegisterPeer(p.DID, h); err != nil {
			return nil, fmt.Errorf("cli setup: register %s: %w", p.Name, err)
		}
	}

	return &Runtime{
		Cfg:      cfg,
		Registry: reg,
		Store:    st,
		storeRaw: st,
		Auth:     authStore,
		Router:   router,
		Resolver: resolver,
	}, nil
}

// Close releases the store.
func (r *Runtime) Close() error {
	if r == nil || r.storeRaw == nil {
		return nil
	}
	return r.storeRaw.Close()
}

// AddPeerToRuntime registers a freshly created peer with the resolver and
// router, also persisting its seed types into the store as
// EntityTypeDefinition / RelationshipTypeDefinition entities so they survive
// restarts.
func AddPeerToRuntime(ctx context.Context, rt *Runtime, p *types.Peer, clientID string) error {
	for _, def := range schema.SeedEntityTypes(p.DID) {
		rt.Resolver.RegisterEntityType(p.DID, def)
		if err := persistEntityTypeDef(ctx, rt, p.DID, def, clientID); err != nil {
			return err
		}
	}
	for _, def := range schema.SeedRelationshipTypes(p.DID) {
		rt.Resolver.RegisterRelationType(p.DID, def)
		if err := persistRelTypeDef(ctx, rt, p.DID, def, clientID); err != nil {
			return err
		}
	}
	h := federation.NewStoreHandler(p.DID, rt.Store, rt.Resolver)
	if err := rt.Router.RegisterPeer(p.DID, h); err != nil {
		return err
	}
	return nil
}

func persistEntityTypeDef(ctx context.Context, rt *Runtime, peerDID string, def *types.EntityTypeDefinition, clientID string) error {
	now := time.Now().UTC()
	payload, _ := json.Marshal(def)
	ev := &types.Event{
		ID:        newULID(),
		PeerDID:   peerDID,
		Type:      types.EventEntityTypeDefined,
		Payload:   payload,
		Timestamp: now,
		AuthorDID: peerDID,
		ClientID:  clientID,
	}
	if err := rt.Store.AppendEvent(ctx, ev); err != nil {
		return err
	}
	var data map[string]any
	_ = json.Unmarshal(payload, &data)
	e := &types.Entity{
		ID: newULID(), Type: "EntityTypeDefinition", PeerDID: peerDID,
		CreatedAt: now, CreatedByDID: peerDID, CreatedByClient: clientID,
		UpdatedAt: now, Data: data,
	}
	return rt.Store.UpsertEntity(ctx, e)
}

func persistRelTypeDef(ctx context.Context, rt *Runtime, peerDID string, def *types.RelationshipTypeDefinition, clientID string) error {
	now := time.Now().UTC()
	payload, _ := json.Marshal(def)
	ev := &types.Event{
		ID:        newULID(),
		PeerDID:   peerDID,
		Type:      types.EventRelationshipTypeDefined,
		Payload:   payload,
		Timestamp: now,
		AuthorDID: peerDID,
		ClientID:  clientID,
	}
	if err := rt.Store.AppendEvent(ctx, ev); err != nil {
		return err
	}
	var data map[string]any
	_ = json.Unmarshal(payload, &data)
	e := &types.Entity{
		ID: newULID(), Type: "RelationshipTypeDefinition", PeerDID: peerDID,
		CreatedAt: now, CreatedByDID: peerDID, CreatedByClient: clientID,
		UpdatedAt: now, Data: data,
	}
	return rt.Store.UpsertEntity(ctx, e)
}

// ---- Federation helpers used by CLI commands ----

// FedCall sends a federation message to the named peer (op + args -> response
// payload). On error response, returns an error built from the payload.
func FedCall(ctx context.Context, rt *Runtime, peerDID, msgType, op string, args any) (json.RawMessage, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("fed: marshal args: %w", err)
	}
	envelope, err := json.Marshal(struct {
		Op   string          `json:"op"`
		Args json.RawMessage `json:"args"`
	}{Op: op, Args: argsJSON})
	if err != nil {
		return nil, err
	}
	var t types.FederationMessageType
	switch msgType {
	case "query":
		t = types.FedMsgQuery
	case "write":
		t = types.FedMsgWrite
	default:
		return nil, fmt.Errorf("fed: unknown msg type %q", msgType)
	}
	msg := types.FederationMessage{
		Version:   "v0.1",
		SourceDID: "cli",
		TargetDID: peerDID,
		Type:      t,
		Payload:   envelope,
		MessageID: newULID(),
		Timestamp: time.Now().UTC(),
	}
	resp, err := rt.Router.Route(ctx, msg)
	if err != nil {
		return nil, err
	}
	if resp.Type == types.FedMsgError {
		var em map[string]string
		_ = json.Unmarshal(resp.Payload, &em)
		if em["error"] != "" {
			return nil, fmt.Errorf("%s", em["error"])
		}
		return nil, fmt.Errorf("federation error response")
	}
	return resp.Payload, nil
}

// CreateEntity is a convenience wrapper around the entity.create op.
func CreateEntity(ctx context.Context, rt *Runtime, peerDID, typ string, fields map[string]any, clientID string) (*types.Entity, error) {
	resp, err := FedCall(ctx, rt, peerDID, "write", "entity.create", map[string]any{
		"type":       typ,
		"fields":     fields,
		"client_id":  clientID,
		"author_did": peerDID,
	})
	if err != nil {
		return nil, err
	}
	var e types.Entity
	if err := json.Unmarshal(resp, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// UpdateEntity wraps entity.update.
func UpdateEntity(ctx context.Context, rt *Runtime, peerDID, id string, fields map[string]any, clientID string) (*types.Entity, error) {
	resp, err := FedCall(ctx, rt, peerDID, "write", "entity.update", map[string]any{
		"id": id, "fields": fields, "client_id": clientID, "author_did": peerDID,
	})
	if err != nil {
		return nil, err
	}
	var e types.Entity
	if err := json.Unmarshal(resp, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// QueryEntities wraps entity.query.
func QueryEntities(ctx context.Context, rt *Runtime, peerDID string, q types.EntityQuery) ([]*types.Entity, error) {
	resp, err := FedCall(ctx, rt, peerDID, "query", "entity.query", q)
	if err != nil {
		return nil, err
	}
	var es []*types.Entity
	if err := json.Unmarshal(resp, &es); err != nil {
		return nil, err
	}
	return es, nil
}

// GetEntity wraps entity.get.
func GetEntity(ctx context.Context, rt *Runtime, peerDID, id string) (*types.Entity, error) {
	resp, err := FedCall(ctx, rt, peerDID, "query", "entity.get", map[string]string{"id": id})
	if err != nil {
		return nil, err
	}
	var e types.Entity
	if err := json.Unmarshal(resp, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// DefineType wraps type.define.
func DefineType(ctx context.Context, rt *Runtime, peerDID, name, description string, fields []types.FieldDef, isRel, directional bool, clientID string) error {
	_, err := FedCall(ctx, rt, peerDID, "write", "type.define", map[string]any{
		"name": name, "description": description, "fields": fields,
		"is_relationship": isRel, "directional": directional,
		"client_id": clientID, "author_did": peerDID,
	})
	return err
}

// CreateRelation wraps relation.create.
func CreateRelation(ctx context.Context, rt *Runtime, peerDID, from, to, typ string, data map[string]any, clientID string) (*types.Relation, error) {
	resp, err := FedCall(ctx, rt, peerDID, "write", "relation.create", map[string]any{
		"from": from, "to": to, "type": typ, "data": data,
		"client_id": clientID, "author_did": peerDID,
	})
	if err != nil {
		return nil, err
	}
	var r types.Relation
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ---- shared helpers ----

// loadConfig reads the config from the path stashed in the global flag.
func loadConfig(cmd *cli.Command) (*config.Config, error) {
	path := cmd.Root().String("config")
	if path == "" {
		path = config.DefaultPath()
	}
	return config.Load(path)
}

// configPath returns the configured path (or the default).
func configPath(cmd *cli.Command) string {
	path := cmd.Root().String("config")
	if path == "" {
		path = config.DefaultPath()
	}
	return path
}

// resolvePeer returns the peer named by --peer, by the fallback default, or
// the first registered peer if neither is set.
func resolvePeer(rt *Runtime, name, fallback string) (*types.Peer, error) {
	candidates := []string{name, fallback}
	for _, n := range candidates {
		if n == "" {
			continue
		}
		p, err := rt.Registry.Resolve(n)
		if err == nil {
			return p, nil
		}
	}
	all := rt.Registry.List()
	if len(all) == 0 {
		return nil, fmt.Errorf("no peers; run `coggo init` first")
	}
	return all[0], nil
}

// entityToEntityTypeDef rehydrates a stored EntityTypeDefinition entity into
// a typed definition.
func entityToEntityTypeDef(e *types.Entity) *types.EntityTypeDefinition {
	if e == nil || e.Data == nil {
		return nil
	}
	b, err := json.Marshal(e.Data)
	if err != nil {
		return nil
	}
	var def types.EntityTypeDefinition
	if err := json.Unmarshal(b, &def); err != nil {
		return nil
	}
	if def.PeerDID == "" {
		def.PeerDID = e.PeerDID
	}
	return &def
}

func entityToRelTypeDef(e *types.Entity) *types.RelationshipTypeDefinition {
	if e == nil || e.Data == nil {
		return nil
	}
	b, err := json.Marshal(e.Data)
	if err != nil {
		return nil
	}
	var def types.RelationshipTypeDefinition
	if err := json.Unmarshal(b, &def); err != nil {
		return nil
	}
	if def.PeerDID == "" {
		def.PeerDID = e.PeerDID
	}
	return &def
}

// ulid generation — local sync.Mutex source.
var (
	ulidMu      sync.Mutex
	ulidEntropy = ulid.Monotonic(&randReader{}, 0)
)

// newULID returns a fresh ULID string.
func newULID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), ulidEntropy).String()
}

type randReader struct{ s uint64 }

func (r *randReader) Read(p []byte) (int, error) {
	// Simple LCG; ULID monotonic entropy reseeds on each call so this is
	// just used for tail randomness — not security-critical.
	for i := range p {
		r.s = r.s*6364136223846793005 + 1442695040888963407
		p[i] = byte(r.s >> 56)
	}
	return len(p), nil
}
