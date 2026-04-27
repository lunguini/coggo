package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunguini/coggo/internal/types"
)

const testPeerDID = "did:key:z6MkTestPeer"

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "coggo-test.db"), 4)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestEventsAppendAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t0 := time.Now().UTC().Truncate(time.Millisecond)
	for i, payload := range []string{`{"n":1}`, `{"n":2}`, `{"n":3}`} {
		ev := &types.Event{
			PeerDID:   testPeerDID,
			Type:      types.EventEntityCreated,
			Payload:   json.RawMessage(payload),
			Timestamp: t0.Add(time.Duration(i) * time.Second),
			AuthorDID: testPeerDID,
			ClientID:  "test",
		}
		if err := s.AppendEvent(ctx, ev); err != nil {
			t.Fatalf("AppendEvent[%d]: %v", i, err)
		}
		if ev.ID == "" {
			t.Fatalf("expected AppendEvent to allocate ID")
		}
	}

	evs, err := s.ListEvents(ctx, testPeerDID, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 3 {
		t.Fatalf("expected 3 events, got %d", len(evs))
	}
	if string(evs[0].Payload) != `{"n":1}` || string(evs[2].Payload) != `{"n":3}` {
		t.Fatalf("events out of order: %+v", evs)
	}

	// Bounded range: only middle event.
	mid, err := s.ListEvents(ctx, testPeerDID, t0.Add(500*time.Millisecond), t0.Add(1500*time.Millisecond))
	if err != nil {
		t.Fatalf("ListEvents bounded: %v", err)
	}
	if len(mid) != 1 || string(mid[0].Payload) != `{"n":2}` {
		t.Fatalf("expected only middle event, got %+v", mid)
	}
}

func TestEntitiesUpsertGetQuery(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mk := func(id, typ, status string) *types.Entity {
		return &types.Entity{
			ID:              id,
			Type:            typ,
			PeerDID:         testPeerDID,
			CreatedByDID:    testPeerDID,
			CreatedByClient: "test",
			Data:            map[string]any{"status": status, "title": "T-" + id},
		}
	}
	for _, e := range []*types.Entity{
		mk("p1", "Project", "active"),
		mk("p2", "Project", "archived-but-not-flagged"),
		mk("d1", "Decision", "active"),
	} {
		if err := s.UpsertEntity(ctx, e); err != nil {
			t.Fatalf("UpsertEntity %s: %v", e.ID, err)
		}
	}

	got, err := s.GetEntity(ctx, testPeerDID, "p1")
	if err != nil || got == nil {
		t.Fatalf("GetEntity p1: %v / %v", got, err)
	}
	if got.Data["title"] != "T-p1" {
		t.Fatalf("unexpected data: %+v", got.Data)
	}

	// Query by type
	projects, err := s.QueryEntities(ctx, testPeerDID, types.EntityQuery{Type: "Project"})
	if err != nil {
		t.Fatalf("QueryEntities Project: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	// Query by JSON filter
	active, err := s.QueryEntities(ctx, testPeerDID, types.EntityQuery{
		Filters: map[string]any{"status": "active"},
	})
	if err != nil {
		t.Fatalf("QueryEntities filter: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active entities, got %d (%+v)", len(active), active)
	}
}

func TestArchiveEntityFiltering(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	e := &types.Entity{
		ID: "x1", Type: "Project", PeerDID: testPeerDID,
		CreatedByDID: testPeerDID, CreatedByClient: "test",
		Data: map[string]any{"title": "to-archive"},
	}
	if err := s.UpsertEntity(ctx, e); err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}
	if err := s.ArchiveEntity(ctx, testPeerDID, "x1", time.Now().UTC()); err != nil {
		t.Fatalf("ArchiveEntity: %v", err)
	}

	// Default: archived excluded.
	res, err := s.QueryEntities(ctx, testPeerDID, types.EntityQuery{Type: "Project"})
	if err != nil {
		t.Fatalf("QueryEntities: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("expected archived to be excluded by default, got %d", len(res))
	}

	// IncludeArchived: returned.
	res, err = s.QueryEntities(ctx, testPeerDID, types.EntityQuery{Type: "Project", IncludeArchived: true})
	if err != nil {
		t.Fatalf("QueryEntities IncludeArchived: %v", err)
	}
	if len(res) != 1 || res[0].ArchivedAt == nil {
		t.Fatalf("expected one archived entity, got %+v", res)
	}
}

func TestRelationsCRUDAndQuery(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rels := []*types.Relation{
		{ID: "r1", From: "a", To: "b", Type: "depends_on", PeerDID: testPeerDID},
		{ID: "r2", From: "a", To: "c", Type: "depends_on", PeerDID: testPeerDID},
		{ID: "r3", From: "d", To: "b", Type: "supersedes", PeerDID: testPeerDID},
	}
	for _, r := range rels {
		if err := s.UpsertRelation(ctx, r); err != nil {
			t.Fatalf("UpsertRelation %s: %v", r.ID, err)
		}
	}

	fromA, err := s.QueryRelations(ctx, testPeerDID, "a", "", "", 0)
	if err != nil || len(fromA) != 2 {
		t.Fatalf("expected 2 from a, got %d (err=%v)", len(fromA), err)
	}

	toB, err := s.QueryRelations(ctx, testPeerDID, "", "b", "", 0)
	if err != nil || len(toB) != 2 {
		t.Fatalf("expected 2 to b, got %d (err=%v)", len(toB), err)
	}

	supersedes, err := s.QueryRelations(ctx, testPeerDID, "", "", "supersedes", 0)
	if err != nil || len(supersedes) != 1 {
		t.Fatalf("expected 1 supersedes, got %d (err=%v)", len(supersedes), err)
	}

	if err := s.DissolveRelation(ctx, testPeerDID, "r1"); err != nil {
		t.Fatalf("DissolveRelation: %v", err)
	}
	fromA, _ = s.QueryRelations(ctx, testPeerDID, "a", "", "", 0)
	if len(fromA) != 1 {
		t.Fatalf("expected 1 after dissolve, got %d", len(fromA))
	}
}

func TestSubstringSearch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, e := range []*types.Entity{
		{ID: "n1", Type: "Note", PeerDID: testPeerDID, CreatedByClient: "t", Data: map[string]any{"body": "the quick brown fox"}},
		{ID: "n2", Type: "Note", PeerDID: testPeerDID, CreatedByClient: "t", Data: map[string]any{"body": "lazy dog naps"}},
		{ID: "n3", Type: "Project", PeerDID: testPeerDID, CreatedByClient: "t", Data: map[string]any{"title": "fox project"}},
	} {
		if err := s.UpsertEntity(ctx, e); err != nil {
			t.Fatalf("UpsertEntity %s: %v", e.ID, err)
		}
	}

	hits, err := s.SubstringSearch(ctx, testPeerDID, "fox", "", 10)
	if err != nil {
		t.Fatalf("SubstringSearch: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits for 'fox', got %d", len(hits))
	}

	hits, err = s.SubstringSearch(ctx, testPeerDID, "fox", "Note", 10)
	if err != nil {
		t.Fatalf("SubstringSearch typed: %v", err)
	}
	if len(hits) != 1 || hits[0].Entity.ID != "n1" {
		t.Fatalf("expected only n1 for typed fox, got %+v", hits)
	}
}

func TestTimeTravelEntities(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t0 := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Millisecond)
	t1 := t0.Add(10 * time.Minute)

	createPayload, _ := json.Marshal(types.Entity{
		ID:      "ent1",
		Type:    "Project",
		PeerDID: testPeerDID,
		Data:    map[string]any{"status": "draft"},
	})
	updatePayload, _ := json.Marshal(types.Entity{
		ID:      "ent1",
		Type:    "Project",
		PeerDID: testPeerDID,
		Data:    map[string]any{"status": "active"},
	})

	if err := s.AppendEvent(ctx, &types.Event{
		PeerDID: testPeerDID, Type: types.EventEntityCreated,
		Payload: createPayload, Timestamp: t0,
		AuthorDID: testPeerDID, ClientID: "test",
	}); err != nil {
		t.Fatalf("append create: %v", err)
	}
	if err := s.AppendEvent(ctx, &types.Event{
		PeerDID: testPeerDID, Type: types.EventEntityUpdated,
		Payload: updatePayload, Timestamp: t1,
		AuthorDID: testPeerDID, ClientID: "test",
	}); err != nil {
		t.Fatalf("append update: %v", err)
	}

	// As of t0 (inclusive): only the create has happened, status=draft.
	asOfCreate, err := s.TimeTravelEntities(ctx, testPeerDID, types.EntityQuery{Type: "Project"}, t0)
	if err != nil {
		t.Fatalf("TimeTravelEntities asOf t0: %v", err)
	}
	if len(asOfCreate) != 1 {
		t.Fatalf("expected 1 entity at t0, got %d", len(asOfCreate))
	}
	if asOfCreate[0].Data["status"] != "draft" {
		t.Fatalf("expected status=draft at t0, got %v", asOfCreate[0].Data)
	}

	// As of t1 (inclusive): update applied, status=active.
	asOfUpdate, err := s.TimeTravelEntities(ctx, testPeerDID, types.EntityQuery{Type: "Project"}, t1)
	if err != nil {
		t.Fatalf("TimeTravelEntities asOf t1: %v", err)
	}
	if len(asOfUpdate) != 1 || asOfUpdate[0].Data["status"] != "active" {
		t.Fatalf("expected status=active at t1, got %+v", asOfUpdate)
	}
}

func TestEventsAppendOnlyTriggers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.AppendEvent(ctx, &types.Event{
		PeerDID: testPeerDID, Type: types.EventEntityCreated,
		Payload: json.RawMessage(`{}`), AuthorDID: testPeerDID, ClientID: "test",
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	if _, err := s.DB().ExecContext(ctx, `UPDATE events SET client_id = 'mutated'`); err == nil {
		t.Fatalf("expected UPDATE on events to be rejected by trigger")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("expected append-only error, got %v", err)
	}

	if _, err := s.DB().ExecContext(ctx, `DELETE FROM events`); err == nil {
		t.Fatalf("expected DELETE on events to be rejected by trigger")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("expected append-only error, got %v", err)
	}
}

func TestSemanticSearchRoundtrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mk := func(id string) *types.Entity {
		return &types.Entity{
			ID: id, Type: "Note", PeerDID: testPeerDID,
			CreatedByClient: "test",
			Data:            map[string]any{"body": id},
		}
	}
	for _, e := range []*types.Entity{mk("a"), mk("b"), mk("c")} {
		if err := s.UpsertEntity(ctx, e); err != nil {
			t.Fatalf("UpsertEntity: %v", err)
		}
	}

	// Three orthogonal-ish vectors of dim 4.
	if err := s.UpsertEmbedding(ctx, testPeerDID, "a", []float32{1, 0, 0, 0}, "test-model"); err != nil {
		t.Fatalf("UpsertEmbedding a: %v", err)
	}
	if err := s.UpsertEmbedding(ctx, testPeerDID, "b", []float32{0, 1, 0, 0}, "test-model"); err != nil {
		t.Fatalf("UpsertEmbedding b: %v", err)
	}
	if err := s.UpsertEmbedding(ctx, testPeerDID, "c", []float32{0, 0, 1, 0}, "test-model"); err != nil {
		t.Fatalf("UpsertEmbedding c: %v", err)
	}

	hits, err := s.SemanticSearch(ctx, testPeerDID, []float32{1, 0, 0, 0}, "", 2)
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected at least one hit")
	}
	if hits[0].Entity.ID != "a" {
		t.Fatalf("expected nearest hit to be a, got %s (score %f)", hits[0].Entity.ID, hits[0].Score)
	}

	// Empty vector returns empty.
	empty, err := s.SemanticSearch(ctx, testPeerDID, nil, "", 5)
	if err != nil {
		t.Fatalf("SemanticSearch empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty result for nil vector, got %d", len(empty))
	}
}
