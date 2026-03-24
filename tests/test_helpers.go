package main_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/GAS85/ownPastebin"
)

func newTestApp(t *testing.T) *ownPastebin.App {
	tmpFile, err := os.CreateTemp("", "pastebin-*.db")
	if err != nil {
		t.Fatalf("failed to create temp DB: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	os.Remove(tmpPath)

	db := storage.NewSQLite(tmpPath)
	app := &ownPastebin.App{}
	app.Init(db)

	return app
}

// doRequest sends HTTP request to handler
func doRequest(handler http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// extractID extracts "id" field from JSON response
func extractID(body string) string {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		panic("invalid JSON in response")
	}
	return data["id"].(string)
}