package main

import (
	"log/slog"
	"net/http"
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
// 404s from the static file handler are logged at DEBUG to avoid noise.
func accessLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		ip := r.RemoteAddr
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ip = xff
		}

		// Build full path including query string so burn=true, ttl=3600 etc. are visible.
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
