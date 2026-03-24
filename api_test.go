package main

import (
	"strings"
	"testing"
	"time"
	"encoding/json"
)

func extractID(body string) string {
	var data map[string]interface{}
	json.Unmarshal([]byte(body), &data)
	return data["id"].(string)
}

func TestCreatePaste(t *testing.T) {
	app := newTestApp(t)
	handler := app.router()

	res := doRequest(handler, "POST", "/", strings.NewReader("hello world"))

	if res.Code != 201 {
		t.Fatalf("expected 201, got %d", res.Code)
	}
}

func TestGetPaste(t *testing.T) {
	app := newTestApp(t)
	handler := app.router()

	res := doRequest(handler, "POST", "/", strings.NewReader("hello"))
	id := extractID(res.Body.String())

	res = doRequest(handler, "GET", "/"+id, nil)

	if res.Code != 200 || !strings.Contains(res.Body.String(), "hello") {
		t.Fatal("paste retrieval failed")
	}
}

func TestRawPaste(t *testing.T) {
	app := newTestApp(t)
	handler := app.router()

	res := doRequest(handler, "POST", "/", strings.NewReader("raw content"))
	id := extractID(res.Body.String())

	res = doRequest(handler, "GET", "/raw/"+id, nil)

	if res.Body.String() != "raw content" {
		t.Fatal("raw mismatch")
	}
}

func TestDeletePaste(t *testing.T) {
	app := newTestApp(t)
	handler := app.router()

	res := doRequest(handler, "POST", "/", strings.NewReader("delete me"))
	id := extractID(res.Body.String())

	doRequest(handler, "DELETE", "/"+id, nil)

	res = doRequest(handler, "GET", "/"+id, nil)

	if res.Code != 404 {
		t.Fatal("paste should be deleted")
	}
}

func TestBurnAfterRead(t *testing.T) {
	app := newTestApp(t)
	handler := app.router()

	res := doRequest(handler, "POST", "/?burn=true", strings.NewReader("burn"))
	id := extractID(res.Body.String())

	doRequest(handler, "GET", "/"+id, nil)
	res = doRequest(handler, "GET", "/"+id, nil)

	if res.Code != 404 {
		t.Fatal("burn-after-read failed")
	}
}

func TestTTLExpiry(t *testing.T) {
	app := newTestApp(t)
	handler := app.router()

	res := doRequest(handler, "POST", "/?ttl=1", strings.NewReader("ttl"))
	id := extractID(res.Body.String())

	time.Sleep(2 * time.Second)

	res = doRequest(handler, "GET", "/"+id, nil)

	if res.Code != 404 {
		t.Fatal("TTL did not expire")
	}
}

func TestLargePayloadRejected(t *testing.T) {
	app := newTestApp(t)
	handler := app.router()

	big := strings.Repeat("x", 6*1024*1024)

	res := doRequest(handler, "POST", "/", strings.NewReader(big))

	if res.Code != 413 {
		t.Fatal("expected 413")
	}
}