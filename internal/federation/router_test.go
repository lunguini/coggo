package federation

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lunguini/coggo/internal/types"
)

type stubHandler struct {
	calls int
	did   string
}

func (s *stubHandler) HandleQuery(ctx context.Context, msg types.FederationMessage) (types.FederationMessage, error) {
	s.calls++
	payload, _ := json.Marshal(map[string]string{"hello": s.did})
	return types.FederationMessage{
		Version: msg.Version, SourceDID: s.did, TargetDID: msg.SourceDID,
		Type: types.FedMsgResponse, Payload: payload, MessageID: "m", Timestamp: time.Now(),
	}, nil
}
func (s *stubHandler) HandleWrite(ctx context.Context, msg types.FederationMessage) (types.FederationMessage, error) {
	return s.HandleQuery(ctx, msg)
}
func (s *stubHandler) HandlePing(ctx context.Context, msg types.FederationMessage) (types.FederationMessage, error) {
	return s.HandleQuery(ctx, msg)
}

func TestRouterDispatch(t *testing.T) {
	r := New()
	a := &stubHandler{did: "did:key:a"}
	b := &stubHandler{did: "did:key:b"}
	if err := r.RegisterPeer("did:key:a", a); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterPeer("did:key:b", b); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterPeer("did:key:a", a); err == nil {
		t.Fatal("expected duplicate registration error")
	}
	resp, err := r.Route(context.Background(), types.FederationMessage{
		SourceDID: "did:key:a", TargetDID: "did:key:b", Type: types.FedMsgQuery,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Type != types.FedMsgResponse {
		t.Fatalf("got type %q", resp.Type)
	}
	if b.calls != 1 {
		t.Fatalf("expected b handler called once, got %d", b.calls)
	}
}

func TestRouterUnknownTarget(t *testing.T) {
	r := New()
	resp, err := r.Route(context.Background(), types.FederationMessage{
		SourceDID: "did:key:a", TargetDID: "did:key:unknown", Type: types.FedMsgQuery,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if resp.Type != types.FedMsgError {
		t.Fatalf("expected Error reply, got %q", resp.Type)
	}
}

func TestRouterListPeers(t *testing.T) {
	r := New()
	_ = r.RegisterPeer("did:key:b", &stubHandler{did: "b"})
	_ = r.RegisterPeer("did:key:a", &stubHandler{did: "a"})
	got := r.ListPeers()
	if len(got) != 2 || got[0] != "did:key:a" || got[1] != "did:key:b" {
		t.Fatalf("unexpected order: %v", got)
	}
}

func TestRouterUnsupportedType(t *testing.T) {
	r := New()
	_ = r.RegisterPeer("did:key:x", &stubHandler{did: "x"})
	_, err := r.Route(context.Background(), types.FederationMessage{
		TargetDID: "did:key:x", Type: types.FederationMessageType("Bogus"),
	})
	if err == nil {
		t.Fatal("expected unsupported type error")
	}
}
