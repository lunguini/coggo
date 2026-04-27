package peer

import (
	"bytes"
	"crypto/ed25519"
	"strings"
	"testing"
)

func TestNewDIDFormat(t *testing.T) {
	for range 50 {
		did, pub, _, err := NewDID()
		if err != nil {
			t.Fatalf("NewDID: %v", err)
		}
		if !strings.HasPrefix(did, "did:key:z6Mk") {
			t.Fatalf("expected did:key:z6Mk prefix, got %q", did)
		}
		body := did[len("did:key:z"):]
		for _, r := range body {
			if !strings.ContainsRune(base58Alphabet, r) {
				t.Fatalf("DID contains non-base58 character %q in %q", r, did)
			}
		}
		decoded, err := DecodePublicKey(did)
		if err != nil {
			t.Fatalf("DecodePublicKey(%q): %v", did, err)
		}
		if !bytes.Equal(decoded, pub) {
			t.Fatalf("round-trip mismatch: got %x want %x", decoded, pub)
		}
		if len(decoded) != ed25519.PublicKeySize {
			t.Fatalf("decoded pubkey wrong size: %d", len(decoded))
		}
	}
}

func TestDecodePublicKeyRejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"did:key:",
		"did:key:abc",                     // missing multibase prefix
		"did:key:zABC",                    // too short to contain multicodec+pubkey
		"did:web:example.com",             // wrong method
		"did:key:z6Mk-invalid-base58-XXX", // dashes are not base58btc
	}
	for _, c := range cases {
		if _, err := DecodePublicKey(c); err == nil {
			t.Errorf("expected error for %q, got nil", c)
		}
	}
}

func TestBase58RoundTrip(t *testing.T) {
	cases := [][]byte{
		{},
		{0x00},
		{0x00, 0x00, 0x01},
		{0xed, 0x01, 0xde, 0xad, 0xbe, 0xef},
	}
	for _, in := range cases {
		got, err := base58Decode(base58Encode(in))
		if err != nil {
			t.Fatalf("decode(%x): %v", in, err)
		}
		if !bytes.Equal(got, in) {
			t.Fatalf("round-trip mismatch: got %x want %x", got, in)
		}
	}
}
