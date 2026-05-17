package peer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lunguini/coggo/internal/types"
)

// Registry is the in-memory + on-disk index of peers hosted by this binary.
// Backed by a single peers.json file under the data dir; rewritten atomically.
//
// Per-peer state (events, entities) lives in the Store, not here. The Registry
// is the directory of identities and their settings only.
type Registry struct {
	path   string
	mu     sync.RWMutex
	byDID  map[string]*types.Peer
	byName map[string]*types.Peer
}

// Open loads the registry from disk, creating an empty file if missing.
func Open(dataDir string) (*Registry, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "peers.json")
	r := &Registry{
		path:   path,
		byDID:  map[string]*types.Peer{},
		byName: map[string]*types.Peer{},
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	var on []peerOnDisk
	if err := json.Unmarshal(b, &on); err != nil {
		return nil, fmt.Errorf("peers.json: %w", err)
	}
	for _, p := range on {
		peer := p.toPeer()
		r.byDID[peer.DID] = peer
		r.byName[peer.Name] = peer
	}
	return r, nil
}

// peerOnDisk is the storage encoding for a peer (PrivateKey IS persisted —
// the registry file is mode 0600 under the data dir).
type peerOnDisk struct {
	DID         string             `json:"did"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	PrivateKey  string             `json:"private_key"` // base64
	PublicKey   string             `json:"public_key"`  // base64
	CreatedAt   time.Time          `json:"created_at"`
	Settings    types.PeerSettings `json:"settings"`
}

func fromPeer(p *types.Peer) peerOnDisk {
	return peerOnDisk{
		DID:         p.DID,
		Name:        p.Name,
		Description: p.Description,
		PrivateKey:  base64StdEncode(p.PrivateKey),
		PublicKey:   base64StdEncode(p.PublicKey),
		CreatedAt:   p.CreatedAt,
		Settings:    p.Settings,
	}
}

func (p peerOnDisk) toPeer() *types.Peer {
	return &types.Peer{
		DID:         p.DID,
		Name:        p.Name,
		Description: p.Description,
		PrivateKey:  base64StdDecode(p.PrivateKey),
		PublicKey:   base64StdDecode(p.PublicKey),
		CreatedAt:   p.CreatedAt,
		Settings:    p.Settings,
	}
}

// Add registers a new peer. Fails if name or DID is already taken.
func (r *Registry) Add(p *types.Peer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byName[p.Name]; ok {
		return fmt.Errorf("peer name %q already exists", p.Name)
	}
	if _, ok := r.byDID[p.DID]; ok {
		return fmt.Errorf("peer DID %q already exists", p.DID)
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	r.byDID[p.DID] = p
	r.byName[p.Name] = p
	return r.flushLocked()
}

// Rename changes a peer's human-readable name.
func (r *Registry) Rename(oldName, newName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byName[oldName]
	if !ok {
		return fmt.Errorf("no peer named %q", oldName)
	}
	if _, taken := r.byName[newName]; taken {
		return fmt.Errorf("peer name %q already exists", newName)
	}
	delete(r.byName, oldName)
	p.Name = newName
	r.byName[newName] = p
	return r.flushLocked()
}

// UpdateSettings replaces the settings for a peer (looked up by name).
func (r *Registry) UpdateSettings(name string, s types.PeerSettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byName[name]
	if !ok {
		return fmt.Errorf("no peer named %q", name)
	}
	p.Settings = s
	return r.flushLocked()
}

// ByName returns the peer registered under name, or nil.
func (r *Registry) ByName(name string) *types.Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byName[name]
}

// ByDID returns the peer with the given DID, or nil.
func (r *Registry) ByDID(did string) *types.Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byDID[did]
}

// List returns all peers in deterministic order (by name).
func (r *Registry) List() []*types.Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*types.Peer, 0, len(r.byDID))
	for _, p := range r.byName {
		out = append(out, p)
	}
	// stable order by name
	sortByName(out)
	return out
}

// Resolve accepts either a peer name or a DID and returns the peer.
func (r *Registry) Resolve(nameOrDID string) (*types.Peer, error) {
	if p := r.ByName(nameOrDID); p != nil {
		return p, nil
	}
	if p := r.ByDID(nameOrDID); p != nil {
		return p, nil
	}
	return nil, fmt.Errorf("no peer matches %q", nameOrDID)
}

func (r *Registry) flushLocked() error {
	on := make([]peerOnDisk, 0, len(r.byDID))
	for _, p := range r.byName {
		on = append(on, fromPeer(p))
	}
	b, err := json.MarshalIndent(on, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}
