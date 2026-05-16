// coggo-oauth-gateway is a small companion binary that puts an OAuth 2.1
// surface in front of a running `coggo serve`. It validates incoming OAuth
// tokens against an upstream IdP (Google for v0.1) and reverse-proxies the
// validated request to coggo on localhost with a Coggo-issued bearer token.
//
// This keeps Coggo's substrate sovereign — coggo never speaks OAuth, never
// trusts a third party for identity. The gateway is a swappable transport:
// Adrian can run coggo without it (Claude Code, curl, local), or in front of
// it (claude.ai mobile via Tailscale Funnel).
//
// Architecture:
//
//	claude.ai  --(OAuth bearer)-->  gateway:8080
//	                                    |
//	                                    | validates token via Google OIDC
//	                                    | strips OAuth bearer
//	                                    | injects coggo bearer
//	                                    v
//	                                coggo:6177/mcp
//
// Config is via environment variables — see usage() below.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	oauth "github.com/tuannvm/oauth-mcp-proxy"
	mark3labsoauth "github.com/tuannvm/oauth-mcp-proxy/mark3labs"
	"golang.org/x/time/rate"
)

const usage = `coggo-oauth-gateway — OAuth 2.1 gateway in front of coggo serve

Usage:
  coggo-oauth-gateway [flags]

Required environment:
  COGGO_TOKEN              Coggo bearer token issued via 'coggo token create --all'.
                           The gateway sends this on every upstream request.
  GOOGLE_CLIENT_ID         OAuth client ID from Google Cloud Console.
  GOOGLE_CLIENT_SECRET     OAuth client secret from Google Cloud Console.
  GATEWAY_PUBLIC_URL       The publicly reachable URL of THIS gateway, e.g.
                           https://ads-macbook-pro.tail3b1f7.ts.net.
                           Used in OAuth discovery + as the redirect URI base.

Optional:
  COGGO_UPSTREAM           Where coggo serve is reachable (default http://localhost:6177).
  GATEWAY_LISTEN           Address to bind (default :8080).
  COGGO_LOG_LEVEL          debug | info | warn | error (default info).
  OAUTH_ALLOWED_CLIENT_DOMAINS
                           Comma-separated domain suffixes allowed as
                           client-supplied redirect_uri values, in addition
                           to localhost. Default "claude.ai,claude.com" so
                           claude.ai's mobile + web connectors work out of
                           the box. Add other domains here as you connect
                           additional MCP clients.
  OAUTH_ALLOWED_EMAILS     Comma-separated allowlist of Google account
                           emails permitted to call /mcp. FAIL-CLOSED: if
                           unset or empty, every request is rejected. Funnel
                           is a public surface — without this, any Google
                           account on the internet could authenticate.

Rate limiting (all optional, sane defaults):
  RATE_GLOBAL_RPS          Tokens per second across the whole gateway
                           (default 50). Floor against floods on the public
                           surface.
  RATE_GLOBAL_BURST        Burst above the global RPS (default 100).
  RATE_PER_EMAIL_RPM       Per-authenticated-email requests per minute on
                           /mcp (default 10).
  RATE_PER_EMAIL_BURST     Burst above the per-email rate (default 30).

Setup with Google Cloud Console:
  1. Create OAuth client (Web application)
  2. Authorized JavaScript origins: $GATEWAY_PUBLIC_URL
  3. Authorized redirect URIs: $GATEWAY_PUBLIC_URL/oauth/callback
  4. Copy Client ID + Client Secret into the env vars above

Setup in claude.ai:
  Custom Connector URL: $GATEWAY_PUBLIC_URL/mcp
  (claude.ai discovers the OAuth endpoints automatically via /.well-known)
`

func main() {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		fmt.Fprintln(os.Stderr)
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	configureLogger(cfg.LogLevel)

	if err := run(cfg); err != nil {
		slog.Error("gateway exited", "err", err)
		os.Exit(1)
	}
}

type config struct {
	Listen               string
	PublicURL            string
	UpstreamURL          *url.URL
	CoggoToken           string
	GoogleClientID       string
	GoogleClientSec      string
	LogLevel             string
	AllowedClientDomains string
	AllowedEmails        []string
	GlobalRPS            float64
	GlobalBurst          int
	PerEmailRPM          float64
	PerEmailBurst        int
}

func loadConfig() (*config, error) {
	c := &config{
		Listen:               envOr("GATEWAY_LISTEN", ":8080"),
		PublicURL:            strings.TrimRight(os.Getenv("GATEWAY_PUBLIC_URL"), "/"),
		CoggoToken:           os.Getenv("COGGO_TOKEN"),
		GoogleClientID:       os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSec:      os.Getenv("GOOGLE_CLIENT_SECRET"),
		LogLevel:             envOr("COGGO_LOG_LEVEL", "info"),
		AllowedClientDomains: envOr("OAUTH_ALLOWED_CLIENT_DOMAINS", "claude.ai,claude.com"),
		AllowedEmails:        splitCSV(os.Getenv("OAUTH_ALLOWED_EMAILS")),
		GlobalRPS:            envFloat("RATE_GLOBAL_RPS", 50),
		GlobalBurst:          envInt("RATE_GLOBAL_BURST", 100),
		PerEmailRPM:          envFloat("RATE_PER_EMAIL_RPM", 10),
		PerEmailBurst:        envInt("RATE_PER_EMAIL_BURST", 30),
	}

	upstreamRaw := envOr("COGGO_UPSTREAM", "http://localhost:6177")
	u, err := url.Parse(upstreamRaw)
	if err != nil {
		return nil, fmt.Errorf("COGGO_UPSTREAM %q: %w", upstreamRaw, err)
	}
	c.UpstreamURL = u

	switch {
	case c.CoggoToken == "":
		return nil, fmt.Errorf("COGGO_TOKEN is required (run 'coggo token create --all' to mint one)")
	case c.GoogleClientID == "":
		return nil, fmt.Errorf("GOOGLE_CLIENT_ID is required")
	case c.GoogleClientSec == "":
		return nil, fmt.Errorf("GOOGLE_CLIENT_SECRET is required")
	case c.PublicURL == "":
		return nil, fmt.Errorf("GATEWAY_PUBLIC_URL is required (the public URL this gateway is reachable at)")
	}
	return c, nil
}

func run(cfg *config) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	slog.Debug("gateway config loaded",
		"listen", cfg.Listen,
		"public", cfg.PublicURL,
		"upstream", cfg.UpstreamURL.String(),
		"coggo_token_fingerprint", tokenFingerprint(cfg.CoggoToken),
		"allowed_client_domains", cfg.AllowedClientDomains,
		"allowed_email_count", len(cfg.AllowedEmails))

	// OAuth in proxy mode with fixed redirect: the gateway exposes the standard
	// /.well-known + /authorize + /token endpoints that claude.ai discovers and
	// drives. Behind the scenes, oauth-mcp-proxy always uses *our* /oauth/callback
	// when talking to Google (so Google's allowlist only ever needs that one URL),
	// then proxies the result back to whatever client redirect_uri came in
	// originally — provided that client domain is allowlisted in
	// AllowedClientRedirectDomains. localhost is always allowed; claude.ai/.com
	// are added by default so the claude.ai mobile + web connectors work.
	oauthServer, _, err := mark3labsoauth.WithOAuth(mux, &oauth.Config{
		Mode:                         "proxy",
		Provider:                     "google",
		Issuer:                       "https://accounts.google.com",
		Audience:                     cfg.GoogleClientID, // Google requires audience == client_id
		ClientID:                     cfg.GoogleClientID,
		ClientSecret:                 cfg.GoogleClientSec,
		ServerURL:                    cfg.PublicURL,
		FixedRedirectURI:             cfg.PublicURL + "/oauth/callback",
		AllowedClientRedirectDomains: cfg.AllowedClientDomains,
	})
	if err != nil {
		return fmt.Errorf("oauth setup: %w", err)
	}

	proxy := newReverseProxy(cfg.UpstreamURL, cfg.CoggoToken)

	// /mcp validated then forwarded; everything else (the OAuth endpoints) is
	// owned by oauthServer.RegisterHandlers above.
	//
	// We use our own validator (not oauthServer.WrapHandler) because Google's
	// `access_token` is opaque, not a JWT — oauth-mcp-proxy's OIDCValidator
	// can only verify JWTs and rejects every Google access token with
	// "compact JWS format must have three parts". See google_validator.go for
	// the workaround: validate via Google's tokeninfo endpoint instead.
	validator := newGoogleValidator(cfg.GoogleClientID, cfg.AllowedEmails)
	if len(cfg.AllowedEmails) == 0 {
		slog.Warn("OAUTH_ALLOWED_EMAILS is empty — all /mcp requests will be rejected. " +
			"Set it to a comma-separated list of Google account emails permitted to use this gateway.")
	} else {
		slog.Info("email allowlist active", "count", len(cfg.AllowedEmails))
	}

	// Per-email rate limit on /mcp. RPM → tokens-per-second.
	perEmail := newPerEmailLimiter(rate.Limit(cfg.PerEmailRPM/60.0), cfg.PerEmailBurst)
	defer perEmail.Close()
	mux.Handle("/mcp", validator.WrapHandler(perEmail.Wrap(proxy), oauthServer))

	// Global token-bucket fronts every gateway endpoint. With Tailscale Funnel
	// all public traffic shares one source IP (the relay), so per-IP limiting
	// is meaningless on the public surface — a global cap is the right shape.
	globalLim := rate.NewLimiter(rate.Limit(cfg.GlobalRPS), cfg.GlobalBurst)
	slog.Info("rate limits configured",
		"global_rps", cfg.GlobalRPS, "global_burst", cfg.GlobalBurst,
		"per_email_rpm", cfg.PerEmailRPM, "per_email_burst", cfg.PerEmailBurst)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           globalLimiter(globalLim, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("gateway listening",
			"addr", cfg.Listen,
			"public", cfg.PublicURL,
			"upstream", cfg.UpstreamURL.String())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		slog.Info("gateway shutdown")
		return nil
	case err := <-errCh:
		return err
	}
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// newReverseProxy forwards validated requests to coggo, replacing the incoming
// OAuth bearer with Coggo's static bearer token. Coggo only ever sees its own
// token — the OAuth identity stays at the gateway boundary, which is the right
// place for it.
func newReverseProxy(upstream *url.URL, coggoToken string) http.Handler {
	tokenFP := tokenFingerprint(coggoToken)
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(upstream)
			pr.SetXForwarded()
			pr.Out.Header.Set("Authorization", "Bearer "+coggoToken)
			slog.Debug("proxying mcp request",
				"method", pr.In.Method,
				"path", pr.In.URL.Path,
				"upstream", pr.Out.URL.String(),
				"outgoing_token_fingerprint", tokenFP)
			// Keep Accept and Content-Type as the client sent them; MCP streamable
			// transport uses them to choose JSON vs SSE framing.
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Warn("upstream proxy error", "err", err, "path", r.URL.Path)
			http.Error(w, "upstream coggo unreachable", http.StatusBadGateway)
		},
	}
	return rp
}

func tokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:12]
}

func configureLogger(level string) {
	lvl := slog.LevelInfo
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		slog.Warn("ignoring invalid env value, using default",
			"key", key, "value", v, "default", fallback)
		return fallback
	}
	return f
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil || i <= 0 {
		slog.Warn("ignoring invalid env value, using default",
			"key", key, "value", v, "default", fallback)
		return fallback
	}
	return i
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
