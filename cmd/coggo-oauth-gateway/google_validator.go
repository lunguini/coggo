package main

// google_validator.go provides token validation for Google opaque access
// tokens. Background:
//
//   - Google's `access_token` is an opaque string (not a JWT). Only Google's
//     `id_token` is a JWT.
//   - oauth-mcp-proxy v1.2.0's OIDCValidator can only validate JWTs.
//   - In proxy mode, oauth-mcp-proxy passes Google's access_token straight
//     through to the OAuth client (claude.ai). claude.ai then sends that
//     access_token back as the Bearer on every MCP request — and the
//     OIDCValidator rejects it with "compact JWS format must have three parts".
//
// We work around this by validating Google access tokens via
// https://oauth2.googleapis.com/tokeninfo — Google's canonical endpoint for
// opaque-token introspection. Results are cached to avoid hammering Google
// (each MCP tool call would otherwise trigger a network round-trip).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	oauth "github.com/tuannvm/oauth-mcp-proxy"
)

const (
	googleTokenInfoURL = "https://oauth2.googleapis.com/tokeninfo"
	// Cap cache entries even if Google says the token has hours of life left,
	// so a revoked token isn't accepted indefinitely.
	maxCacheTTL = 5 * time.Minute
)

type googleTokenInfo struct {
	Aud              string `json:"aud"`
	Sub              string `json:"sub"`
	Email            string `json:"email"`
	EmailVerified    string `json:"email_verified"`
	Exp              string `json:"exp"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type cachedUser struct {
	Sub       string
	Email     string
	ExpiresAt time.Time
}

type googleValidator struct {
	expectedAudience string
	allowedEmails    map[string]struct{} // lower-cased; empty => deny all
	httpClient       *http.Client

	mu    sync.Mutex
	cache map[string]cachedUser // key: sha256 of token
}

// newGoogleValidator builds a validator that accepts Google access tokens
// whose verified email is in allowedEmails. Fail-closed: an empty allowlist
// means every request is denied — there is intentionally no "allow all" mode.
func newGoogleValidator(audience string, allowedEmails []string) *googleValidator {
	allow := make(map[string]struct{}, len(allowedEmails))
	for _, e := range allowedEmails {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" {
			allow[e] = struct{}{}
		}
	}
	return &googleValidator{
		expectedAudience: audience,
		allowedEmails:    allow,
		httpClient:       &http.Client{Timeout: 5 * time.Second},
		cache:            map[string]cachedUser{},
	}
}

// Validate verifies token against Google's tokeninfo endpoint. On success
// returns the authenticated user. On failure returns the email Google
// reported for the token (when known — empty string otherwise) so callers
// can log who attempted authentication, plus an error suitable for logging.
// The public-facing 401 message is intentionally generic.
func (v *googleValidator) Validate(ctx context.Context, token string) (*cachedUser, string, error) {
	key := tokenKey(token)

	v.mu.Lock()
	if u, ok := v.cache[key]; ok && time.Now().Before(u.ExpiresAt) {
		v.mu.Unlock()
		return &u, u.Email, nil
	}
	delete(v.cache, key) // prune any stale entry under the same key
	v.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		googleTokenInfoURL+"?access_token="+url.QueryEscape(token), nil)
	if err != nil {
		return nil, "", fmt.Errorf("build tokeninfo request: %w", err)
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("call tokeninfo: %w", err)
	}
	defer resp.Body.Close()

	var info googleTokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, "", fmt.Errorf("decode tokeninfo: %w", err)
	}
	if info.Error != "" {
		return nil, info.Email, fmt.Errorf("google rejected token: %s: %s", info.Error, info.ErrorDescription)
	}
	if info.Aud != v.expectedAudience {
		return nil, info.Email, fmt.Errorf("audience mismatch: got %q want %q", info.Aud, v.expectedAudience)
	}
	// Fail-closed: if no emails are allowlisted, deny everything. This is the
	// safety net when OAUTH_ALLOWED_EMAILS is unset — without it, a valid
	// Google token from *any* account would be accepted, since Funnel exposes
	// the gateway publicly.
	if len(v.allowedEmails) == 0 {
		return nil, info.Email, fmt.Errorf("no allowed emails configured (set OAUTH_ALLOWED_EMAILS)")
	}
	if info.EmailVerified != "true" {
		return nil, info.Email, fmt.Errorf("email not verified")
	}
	if _, ok := v.allowedEmails[strings.ToLower(info.Email)]; !ok {
		return nil, info.Email, fmt.Errorf("email not in allowlist")
	}
	expUnix, err := strconv.ParseInt(info.Exp, 10, 64)
	if err != nil {
		return nil, info.Email, fmt.Errorf("invalid exp %q: %w", info.Exp, err)
	}
	expiry := time.Unix(expUnix, 0)
	if time.Now().After(expiry) {
		return nil, info.Email, fmt.Errorf("token expired at %s", expiry.Format(time.RFC3339))
	}

	// Cache for the lesser of (token's remaining lifetime, maxCacheTTL).
	cacheUntil := expiry
	if cap := time.Now().Add(maxCacheTTL); cap.Before(cacheUntil) {
		cacheUntil = cap
	}
	u := cachedUser{Sub: info.Sub, Email: info.Email, ExpiresAt: cacheUntil}

	v.mu.Lock()
	v.cache[key] = u
	v.mu.Unlock()

	return &u, u.Email, nil
}

// WrapHandler returns an http.Handler that validates the Bearer token via
// Google before delegating to next. On failure, returns a 401 that points
// claude.ai (or any MCP client) at the OAuth discovery metadata so it can
// re-initiate the auth dance.
func (v *googleValidator) WrapHandler(next http.Handler, oauthServer *oauth.Server) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			respondUnauthorized(w, oauthServer, "missing access token")
			return
		}
		user, observedEmail, err := v.Validate(r.Context(), token)
		if err != nil {
			// Log every failed auth with the email Google reported (empty if we
			// never got that far — e.g. tokeninfo unreachable). At Warn so it's
			// visible without DEBUG; this is the audit trail for the public
			// surface.
			slog.Warn("token validation failed",
				"email", observedEmail,
				"remote_addr", r.RemoteAddr,
				"err", err.Error())
			respondUnauthorized(w, oauthServer, "authentication failed")
			return
		}
		slog.Debug("token validated", "sub", user.Sub, "email", user.Email)
		// Stamp the authenticated email onto the request so downstream
		// middleware (per-email rate limiter) can key off it.
		next.ServeHTTP(w, withEmail(r, user.Email))
	})
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) <= len(prefix) || h[:len(prefix)] != prefix {
		return ""
	}
	return h[len(prefix):]
}

func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func respondUnauthorized(w http.ResponseWriter, oauthServer *oauth.Server, description string) {
	metadataURL := oauthServer.GetProtectedResourceMetadataURL()
	w.Header().Add("WWW-Authenticate",
		`Bearer realm="OAuth", error="invalid_token", error_description="`+description+`"`)
	w.Header().Add("WWW-Authenticate",
		`resource_metadata="`+metadataURL+`"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             "invalid_token",
		"error_description": description,
	})
}
