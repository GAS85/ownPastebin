package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestResponseRecorderCapturesStatusAndBytes(t *testing.T) {
	rw := httptest.NewRecorder()
	rec := &responseRecorder{ResponseWriter: rw, status: http.StatusOK}
	rec.WriteHeader(http.StatusTeapot)
	n, err := rec.Write([]byte("abc"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != 3 || rec.bytes != 3 {
		t.Fatalf("expected 3 bytes written, got %d/%d", n, rec.bytes)
	}
	if rw.Code != http.StatusTeapot {
		t.Fatalf("expected status %d, got %d", http.StatusTeapot, rw.Code)
	}
}

func TestNoCacheMiddlewareSetsHeaders(t *testing.T) {
	app := &App{cfg: &Settings{BaseURL: "http://localhost:8080", Origin: "http://localhost:8080"}}
	h := app.noCacheMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := w.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("unexpected Cache-Control header: %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:8080" {
		t.Fatalf("unexpected Access-Control-Allow-Origin: %q", got)
	}
	// The view page renders Prism/Mermaid JS — it must NOT get a
	// script-blocking CSP from this middleware.
	if got := w.Header().Get("Content-Security-Policy"); got != "" {
		t.Fatalf("noCacheMiddleware must not set its own CSP, got %q", got)
	}
}

func TestNoCacheMiddlewareOmitsACAOWhenOriginEmpty(t *testing.T) {
	app := &App{cfg: &Settings{}}
	h := app.noCacheMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no ACAO header when Origin is unset, got %q", got)
	}
}

func TestLongCacheMiddlewareSetsHeaders(t *testing.T) {
	app := &App{cfg: &Settings{BaseURL: "http://localhost:8080", Origin: "http://localhost:8080"}}
	h := app.longCacheMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := w.Header().Get("Cache-Control"); got != "public, max-age=15552000, immutable" {
		t.Fatalf("unexpected Cache-Control header: %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:8080" {
		t.Fatalf("unexpected Access-Control-Allow-Origin: %q", got)
	}
	// Swagger UI needs to run its own JS — must not get a restrictive CSP here.
	if got := w.Header().Get("Content-Security-Policy"); got != "" {
		t.Fatalf("longCacheMiddleware must not set its own CSP, got %q", got)
	}
}

func TestRawContentMiddlewareSandboxesCSP(t *testing.T) {
	app := &App{cfg: &Settings{BaseURL: "http://localhost:8080", Origin: "http://localhost:8080"}}
	h := app.rawContentMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := w.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("unexpected Cache-Control header: %q", got)
	}
	if got := w.Header().Get("Content-Security-Policy"); got != "sandbox" {
		t.Fatalf("expected sandboxed CSP for raw content, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:8080" {
		t.Fatalf("unexpected Access-Control-Allow-Origin: %q", got)
	}
}

func TestSecurityHeadersMiddlewareSetsBaseline(t *testing.T) {
	h := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	cases := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "SAMEORIGIN",
		"Referrer-Policy":        "no-referrer",
	}
	for header, want := range cases {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s: got %q, want %q", header, got, want)
		}
	}
}

// securityHeadersMiddleware runs before route-specific middleware in the
// chain (r.Use before r.With), so a stricter downstream CSP must win over
// the baseline via plain header overwrite — this locks in that ordering.
func TestSecurityHeadersOverriddenByRawContentMiddleware(t *testing.T) {
	app := &App{cfg: &Settings{}}
	h := securityHeadersMiddleware(app.rawContentMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := w.Header().Get("Content-Security-Policy"); got != "sandbox" {
		t.Fatalf("expected rawContentMiddleware's sandbox CSP to win, got %q", got)
	}
	// Headers securityHeadersMiddleware sets and nothing downstream touches
	// must still be present.
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected baseline nosniff to survive, got %q", got)
	}
}

func TestRateLimitMiddlewareRejectsWhenExceeded(t *testing.T) {
	limiter := rate.NewLimiter(rate.Limit(0), 1)
	limiter.Allow() // consume the initial burst token so the next request is rejected
	app := &App{
		cfg:     &Settings{},
		limiter: &ipRateLimiter{entries: map[string]*limiterEntry{"1.2.3.4:1234": {lim: limiter}}},
	}

	h := app.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
}

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
	}
	for _, tc := range cases {
		if got := parseLogLevel(tc.input); got != tc.want {
			t.Fatalf("parseLogLevel(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestLevelString(t *testing.T) {
	if got := levelString(slog.LevelError); got != "ERROR" {
		t.Fatalf("expected ERROR, got %q", got)
	}
	if got := levelString(slog.LevelInfo); got != "INFO" {
		t.Fatalf("expected INFO, got %q", got)
	}
	if got := levelString(slog.LevelDebug); got != "DEBUG" {
		t.Fatalf("expected DEBUG, got %q", got)
	}
}

func TestTextHandlerFormatsLine(t *testing.T) {
	var buf bytes.Buffer
	h := &textHandler{w: &buf, level: slog.LevelDebug, dateFormat: "2006-01-02"}
	rec := slog.NewRecord(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC), slog.LevelInfo, "hello", 0)
	rec.AddAttrs(slog.String("component", "storage"), slog.String("foo", "bar"))
	if err := h.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "INFO") || !strings.Contains(output, "storage") || !strings.Contains(output, "foo=bar") {
		t.Fatalf("unexpected formatted text: %q", output)
	}
}

func TestTextHandlerWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := (&textHandler{w: &buf, level: slog.LevelDebug, dateFormat: "2006-01-02"}).WithAttrs([]slog.Attr{slog.String("component", "app"), slog.String("x", "y")}).(*textHandler)
	rec := slog.NewRecord(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC), slog.LevelInfo, "hello", 0)
	rec.AddAttrs(slog.String("foo", "bar"))
	if err := h.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, " - app - hello") || !strings.Contains(output, "x=y") || !strings.Contains(output, "foo=bar") {
		t.Fatalf("unexpected formatted text with attrs: %q", output)
	}
}

func TestJSONMsgHandlerFormatsJSON(t *testing.T) {
	var buf bytes.Buffer
	h := &jsonMsgHandler{w: &buf, level: slog.LevelDebug}
	rec := slog.NewRecord(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC), slog.LevelWarn, "failed", 0)
	rec.AddAttrs(slog.String("component", "storage"), slog.String("detail", "x"))
	if err := h.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}
	if entry["level"] != "WARN" || entry["component"] != "storage" {
		t.Fatalf("unexpected JSON fields: %v", entry)
	}
	msg, ok := entry["msg"].(map[string]any)
	if !ok || msg["detail"] != "x" || msg["message"] != "failed" {
		t.Fatalf("unexpected msg object: %v", msg)
	}
}

func TestIPRateLimiterCleanupRemovesStaleEntries(t *testing.T) {
	l := newIPRateLimiter(rate.Limit(1), 1, 30*time.Millisecond)
	defer l.Close()

	l.mu.Lock()
	l.entries["stale"] = &limiterEntry{lim: rate.NewLimiter(rate.Limit(1), 1), lastSeen: time.Now().Add(-time.Minute)}
	l.mu.Unlock()

	time.Sleep(120 * time.Millisecond)

	l.mu.Lock()
	_, ok := l.entries["stale"]
	l.mu.Unlock()
	if ok {
		t.Fatal("expected stale entry to be cleaned up")
	}
}

func TestPeerIPParsesHostAndHostPort(t *testing.T) {
	if got := peerIP("1.2.3.4:8080").String(); got != "1.2.3.4" {
		t.Fatalf("unexpected peer IP: %q", got)
	}
	if got := peerIP("127.0.0.1").String(); got != "127.0.0.1" {
		t.Fatalf("unexpected peer IP for bare host: %q", got)
	}
}

func TestRealIPUsesTrustedXFF(t *testing.T) {
	_, trustedNet, _ := net.ParseCIDR("1.2.3.0/24")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 1.2.3.4")
	got := realIP(req, trustedNet)
	if got != "10.0.0.1" {
		t.Fatalf("expected trusted XFF to be used, got %q", got)
	}
}

func TestRealIPRejectsUntrustedXFF(t *testing.T) {
	_, trustedNet, _ := net.ParseCIDR("2.2.2.0/24")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	got := realIP(req, trustedNet)
	if got != "1.2.3.4:1234" {
		t.Fatalf("expected remote addr when proxy is untrusted, got %q", got)
	}
}

func TestNewCryptoRejectsInvalidLength(t *testing.T) {
	badKey := base64.StdEncoding.EncodeToString([]byte("short-key"))
	_, err := newCrypto(badKey)
	if err == nil {
		t.Fatal("expected error for invalid key length")
	}
}

func TestNewCryptoRejectsInvalidBase64(t *testing.T) {
	_, err := newCrypto("not-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}
