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

func TestLoadConfigReadsStableOAuthStateSecret(t *testing.T) {
	t.Setenv("COGGO_TOKEN", "coggo-secret")
	t.Setenv("GOOGLE_CLIENT_ID", "google-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "google-secret")
	t.Setenv("GATEWAY_PUBLIC_URL", "https://coggo.example.com/")
	t.Setenv("OAUTH_STATE_SECRET", "stable-oauth-state-secret")
	t.Setenv("JWT_SECRET", "legacy-secret")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicURL != "https://coggo.example.com" {
		t.Fatalf("PublicURL = %q", cfg.PublicURL)
	}
	if cfg.OAuthStateSecret != "stable-oauth-state-secret" {
		t.Fatalf("OAuthStateSecret = %q, want OAUTH_STATE_SECRET", cfg.OAuthStateSecret)
	}
}

func TestLoadConfigFallsBackToJWTSecret(t *testing.T) {
	t.Setenv("COGGO_TOKEN", "coggo-secret")
	t.Setenv("GOOGLE_CLIENT_ID", "google-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "google-secret")
	t.Setenv("GATEWAY_PUBLIC_URL", "https://coggo.example.com")
	t.Setenv("JWT_SECRET", "legacy-secret")
	t.Setenv("OAUTH_STATE_SECRET", "")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OAuthStateSecret != "legacy-secret" {
		t.Fatalf("OAuthStateSecret = %q, want JWT_SECRET fallback", cfg.OAuthStateSecret)
	}
}
