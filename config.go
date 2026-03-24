package main

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Settings struct {
	// Storage
	RedisURL    string
	PostgresURL string
	SQLitePath  string

	// App
	BaseURL      string
	PathPrefix   string // e.g. "" for http://host:port  or  "/pastebin" for http://host:port/pastebin
	DefaultTTL   time.Duration
	MaxTTL       time.Duration
	SlugLen      int
	MaxPasteSize int64
	Version      string

	// Security
	ServerSideEncryptionEnabled bool
	ServerSideEncryptionKey     string
}

func loadSettings() *Settings {
	baseURL := getEnv("PASTEBIN_BASE_URL", "http://localhost:8080")

	s := &Settings{
		RedisURL:    os.Getenv("PASTEBIN_REDIS_URL"),
		PostgresURL: os.Getenv("PASTEBIN_POSTGRES_URL"),
		SQLitePath:  getEnv("PASTEBIN_SQLITE_PATH", "/app/data/pastes.db"),

		BaseURL:      baseURL,
		PathPrefix:   extractPathPrefix(baseURL),
		SlugLen:      getEnvInt("PASTEBIN_SLUG_LEN", 20),
		MaxPasteSize: parseSize(getEnv("PASTEBIN_MAX_PASTE_SIZE", "5MB")),

		ServerSideEncryptionEnabled: getEnvBool("PASTEBIN_SERVER_SIDE_ENCRYPTION_ENABLED", false),
		ServerSideEncryptionKey:     os.Getenv("PASTEBIN_SERVER_SIDE_ENCRYPTION_KEY"),

		Version: os.Getenv("VERSION"),
	}

	// "m" = months to avoid the minutes ambiguity from the Python version —
	// use explicit units: 300s, 1h, 7d, 1mo
	s.DefaultTTL = parseTime(getEnv("PASTEBIN_DEFAULT_TTL", "0"))
	s.MaxTTL = parseTime(os.Getenv("PASTEBIN_MAX_TTL"))

	return s
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := strings.ToLower(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// parseSize parses "5MB", "512KB", "1GB" or raw bytes.
func parseSize(v string) int64 {
	v = strings.TrimSpace(strings.ToUpper(v))
	multipliers := []struct {
		suffix string
		factor int64
	}{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
	}
	for _, m := range multipliers {
		if strings.HasSuffix(v, m.suffix) {
			n, _ := strconv.ParseInt(strings.TrimSuffix(v, m.suffix), 10, 64)
			return n * m.factor
		}
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

// parseTime parses durations: "300s", "1h", "7d", "1mo". Returns 0 for "0" or "".
func parseTime(v string) time.Duration {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" || v == "0" {
		return 0
	}
	// months: "1mo", "3mo"
	if strings.HasSuffix(v, "mo") {
		n, _ := strconv.Atoi(strings.TrimSuffix(v, "mo"))
		return time.Duration(n) * 30 * 24 * time.Hour
	}
	// days: "7d"
	if strings.HasSuffix(v, "d") {
		n, _ := strconv.Atoi(strings.TrimSuffix(v, "d"))
		return time.Duration(n) * 24 * time.Hour
	}
	// hours: "1h"
	if strings.HasSuffix(v, "h") {
		n, _ := strconv.Atoi(strings.TrimSuffix(v, "h"))
		return time.Duration(n) * time.Hour
	}
	// seconds (explicit or bare int)
	raw := strings.TrimSuffix(v, "s")
	n, _ := strconv.Atoi(raw)
	return time.Duration(n) * time.Second
}

// resolveTTL applies PASTEBIN_MAX_TTL clamping, mirroring the Python logic.
func (s *Settings) resolveTTL(requested time.Duration) time.Duration {
	if s.MaxTTL == 0 {
		return requested
	}
	if requested == 0 {
		return s.MaxTTL
	}
	if requested > s.MaxTTL {
		return s.MaxTTL
	}
	return requested
}

// extractPathPrefix pulls the path component out of a full base URL and
// normalises it for use as a router prefix:
//
//	"http://localhost:8080"           → ""
//	"http://localhost:8080/"          → ""
//	"http://localhost:8080/pastebin"  → "/pastebin"
//	"http://localhost:8080/a/b/"      → "/a/b"
//
// It never returns a trailing slash so it can be safely prepended to paths:
// prefix + "/static/..." always produces a valid path.
func extractPathPrefix(rawURL string) string {
	// Strip scheme so we don't need net/url just for this.
	s := rawURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// Strip host:port — everything up to the first /
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[i:]
	} else {
		return ""
	}
	// Trim trailing slashes
	s = strings.TrimRight(s, "/")
	return s
}
