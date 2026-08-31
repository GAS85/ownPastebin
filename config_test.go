package main

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// parseExpiryTimes
// ---------------------------------------------------------------------------

func TestParseExpiryTimesValid(t *testing.T) {
	opts := parseExpiryTimes("Never:0,5 min:300,1 hour:3600")
	if len(opts) != 3 {
		t.Fatalf("expected 3 options, got %d", len(opts))
	}
	if opts[0].Label != "Never" || opts[0].Value != "0" {
		t.Errorf("unexpected first entry: %+v", opts[0])
	}
	if opts[2].Label != "1 hour" || opts[2].Value != "3600" {
		t.Errorf("unexpected third entry: %+v", opts[2])
	}
}

func TestParseExpiryTimesEmpty(t *testing.T) {
	if got := parseExpiryTimes(""); got != nil {
		t.Fatalf("expected nil for empty input, got %v", got)
	}
}

func TestParseExpiryTimesMalformed(t *testing.T) {
	got := parseExpiryTimes("nocolon,bad:notanumber")
	if got != nil {
		t.Fatalf("expected nil for all-malformed input, got %v", got)
	}
}

func TestParseExpiryTimesPartiallyMalformed(t *testing.T) {
	got := parseExpiryTimes("bad,Good:60,alsobad:")
	if len(got) != 1 {
		t.Fatalf("expected 1 good entry, got %d", len(got))
	}
	if got[0].Label != "Good" {
		t.Errorf("unexpected label: %s", got[0].Label)
	}
}

func TestParseExpiryTimesStripsQuotes(t *testing.T) {
	got := parseExpiryTimes(`"Never:0,1 hour:3600"`)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
}

func TestParseExpiryTimesNegativeSeconds(t *testing.T) {
	got := parseExpiryTimes("Negative:-1")
	if got != nil {
		t.Fatalf("expected nil for negative seconds, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// parseCIDR
// ---------------------------------------------------------------------------

func TestParseCIDRBareIPv4(t *testing.T) {
	n := parseCIDR("127.0.0.1")
	if n == nil {
		t.Fatal("expected non-nil *net.IPNet")
	}
}

func TestParseCIDRRange(t *testing.T) {
	n := parseCIDR("10.0.0.0/8")
	if n == nil {
		t.Fatal("expected non-nil *net.IPNet")
	}
}

func TestParseCIDRInvalid(t *testing.T) {
	n := parseCIDR("notanip")
	if n != nil {
		t.Fatal("expected nil for invalid input")
	}
}

func TestParseCIDRInvalidCIDR(t *testing.T) {
	n := parseCIDR("10.0.0.0/999")
	if n != nil {
		t.Fatal("expected nil for invalid CIDR")
	}
}

// ---------------------------------------------------------------------------
// parseSize
// ---------------------------------------------------------------------------

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"5MB", 5 * 1024 * 1024},
		{"1GB", 1024 * 1024 * 1024},
		{"512KB", 512 * 1024},
		{"1024", 1024},
	}
	for _, c := range cases {
		got := parseSize(c.in)
		if got != c.want {
			t.Errorf("parseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// parseTime
// ---------------------------------------------------------------------------

func TestParseTime(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"0", 0},
		{"", 0},
		{"300s", 300 * time.Second},
		{"300", 300 * time.Second},
		{"1h", time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"1mo", 30 * 24 * time.Hour},
		{"3mo", 90 * 24 * time.Hour},
	}
	for _, c := range cases {
		got := parseTime(c.in)
		if got != c.want {
			t.Errorf("parseTime(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// resolveTTL
// ---------------------------------------------------------------------------

func TestResolveTTLNoMax(t *testing.T) {
	s := &Settings{MaxTTL: 0}
	want := 5 * time.Hour
	if got := s.resolveTTL(want); got != want {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestResolveTTLClampsToMax(t *testing.T) {
	s := &Settings{MaxTTL: time.Hour}
	got := s.resolveTTL(5 * time.Hour)
	if got != time.Hour {
		t.Errorf("expected %v, got %v", time.Hour, got)
	}
}

func TestResolveTTLZeroRequestedUsesMax(t *testing.T) {
	s := &Settings{MaxTTL: 2 * time.Hour}
	got := s.resolveTTL(0)
	if got != 2*time.Hour {
		t.Errorf("expected %v, got %v", 2*time.Hour, got)
	}
}

func TestResolveTTLBelowMax(t *testing.T) {
	s := &Settings{MaxTTL: 10 * time.Hour}
	want := 2 * time.Hour
	if got := s.resolveTTL(want); got != want {
		t.Errorf("expected %v, got %v", want, got)
	}
}

// ---------------------------------------------------------------------------
// extractPathPrefix
// ---------------------------------------------------------------------------

func TestExtractPathPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"http://localhost:8080", ""},
		{"http://localhost:8080/", ""},
		{"http://localhost:8080/pastebin", "/pastebin"},
		{"http://localhost:8080/a/b/", "/a/b"},
		{"http://host/x", "/x"},
	}
	for _, c := range cases {
		got := extractPathPrefix(c.in)
		if got != c.want {
			t.Errorf("extractPathPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// originFromBaseURL
// ---------------------------------------------------------------------------

func TestOriginFromBaseURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"http://localhost:8080", "http://localhost:8080"},
		{"http://localhost:8080/", "http://localhost:8080"},
		{"http://localhost:8080/pastebin", "http://localhost:8080"},
		{"https://paste.example.com/a/b", "https://paste.example.com"},
		{"https://paste.example.com:8443/x", "https://paste.example.com:8443"},
		{"not a url", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := originFromBaseURL(c.in); got != c.want {
			t.Errorf("originFromBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLoadSettingsPopulatesOrigin(t *testing.T) {
	t.Setenv("PASTEBIN_BASE_URL", "https://paste.example.com/pastebin")
	cfg := loadSettings()
	if cfg.Origin != "https://paste.example.com" {
		t.Fatalf("expected origin without path, got %q", cfg.Origin)
	}
}

// ---------------------------------------------------------------------------
// expiryTimes fallback
// ---------------------------------------------------------------------------

func TestExpiryTimesFallback(t *testing.T) {
	s := &Settings{}
	opts := s.expiryTimes()
	if len(opts) == 0 {
		t.Fatal("expected default expiry options, got none")
	}
}

func TestExpiryTimesCustom(t *testing.T) {
	s := &Settings{ExpiryTimes: []ExpiryOption{{Label: "X", Value: "1"}}}
	opts := s.expiryTimes()
	if len(opts) != 1 || opts[0].Label != "X" {
		t.Fatalf("unexpected: %v", opts)
	}
}

func TestGetEnvHelpers(t *testing.T) {
	t.Setenv("PASTEBIN_TEST_STRING", "hello")
	t.Setenv("PASTEBIN_TEST_INT", "42")
	t.Setenv("PASTEBIN_TEST_BOOL", "yes")

	if got := getEnv("PASTEBIN_TEST_STRING", "default"); got != "hello" {
		t.Fatalf("expected hello, got %q", got)
	}
	if got := getEnvInt("PASTEBIN_TEST_INT", 1); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
	if got := getEnvBool("PASTEBIN_TEST_BOOL", false); !got {
		t.Fatal("expected true for yes")
	}
}

func TestLoadSettingsReadsEnvironment(t *testing.T) {
	t.Setenv("PASTEBIN_BASE_URL", "http://localhost:8080/pastebin")
	t.Setenv("PASTEBIN_DEFAULT_TTL", "1h")
	t.Setenv("PASTEBIN_MAX_TTL", "2h")
	t.Setenv("PASTEBIN_TRUSTED_PROXY", "127.0.0.1")
	cfg := loadSettings()
	if cfg.PathPrefix != "/pastebin" {
		t.Fatalf("expected /pastebin path prefix, got %q", cfg.PathPrefix)
	}
	if cfg.DefaultTTL != time.Hour {
		t.Fatalf("expected DefaultTTL 1h, got %v", cfg.DefaultTTL)
	}
	if cfg.MaxTTL != 2*time.Hour {
		t.Fatalf("expected MaxTTL 2h, got %v", cfg.MaxTTL)
	}
	if cfg.TrustedProxy == nil || cfg.TrustedProxy.String() != "127.0.0.1/32" {
		t.Fatalf("unexpected TrustedProxy: %v", cfg.TrustedProxy)
	}
}
