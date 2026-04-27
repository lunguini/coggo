// Package auth implements bearer-token issuance, storage, and verification
// for v0.1. Tokens are stored in <dataDir>/tokens.json mode 0600. Secrets are
// shown once on issue; only sha256 hashes are persisted.
package auth

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lunguini/coggo/internal/types"
	"github.com/oklog/ulid/v2"
)

// Store implements types.Authority backed by a JSON file on disk.
type Store struct {
	path   string
	mu     sync.RWMutex
	tokens []*types.Token
}

// Open loads tokens.json from dataDir, creating an empty store if missing.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	path := filepath.Join(dataDir, "tokens.json")
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.tokens); err != nil {
		return nil, fmt.Errorf("auth: tokens.json: %w", err)
	}
	return s, nil
}

// Issue generates a new token authorized for the given peer names.
// Returns the ID and the plaintext secret; the secret is shown to the user
// once and never persisted.
func (s *Store) Issue(ctx context.Context, peers []string, label string) (string, string, error) {
	if len(peers) == 0 {
		return "", "", errors.New("auth: at least one peer required")
	}
	id := "tok_" + newULID()
	secret, err := newSecret()
	if err != nil {
		return "", "", fmt.Errorf("auth: %w", err)
	}
	tok := &types.Token{
		ID:         id,
		SecretHash: hashSecret(secret),
		Peers:      append([]string(nil), peers...),
		Label:      label,
		CreatedAt:  time.Now().UTC(),
	}
	s.mu.Lock()
	s.tokens = append(s.tokens, tok)
	if err := s.flushLocked(); err != nil {
		s.mu.Unlock()
		return "", "", fmt.Errorf("auth: %w", err)
	}
	s.mu.Unlock()
	return id, secret, nil
}

// Verify looks up a token by hashing the supplied secret and confirming it
// has authority over peerName. Updates LastUsedAt asynchronously.
func (s *Store) Verify(ctx context.Context, secret, peerName string) (*types.Token, error) {
	hash := hashSecret(secret)
	s.mu.RLock()
	var match *types.Token
	for _, t := range s.tokens {
		if t.SecretHash == hash {
			match = t
			break
		}
	}
	s.mu.RUnlock()
	if match == nil {
		return nil, errors.New("auth: unauthorized: unknown token")
	}
	if !tokenCoversPeer(match, peerName) {
		return nil, fmt.Errorf("auth: unauthorized: token not valid for peer %s", peerName)
	}
	go s.touch(match.ID)
	return match, nil
}

// List returns all known tokens (including their hashes; the caller decides
// what to display).
func (s *Store) List(ctx context.Context) ([]*types.Token, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*types.Token, len(s.tokens))
	copy(out, s.tokens)
	return out, nil
}

// Revoke removes the token with the given ID and flushes to disk.
func (s *Store) Revoke(ctx context.Context, tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tokens {
		if t.ID == tokenID {
			s.tokens = append(s.tokens[:i], s.tokens[i+1:]...)
			return s.flushLocked()
		}
	}
	return fmt.Errorf("auth: no token with id %q", tokenID)
}

func (s *Store) touch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tokens {
		if t.ID == id {
			t.LastUsedAt = time.Now().UTC()
			_ = s.flushLocked()
			return
		}
	}
}

func (s *Store) flushLocked() error {
	b, err := json.MarshalIndent(s.tokens, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func tokenCoversPeer(t *types.Token, name string) bool {
	for _, p := range t.Peers {
		if p == "*" || p == name {
			return true
		}
	}
	return false
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := cryptorand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// ulidEntropy is a non-crypto monotonic source for ULIDs; secret material
// uses crypto/rand above.
var (
	ulidMu      sync.Mutex
	ulidEntropy = ulid.Monotonic(newRandReader(), 0)
)

func newULID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), ulidEntropy).String()
}

// newRandReader returns a math/rand/v2 source adapted to io.Reader.
func newRandReader() *randReader {
	return &randReader{}
}

type randReader struct{}

func (r *randReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(mathrand.Uint32())
	}
	return len(p), nil
}
