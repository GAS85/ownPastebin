package main

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// responseRecorder wraps http.ResponseWriter to capture the status code
// written by the handler, which is not accessible after the fact otherwise.
type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// accessLogMiddleware logs one line per request via slog at INFO level:
//
//	method=GET path=/abc?ttl=3600&burn=true status=200 duration=1.23ms bytes=4096 ip=1.2.3.4
//
// 404s are logged at DEBUG to avoid noise; 5xx at WARN.
//
// X-Forwarded-For is only trusted when the direct TCP peer (r.RemoteAddr)
// falls inside cfg.TrustedProxy. Leave PASTEBIN_TRUSTED_PROXY unset to
// always use r.RemoteAddr and never trust client-supplied headers.
func (a *App) accessLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		ip := realIP(r, a.cfg.TrustedProxy)

		fullPath := r.URL.Path
		if r.URL.RawQuery != "" {
			fullPath = r.URL.Path + "?" + r.URL.RawQuery
		}

		level := slog.LevelInfo
		if rec.status == http.StatusNotFound {
			level = slog.LevelDebug
		} else if rec.status >= 500 {
			level = slog.LevelWarn
		}

		slog.Log(r.Context(), level, "access",
			"method", r.Method,
			"path", fullPath,
			"status", rec.status,
			"duration", duration.Round(time.Microsecond).String(),
			"bytes", rec.bytes,
			"ip", ip,
		)
	})
}

// realIP returns the best available client IP for logging.
//
// It uses X-Forwarded-For only when trustedProxy is non-nil AND the direct
// TCP peer address is contained within that network. This prevents clients
// from injecting arbitrary IPs when no proxy is present.
//
// When XFF contains multiple addresses (left-most = original client,
// right-most = last proxy), the left-most value is used.
func realIP(r *http.Request, trustedProxy *net.IPNet) string {
	peer := peerIP(r.RemoteAddr)

	if trustedProxy == nil || peer == nil || !trustedProxy.Contains(peer) {
		return r.RemoteAddr
	}

	// Proxy is trusted — use the left-most (original client) XFF address.
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		// XFF may be a comma-separated list: "client, proxy1, proxy2"
		if idx := strings.IndexByte(xff, ','); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return xff
	}

	return r.RemoteAddr
}

// peerIP extracts the IP from a "host:port" or bare "host" remote address.
func peerIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// No port — try parsing directly.
		host = remoteAddr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

// ---------------------------------------------------------------------------
// Per-IP rate limiting
// ---------------------------------------------------------------------------

// ipRateLimiter holds one token-bucket limiter per remote IP and evicts
// entries that have not been seen for more than ttl (default 5 minutes).
// This prevents the map from growing unboundedly on servers with many
// distinct visitors.
type ipRateLimiter struct {
	mu       sync.Mutex
	entries  map[string]*limiterEntry
	r        rate.Limit
	burst    int
	ttl      time.Duration
	stopOnce sync.Once
	stop     chan struct{}
}

type limiterEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// newIPRateLimiter creates a limiter that allows `r` requests per second with
// a burst of `burst`, and evicts idle entries after `ttl`.
// A background goroutine is started; call Close() (or rely on process exit) to stop it.
func newIPRateLimiter(r rate.Limit, burst int, ttl time.Duration) *ipRateLimiter {
	l := &ipRateLimiter{
		entries: make(map[string]*limiterEntry),
		r:       r,
		burst:   burst,
		ttl:     ttl,
		stop:    make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

func (l *ipRateLimiter) get(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[ip]
	if !ok {
		e = &limiterEntry{lim: rate.NewLimiter(l.r, l.burst)}
		l.entries[ip] = e
	}
	e.lastSeen = time.Now()
	return e.lim
}

func (l *ipRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(l.ttl / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.mu.Lock()
			cutoff := time.Now().Add(-l.ttl)
			for ip, e := range l.entries {
				if e.lastSeen.Before(cutoff) {
					delete(l.entries, ip)
				}
			}
			l.mu.Unlock()
		case <-l.stop:
			return
		}
	}
}

func (l *ipRateLimiter) Close() {
	l.stopOnce.Do(func() { close(l.stop) })
}

// ---------------------------------------------------------------------------
// Cache-Control headers
// ---------------------------------------------------------------------------

// noCacheMiddleware sets headers that prevent any caching of the response — used for paste content endpoints (/raw, /download, /{id}) where stale data must never be served, especially after a burn-on-read deletion.
func noCacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		next.ServeHTTP(w, r)
	})
}

// longCacheMiddleware allows public caches to store the response for 6 months (15 552 000 s). Used for static assets, the OpenAPI spec, the Swagger UI, and the /config endpoint — all of which are either truly static or change only on a new deployment.
func longCacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=15552000, immutable")
		next.ServeHTTP(w, r)
	})
}

// rateLimitMiddleware rejects requests that exceed the per-IP rate limit with
// 429 Too Many Requests. The client IP is resolved the same way as in the
// access log (respecting PASTEBIN_TRUSTED_PROXY).
func (a *App) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := realIP(r, a.cfg.TrustedProxy)
		if !a.limiter.get(ip).Allow() {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
