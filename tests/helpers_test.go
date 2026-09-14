package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// doRequest fires an HTTP request directly against a handler with no real
// network involved, and returns the recorded response.
func doRequest(t *testing.T, handler http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// extractID parses the "id" field from a JSON create response.
func extractID(t *testing.T, body string) string {
	t.Helper()
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		t.Fatalf("extractID: invalid JSON %q: %v", body, err)
	}
	id, ok := data["id"].(string)
	if !ok || id == "" {
		t.Fatalf("extractID: missing or empty 'id' in %q", body)
	}
	return id
}
