package briefing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunguini/coggo/internal/peer"
	"github.com/lunguini/coggo/internal/types"
)

// minimal in-memory Store for briefing tests.
type mockStore struct {
	mu       sync.Mutex
	events   []*types.Event
	entities []*types.Entity
}

func (m *mockStore) Init(context.Context) error { return nil }
func (m *mockStore) Close() error               { return nil }
func (m *mockStore) AppendEvent(ctx context.Context, ev *types.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}
func (m *mockStore) ListEvents(ctx context.Context, peerDID string, since, until time.Time) ([]*types.Event, error) {
	var out []*types.Event
	for _, e := range m.events {
		if e.PeerDID != peerDID {
			continue
		}
		if !since.IsZero() && e.Timestamp.Before(since) {
			continue
		}
		if !until.IsZero() && e.Timestamp.After(until) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
func (m *mockStore) UpsertEntity(ctx context.Context, e *types.Entity) error {
	m.entities = append(m.entities, e)
	return nil
}
func (m *mockStore) ArchiveEntity(context.Context, string, string, time.Time) error { return nil }
func (m *mockStore) GetEntity(context.Context, string, string) (*types.Entity, error) {
	return nil, fmt.Errorf("nope")
}
func (m *mockStore) QueryEntities(ctx context.Context, peerDID string, q types.EntityQuery) ([]*types.Entity, error) {
	var out []*types.Entity
	for _, e := range m.entities {
		if e.PeerDID != peerDID {
			continue
		}
		if q.Type != "" && e.Type != q.Type {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
func (m *mockStore) UpsertRelation(context.Context, *types.Relation) error { return nil }
func (m *mockStore) DissolveRelation(context.Context, string, string) error { return nil }
func (m *mockStore) QueryRelations(context.Context, string, string, string, string, int) ([]*types.Relation, error) {
	return nil, nil
}
func (m *mockStore) UpsertEmbedding(context.Context, string, string, []float32, string) error {
	return nil
}
func (m *mockStore) SemanticSearch(context.Context, string, []float32, string, int) ([]types.SemanticHit, error) {
	return nil, nil
}
func (m *mockStore) SubstringSearch(context.Context, string, string, string, int) ([]types.SemanticHit, error) {
	return nil, nil
}
func (m *mockStore) TimeTravelEntities(context.Context, string, types.EntityQuery, time.Time) ([]*types.Entity, error) {
	return nil, nil
}

func setup(t *testing.T) (*peer.Registry, *mockStore, *types.Peer, *types.Peer) {
	t.Helper()
	dir := t.TempDir()
	reg, err := peer.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	business, _ := peer.NewPeer("business", "")
	if err := reg.Add(business); err != nil {
		t.Fatal(err)
	}
	coggo, _ := peer.NewPeer("coggo", "")
	if err := reg.Add(coggo); err != nil {
		t.Fatal(err)
	}
	return reg, &mockStore{}, business, coggo
}

func ent(peerDID, typ string, data map[string]any, createdAgo time.Duration, updatedAgo time.Duration, now time.Time) *types.Entity {
	id := fmt.Sprintf("%s-%d", typ, time.Now().UnixNano())
	return &types.Entity{
		ID: id, Type: typ, PeerDID: peerDID,
		CreatedAt: now.Add(-createdAgo), UpdatedAt: now.Add(-updatedAgo),
		Data: data,
	}
}

func TestGenerateAllPeers(t *testing.T) {
	reg, store, business, coggo := setup(t)
	now := time.Now().UTC()
	ctx := context.Background()

	store.entities = append(store.entities,
		ent(business.DID, "Goal", map[string]any{"title": "ship v0.1", "status": "open"}, 2*24*time.Hour, 2*24*time.Hour, now),
		ent(business.DID, "Goal", map[string]any{"title": "done", "status": "achieved"}, 30*24*time.Hour, 30*24*time.Hour, now),
		ent(business.DID, "Project", map[string]any{"title": "Coggo", "status": "active", "completion_estimate": 30}, 30*24*time.Hour, 1*24*time.Hour, now),
		ent(business.DID, "Observation", map[string]any{"text": "noticed something cool"}, 1*24*time.Hour, 1*24*time.Hour, now),
		ent(business.DID, "Observation", map[string]any{"text": "old observation"}, 30*24*time.Hour, 30*24*time.Hour, now),
		ent(business.DID, "Decision", map[string]any{"title": "use SurrealDB"}, 1*24*time.Hour, 1*24*time.Hour, now),
		ent(coggo.DID, "Decision", map[string]any{"title": "v0.1 scope"}, 5*24*time.Hour, 5*24*time.Hour, now),
	)
	// One create event in coggo last 7 days.
	store.events = append(store.events, &types.Event{
		ID: "e1", PeerDID: coggo.DID, Type: types.EventEntityCreated,
		Payload: json.RawMessage("{}"), Timestamp: now.Add(-2 * 24 * time.Hour),
	})

	b, err := Generate(ctx, reg, store, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(b.Peers))
	}

	var biz, cog *PeerSection
	for i := range b.Peers {
		switch b.Peers[i].Name {
		case "business":
			biz = &b.Peers[i]
		case "coggo":
			cog = &b.Peers[i]
		}
	}
	if biz == nil || cog == nil {
		t.Fatalf("missing peers: %+v", b.Peers)
	}

	if !strings.Contains(biz.Sections[0].Title, "1)") { // open goals
		t.Errorf("expected 1 open goal, got %q", biz.Sections[0].Title)
	}
	if !strings.Contains(biz.Sections[1].Title, "1)") { // recent observations
		t.Errorf("expected 1 recent observation, got %q", biz.Sections[1].Title)
	}
	if !strings.Contains(biz.Sections[3].Title, "1)") { // active projects
		t.Errorf("expected 1 active project, got %q", biz.Sections[3].Title)
	}
	if !strings.Contains(biz.Sections[4].Title, "1)") { // recent decisions
		t.Errorf("expected 1 recent decision, got %q", biz.Sections[4].Title)
	}

	// coggo gets two extra sections at the end
	if len(cog.Sections) < 7 {
		t.Fatalf("coggo expected extra sections, got %d", len(cog.Sections))
	}
	last := cog.Sections[len(cog.Sections)-1]
	if !strings.Contains(last.Title, "1)") {
		t.Errorf("expected 1 recent change in coggo, got %q", last.Title)
	}

	out := Render(b)
	if !strings.Contains(out, "Coggo daily briefing") {
		t.Errorf("render missing header: %s", out)
	}
}

func TestGenerateSinglePeer(t *testing.T) {
	reg, store, business, _ := setup(t)
	store.entities = append(store.entities,
		ent(business.DID, "Goal", map[string]any{"title": "x"}, 0, 0, time.Now().UTC()),
	)
	b, err := Generate(context.Background(), reg, store, "business", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Peers) != 1 || b.Peers[0].Name != "business" {
		t.Fatalf("expected one business peer, got %+v", b.Peers)
	}
}
