package main

// This file is compiled only during `go test`. It exports a constructor that
// gives tests access to a fully wired App without starting a real HTTP server.
// All tests live in the root package as `package main` so they can access
// unexported symbols — no separate module or import path needed.

import (
	"encoding/base64"
	"html/template"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/GAS85/ownPastebin/plugins"
)

// TestConfig holds options for NewAppForTest.
type TestConfig struct {
	EncryptionEnabled bool
	// EncryptionKey must be exactly 32 bytes. If empty and EncryptionEnabled
	// is true, a fixed test key is used.
	EncryptionKey string
	// PathPrefix e.g. "/pastebin" — leave empty for root deployment.
	PathPrefix string
	// MaxPasteSize defaults to 5MB if 0.
	MaxPasteSize int64
}

// NewAppForTest builds a fully wired *App backed by a throwaway SQLite DB.
// The DB file is automatically cleaned up when the test finishes.
// Templates are rendered with a minimal stub so tests don't need the
// real templates/index.html on disk.
func NewAppForTest(t *testing.T, tc TestConfig) (*App, http.Handler) {
	t.Helper()

	// ── Temp DB ──────────────────────────────────────────────────────────────
	tmpFile, err := os.CreateTemp("", "pastebin-test-*.db")
	if err != nil {
		t.Fatalf("NewAppForTest: create temp db: %v", err)
	}
	dbPath := tmpFile.Name()
	tmpFile.Close()
	os.Remove(dbPath) // SQLite creates it fresh on open
	t.Cleanup(func() { os.Remove(dbPath) })

	// ── Config ────────────────────────────────────────────────────────────────
	maxSize := tc.MaxPasteSize
	if maxSize == 0 {
		maxSize = 5 * 1024 * 1024
	}
	cfg := &Settings{
		SQLitePath:                  dbPath,
		BaseURL:                     "http://localhost:8080" + tc.PathPrefix,
		PathPrefix:                  tc.PathPrefix,
		SlugLen:                     20,
		MaxPasteSize:                maxSize,
		ServerSideEncryptionEnabled: tc.EncryptionEnabled,
	}
	if tc.EncryptionEnabled {
		key := tc.EncryptionKey
		if key == "" {
			// Fixed 32-byte test key, base64-encoded.
			key = base64.StdEncoding.EncodeToString([]byte("pastebin-test-key-32bytes!!!!!!!"))
		}
		cfg.ServerSideEncryptionKey = key
	}

	// ── Storage ───────────────────────────────────────────────────────────────
	store, err := newSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewAppForTest: open sqlite: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// ── Crypto ────────────────────────────────────────────────────────────────
	var cry *Crypto
	if cfg.ServerSideEncryptionEnabled {
		cry, err = newCrypto(cfg.ServerSideEncryptionKey)
		if err != nil {
			t.Fatalf("NewAppForTest: newCrypto: %v", err)
		}
	}

	// ── Minimal stub template — avoids needing the real HTML file ─────────────
	// Tests only check HTTP status codes and JSON/plain-text bodies,
	// so a minimal template that renders the paste content is enough.
	const stubTmpl = `<!doctype html><html><body>` +
		`{{if .IsCreated}}<pre id="pastebin-code-block">{{.PastebinCode}}</pre>{{end}}` +
		`{{if .IsError}}<p>404</p>{{end}}` +
		`</body></html>`

	funcMap := template.FuncMap{
		"not":        func(b bool) bool { return !b },
		"safeJS":     func(s string) template.JS { return template.JS(s) },
		"formatTime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format(time.RFC3339)
		},
	}
	tmpl, err := template.New("index.html").Funcs(funcMap).Parse(stubTmpl)
	if err != nil {
		t.Fatalf("NewAppForTest: parse stub template: %v", err)
	}

	// ── Plugins ───────────────────────────────────────────────────────────────
	mgr := plugins.NewManager(plugins.DefaultBase(cfg.PathPrefix), nil)

	// ── App ───────────────────────────────────────────────────────────────────
	app := &App{
		cfg:     cfg,
		storage: store,
		crypto:  cry,
		tmpl:    tmpl,
		plugins: mgr,
	}

	return app, app.router()
}
