package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunguini/coggo/internal/auth"
	"github.com/lunguini/coggo/internal/federation"
	"github.com/lunguini/coggo/internal/peer"
	"github.com/lunguini/coggo/internal/schema"
	"github.com/lunguini/coggo/internal/store"
	"github.com/lunguini/coggo/internal/types"
)

// testHarness wires the same components the prod binary will: store, peer
// registry, auth store, federation router, and the MCP server itself, all
// rooted in t.TempDir() and bound to a random localhost port.
type testHarness struct {
	t           *testing.T
	srv         *Server
	url         string
	tokenA      string
	tokenAOnly  string
	peerA       *types.Peer
	peerB       *types.Peer
	cancel      context.CancelFunc
	doneCh      chan struct{}
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	dir := t.TempDir()

	st, err := store.New(filepath.Join(dir, "coggo.db"), 64)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := st.Init(ctx); err != nil {
		t.Fatalf("store init: %v", err)
	}

	reg, err := peer.Open(dir)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	peerA, err := peer.NewPeer("alpha", "alpha test peer")
	if err != nil {
		t.Fatalf("new peer: %v", err)
	}
	if err := reg.Add(peerA); err != nil {
		t.Fatalf("add peer A: %v", err)
	}
	peerB, err := peer.NewPeer("beta", "beta test peer")
	if err != nil {
		t.Fatalf("new peer: %v", err)
	}
	if err := reg.Add(peerB); err != nil {
		t.Fatalf("add peer B: %v", err)
	}

	authStore, err := auth.Open(dir)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	_, secretAOnly, err := authStore.Issue(ctx, []string{"alpha"}, "alpha-only")
	if err != nil {
		t.Fatalf("issue alpha-only: %v", err)
	}
	_, secretBoth, err := authStore.Issue(ctx, []string{"alpha", "beta"}, "both")
	if err != nil {
		t.Fatalf("issue both: %v", err)
	}

	resolver := schema.NewResolver()
	for _, p := range []*types.Peer{peerA, peerB} {
		for _, def := range schema.SeedEntityTypes(p.DID) {
			resolver.RegisterEntityType(p.DID, def)
		}
		for _, def := range schema.SeedRelationshipTypes(p.DID) {
			resolver.RegisterRelationType(p.DID, def)
		}
	}

	router := federation.New()
	for _, p := range []*types.Peer{peerA, peerB} {
		h := federation.NewStoreHandler(p.DID, st, resolver)
		if err := router.RegisterPeer(p.DID, h); err != nil {
			t.Fatalf("register peer %s: %v", p.Name, err)
		}
	}

	// Pick a free port to avoid clashes when tests run in parallel.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	srv := New(router, reg, authStore, addr)
	runCtx, runCancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		_ = srv.Start(runCtx)
		close(doneCh)
	}()
	t.Cleanup(func() {
		runCancel()
		select {
		case <-doneCh:
		case <-time.After(2 * time.Second):
		}
	})

	url := "http://" + addr + srv.EndpointPath
	// Wait for the server to start accepting connections.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	return &testHarness{
		t: t, srv: srv, url: url,
		tokenAOnly: secretAOnly, tokenA: secretBoth,
		peerA: peerA, peerB: peerB,
		cancel: runCancel, doneCh: doneCh,
	}
}

// rpcCall posts a JSON-RPC request and returns the parsed response object.
func (h *testHarness) rpcCall(t *testing.T, token string, method string, params any) map[string]any {
	t.Helper()
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		body["params"] = params
	}
	bb, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.url, bytes.NewReader(bb))
	req.Header.Set("Content-Type", "application/json")
	// MCP streamable transport requires Accept include both JSON and SSE.
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rpc %s: %v", method, err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rpc %s: status %d body=%s", method, resp.StatusCode, string(rb))
	}
	// Streamable HTTP may return either application/json or text/event-stream.
	ct := resp.Header.Get("Content-Type")
	var raw []byte
	if strings.HasPrefix(ct, "text/event-stream") {
		raw = extractSSEData(rb)
	} else {
		raw = rb
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("rpc %s: parse %s: %v\nbody=%s", method, ct, err, string(rb))
	}
	return out
}

// extractSSEData pulls the first "data: ..." JSON payload out of an SSE blob.
func extractSSEData(b []byte) []byte {
	for _, line := range bytes.Split(b, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data: ")) {
			return bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data: ")))
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			return bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		}
	}
	return b
}

// callTool is a convenience wrapper for tools/call.
func (h *testHarness) callTool(t *testing.T, token, name string, args map[string]any) map[string]any {
	t.Helper()
	return h.rpcCall(t, token, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
}

// toolResult extracts the inner CallToolResult shape.
func toolResult(t *testing.T, resp map[string]any) (text string, isError bool) {
	t.Helper()
	if e, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("rpc error: %v", e)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in response: %v", resp)
	}
	if v, ok := res["isError"].(bool); ok {
		isError = v
	}
	contents, _ := res["content"].([]any)
	for _, c := range contents {
		cm, _ := c.(map[string]any)
		if cm["type"] == "text" {
			if s, ok := cm["text"].(string); ok {
				text = s
				break
			}
		}
	}
	return text, isError
}

func TestToolsListReturnsTwelveTools(t *testing.T) {
	h := newTestHarness(t)
	resp := h.rpcCall(t, h.tokenA, "tools/list", nil)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", resp)
	}
	tools, _ := res["tools"].([]any)
	if len(tools) != 12 {
		names := make([]string, 0, len(tools))
		for _, tt := range tools {
			tm, _ := tt.(map[string]any)
			names = append(names, fmt.Sprintf("%v", tm["name"]))
		}
		t.Fatalf("expected 12 tools, got %d: %v", len(tools), names)
	}
	// sanity: make sure the 12 we expect are present
	want := map[string]bool{
		"coggo_entity_get": true, "coggo_entity_query": true, "coggo_relation_query": true,
		"coggo_semantic_search": true, "coggo_time_travel": true, "coggo_type_list": true,
		"coggo_type_describe": true, "coggo_entity_create": true, "coggo_entity_update": true,
		"coggo_relation_create": true, "coggo_type_define": true, "coggo_cross_peer_query": true,
	}
	for _, tt := range tools {
		tm := tt.(map[string]any)
		delete(want, tm["name"].(string))
	}
	if len(want) != 0 {
		t.Fatalf("missing tools: %v", want)
	}
}

func TestEntityCreateThenQuery(t *testing.T) {
	h := newTestHarness(t)
	createResp := h.callTool(t, h.tokenA, "coggo_entity_create", map[string]any{
		"peer": "alpha",
		"type": "Decision",
		"fields": map[string]any{
			"title":     "Use SQLite over SurrealDB",
			"rationale": "Embedded SDK was incomplete",
		},
	})
	text, isErr := toolResult(t, createResp)
	if isErr {
		t.Fatalf("create returned error: %s", text)
	}
	var created types.Entity
	if err := json.Unmarshal([]byte(text), &created); err != nil {
		t.Fatalf("parse created: %v\n%s", err, text)
	}
	if created.ID == "" {
		t.Fatalf("expected ID in created entity: %+v", created)
	}
	if created.CreatedByClient != "both" {
		t.Fatalf("expected created_by_client to match token label %q, got %q", "both", created.CreatedByClient)
	}

	queryResp := h.callTool(t, h.tokenA, "coggo_entity_query", map[string]any{
		"peer": "alpha",
		"type": "Decision",
	})
	text, isErr = toolResult(t, queryResp)
	if isErr {
		t.Fatalf("query returned error: %s", text)
	}
	var entities []*types.Entity
	if err := json.Unmarshal([]byte(text), &entities); err != nil {
		t.Fatalf("parse query result: %v\n%s", err, text)
	}
	if len(entities) == 0 {
		t.Fatalf("expected at least one Decision, got 0")
	}
	found := false
	for _, e := range entities {
		if e.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created entity not found in query: %v", entities)
	}
}

func TestMissingAuthorizationIsRejected(t *testing.T) {
	h := newTestHarness(t)
	resp := h.callTool(t, "", "coggo_type_list", map[string]any{"peer": "alpha"})
	text, isErr := toolResult(t, resp)
	if !isErr {
		t.Fatalf("expected error for missing auth, got: %s", text)
	}
	if !strings.Contains(text, "unauthorized") {
		t.Fatalf("expected unauthorized error, got: %s", text)
	}
}

func TestTokenScopedToWrongPeerIsRejected(t *testing.T) {
	h := newTestHarness(t)
	// tokenAOnly is scoped to "alpha" only; calling on "beta" must be rejected.
	resp := h.callTool(t, h.tokenAOnly, "coggo_type_list", map[string]any{"peer": "beta"})
	text, isErr := toolResult(t, resp)
	if !isErr {
		t.Fatalf("expected error for cross-peer, got: %s", text)
	}
	if !strings.Contains(text, "unauthorized") {
		t.Fatalf("expected unauthorized error, got: %s", text)
	}
}
