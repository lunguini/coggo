package auth

import (
	"context"
	"strings"
	"testing"
)

func TestIssueAndVerify(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, secret, err := s.Issue(ctx, []string{"business"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "tok_") {
		t.Fatalf("id missing prefix: %q", id)
	}
	if len(secret) < 32 {
		t.Fatalf("secret too short: %d", len(secret))
	}
	tok, err := s.Verify(ctx, secret, "business")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if tok.ID != id {
		t.Fatalf("verified id mismatch: %s vs %s", tok.ID, id)
	}
}

func TestVerifyWrongPeer(t *testing.T) {
	s, _ := Open(t.TempDir())
	ctx := context.Background()
	_, secret, _ := s.Issue(ctx, []string{"business"}, "")
	if _, err := s.Verify(ctx, secret, "personal"); err == nil {
		t.Fatal("expected error for wrong peer")
	}
}

func TestWildcardPeer(t *testing.T) {
	s, _ := Open(t.TempDir())
	ctx := context.Background()
	_, secret, _ := s.Issue(ctx, []string{"*"}, "")
	if _, err := s.Verify(ctx, secret, "anything"); err != nil {
		t.Fatalf("wildcard should match: %v", err)
	}
}

func TestRevoke(t *testing.T) {
	s, _ := Open(t.TempDir())
	ctx := context.Background()
	id, secret, _ := s.Issue(ctx, []string{"business"}, "")
	if err := s.Revoke(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, secret, "business"); err == nil {
		t.Fatal("expected revoked token to fail verify")
	}
}

func TestPersistAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s1, _ := Open(dir)
	ctx := context.Background()
	_, secret, _ := s1.Issue(ctx, []string{"business"}, "label")
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Verify(ctx, secret, "business"); err != nil {
		t.Fatalf("verify after reopen: %v", err)
	}
}

func TestUnknownTokenRejected(t *testing.T) {
	s, _ := Open(t.TempDir())
	if _, err := s.Verify(context.Background(), "not-a-real-secret", "business"); err == nil {
		t.Fatal("expected error for unknown token")
	}
}
