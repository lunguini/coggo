package main

// ratelimit.go provides two layers of throttling for the gateway:
//
//   1. globalLimiter — a single token-bucket across every request the gateway
//      handles. Public tunnel traffic can collapse many clients onto a small
//      set of relay addresses, so per-IP limiting on the public surface is
//      less useful than a global cap against floods of OAuth or tokeninfo
//      traffic.
//
//   2. perEmailLimiter — applied after token validation on /mcp. Each
//      authenticated email gets its own bucket so one noisy account can't
//      starve the others. Keys are reaped on a periodic sweep.

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type emailCtxKey struct{}

// withEmail returns r with the authenticated email attached, for downstream
// middleware to read. The validator calls this once auth succeeds.
func withEmail(r *http.Request, email string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), emailCtxKey{}, email))
}

func emailFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(emailCtxKey{}).(string)
	return v
}

// globalLimiter wraps next with a single token-bucket. Rejected requests get
// HTTP 429 with a Retry-After hint of 1 second.
func globalLimiter(lim *rate.Limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !lim.Allow() {
			slog.Warn("global rate limit hit", "path", r.URL.Path)
			tooManyRequests(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// perEmailLimiter holds one limiter per authenticated email and reaps idle
// entries so the map can't grow unbounded. Configured limits apply to every
// email equally — there is no privileged caller.
type perEmailLimiter struct {
	rps   rate.Limit
	burst int

	mu       sync.Mutex
	buckets  map[string]*emailBucket
	stopOnce sync.Once
	stop     chan struct{}
}

type emailBucket struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

func newPerEmailLimiter(rps rate.Limit, burst int) *perEmailLimiter {
	p := &perEmailLimiter{
		rps:     rps,
		burst:   burst,
		buckets: map[string]*emailBucket{},
		stop:    make(chan struct{}),
	}
	go p.sweep()
	return p
}

func (p *perEmailLimiter) Close() {
	p.stopOnce.Do(func() { close(p.stop) })
}

func (p *perEmailLimiter) allow(email string) bool {
	email = strings.ToLower(email)
	p.mu.Lock()
	b, ok := p.buckets[email]
	if !ok {
		b = &emailBucket{lim: rate.NewLimiter(p.rps, p.burst)}
		p.buckets[email] = b
	}
	b.lastSeen = time.Now()
	p.mu.Unlock()
	return b.lim.Allow()
}

// sweep evicts entries idle for >10m. Bound on map growth assuming a small
// allowlist; still a safety net in case of allowlist churn.
func (p *perEmailLimiter) sweep() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case now := <-t.C:
			p.mu.Lock()
			for k, b := range p.buckets {
				if now.Sub(b.lastSeen) > 10*time.Minute {
					delete(p.buckets, k)
				}
			}
			p.mu.Unlock()
		}
	}
}

// Wrap returns middleware that enforces the per-email bucket. It assumes the
// validator has already populated the email on the request context — if not
// (e.g. someone wires this up wrong), it fails closed by rejecting the call,
// which is loud enough to catch in dev.
func (p *perEmailLimiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email := emailFromCtx(r.Context())
		if email == "" {
			slog.Error("per-email limiter called without authenticated email — middleware order bug")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !p.allow(email) {
			slog.Warn("per-email rate limit hit", "email", email, "path", r.URL.Path)
			tooManyRequests(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func tooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
}
