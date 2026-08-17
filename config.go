package main

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// ExpiryOption is a label/value pair shown in the expiry dropdown.
// It lives here (rather than routes.go) because Settings owns the configured list.
type ExpiryOption struct {
	Label string
	Value string
}

// defaultExpiryTimes is the built-in list used when PASTEBIN_EXPIRY_TIMES is unset.
var defaultExpiryTimes = []ExpiryOption{
	{"Never", "0"},
	{"5 min", "300"},
	{"10 min", "600"},
	{"1 hour", "3600"},
	{"1 day", "86400"},
	{"1 week", "604800"},
	{"1 month", "2592000"},
	{"1 year", "31536000"},
}

type Settings struct {
	// Storage
	RedisURL    string
	PostgresURL string
	SQLitePath  string

	// App
	BaseURL            string
	PathPrefix         string // e.g. "" for http://host:port  or  "/pastebin" for http://host:port/pastebin
	DefaultBurn        bool
	DefaultTTL         time.Duration
	MaxTTL             time.Duration
	SlugLen            int
	MaxPasteSize       int64
	MaxParallelUploads int
	SQLitePageSize     int // 0 = SQLite default (4096); only effective on new databases
	Version            string

	// Cookie and Privacy note URLs to your Services.
	// We do not set any cookies, but if used as sub path probably you have to provide it.
	CookieURL          string
	PrivacyNoteURL     string

	// ExpiryTimes is the list of expiry options shown in the UI dropdown.
	// Populated from PASTEBIN_EXPIRY_TIMES; falls back to defaultExpiryTimes when empty.
	ExpiryTimes []ExpiryOption

	// Security
	ServerSideEncryptionEnabled bool
	ServerSideEncryptionKey     string

	// ProtectedPasteEnabled enables the ?protected=true creation flag.
	// When true, pastes created with protected=true cannot be deleted via the
	// DELETE API — attempts return HTTP 403.  Existing pastes are unaffected by
	// toggling this flag at runtime (the protected column is always persisted).
	ProtectedPasteEnabled bool

	// TrustedProxy is the CIDR range (or single IP) of a reverse proxy whose
	// X-Forwarded-For header is trusted for real-IP logging.
	// Empty / unset means XFF is never trusted — r.RemoteAddr is always used.
	// Set via PASTEBIN_TRUSTED_PROXY, e.g. "127.0.0.1" or "10.0.0.0/8".
	TrustedProxy *net.IPNet

	// Custom Theme CSS
	ThemeCSS string
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
		DefaultBurn:  getEnvBool("PASTEBIN_DEFAULT_BURN", false),
		MaxPasteSize: parseSize(getEnv("PASTEBIN_MAX_PASTE_SIZE", "5MB")),
		MaxParallelUploads: getEnvInt("PASTEBIN_MAX_PARALLEL_UPLOADS", 20), // 50 concurrent uploads max
																			// It needs 2 GB RAM for 25 MB pastes
																			// uploadSem = RAM / Max Upload size
																			// uploadSem = 1,5GB / 30 MB = 50
		SQLitePageSize:     getEnvInt("PASTEBIN_SQLITE_PAGE_SIZE", 0),

		// Optional Cookie Policy / Privacy URLs.
		// Empty when the environment variables are not set.
		CookieURL:      os.Getenv("PASTEBIN_COOKIE_URL"),
		PrivacyNoteURL: os.Getenv("PASTEBIN_PRIVACY_URL"),

		ServerSideEncryptionEnabled: getEnvBool("PASTEBIN_SERVER_SIDE_ENCRYPTION_ENABLED", false),
		ServerSideEncryptionKey:     os.Getenv("PASTEBIN_SERVER_SIDE_ENCRYPTION_KEY"),

		ProtectedPasteEnabled: getEnvBool("PASTEBIN_PROTECTED_PASTE_ENABLED", false),

		ThemeCSS: os.Getenv("PASTEBIN_THEME_CUSTOM_CSS"),

		Version: os.Getenv("VERSION"),
	}

	// "m" = months to avoid the minutes ambiguity from the Python version —
	// use explicit units: 300s, 1h, 7d, 1mo
	s.DefaultTTL = parseTime(getEnv("PASTEBIN_DEFAULT_TTL", "0"))
	s.MaxTTL = parseTime(os.Getenv("PASTEBIN_MAX_TTL"))

	if raw := os.Getenv("PASTEBIN_TRUSTED_PROXY"); raw != "" {
		s.TrustedProxy = parseCIDR(raw)
	}

	// PASTEBIN_EXPIRY_TIMES — optional, comma-separated "Label:seconds" pairs.
	// Example: "Never:0,5 min:300,1 hour:3600,1 day:86400"
	// Falls back to defaultExpiryTimes when unset or empty.
	s.ExpiryTimes = parseExpiryTimes(os.Getenv("PASTEBIN_EXPIRY_TIMES"))

	return s
}

// parseExpiryTimes parses a comma-separated list of "Label:seconds" pairs into
// []ExpiryOption.  Each entry must contain exactly one colon; the seconds value
// must be a non-negative integer.  Malformed entries are silently skipped so
// that a single typo does not disable the entire dropdown.
//
// Returns nil (causing the caller to fall back to defaultExpiryTimes) when raw
// is empty or every entry is malformed.
//
// Examples of valid input:
//
//	"Never:0,5 min:300,1 hour:3600,1 day:86400,1 week:604800"
//	"10 minutes:600,1 day:86400"
func parseExpiryTimes(raw string) []ExpiryOption {
	raw = strings.TrimSpace(raw)
	// Strip surrounding quote characters that container runtimes (Docker,
	// Kubernetes) sometimes pass literally when the env value is written as
	// PASTEBIN_EXPIRY_TIMES="…" or PASTEBIN_EXPIRY_TIMES='…' in a config file.
	raw = strings.Trim(raw, `"'`)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var out []ExpiryOption
	for _, entry := range strings.Split(raw, ",") {
		// Trim whitespace and any stray quote characters that may surround
		// individual entries after splitting (e.g. the very first or last token
		// when the value arrived as "Never:0,...,6 year:31536000").
		entry = strings.Trim(strings.TrimSpace(entry), `"'`)
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		// Split on the first colon only so labels may never contain one,
		// keeping the format unambiguous.
		idx := strings.Index(entry, ":")
		if idx <= 0 || idx == len(entry)-1 {
			os.Stderr.WriteString("PASTEBIN_EXPIRY_TIMES: skipping malformed entry \"" + entry + "\" (expected \"Label:seconds\")\n")
			continue
		}

		label := strings.TrimSpace(entry[:idx])
		valueStr := strings.TrimSpace(entry[idx+1:])

		n, err := strconv.ParseInt(valueStr, 10, 64)
		if err != nil || n < 0 {
			os.Stderr.WriteString("PASTEBIN_EXPIRY_TIMES: skipping entry \"" + entry + "\": seconds must be a non-negative integer\n")
			continue
		}

		out = append(out, ExpiryOption{Label: label, Value: valueStr})
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// expiryTimes returns the configured expiry options, falling back to the
// built-in defaults when none were provided via PASTEBIN_EXPIRY_TIMES.
func (s *Settings) expiryTimes() []ExpiryOption {
	if len(s.ExpiryTimes) > 0 {
		return s.ExpiryTimes
	}
	return defaultExpiryTimes
}

// parseCIDR parses a CIDR string ("10.0.0.0/8") or a bare IP ("127.0.0.1")
// into a *net.IPNet. Returns nil and logs a warning on invalid input so that
// a misconfigured value never silently trusts all peers.
func parseCIDR(raw string) *net.IPNet {
	input := strings.TrimSpace(raw)

	// Bare IP — promote to host-only CIDR (/32 for IPv4, /128 for IPv6).
	if !strings.Contains(input, "/") {
		ip := net.ParseIP(input)
		if ip == nil {
			// Logger may not be initialised yet at config-load time.
			os.Stderr.WriteString("PASTEBIN_TRUSTED_PROXY: invalid IP \"" + raw + "\"; XFF will not be trusted\n")
			return nil
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		_, network, _ := net.ParseCIDR(ip.String() + "/" + strconv.Itoa(bits))
		return network
	}

	_, network, err := net.ParseCIDR(input)
	if err != nil {
		os.Stderr.WriteString("PASTEBIN_TRUSTED_PROXY: invalid CIDR \"" + raw + "\"; XFF will not be trusted\n")
		return nil
	}
	return network
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