// Package embed provides text embedding adapters. v0.1 ships:
//   - "none"   — disabled; semantic_search falls back to substring search
//   - "voyage" — Voyage AI HTTP API (recommended default for quality)
//   - "openai" — OpenAI text-embedding-3-small/large
//   - "local"  — Ollama on localhost (e.g. nomic-embed-text)
//
// Only the disabled embedder is implemented in v0.1's first cut. Real
// providers can be slotted in without changing the storage interface.
package embed

import (
	"context"
	"fmt"
)

// Disabled returns an Embedder whose Enabled() reports false. Callers should
// route to substring search when this is in use.
func Disabled() Embedder { return disabled{} }

// Embedder mirrors types.Embedder but is repeated here to avoid an import
// cycle with packages that pull only the embed adapter.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimension() int
	Name() string
	Enabled() bool
}

type disabled struct{}

func (disabled) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, fmt.Errorf("embedding provider is disabled; configure one in coggo config")
}
func (disabled) Dimension() int { return 0 }
func (disabled) Name() string   { return "none" }
func (disabled) Enabled() bool  { return false }

// New constructs an embedder by provider name. v0.1 only returns the disabled
// adapter for unknown/unimplemented providers and logs the situation upstream.
func New(provider, model, apiKey string, dim int) Embedder {
	switch provider {
	case "", "none":
		return Disabled()
	default:
		// Other providers will be implemented; for v0.1 we degrade gracefully
		// rather than fail at startup.
		return Disabled()
	}
}
