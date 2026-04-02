package main

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
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
