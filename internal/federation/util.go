package federation

import (
	"math/rand/v2"
)

// randReader adapts math/rand/v2 to io.Reader. ULID entropy only — never used
// for secrets.
type randReader struct{}

func (r *randReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(rand.Uint32())
	}
	return len(p), nil
}
