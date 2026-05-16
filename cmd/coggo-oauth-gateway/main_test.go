package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handleHealthz(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "ok\n" {
		t.Fatalf("body = %q, want ok newline", got)
	}
}

func TestTokenFingerprintIsStableAndDoesNotExposeToken(t *testing.T) {
	token := "secret-token-value"
	sum := sha256.Sum256([]byte(token))
	want := hex.EncodeToString(sum[:])[:12]

	got := tokenFingerprint(token)
	if got != want {
		t.Fatalf("tokenFingerprint() = %q, want %q", got, want)
	}
	if got == token || len(got) != 12 {
		t.Fatalf("tokenFingerprint() exposed unexpected value %q", got)
	}
}
