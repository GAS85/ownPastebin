package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func newTestApp(t *testing.T) *App {
	t.Helper()

	// Use temp SQLite
	tmpFile, err := os.CreateTemp("", "pastebin-*.db")
	if err != nil {
		t.Fatal(err)
	}

	os.Setenv("PASTEBIN_SQLITE_PATH", tmpFile.Name())
	os.Unsetenv("PASTEBIN_REDIS_URL")
	os.Unsetenv("PASTEBIN_POSTGRES_URL")

	cfg := loadSettings()

	store := newStorage(cfg)

	app := &App{
		cfg:     cfg,
		storage: store,
	}

	return app
}

func doRequest(ts http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	w := httptest.NewRecorder()
	ts.ServeHTTP(w, req)
	return w
}