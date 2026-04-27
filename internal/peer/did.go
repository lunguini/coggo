// Package peer implements peer identity using did:key with ed25519 keys.
//
// did:key is the simplest DID method: the DID itself encodes the public key,
// so resolution requires no network and no registry. Sufficient for v0.1
// (single binary, no inter-Coggo federation). v0.3+ may add did:web.
package peer

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/lunguini/coggo/internal/types"
)

// did:key for ed25519 encodes (multicodec prefix || pubkey) in multibase
// z-base58btc per https://w3c-ccg.github.io/did-method-key/. The multicodec
// varint for ed25519-pub is 0xed 0x01. The literal "z6Mk" prefix on the
// resulting DID is an emergent property of base58btc-encoding any 34-byte
// buffer that starts with 0xed 0x01 — it's not a string we should construct
// by hand.
var ed25519MulticodecPrefix = []byte{0xed, 0x01}

const (
	didKeyPrefix    = "did:key:"
	multibaseBase58 = 'z'
	base58Alphabet  = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
)

// NewDID generates a fresh ed25519 keypair and the corresponding did:key DID.
func NewDID() (did string, pub, priv []byte, err error) {
	pub, priv, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, nil, fmt.Errorf("ed25519: %w", err)
	}
	body := make([]byte, 0, len(ed25519MulticodecPrefix)+len(pub))
	body = append(body, ed25519MulticodecPrefix...)
	body = append(body, pub...)
	return didKeyPrefix + string(multibaseBase58) + base58Encode(body), pub, priv, nil
}

// DecodePublicKey parses a did:key DID back to its 32-byte ed25519 public key.
func DecodePublicKey(did string) ([]byte, error) {
	if !strings.HasPrefix(did, didKeyPrefix) {
		return nil, errors.New("not a did:key")
	}
	body := did[len(didKeyPrefix):]
	if len(body) == 0 || body[0] != multibaseBase58 {
		return nil, errors.New("did:key: only multibase base58btc ('z') supported")
	}
	raw, err := base58Decode(body[1:])
	if err != nil {
		return nil, fmt.Errorf("did:key: base58: %w", err)
	}
	if len(raw) < len(ed25519MulticodecPrefix)+ed25519.PublicKeySize {
		return nil, errors.New("did:key: payload too short")
	}
	for i, b := range ed25519MulticodecPrefix {
		if raw[i] != b {
			return nil, errors.New("did:key: not an ed25519-pub multicodec")
		}
	}
	pub := raw[len(ed25519MulticodecPrefix):]
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("did:key: expected %d-byte ed25519 pubkey, got %d", ed25519.PublicKeySize, len(pub))
	}
	return pub, nil
}

// NewPeer constructs a fresh Peer with default settings. The caller assigns
// the human-readable name and persists the result.
func NewPeer(name, description string) (*types.Peer, error) {
	did, pub, priv, err := NewDID()
	if err != nil {
		return nil, err
	}
	return &types.Peer{
		DID:         did,
		Name:        name,
		Description: description,
		PrivateKey:  priv,
		PublicKey:   pub,
		Settings: types.PeerSettings{
			DefaultClarificationThreshold: "ask_when_ambiguous",
			BriefingFrequency:             "on_demand",
			CaptureConfirmation:           "log_and_tell",
		},
	}, nil
}

func base58Encode(input []byte) string {
	zeros := 0
	for zeros < len(input) && input[zeros] == 0 {
		zeros++
	}
	x := new(big.Int).SetBytes(input)
	base := big.NewInt(58)
	mod := new(big.Int)
	var out []byte
	for x.Sign() > 0 {
		x.DivMod(x, base, mod)
		out = append(out, base58Alphabet[mod.Int64()])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	prefix := strings.Repeat(string(base58Alphabet[0]), zeros)
	return prefix + string(out)
}

func base58Decode(s string) ([]byte, error) {
	zeros := 0
	for zeros < len(s) && s[zeros] == base58Alphabet[0] {
		zeros++
	}
	x := new(big.Int)
	base := big.NewInt(58)
	for i := 0; i < len(s); i++ {
		idx := strings.IndexByte(base58Alphabet, s[i])
		if idx < 0 {
			return nil, fmt.Errorf("invalid base58 character %q", s[i])
		}
		x.Mul(x, base)
		x.Add(x, big.NewInt(int64(idx)))
	}
	body := x.Bytes()
	out := make([]byte, zeros+len(body))
	copy(out[zeros:], body)
	return out, nil
}
