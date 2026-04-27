// Package federation implements Coggo's cross-peer messaging primitive.
// In v0.1 the transport is in-process function calls; the protocol shape is
// the same as the network transport that arrives in v0.4.
package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/lunguini/coggo/internal/types"
	"github.com/oklog/ulid/v2"
)

// Router is the in-process implementation of types.Router.
type Router struct {
	mu       sync.RWMutex
	handlers map[string]types.PeerHandler
}

// New returns an empty router.
func New() *Router {
	return &Router{handlers: map[string]types.PeerHandler{}}
}

// RegisterPeer wires a handler under the given DID. Returns an error if the
// DID is already registered.
func (r *Router) RegisterPeer(did string, h types.PeerHandler) error {
	if did == "" {
		return fmt.Errorf("federation: empty DID")
	}
	if h == nil {
		return fmt.Errorf("federation: nil handler for %s", did)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.handlers[did]; ok {
		return fmt.Errorf("federation: peer %s already registered", did)
	}
	r.handlers[did] = h
	return nil
}

// Route dispatches a federation message to its TargetDID's handler. On lookup
// failure or handler error, returns a FedMsgError reply (and the error).
func (r *Router) Route(ctx context.Context, msg types.FederationMessage) (types.FederationMessage, error) {
	r.mu.RLock()
	h, ok := r.handlers[msg.TargetDID]
	r.mu.RUnlock()
	if !ok {
		return errorReply(msg, fmt.Errorf("federation: no handler for target DID %s", msg.TargetDID))
	}
	var (
		resp types.FederationMessage
		err  error
	)
	switch msg.Type {
	case types.FedMsgQuery:
		resp, err = h.HandleQuery(ctx, msg)
	case types.FedMsgWrite:
		resp, err = h.HandleWrite(ctx, msg)
	case types.FedMsgPing:
		resp, err = h.HandlePing(ctx, msg)
	default:
		return errorReply(msg, fmt.Errorf("federation: unsupported message type %q", msg.Type))
	}
	if err != nil {
		em, _ := errorReply(msg, err)
		return em, err
	}
	return resp, nil
}

// ListPeers returns registered peer DIDs in deterministic order.
func (r *Router) ListPeers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.handlers))
	for did := range r.handlers {
		out = append(out, did)
	}
	sort.Strings(out)
	return out
}

// errorReply constructs a FedMsgError mirror of the inbound message.
func errorReply(in types.FederationMessage, err error) (types.FederationMessage, error) {
	payload, _ := json.Marshal(map[string]string{"error": err.Error()})
	return types.FederationMessage{
		Version:   in.Version,
		SourceDID: in.TargetDID,
		TargetDID: in.SourceDID,
		Type:      types.FedMsgError,
		Payload:   payload,
		MessageID: newMessageID(),
		Timestamp: time.Now().UTC(),
	}, err
}

// newMessageID returns a fresh ULID-derived federation message ID.
func newMessageID() string {
	idMu.Lock()
	defer idMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), idEntropy).String()
}

var (
	idMu      sync.Mutex
	idEntropy = ulid.Monotonic(&randReader{}, 0)
)
