package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAPISpecRendersTemplate(t *testing.T) {
	cfg := &Settings{
		ServerSideEncryptionEnabled: true,
		MaxPasteSize:                12345,
		MaxTTL:                      24 * time.Hour,
		Version:                     "v1.2.3",
		BaseURL:                     "http://localhost:8080",
		SlugLen:                     12,
		ProtectedPasteEnabled:       true,
	}
	a := &App{cfg: cfg}
	spec, err := a.openAPISpec()
	if err != nil {
		t.Fatalf("openAPISpec: %v", err)
	}
	if !strings.Contains(spec, "v1.2.3") || !strings.Contains(spec, "12345") || !strings.Contains(spec, "true") {
		t.Fatalf("unexpected spec output: %s", spec)
	}
}

func TestHandleOpenAPISpec(t *testing.T) {
	cfg := &Settings{MaxPasteSize: 42, Version: "test"}
	a := &App{cfg: cfg}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	a.handleOpenAPISpec(rr, req)
	if got := rr.Result().Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected content type: %q", got)
	}
	if !strings.Contains(rr.Body.String(), "test") {
		t.Fatal("expected version in response")
	}
}

func TestHandleSwaggerUI(t *testing.T) {
	cfg := &Settings{BaseURL: "http://localhost:8080"}
	a := &App{cfg: cfg}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/swagger", nil)
	a.handleSwaggerUI(rr, req)
	if got := rr.Result().Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", got)
	}
	if !strings.Contains(rr.Body.String(), "<html") {
		t.Fatalf("unexpected swagger UI body: %q", rr.Body.String())
	}
}
