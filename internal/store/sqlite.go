// Package store implements the persistence layer for Coggo v0.1.
//
// The backend is SQLite (via mattn/go-sqlite3) augmented with the sqlite-vec
// extension for vector similarity search. A single SQLite database backs the
// whole binary; per-peer isolation is enforced at the application layer by
// tagging every row with PeerDID.
//
// See .ai/docs/decisions.md for the rationale on choosing SQLite over the
// originally planned SurrealDB embedded backend.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
	"github.com/oklog/ulid/v2"

	"github.com/lunguini/coggo/internal/types"
)

// Store is the SQLite-backed implementation of types.Store.
type Store struct {
	db  *sql.DB
	dim int

	mu      sync.Mutex
	entropy *ulid.MonotonicEntropy
}

var registerVecOnce sync.Once

// New constructs a Store backed by a SQLite database at the given path. The
// dim argument fixes the dimensionality of vectors stored in the
// entity_embeddings vec0 virtual table; it must match the embedder used to
// generate vectors that will be inserted later.
//
// The returned Store is not yet initialised — call Init before use.
func New(dbPath string, dim int) (*Store, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("store: db path required")
	}
	if dim <= 0 {
		return nil, fmt.Errorf("store: vector dimension must be positive, got %d", dim)
	}

	// sqlite_vec.Auto() registers the sqlite-vec extension as auto-loaded for
	// every subsequently-opened sqlite3 connection. Safe to call once per
	// process.
	var registerErr error
	registerVecOnce.Do(func() {
		sqlite_vec.Auto()
	})
	if registerErr != nil {
		return nil, fmt.Errorf("store: register sqlite-vec: %w", registerErr)
	}

	dsn := fmt.Sprintf("file:%s?_journal=WAL&_busy_timeout=5000&_foreign_keys=on", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite (%s): %w", dbPath, err)
	}

	// SQLite is single-writer; cap connection pool to keep behaviour
	// predictable across goroutines.
	db.SetMaxOpenConns(1)

	s := &Store{
		db:      db,
		dim:     dim,
		entropy: ulid.Monotonic(rand.Reader, 0),
	}
	return s, nil
}

// Init creates the schema if it does not exist. Idempotent.
func (s *Store) Init(ctx context.Context) error {
	stmts := []string{
		// events: append-only source of truth
		`CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY,
			peer_did TEXT NOT NULL,
			type TEXT NOT NULL,
			payload BLOB NOT NULL,
			timestamp TEXT NOT NULL,
			author_did TEXT NOT NULL,
			client_id TEXT NOT NULL,
			signature BLOB
		)`,
		`CREATE INDEX IF NOT EXISTS events_peer_did ON events(peer_did)`,
		`CREATE INDEX IF NOT EXISTS events_timestamp ON events(timestamp)`,
		`CREATE INDEX IF NOT EXISTS events_type ON events(type)`,

		// Triggers enforce immutability at the database layer.
		`CREATE TRIGGER IF NOT EXISTS events_no_update
			BEFORE UPDATE ON events
			BEGIN
				SELECT RAISE(ABORT, 'events table is append-only');
			END`,
		`CREATE TRIGGER IF NOT EXISTS events_no_delete
			BEFORE DELETE ON events
			BEGIN
				SELECT RAISE(ABORT, 'events table is append-only');
			END`,

		// entities: materialised state
		`CREATE TABLE IF NOT EXISTS entities (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			peer_did TEXT NOT NULL,
			created_at TEXT NOT NULL,
			created_by_did TEXT NOT NULL,
			created_by_client TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			archived_at TEXT,
			data BLOB NOT NULL,
			embedding_id TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS entities_peer_type ON entities(peer_did, type)`,
		`CREATE INDEX IF NOT EXISTS entities_archived ON entities(archived_at)`,

		// relations: materialised edges
		`CREATE TABLE IF NOT EXISTS relations (
			id TEXT PRIMARY KEY,
			from_id TEXT NOT NULL,
			to_id TEXT NOT NULL,
			type TEXT NOT NULL,
			peer_did TEXT NOT NULL,
			created_at TEXT NOT NULL,
			created_by_did TEXT NOT NULL,
			created_by_client TEXT NOT NULL,
			data BLOB NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS relations_from ON relations(from_id)`,
		`CREATE INDEX IF NOT EXISTS relations_to ON relations(to_id)`,
		`CREATE INDEX IF NOT EXISTS relations_type ON relations(type)`,

		// embedding metadata (vec0 has no general-purpose filter columns)
		`CREATE TABLE IF NOT EXISTS embedding_meta (
			entity_id TEXT PRIMARY KEY,
			peer_did TEXT NOT NULL,
			model TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS embedding_meta_peer ON embedding_meta(peer_did)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("store: exec schema (%q): %w", firstLine(stmt), err)
		}
	}

	// vec0 virtual table: dimension is fixed at create time.
	vecStmt := fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS entity_embeddings USING vec0(
			entity_id TEXT PRIMARY KEY,
			embedding float[%d]
		)`, s.dim)
	if _, err := s.db.ExecContext(ctx, vecStmt); err != nil {
		return fmt.Errorf("store: create entity_embeddings vec0 table: %w", err)
	}

	return nil
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("store: close: %w", err)
	}
	return nil
}

// DB returns the underlying *sql.DB. Intended for test code that needs to
// poke at the schema directly (e.g. asserting append-only triggers fire).
// Production code MUST go through the Store interface.
func (s *Store) DB() *sql.DB {
	return s.db
}

// ---------- Events ----------

// AppendEvent inserts an event into the append-only log. If ev.ID is empty a
// fresh ULID is allocated; if ev.Timestamp is zero, the current UTC time is
// used.
func (s *Store) AppendEvent(ctx context.Context, ev *types.Event) error {
	if ev == nil {
		return fmt.Errorf("store: AppendEvent: nil event")
	}
	if ev.PeerDID == "" {
		return fmt.Errorf("store: AppendEvent: missing peer_did")
	}
	if ev.Type == "" {
		return fmt.Errorf("store: AppendEvent: missing type")
	}
	if ev.ID == "" {
		ev.ID = s.newULID()
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}

	payload := []byte(ev.Payload)
	if len(payload) == 0 {
		payload = []byte("null")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO events (id, peer_did, type, payload, timestamp, author_did, client_id, signature)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.PeerDID, string(ev.Type), payload,
		formatTime(ev.Timestamp), ev.AuthorDID, ev.ClientID, ev.Signature,
	)
	if err != nil {
		return fmt.Errorf("store: insert event %s: %w", ev.ID, err)
	}
	slog.Debug("store.event.appended",
		"id", ev.ID, "peer_did", ev.PeerDID, "type", string(ev.Type))
	return nil
}

// ListEvents returns events in [since, until], ascending by timestamp then ID.
// Zero values for since/until mean unbounded on that side.
func (s *Store) ListEvents(ctx context.Context, peerDID string, since, until time.Time) ([]*types.Event, error) {
	q := `SELECT id, peer_did, type, payload, timestamp, author_did, client_id, signature
		FROM events WHERE peer_did = ?`
	args := []any{peerDID}
	if !since.IsZero() {
		q += ` AND timestamp >= ?`
		args = append(args, formatTime(since))
	}
	if !until.IsZero() {
		q += ` AND timestamp <= ?`
		args = append(args, formatTime(until))
	}
	q += ` ORDER BY timestamp ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query events for %s: %w", peerDID, err)
	}
	defer rows.Close()

	var out []*types.Event
	for rows.Next() {
		ev := &types.Event{}
		var typeStr, ts string
		var payload []byte
		var sig []byte
		if err := rows.Scan(&ev.ID, &ev.PeerDID, &typeStr, &payload, &ts, &ev.AuthorDID, &ev.ClientID, &sig); err != nil {
			return nil, fmt.Errorf("store: scan event: %w", err)
		}
		ev.Type = types.EventType(typeStr)
		ev.Payload = json.RawMessage(payload)
		ev.Signature = sig
		t, err := parseTime(ts)
		if err != nil {
			return nil, fmt.Errorf("store: parse event timestamp %q: %w", ts, err)
		}
		ev.Timestamp = t
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate events: %w", err)
	}
	return out, nil
}

// ---------- Entities ----------

// UpsertEntity inserts or replaces an entity row.
func (s *Store) UpsertEntity(ctx context.Context, e *types.Entity) error {
	if e == nil {
		return fmt.Errorf("store: UpsertEntity: nil entity")
	}
	if e.ID == "" {
		return fmt.Errorf("store: UpsertEntity: missing id")
	}
	if e.PeerDID == "" {
		return fmt.Errorf("store: UpsertEntity: missing peer_did")
	}
	if e.Type == "" {
		slog.Warn("store.entity.upsert.missing_type", "id", e.ID, "peer_did", e.PeerDID)
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = now
	}
	data := e.Data
	if data == nil {
		data = map[string]any{}
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("store: marshal entity %s data: %w", e.ID, err)
	}

	var archived any
	if e.ArchivedAt != nil {
		archived = formatTime(*e.ArchivedAt)
	}
	var embedding any
	if e.EmbeddingID != nil {
		embedding = *e.EmbeddingID
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO entities
			(id, type, peer_did, created_at, created_by_did, created_by_client,
			 updated_at, archived_at, data, embedding_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Type, e.PeerDID,
		formatTime(e.CreatedAt), e.CreatedByDID, e.CreatedByClient,
		formatTime(e.UpdatedAt), archived, dataBytes, embedding,
	)
	if err != nil {
		return fmt.Errorf("store: upsert entity %s: %w", e.ID, err)
	}
	return nil
}

// ArchiveEntity marks an entity as archived without deleting it.
func (s *Store) ArchiveEntity(ctx context.Context, peerDID, id string, at time.Time) error {
	if peerDID == "" || id == "" {
		return fmt.Errorf("store: ArchiveEntity: peer_did and id required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE entities SET archived_at = ?, updated_at = ?
		WHERE peer_did = ? AND id = ?`,
		formatTime(at), formatTime(at), peerDID, id)
	if err != nil {
		return fmt.Errorf("store: archive entity %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: archive entity %s: not found for peer %s", id, peerDID)
	}
	return nil
}

// GetEntity returns the entity with the given (peer, id), or (nil, nil) if not
// found.
func (s *Store) GetEntity(ctx context.Context, peerDID, id string) (*types.Entity, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, peer_did, created_at, created_by_did, created_by_client,
		       updated_at, archived_at, data, embedding_id
		FROM entities WHERE peer_did = ? AND id = ?`, peerDID, id)
	e, err := scanEntity(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get entity %s: %w", id, err)
	}
	return e, nil
}

// QueryEntities lists entities for a peer, optionally filtered by type and
// JSON field equality. Archived rows are excluded unless q.IncludeArchived is
// true. Default Limit is 50.
func (s *Store) QueryEntities(ctx context.Context, peerDID string, q types.EntityQuery) ([]*types.Entity, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}

	var (
		conds = []string{"peer_did = ?"}
		args  = []any{peerDID}
	)
	if q.Type != "" {
		conds = append(conds, "type = ?")
		args = append(args, q.Type)
	}
	if !q.IncludeArchived {
		conds = append(conds, "archived_at IS NULL")
	}

	// Stable order over filter keys for deterministic SQL and predictable
	// query plans.
	keys := make([]string, 0, len(q.Filters))
	for k := range q.Filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		conds = append(conds, "json_extract(data, '$.'||?) = ?")
		args = append(args, k, q.Filters[k])
	}

	sqlStr := fmt.Sprintf(`
		SELECT id, type, peer_did, created_at, created_by_did, created_by_client,
		       updated_at, archived_at, data, embedding_id
		FROM entities WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, strings.Join(conds, " AND "))
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query entities: %w", err)
	}
	defer rows.Close()

	var out []*types.Entity
	for rows.Next() {
		e, err := scanEntity(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("store: scan entity row: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate entity rows: %w", err)
	}
	return out, nil
}

// ---------- Relations ----------

// UpsertRelation inserts or replaces a relation row.
func (s *Store) UpsertRelation(ctx context.Context, r *types.Relation) error {
	if r == nil {
		return fmt.Errorf("store: UpsertRelation: nil relation")
	}
	if r.ID == "" {
		return fmt.Errorf("store: UpsertRelation: missing id")
	}
	if r.PeerDID == "" || r.From == "" || r.To == "" || r.Type == "" {
		return fmt.Errorf("store: UpsertRelation: peer_did, from, to, type required")
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	data := r.Data
	if data == nil {
		data = map[string]any{}
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("store: marshal relation %s data: %w", r.ID, err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO relations
			(id, from_id, to_id, type, peer_did, created_at, created_by_did, created_by_client, data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.From, r.To, r.Type, r.PeerDID,
		formatTime(r.CreatedAt), r.CreatedByDID, r.CreatedByClient, dataBytes,
	)
	if err != nil {
		return fmt.Errorf("store: upsert relation %s: %w", r.ID, err)
	}
	return nil
}

// DissolveRelation removes a relation row. v0.1 deletes outright; the
// canonical record lives in the events log.
func (s *Store) DissolveRelation(ctx context.Context, peerDID, id string) error {
	if peerDID == "" || id == "" {
		return fmt.Errorf("store: DissolveRelation: peer_did and id required")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM relations WHERE peer_did = ? AND id = ?`, peerDID, id)
	if err != nil {
		return fmt.Errorf("store: dissolve relation %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: dissolve relation %s: not found for peer %s", id, peerDID)
	}
	return nil
}

// QueryRelations returns relations matching the given non-empty filters.
func (s *Store) QueryRelations(ctx context.Context, peerDID, from, to, relType string, limit int) ([]*types.Relation, error) {
	if limit <= 0 {
		limit = 50
	}
	conds := []string{"peer_did = ?"}
	args := []any{peerDID}
	if from != "" {
		conds = append(conds, "from_id = ?")
		args = append(args, from)
	}
	if to != "" {
		conds = append(conds, "to_id = ?")
		args = append(args, to)
	}
	if relType != "" {
		conds = append(conds, "type = ?")
		args = append(args, relType)
	}
	sqlStr := fmt.Sprintf(`
		SELECT id, from_id, to_id, type, peer_did, created_at, created_by_did, created_by_client, data
		FROM relations WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, strings.Join(conds, " AND "))
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query relations: %w", err)
	}
	defer rows.Close()

	var out []*types.Relation
	for rows.Next() {
		r := &types.Relation{}
		var ts string
		var data []byte
		if err := rows.Scan(&r.ID, &r.From, &r.To, &r.Type, &r.PeerDID, &ts,
			&r.CreatedByDID, &r.CreatedByClient, &data); err != nil {
			return nil, fmt.Errorf("store: scan relation: %w", err)
		}
		t, err := parseTime(ts)
		if err != nil {
			return nil, fmt.Errorf("store: parse relation timestamp: %w", err)
		}
		r.CreatedAt = t
		if err := json.Unmarshal(data, &r.Data); err != nil {
			return nil, fmt.Errorf("store: unmarshal relation %s data: %w", r.ID, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate relation rows: %w", err)
	}
	return out, nil
}

// ---------- Embeddings ----------

// UpsertEmbedding writes or replaces the embedding for an entity.
func (s *Store) UpsertEmbedding(ctx context.Context, peerDID, entityID string, vector []float32, model string) error {
	if peerDID == "" || entityID == "" {
		return fmt.Errorf("store: UpsertEmbedding: peer_did and entity_id required")
	}
	if len(vector) != s.dim {
		return fmt.Errorf("store: UpsertEmbedding: vector dim %d does not match store dim %d", len(vector), s.dim)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin embedding tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM entity_embeddings WHERE entity_id = ?`, entityID); err != nil {
		return fmt.Errorf("store: clear existing embedding: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM embedding_meta WHERE entity_id = ?`, entityID); err != nil {
		return fmt.Errorf("store: clear existing embedding meta: %w", err)
	}

	blob, err := sqlite_vec.SerializeFloat32(vector)
	if err != nil {
		return fmt.Errorf("store: serialize vector: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO entity_embeddings (entity_id, embedding) VALUES (?, ?)`,
		entityID, blob,
	); err != nil {
		return fmt.Errorf("store: insert embedding for %s: %w", entityID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO embedding_meta (entity_id, peer_did, model, created_at)
		VALUES (?, ?, ?, ?)`,
		entityID, peerDID, model, formatTime(time.Now().UTC()),
	); err != nil {
		return fmt.Errorf("store: insert embedding meta for %s: %w", entityID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit embedding tx: %w", err)
	}
	return nil
}

// SemanticSearch returns the top-`limit` entities by cosine similarity to the
// given vector, restricted to the given peer (and optionally entity type).
// Returns an empty slice if vector is empty.
func (s *Store) SemanticSearch(ctx context.Context, peerDID string, vector []float32, typeFilter string, limit int) ([]types.SemanticHit, error) {
	if len(vector) == 0 {
		return nil, nil
	}
	if len(vector) != s.dim {
		return nil, fmt.Errorf("store: SemanticSearch: vector dim %d does not match store dim %d", len(vector), s.dim)
	}
	if limit <= 0 {
		limit = 10
	}

	blob, err := sqlite_vec.SerializeFloat32(vector)
	if err != nil {
		return nil, fmt.Errorf("store: serialize query vector: %w", err)
	}

	// vec0 supports KNN via MATCH + k. We over-fetch slightly so that
	// peer/type filtering doesn't starve the result set in common cases.
	k := limit * 4
	if k < 16 {
		k = 16
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT entity_id, distance FROM entity_embeddings
		WHERE embedding MATCH ? AND k = ?
		ORDER BY distance ASC`, blob, k)
	if err != nil {
		return nil, fmt.Errorf("store: vec0 knn query: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		id       string
		distance float64
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.distance); err != nil {
			return nil, fmt.Errorf("store: scan vec hit: %w", err)
		}
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate vec hits: %w", err)
	}

	out := make([]types.SemanticHit, 0, len(cands))
	for _, c := range cands {
		ent, err := s.GetEntity(ctx, peerDID, c.id)
		if err != nil {
			return nil, fmt.Errorf("store: hydrate semantic hit %s: %w", c.id, err)
		}
		if ent == nil {
			continue // belongs to another peer or has been hard-deleted
		}
		if ent.PeerDID != peerDID {
			continue
		}
		if typeFilter != "" && ent.Type != typeFilter {
			continue
		}
		if ent.ArchivedAt != nil {
			continue
		}
		out = append(out, types.SemanticHit{
			Entity: ent,
			Score:  1.0 - c.distance,
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// SubstringSearch returns entities whose JSON data contains the given query
// substring. Score is always 1.0.
func (s *Store) SubstringSearch(ctx context.Context, peerDID, query, typeFilter string, limit int) ([]types.SemanticHit, error) {
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}

	conds := []string{"peer_did = ?", "archived_at IS NULL", "data LIKE '%' || ? || '%'"}
	args := []any{peerDID, query}
	if typeFilter != "" {
		conds = append(conds, "type = ?")
		args = append(args, typeFilter)
	}
	sqlStr := fmt.Sprintf(`
		SELECT id, type, peer_did, created_at, created_by_did, created_by_client,
		       updated_at, archived_at, data, embedding_id
		FROM entities WHERE %s
		ORDER BY updated_at DESC, id DESC
		LIMIT ?`, strings.Join(conds, " AND "))
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("store: substring search: %w", err)
	}
	defer rows.Close()

	var out []types.SemanticHit
	for rows.Next() {
		e, err := scanEntity(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("store: scan substring hit: %w", err)
		}
		out = append(out, types.SemanticHit{Entity: e, Score: 1.0})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate substring hits: %w", err)
	}
	return out, nil
}

// ---------- Time travel ----------

// TimeTravelEntities reconstructs entity state by folding the event log up to
// (and including) asOf, then applying the EntityQuery filters in Go. Intended
// for occasional historical queries — the spec marks this "use sparingly".
func (s *Store) TimeTravelEntities(ctx context.Context, peerDID string, q types.EntityQuery, asOf time.Time) ([]*types.Entity, error) {
	events, err := s.ListEvents(ctx, peerDID, time.Time{}, asOf)
	if err != nil {
		return nil, fmt.Errorf("store: time travel: load events: %w", err)
	}

	state := map[string]*types.Entity{}
	for _, ev := range events {
		switch ev.Type {
		case types.EventEntityCreated:
			ent, err := decodeEntityPayload(ev)
			if err != nil {
				slog.Warn("store.timetravel.bad_create_payload", "event_id", ev.ID, "err", err)
				continue
			}
			state[ent.ID] = ent

		case types.EventEntityUpdated:
			ent, err := decodeEntityPayload(ev)
			if err != nil {
				slog.Warn("store.timetravel.bad_update_payload", "event_id", ev.ID, "err", err)
				continue
			}
			cur, ok := state[ent.ID]
			if !ok {
				// Update without prior create — accept the snapshot as-is.
				state[ent.ID] = ent
				continue
			}
			// Merge: replace data + bump updated_at, preserve creation metadata.
			if ent.Data != nil {
				cur.Data = ent.Data
			}
			if ent.Type != "" {
				cur.Type = ent.Type
			}
			if !ev.Timestamp.IsZero() {
				cur.UpdatedAt = ev.Timestamp
			} else if !ent.UpdatedAt.IsZero() {
				cur.UpdatedAt = ent.UpdatedAt
			}

		case types.EventEntityArchived:
			var p struct {
				ID         string    `json:"id"`
				ArchivedAt time.Time `json:"archived_at"`
			}
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				slog.Warn("store.timetravel.bad_archive_payload", "event_id", ev.ID, "err", err)
				continue
			}
			cur, ok := state[p.ID]
			if !ok {
				continue
			}
			at := p.ArchivedAt
			if at.IsZero() {
				at = ev.Timestamp
			}
			cur.ArchivedAt = &at
		}
	}

	// Apply EntityQuery filters in Go.
	out := make([]*types.Entity, 0, len(state))
	for _, ent := range state {
		if ent.PeerDID == "" {
			ent.PeerDID = peerDID
		}
		if ent.PeerDID != peerDID {
			continue
		}
		if !q.IncludeArchived && ent.ArchivedAt != nil {
			continue
		}
		if q.Type != "" && ent.Type != q.Type {
			continue
		}
		if !matchesFilters(ent.Data, q.Filters) {
			continue
		}
		out = append(out, ent)
	}

	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})

	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------- helpers ----------

func decodeEntityPayload(ev *types.Event) (*types.Entity, error) {
	if len(ev.Payload) == 0 {
		return nil, fmt.Errorf("empty payload")
	}
	ent := &types.Entity{}
	if err := json.Unmarshal(ev.Payload, ent); err != nil {
		return nil, err
	}
	if ent.ID == "" {
		return nil, fmt.Errorf("payload missing id")
	}
	if ent.CreatedAt.IsZero() {
		ent.CreatedAt = ev.Timestamp
	}
	if ent.UpdatedAt.IsZero() {
		ent.UpdatedAt = ev.Timestamp
	}
	if ent.PeerDID == "" {
		ent.PeerDID = ev.PeerDID
	}
	return ent, nil
}

func matchesFilters(data map[string]any, filters map[string]any) bool {
	for k, want := range filters {
		got, ok := data[k]
		if !ok {
			return false
		}
		// Loose compare via JSON canonicalisation: matches SQLite json_extract
		// equality semantics closely enough for v0.1.
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(want)
		if string(gotJSON) != string(wantJSON) {
			return false
		}
	}
	return true
}

// scanEntity scans one entity row from either *sql.Row or *sql.Rows via the
// given Scan func.
func scanEntity(scan func(dest ...any) error) (*types.Entity, error) {
	e := &types.Entity{}
	var (
		createdAt, updatedAt string
		archivedAt           sql.NullString
		data                 []byte
		embedding            sql.NullString
	)
	if err := scan(&e.ID, &e.Type, &e.PeerDID, &createdAt, &e.CreatedByDID, &e.CreatedByClient,
		&updatedAt, &archivedAt, &data, &embedding); err != nil {
		return nil, err
	}
	t, err := parseTime(createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at %q: %w", createdAt, err)
	}
	e.CreatedAt = t
	t, err = parseTime(updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at %q: %w", updatedAt, err)
	}
	e.UpdatedAt = t
	if archivedAt.Valid {
		t, err := parseTime(archivedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse archived_at %q: %w", archivedAt.String, err)
		}
		e.ArchivedAt = &t
	}
	if embedding.Valid {
		v := embedding.String
		e.EmbeddingID = &v
	}
	if len(data) == 0 {
		e.Data = map[string]any{}
	} else {
		if err := json.Unmarshal(data, &e.Data); err != nil {
			return nil, fmt.Errorf("unmarshal entity data: %w", err)
		}
	}
	return e, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unrecognised time format")
}

func (s *Store) newULID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), s.entropy).String()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Compile-time assertion that *Store satisfies types.Store.
var _ types.Store = (*Store)(nil)
