package schema

import (
	"fmt"
	"sort"
	"sync"

	"github.com/lunguini/coggo/internal/types"
)

// Resolver is the in-memory registry of entity and relationship type
// definitions, keyed by peer DID. Federation handlers consult it on every
// write to avoid Store roundtrips during validation.
//
// The Store remains the durable source of truth; Resolver is a cache. Wiring
// code is responsible for re-hydrating the resolver from the Store at startup.
type Resolver struct {
	mu          sync.RWMutex
	entityTypes map[string]map[string]*types.EntityTypeDefinition
	relTypes    map[string]map[string]*types.RelationshipTypeDefinition
}

// NewResolver returns an empty resolver.
func NewResolver() *Resolver {
	return &Resolver{
		entityTypes: map[string]map[string]*types.EntityTypeDefinition{},
		relTypes:    map[string]map[string]*types.RelationshipTypeDefinition{},
	}
}

// RegisterEntityType adds or replaces an entity-type definition for a peer.
func (r *Resolver) RegisterEntityType(peerDID string, def *types.EntityTypeDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entityTypes[peerDID]; !ok {
		r.entityTypes[peerDID] = map[string]*types.EntityTypeDefinition{}
	}
	r.entityTypes[peerDID][def.Name] = def
}

// RegisterRelationType adds or replaces a relationship-type definition.
func (r *Resolver) RegisterRelationType(peerDID string, def *types.RelationshipTypeDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.relTypes[peerDID]; !ok {
		r.relTypes[peerDID] = map[string]*types.RelationshipTypeDefinition{}
	}
	r.relTypes[peerDID][def.Name] = def
}

// EntityType returns the named entity-type definition, or an error.
func (r *Resolver) EntityType(peerDID, name string) (*types.EntityTypeDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if peer, ok := r.entityTypes[peerDID]; ok {
		if def, ok := peer[name]; ok {
			return def, nil
		}
	}
	return nil, fmt.Errorf("schema: entity type %q not defined for peer %s", name, peerDID)
}

// RelationType returns the named relationship-type definition, or an error.
func (r *Resolver) RelationType(peerDID, name string) (*types.RelationshipTypeDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if peer, ok := r.relTypes[peerDID]; ok {
		if def, ok := peer[name]; ok {
			return def, nil
		}
	}
	return nil, fmt.Errorf("schema: relationship type %q not defined for peer %s", name, peerDID)
}

// EntityTypes returns all entity-type definitions registered for a peer,
// ordered by name.
func (r *Resolver) EntityTypes(peerDID string) []*types.EntityTypeDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	peer := r.entityTypes[peerDID]
	out := make([]*types.EntityTypeDefinition, 0, len(peer))
	for _, d := range peer {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RelationTypes returns all relationship-type definitions registered for a
// peer, ordered by name.
func (r *Resolver) RelationTypes(peerDID string) []*types.RelationshipTypeDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	peer := r.relTypes[peerDID]
	out := make([]*types.RelationshipTypeDefinition, 0, len(peer))
	for _, d := range peer {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
