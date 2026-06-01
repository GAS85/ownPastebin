package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// GET /
// ---------------------------------------------------------------------------

func TestHandleNewPaste(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	res := doRequest(t, handler, "GET", "/", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if ct := res.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected text/html, got %q", ct)
	}
}

// ---------------------------------------------------------------------------
// POST / — basic create
// ---------------------------------------------------------------------------

func TestHandleCreatePaste_ReturnsCreated(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	res := doRequest(t, handler, "POST", "/", strings.NewReader("hello"))
	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Location") == "" {
		t.Fatal("expected Location header")
	}
}

func TestHandleCreatePaste_JSONResponse(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	res := doRequest(t, handler, "POST", "/", strings.NewReader("data"))
	var out map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, k := range []string{"id", "url", "lang"} {
		if _, ok := out[k]; !ok {
			t.Errorf("response missing %q", k)
		}
	}
}

func TestHandleCreatePaste_EmptyBody(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	res := doRequest(t, handler, "POST", "/", strings.NewReader(""))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestHandleCreatePaste_TooLarge(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{MaxPasteSize: 10})
	req := httptest.NewRequest("POST", "/", strings.NewReader("x"))
	req.ContentLength = 1000
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /config
// ---------------------------------------------------------------------------

func TestHandleConfig(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	res := doRequest(t, handler, "GET", "/config", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"max_ttl", "default_ttl", "max_paste_size", "server_side_encryption", "protected_paste_enabled"} {
		if _, ok := cfg[key]; !ok {
			t.Errorf("config missing key %q", key)
		}
	}
}

// ---------------------------------------------------------------------------
// GET /raw/{id}
// ---------------------------------------------------------------------------

func TestHandleRaw_NotFound(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	res := doRequest(t, handler, "GET", "/raw/doesnotexist", nil)
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
}

func TestHandleRaw_Found(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	post := doRequest(t, handler, "POST", "/", strings.NewReader("raw content"))
	id := extractID(t, post.Body.String())

	res := doRequest(t, handler, "GET", "/raw/"+id, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if res.Body.String() != "raw content" {
		t.Errorf("unexpected body: %q", res.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GET /download/{id}
// ---------------------------------------------------------------------------

func TestHandleDownload_NotFound(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	res := doRequest(t, handler, "GET", "/download/missing", nil)
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
}

func TestHandleDownload_Found(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	post := doRequest(t, handler, "POST", "/", strings.NewReader("dl content"))
	id := extractID(t, post.Body.String())

	res := doRequest(t, handler, "GET", "/download/"+id, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	cd := res.Header().Get("Content-Disposition")
	if !strings.Contains(cd, id) {
		t.Errorf("Content-Disposition should contain id %q, got %q", id, cd)
	}
}

// ---------------------------------------------------------------------------
// GET /{id}
// ---------------------------------------------------------------------------

func TestHandleView_NotFound(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	res := doRequest(t, handler, "GET", "/nosuchid", nil)
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
}

func TestHandleView_Found(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	post := doRequest(t, handler, "POST", "/", strings.NewReader("view me"))
	id := extractID(t, post.Body.String())

	res := doRequest(t, handler, "GET", "/"+id, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if !strings.Contains(res.Body.String(), "view me") {
		t.Error("body should contain paste content")
	}
}

// ---------------------------------------------------------------------------
// DELETE /{id}
// ---------------------------------------------------------------------------

func TestHandleDelete(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	post := doRequest(t, handler, "POST", "/", strings.NewReader("deleteme"))
	id := extractID(t, post.Body.String())

	del := doRequest(t, handler, "DELETE", "/"+id, nil)
	if del.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", del.Code)
	}
	get := doRequest(t, handler, "GET", "/raw/"+id, nil)
	if get.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", get.Code)
	}
}

func TestHandleDelete_Protected(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{ProtectedPasteEnabled: true})
	post := doRequest(t, handler, "POST", "/?protected=true", strings.NewReader("keepme"))
	id := extractID(t, post.Body.String())

	del := doRequest(t, handler, "DELETE", "/"+id, nil)
	if del.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", del.Code)
	}
}

func TestHandleDelete_NonExistent(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	res := doRequest(t, handler, "DELETE", "/fakeid", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
}

// ---------------------------------------------------------------------------
// Burn-on-read
// ---------------------------------------------------------------------------

func TestBurnOnReadViaView(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	post := doRequest(t, handler, "POST", "/?burn=true", strings.NewReader("burnit"))
	id := extractID(t, post.Body.String())

	first := doRequest(t, handler, "GET", "/"+id, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first read: expected 200, got %d", first.Code)
	}
	second := doRequest(t, handler, "GET", "/"+id, nil)
	if second.Code != http.StatusNotFound {
		t.Fatalf("second read: expected 404, got %d", second.Code)
	}
}

func TestBurnOnReadViaRaw(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	post := doRequest(t, handler, "POST", "/?burn=true", strings.NewReader("rawburn"))
	id := extractID(t, post.Body.String())

	first := doRequest(t, handler, "GET", "/raw/"+id, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first raw: expected 200, got %d", first.Code)
	}
	second := doRequest(t, handler, "GET", "/raw/"+id, nil)
	if second.Code != http.StatusNotFound {
		t.Fatalf("second raw: expected 404, got %d", second.Code)
	}
}

// ---------------------------------------------------------------------------
// ?lang preserved
// ---------------------------------------------------------------------------

func TestCreateWithLang(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	res := doRequest(t, handler, "POST", "/?lang=python", strings.NewReader("print('hi')"))
	if res.Code != 201 {
		t.Fatalf("expected 201, got %d", res.Code)
	}
	var out map[string]interface{}
	json.Unmarshal(res.Body.Bytes(), &out)
	if out["lang"] != "python" {
		t.Errorf("expected lang=python, got %v", out["lang"])
	}
}

// ---------------------------------------------------------------------------
// TTL validation
// ---------------------------------------------------------------------------

func TestCreateInvalidTTL(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	res := doRequest(t, handler, "POST", "/?ttl=notanumber", strings.NewReader("x"))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid ttl, got %d", res.Code)
	}
}

func TestCreateNegativeTTL(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{})
	res := doRequest(t, handler, "POST", "/?ttl=-1", strings.NewReader("x"))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative ttl, got %d", res.Code)
	}
}

// ---------------------------------------------------------------------------
// Upload semaphore — reject when full
// ---------------------------------------------------------------------------

func TestUploadSemaphoreRejection(t *testing.T) {
	app, handler := NewAppForTest(t, TestConfig{MaxParallelUploads: 1})
	app.uploadSem <- struct{}{} // pre-fill

	res := doRequest(t, handler, "POST", "/", strings.NewReader("should be rejected"))
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when semaphore full, got %d", res.Code)
	}
	<-app.uploadSem // drain so cleanup can proceed
}

// ---------------------------------------------------------------------------
// Storage: PeekMeta
// ---------------------------------------------------------------------------

func TestStoragePeekMeta(t *testing.T) {
	s := newTestStorage(t)
	s.Save("meta1", &PasteData{Content: []byte("data"), Burn: true, Lang: "go"}, 0)

	meta, err := s.PeekMeta("meta1")
	if err != nil {
		t.Fatalf("PeekMeta: %v", err)
	}
	if meta == nil {
		t.Fatal("expected non-nil meta")
	}
	if !meta.Burn {
		t.Error("expected Burn=true")
	}
	if meta.Lang != "go" {
		t.Errorf("expected Lang=go, got %s", meta.Lang)
	}
	if meta.Content != nil {
		t.Error("PeekMeta should not populate Content")
	}
}

func TestStoragePeekMetaMissing(t *testing.T) {
	s := newTestStorage(t)
	meta, err := s.PeekMeta("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta != nil {
		t.Fatal("expected nil for missing key")
	}
}

// ---------------------------------------------------------------------------
// Storage: slug collision
// ---------------------------------------------------------------------------

func TestStorageSlugConflict(t *testing.T) {
	s := newTestStorage(t)
	if err := s.Save("dup", &PasteData{Content: []byte("first")}, 0); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := s.Save("dup", &PasteData{Content: []byte("second")}, 0); err != ErrSlugConflict {
		t.Fatalf("expected ErrSlugConflict, got %v", err)
	}
	got, _ := s.Get("dup")
	if string(got.Content) != "first" {
		t.Errorf("content changed after conflict: %q", got.Content)
	}
}

// ---------------------------------------------------------------------------
// Storage: flags round-trip
// ---------------------------------------------------------------------------

func TestStorageFlagsRoundtrip(t *testing.T) {
	s := newTestStorage(t)
	paste := &PasteData{
		Content:      []byte("flags"),
		Burn:         true,
		Encrypted:    true,
		E2EEncrypted: true,
		Protected:    true,
		Lang:         "mermaid",
	}
	s.Save("flags1", paste, 0)

	got, err := s.Get("flags1")
	if err != nil || got == nil {
		t.Fatalf("Get: err=%v got=%v", err, got)
	}
	if !got.Burn || !got.Encrypted || !got.E2EEncrypted || !got.Protected {
		t.Errorf("flags not preserved: %+v", got)
	}
	if got.Lang != "mermaid" {
		t.Errorf("lang not preserved: %s", got.Lang)
	}
}

// ---------------------------------------------------------------------------
// Storage: Stats
// ---------------------------------------------------------------------------

func TestStorageStats(t *testing.T) {
	s := newTestStorage(t)
	s.Save("st1", &PasteData{Content: []byte("a"), Burn: true}, 0)
	s.Save("st2", &PasteData{Content: []byte("b")}, time.Hour)

	st := s.Stats()
	if st.Backend != "sqlite" {
		t.Errorf("unexpected backend: %s", st.Backend)
	}
	if st.Total < 2 {
		t.Errorf("expected at least 2 total, got %d", st.Total)
	}
	if st.BurnOnRead < 1 {
		t.Errorf("expected at least 1 burn-on-read, got %d", st.BurnOnRead)
	}
	if st.Expiring < 1 {
		t.Errorf("expected at least 1 expiring, got %d", st.Expiring)
	}
}

// ---------------------------------------------------------------------------
// Storage: GetAndDelete on expired paste returns nil
// ---------------------------------------------------------------------------

func TestStorageGetAndDeleteExpired(t *testing.T) {
	s := newTestStorage(t)
	s.Save("exp2", &PasteData{Content: []byte("temp"), Burn: true}, 1*time.Second)
	time.Sleep(2 * time.Second)

	got, err := s.GetAndDelete("exp2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for expired paste")
	}
}

// ---------------------------------------------------------------------------
// Crypto
// ---------------------------------------------------------------------------

func TestCryptoNewWithEmptyKey(t *testing.T) {
	_, err := newCrypto("")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestCryptoDecryptTooShort(t *testing.T) {
	import_b64 := "cGFzdGViaW4tdGVzdC1rZXktMzJieXRlcyEhISEhISE="
	c, err := newCrypto(import_b64)
	if err != nil {
		t.Fatalf("newCrypto: %v", err)
	}
	_, err = c.Decrypt([]byte("short"))
	if err == nil {
		t.Fatal("expected error for too-short ciphertext")
	}
}

// ---------------------------------------------------------------------------
// middleware: realIP
// ---------------------------------------------------------------------------

func TestRealIP_NoProxy(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	req.Header.Set("X-Forwarded-For", "9.9.9.9")

	ip := realIP(req, nil)
	if ip != "1.2.3.4:5678" {
		t.Errorf("expected RemoteAddr, got %q", ip)
	}
}

func TestRealIP_TrustedProxy(t *testing.T) {
	_, net32, _ := net.ParseCIDR("127.0.0.1/32")
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "5.6.7.8")

	ip := realIP(req, net32)
	if ip != "5.6.7.8" {
		t.Errorf("expected XFF address, got %q", ip)
	}
}

func TestRealIP_MultipleXFF(t *testing.T) {
	_, net32, _ := net.ParseCIDR("127.0.0.1/32")
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")

	ip := realIP(req, net32)
	if ip != "10.0.0.1" {
		t.Errorf("expected left-most XFF, got %q", ip)
	}
}

func TestRealIP_UntrustedProxy(t *testing.T) {
	_, net32, _ := net.ParseCIDR("10.0.0.0/8")
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:999" // not in trusted range
	req.Header.Set("X-Forwarded-For", "9.9.9.9")

	ip := realIP(req, net32)
	if ip != "1.2.3.4:999" {
		t.Errorf("expected RemoteAddr for untrusted peer, got %q", ip)
	}
}

// ---------------------------------------------------------------------------
// peerIP
// ---------------------------------------------------------------------------

func TestPeerIP(t *testing.T) {
	ip := peerIP("192.168.1.1:1234")
	if ip == nil || ip.String() != "192.168.1.1" {
		t.Errorf("unexpected peerIP: %v", ip)
	}
}

func TestPeerIPNoPort(t *testing.T) {
	ip := peerIP("192.168.1.1")
	if ip == nil || ip.String() != "192.168.1.1" {
		t.Errorf("unexpected peerIP: %v", ip)
	}
}

// ---------------------------------------------------------------------------
// boolToInt / intToBool
// ---------------------------------------------------------------------------

func TestBoolToInt(t *testing.T) {
	if boolToInt(true) != 1 {
		t.Error("boolToInt(true) should be 1")
	}
	if boolToInt(false) != 0 {
		t.Error("boolToInt(false) should be 0")
	}
}

func TestIntToBool(t *testing.T) {
	if !intToBool(1) {
		t.Error("intToBool(1) should be true")
	}
	if intToBool(0) {
		t.Error("intToBool(0) should be false")
	}
}

// ---------------------------------------------------------------------------
// toJSON
// ---------------------------------------------------------------------------

func TestToJSON(t *testing.T) {
	out := toJSON([]string{"a", "b"})
	if string(out) != `["a","b"]` {
		t.Errorf("unexpected toJSON output: %s", out)
	}
}

func TestToJSONFallback(t *testing.T) {
	out := toJSON(make(chan int))
	if string(out) != "[]" {
		t.Errorf("expected fallback [], got %s", out)
	}
}