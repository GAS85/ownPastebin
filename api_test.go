package main

import (
	"strings"
	"testing"
	"time"
)

func TestCreatePaste(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/", strings.NewReader("hello world"))
	if res.Code != 201 {
		t.Fatalf("expected 201, got %d — body: %s", res.Code, res.Body.String())
	}
}

func TestGetPaste(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/", strings.NewReader("hello"))
	id := extractID(t, res.Body.String())

	res = doRequest(t, handler, "GET", "/"+id, nil)
	if res.Code != 200 {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if !strings.Contains(res.Body.String(), "hello") {
		t.Fatalf("body does not contain paste content: %s", res.Body.String())
	}
}

func TestRawPaste(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/", strings.NewReader("raw content"))
	id := extractID(t, res.Body.String())

	res = doRequest(t, handler, "GET", "/raw/"+id, nil)
	if res.Code != 200 {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if res.Body.String() != "raw content" {
		t.Fatalf("raw mismatch: got %q", res.Body.String())
	}
}

func TestDownloadPaste(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/", strings.NewReader("download me"))
	id := extractID(t, res.Body.String())

	res = doRequest(t, handler, "GET", "/download/"+id, nil)
	if res.Code != 200 {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	cd := res.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Fatalf("expected attachment disposition, got %q", cd)
	}
}

func TestDeletePaste(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/", strings.NewReader("delete me"))
	id := extractID(t, res.Body.String())

	doRequest(t, handler, "DELETE", "/"+id, nil)

	res = doRequest(t, handler, "GET", "/"+id, nil)
	if res.Code != 404 {
		t.Fatalf("expected 404 after delete, got %d", res.Code)
	}
}

func TestBurnAfterRead(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/?burn=true", strings.NewReader("burn me"))
	id := extractID(t, res.Body.String())

	// First read — should succeed.
	res = doRequest(t, handler, "GET", "/"+id, nil)
	if res.Code != 200 {
		t.Fatalf("first read: expected 200, got %d", res.Code)
	}

	// Second read — paste must be gone.
	res = doRequest(t, handler, "GET", "/"+id, nil)
	if res.Code != 404 {
		t.Fatalf("second read: expected 404 (burned), got %d", res.Code)
	}
}

func TestTTLExpiry(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/?ttl=1", strings.NewReader("expires soon"))
	id := extractID(t, res.Body.String())

	// Confirm it exists immediately.
	res = doRequest(t, handler, "GET", "/"+id, nil)
	if res.Code != 200 {
		t.Fatalf("before expiry: expected 200, got %d", res.Code)
	}

	time.Sleep(2 * time.Second)

	res = doRequest(t, handler, "GET", "/"+id, nil)
	if res.Code != 404 {
		t.Fatalf("after expiry: expected 404, got %d", res.Code)
	}
}

func TestLargePayloadRejected(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{
		MaxPasteSize: 1024, // 1KB limit for this test
	})

	big := strings.Repeat("x", 2048)
	res := doRequest(t, handler, "POST", "/", strings.NewReader(big))
	if res.Code != 413 {
		t.Fatalf("expected 413 for oversized payload, got %d", res.Code)
	}
}

func TestEmptyPayloadRejected(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/", strings.NewReader(""))
	if res.Code != 400 {
		t.Fatalf("expected 400 for empty payload, got %d", res.Code)
	}
}

func TestInvalidTTLRejected(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/?ttl=notanumber", strings.NewReader("data"))
	if res.Code != 400 {
		t.Fatalf("expected 400 for invalid TTL, got %d", res.Code)
	}
}

func TestNegativeTTLRejected(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/?ttl=-1", strings.NewReader("data"))
	if res.Code != 400 {
		t.Fatalf("expected 400 for negative TTL, got %d", res.Code)
	}
}

func TestConfigEndpoint(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "GET", "/config", nil)
	if res.Code != 200 {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	body := res.Body.String()
	for _, field := range []string{"max_ttl", "default_ttl", "max_paste_size", "server_side_encryption"} {
		if !strings.Contains(body, field) {
			t.Errorf("config response missing field %q: %s", field, body)
		}
	}
}

func TestUnknownIDReturns404(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "GET", "/doesnotexist123", nil)
	if res.Code != 404 {
		t.Fatalf("expected 404 for unknown id, got %d", res.Code)
	}
}

func TestLangPreserved(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/?lang=python", strings.NewReader("print('hi')"))
	if res.Code != 201 {
		t.Fatalf("create: expected 201, got %d", res.Code)
	}
	body := res.Body.String()
	if !strings.Contains(body, `"lang":"python"`) {
		t.Fatalf("lang not reflected in response: %s", body)
	}
}

func TestSubpathPrefix(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{PathPrefix: "/pastebin"})

	// Without prefix stripping the handler receives "/" after stripping,
	// so a plain POST to "/" should still work as the router sees it.
	res := doRequest(t, handler, "POST", "/", strings.NewReader("subpath"))
	if res.Code != 201 {
		t.Fatalf("expected 201, got %d", res.Code)
	}
	// The returned URL should contain the prefix.
	if !strings.Contains(res.Body.String(), "/pastebin/") {
		t.Fatalf("URL missing prefix: %s", res.Body.String())
	}
}

// ---- Protected paste tests --------------------------------------------------

func TestProtectedPasteDeleteBlocked(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{ProtectedPasteEnabled: true})

	res := doRequest(t, handler, "POST", "/?protected=true", strings.NewReader("secret"))
	if res.Code != 201 {
		t.Fatalf("create: expected 201, got %d", res.Code)
	}
	body := res.Body.String()
	if !strings.Contains(body, `"protected":true`) {
		t.Fatalf("response missing protected:true — got: %s", body)
	}
	id := extractID(t, body)

	res = doRequest(t, handler, "DELETE", "/"+id, nil)
	if res.Code != 403 {
		t.Fatalf("delete protected: expected 403, got %d", res.Code)
	}
}

func TestProtectedPasteStillReadable(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{ProtectedPasteEnabled: true})

	res := doRequest(t, handler, "POST", "/?protected=true", strings.NewReader("readable secret"))
	id := extractID(t, res.Body.String())

	res = doRequest(t, handler, "GET", "/"+id, nil)
	if res.Code != 200 {
		t.Fatalf("read protected: expected 200, got %d", res.Code)
	}
	if !strings.Contains(res.Body.String(), "readable secret") {
		t.Fatalf("body missing paste content: %s", res.Body.String())
	}
}

func TestProtectedPasteRawAndDownloadWork(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{ProtectedPasteEnabled: true})

	res := doRequest(t, handler, "POST", "/?protected=true", strings.NewReader("raw secret"))
	id := extractID(t, res.Body.String())

	res = doRequest(t, handler, "GET", "/raw/"+id, nil)
	if res.Code != 200 {
		t.Fatalf("raw: expected 200, got %d", res.Code)
	}

	res = doRequest(t, handler, "GET", "/download/"+id, nil)
	if res.Code != 200 {
		t.Fatalf("download: expected 200, got %d", res.Code)
	}
}

func TestProtectedFlagIgnoredWhenFeatureDisabled(t *testing.T) {
	// With ProtectedPasteEnabled=false (default), ?protected=true is silently
	// ignored and DELETE must succeed normally.
	_, handler := NewAppForTest(t, TestConfig{})

	res := doRequest(t, handler, "POST", "/?protected=true", strings.NewReader("not really protected"))
	if res.Code != 201 {
		t.Fatalf("create: expected 201, got %d", res.Code)
	}
	body := res.Body.String()
	// Feature disabled → protected should be false in the response.
	if !strings.Contains(body, `"protected":false`) {
		t.Fatalf("expected protected:false when feature disabled — got: %s", body)
	}
	id := extractID(t, body)

	res = doRequest(t, handler, "DELETE", "/"+id, nil)
	if res.Code != 200 {
		t.Fatalf("delete when feature disabled: expected 200, got %d", res.Code)
	}
}

func TestProtectedPasteWithTTL(t *testing.T) {
	// TTL expiry must still work on protected pastes.
	_, handler := NewAppForTest(t, TestConfig{ProtectedPasteEnabled: true})

	res := doRequest(t, handler, "POST", "/?protected=true&ttl=1", strings.NewReader("expires soon"))
	id := extractID(t, res.Body.String())

	res = doRequest(t, handler, "GET", "/"+id, nil)
	if res.Code != 200 {
		t.Fatalf("before expiry: expected 200, got %d", res.Code)
	}

	time.Sleep(2 * time.Second)

	res = doRequest(t, handler, "GET", "/"+id, nil)
	if res.Code != 404 {
		t.Fatalf("after expiry: expected 404, got %d", res.Code)
	}
}

func TestProtectedPasteWithBurn(t *testing.T) {
	// Burn-on-read must still work on protected pastes — protection only blocks
	// the explicit DELETE endpoint.
	_, handler := NewAppForTest(t, TestConfig{ProtectedPasteEnabled: true})

	res := doRequest(t, handler, "POST", "/?protected=true&burn=true", strings.NewReader("burn and protect"))
	id := extractID(t, res.Body.String())

	res = doRequest(t, handler, "GET", "/"+id, nil)
	if res.Code != 200 {
		t.Fatalf("first read: expected 200, got %d", res.Code)
	}

	res = doRequest(t, handler, "GET", "/"+id, nil)
	if res.Code != 404 {
		t.Fatalf("second read (burn): expected 404, got %d", res.Code)
	}
}

func TestConfigIncludesProtectedPasteFlag(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{ProtectedPasteEnabled: true})

	res := doRequest(t, handler, "GET", "/config", nil)
	if res.Code != 200 {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if !strings.Contains(res.Body.String(), `"protected_paste_enabled":true`) {
		t.Fatalf("config missing protected_paste_enabled: %s", res.Body.String())
	}
}
