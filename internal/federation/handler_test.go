package federation

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lunguini/coggo/internal/schema"
	"github.com/lunguini/coggo/internal/types"
)

const peerDID = "did:key:peer"

func newTestHandler(t *testing.T) (*StoreHandler, *mockStore, *schema.Resolver) {
	t.Helper()
	store := newMockStore()
	r := schema.NewResolver()
	for _, def := range schema.SeedEntityTypes(peerDID) {
		r.RegisterEntityType(peerDID, def)
	}
	for _, def := range schema.SeedRelationshipTypes(peerDID) {
		r.RegisterRelationType(peerDID, def)
	}
	return NewStoreHandler(peerDID, store, r), store, r
}

func mustEnv(t *testing.T, op string, args any) []byte {
	t.Helper()
	rb, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	env := envelope{Op: op, Args: rb}
	out, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func writeMsg(payload []byte) types.FederationMessage {
	return types.FederationMessage{
		Version: "0.1", SourceDID: "did:key:caller", TargetDID: peerDID,
		Type: types.FedMsgWrite, Payload: payload, Timestamp: time.Now().UTC(),
	}
}

func queryMsg(payload []byte) types.FederationMessage {
	m := writeMsg(payload)
	m.Type = types.FedMsgQuery
	return m
}

func TestEntityCreateQueryUpdateArchive(t *testing.T) {
	h, store, _ := newTestHandler(t)
	ctx := context.Background()

	// create
	resp, err := h.HandleWrite(ctx, writeMsg(mustEnv(t, "entity.create", entityCreateArgs{
		Type: "Project", Fields: map[string]any{"title": "Coggo v0.1"},
	})))
	if err != nil {
		t.Fatalf("create: %v (%s)", err, string(resp.Payload))
	}
	var created types.Entity
	if err := json.Unmarshal(resp.Payload, &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Data["title"] != "Coggo v0.1" {
		t.Fatalf("unexpected entity: %+v", created)
	}

	// query
	resp, err = h.HandleQuery(ctx, queryMsg(mustEnv(t, "entity.query", types.EntityQuery{Type: "Project"})))
	if err != nil {
		t.Fatal(err)
	}
	var entities []*types.Entity
	if err := json.Unmarshal(resp.Payload, &entities); err != nil {
		t.Fatal(err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected 1, got %d", len(entities))
	}

	// update
	resp, err = h.HandleWrite(ctx, writeMsg(mustEnv(t, "entity.update", entityUpdateArgs{
		ID: created.ID, Fields: map[string]any{"status": "completed"},
	})))
	if err != nil {
		t.Fatal(err)
	}
	var updated types.Entity
	_ = json.Unmarshal(resp.Payload, &updated)
	if updated.Data["status"] != "completed" {
		t.Fatalf("status not updated: %+v", updated.Data)
	}

	// archive
	if _, err := h.HandleWrite(ctx, writeMsg(mustEnv(t, "entity.archive", entityArchiveArgs{ID: created.ID}))); err != nil {
		t.Fatal(err)
	}

	// verify events appended: created + updated + archived = 3
	evs, _ := store.ListEvents(ctx, peerDID, time.Time{}, time.Time{})
	if len(evs) != 3 {
		t.Fatalf("expected 3 events, got %d", len(evs))
	}
}

func TestEntityCreateMissingRequired(t *testing.T) {
	h, _, _ := newTestHandler(t)
	resp, err := h.HandleWrite(context.Background(), writeMsg(mustEnv(t, "entity.create", entityCreateArgs{
		Type: "Decision", Fields: map[string]any{"title": "x"}, // missing rationale
	})))
	if err == nil {
		t.Fatalf("expected validation error, got: %s", string(resp.Payload))
	}
	if resp.Type != types.FedMsgError {
		t.Fatalf("expected error type, got %q", resp.Type)
	}
}

func TestRelationCreateAndQuery(t *testing.T) {
	h, _, _ := newTestHandler(t)
	ctx := context.Background()
	create := func(title string) string {
		resp, err := h.HandleWrite(ctx, writeMsg(mustEnv(t, "entity.create", entityCreateArgs{
			Type: "Project", Fields: map[string]any{"title": title},
		})))
		if err != nil {
			t.Fatal(err)
		}
		var e types.Entity
		_ = json.Unmarshal(resp.Payload, &e)
		return e.ID
	}
	a := create("a")
	b := create("b")

	if _, err := h.HandleWrite(ctx, writeMsg(mustEnv(t, "relation.create", relationCreateArgs{
		From: a, To: b, Type: "depends_on",
	}))); err != nil {
		t.Fatal(err)
	}

	resp, err := h.HandleQuery(ctx, queryMsg(mustEnv(t, "relation.query", relationQueryArgs{From: a})))
	if err != nil {
		t.Fatal(err)
	}
	var rels []*types.Relation
	_ = json.Unmarshal(resp.Payload, &rels)
	if len(rels) != 1 || rels[0].To != b {
		t.Fatalf("unexpected rels: %+v", rels)
	}
}

func TestTypeDefineRegistersAndPersists(t *testing.T) {
	h, store, r := newTestHandler(t)
	ctx := context.Background()
	resp, err := h.HandleWrite(ctx, writeMsg(mustEnv(t, "type.define", typeDefineArgs{
		Name:        "Habit",
		Description: "Recurring practice",
		Fields:      []types.FieldDef{{Name: "title", Type: types.FieldString, Required: true}},
	})))
	if err != nil {
		t.Fatalf("define: %v", err)
	}
	if resp.Type != types.FedMsgResponse {
		t.Fatalf("got %q", resp.Type)
	}
	if _, err := r.EntityType(peerDID, "Habit"); err != nil {
		t.Fatal(err)
	}
	es, _ := store.QueryEntities(ctx, peerDID, types.EntityQuery{Type: "EntityTypeDefinition"})
	if len(es) != 1 {
		t.Fatalf("expected type def stored as entity, got %d", len(es))
	}
}

func TestTimeTravel(t *testing.T) {
	h, _, _ := newTestHandler(t)
	ctx := context.Background()
	resp, _ := h.HandleWrite(ctx, writeMsg(mustEnv(t, "entity.create", entityCreateArgs{
		Type: "Project", Fields: map[string]any{"title": "snapshot"},
	})))
	var e types.Entity
	_ = json.Unmarshal(resp.Payload, &e)
	beforeUpdate := time.Now().UTC()
	time.Sleep(5 * time.Millisecond)
	_, _ = h.HandleWrite(ctx, writeMsg(mustEnv(t, "entity.update", entityUpdateArgs{
		ID: e.ID, Fields: map[string]any{"status": "completed"},
	})))

	resp, err := h.HandleQuery(ctx, queryMsg(mustEnv(t, "time.travel", timeTravelArgs{
		Query: types.EntityQuery{Type: "Project"}, AsOf: beforeUpdate,
	})))
	if err != nil {
		t.Fatal(err)
	}
	var es []*types.Entity
	_ = json.Unmarshal(resp.Payload, &es)
	if len(es) != 1 {
		t.Fatalf("expected 1 historical, got %d", len(es))
	}
	// Pre-update snapshot reflects the default status from validation, not the post-update value.
	if got := es[0].Data["status"]; got == "completed" {
		t.Fatalf("post-update value leaked into pre-update snapshot: %+v", es[0].Data)
	}
}

func TestPing(t *testing.T) {
	h, _, _ := newTestHandler(t)
	resp, err := h.HandlePing(context.Background(), types.FederationMessage{
		SourceDID: "x", TargetDID: peerDID, Type: types.FedMsgPing,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Type != types.FedMsgResponse {
		t.Fatalf("got %q", resp.Type)
	}
}
