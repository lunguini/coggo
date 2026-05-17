package federation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lunguini/coggo/internal/types"
)

// mockStore is a minimal in-memory types.Store for unit tests in this package.
// It deliberately keeps semantics simple: no persistence, no indexing.
type mockStore struct {
	mu        sync.Mutex
	events    []*types.Event
	entities  map[string]*types.Entity // key = peerDID + "|" + id
	relations map[string]*types.Relation
}

func newMockStore() *mockStore {
	return &mockStore{
		entities:  map[string]*types.Entity{},
		relations: map[string]*types.Relation{},
	}
}

func key(peer, id string) string { return peer + "|" + id }

func (m *mockStore) Init(ctx context.Context) error { return nil }
func (m *mockStore) Close() error                   { return nil }

func (m *mockStore) AppendEvent(ctx context.Context, ev *types.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *ev
	m.events = append(m.events, &cp)
	return nil
}

func (m *mockStore) ListEvents(ctx context.Context, peerDID string, since, until time.Time) ([]*types.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *e
	m.entities[key(e.PeerDID, e.ID)] = &cp
	return nil
}

func (m *mockStore) ArchiveEntity(ctx context.Context, peerDID, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entities[key(peerDID, id)]
	if !ok {
		return fmt.Errorf("not found")
	}
	t := at
	e.ArchivedAt = &t
	return nil
}

func (m *mockStore) GetEntity(ctx context.Context, peerDID, id string) (*types.Entity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entities[key(peerDID, id)]
	if !ok {
		return nil, fmt.Errorf("entity %q not found", id)
	}
	cp := *e
	return &cp, nil
}

func (m *mockStore) QueryEntities(ctx context.Context, peerDID string, q types.EntityQuery) ([]*types.Entity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*types.Entity
	for _, e := range m.entities {
		if e.PeerDID != peerDID {
			continue
		}
		if q.Type != "" && e.Type != q.Type {
			continue
		}
		if !q.IncludeArchived && e.ArchivedAt != nil {
			continue
		}
		match := true
		for k, v := range q.Filters {
			if e.Data[k] != v {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		cp := *e
		out = append(out, &cp)
		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}
	return out, nil
}

func (m *mockStore) UpsertRelation(ctx context.Context, r *types.Relation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *r
	m.relations[key(r.PeerDID, r.ID)] = &cp
	return nil
}

func (m *mockStore) DissolveRelation(ctx context.Context, peerDID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.relations, key(peerDID, id))
	return nil
}

func (m *mockStore) QueryRelations(ctx context.Context, peerDID, from, to, relType string, limit int) ([]*types.Relation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*types.Relation
	for _, r := range m.relations {
		if r.PeerDID != peerDID {
			continue
		}
		if from != "" && r.From != from {
			continue
		}
		if to != "" && r.To != to {
			continue
		}
		if relType != "" && r.Type != relType {
			continue
		}
		cp := *r
		out = append(out, &cp)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *mockStore) UpsertEmbedding(ctx context.Context, peerDID, entityID string, vector []float32, model string) error {
	return nil
}

func (m *mockStore) SemanticSearch(ctx context.Context, peerDID string, vector []float32, typeFilter string, limit int) ([]types.SemanticHit, error) {
	return nil, nil
}

func (m *mockStore) SubstringSearch(ctx context.Context, peerDID, query, typeFilter string, limit int) ([]types.SemanticHit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []types.SemanticHit
	q := strings.ToLower(query)
	for _, e := range m.entities {
		if e.PeerDID != peerDID {
			continue
		}
		if typeFilter != "" && e.Type != typeFilter {
			continue
		}
		for _, v := range e.Data {
			if s, ok := v.(string); ok && strings.Contains(strings.ToLower(s), q) {
				cp := *e
				out = append(out, types.SemanticHit{Entity: &cp, Score: 1.0})
				break
			}
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *mockStore) TimeTravelEntities(ctx context.Context, peerDID string, q types.EntityQuery, asOf time.Time) ([]*types.Entity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Replay events up to asOf; rebuild entity state.
	state := map[string]*types.Entity{}
	for _, ev := range m.events {
		if ev.PeerDID != peerDID || ev.Timestamp.After(asOf) {
			continue
		}
		switch ev.Type {
		case types.EventEntityCreated, types.EventEntityUpdated:
			var e types.Entity
			if err := jsonUnmarshalLax(ev.Payload, &e); err == nil {
				state[e.ID] = &e
			}
		case types.EventEntityArchived:
			var p struct {
				ID         string    `json:"id"`
				ArchivedAt time.Time `json:"archived_at"`
			}
			if err := jsonUnmarshalLax(ev.Payload, &p); err == nil {
				if e, ok := state[p.ID]; ok {
					t := p.ArchivedAt
					e.ArchivedAt = &t
				}
			}
		}
	}
	var out []*types.Entity
	for _, e := range state {
		if q.Type != "" && e.Type != q.Type {
			continue
		}
		if !q.IncludeArchived && e.ArchivedAt != nil {
			continue
		}
		cp := *e
		out = append(out, &cp)
	}
	return out, nil
}
