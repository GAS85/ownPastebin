package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/GAS85/ownPastebin/plugins"
)

func TestBuildTemplateFuncMap(t *testing.T) {
	funcs := buildTemplateFuncMap()

	lower := funcs["lower"].(func(string) string)
	if got := lower("HELLO"); got != "hello" {
		t.Fatalf("expected lower to convert case, got %q", got)
	}

	replace := funcs["replace"].(func(string, string, string) string)
	if got := replace("a b", " ", "_"); got != "a_b" {
		t.Fatalf("expected replace to substitute spaces, got %q", got)
	}

	split := funcs["split"].(func(string, string) []string)
	if got := split("a b", " "); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("expected split to separate words, got %v", got)
	}

	not := funcs["not"].(func(bool) bool)
	if got := not(true); got {
		t.Fatal("expected not(true) to be false")
	}

	safeJS := funcs["safeJS"].(func(string) template.JS)
	if got := safeJS("<x>"); got != template.JS("<x>") {
		t.Fatalf("expected safeJS to wrap string, got %v", got)
	}

	formatTime := funcs["formatTime"].(func(*time.Time) string)
	if got := formatTime(nil); got != "" {
		t.Fatalf("expected empty for nil time, got %q", got)
	}
	timeVal := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	if got := formatTime(&timeVal); got != "2025-12-31 23:59:59 UTC" {
		t.Fatalf("unexpected formatted time: %q", got)
	}

	toJSON := funcs["toJSON"].(func(any) template.JS)
	if got := string(toJSON(map[string]string{"x": "y"})); !strings.Contains(got, `"x":"y"`) {
		t.Fatalf("unexpected JSON output: %v", got)
	}
}

func TestComputeLimiterParams(t *testing.T) {
	if rate, burst := computeLimiterParams(1); rate != 1 || burst != 1 {
		t.Fatalf("expected rate=1 burst=1, got %v %v", rate, burst)
	}
	if rate, burst := computeLimiterParams(7); rate != 3.5 || burst != 7 {
		t.Fatalf("expected rate=3.5 burst=7, got %v %v", rate, burst)
	}
}

func TestNewPrefixHandler(t *testing.T) {
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("app"))
	})
	staticHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("static"))
	})
	cfg := &Settings{PathPrefix: "/pastebin"}
	handler := newPrefixHandler(cfg, mux, staticHandler)

	req := httptest.NewRequest(http.MethodGet, "/pastebin/static/index.html", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Body.String() != "static" {
		t.Fatalf("expected static handler, got %q", res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/pastebin/hello", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Body.String() != "app" {
		t.Fatalf("expected app handler, got %q", res.Body.String())
	}
}

func TestListenAddress(t *testing.T) {
	t.Setenv("PASTEBIN_HOST", "127.0.0.1")
	t.Setenv("PASTEBIN_PORT", "1234")
	if got := listenAddress(); got != "127.0.0.1:1234" {
		t.Fatalf("unexpected listen address: %q", got)
	}
}

func TestListenAddressDefaults(t *testing.T) {
	if got := listenAddress(); got != "0.0.0.0:8080" {
		t.Fatalf("unexpected default listen address: %q", got)
	}
}

func TestRunWithTLS(t *testing.T) {
	oldListenTLS := listenAndServeTLS
	oldListen := listenAndServe
	listenAndServeTLS = func(addr, cert, key string, handler http.Handler) error {
		if addr != "127.0.0.1:0" || cert != "missing.crt" || key != "missing.key" {
			t.Fatalf("unexpected TLS args: %s %s %s", addr, cert, key)
		}
		return nil
	}
	listenAndServe = func(addr string, handler http.Handler) error {
		t.Fatal("expected TLS branch, got non-TLS server")
		return nil
	}
	defer func() { listenAndServeTLS = oldListenTLS; listenAndServe = oldListen }()

	dir := t.TempDir()
	t.Setenv("PASTEBIN_HOST", "127.0.0.1")
	t.Setenv("PASTEBIN_PORT", "0")
	t.Setenv("PASTEBIN_SQLITE_PATH", dir+"/pastebin.db")
	t.Setenv("PASTEBIN_TLS_KEY", "missing.key")
	t.Setenv("PASTEBIN_TLS_CERT", "missing.crt")

	if err := run(); err != nil {
		t.Fatalf("run failed: %v", err)
	}
}

func TestRunWithoutTLS(t *testing.T) {
	oldListenTLS := listenAndServeTLS
	oldListen := listenAndServe
	listenAndServeTLS = func(addr, cert, key string, handler http.Handler) error {
		t.Fatal("expected non-TLS branch, got TLS server")
		return nil
	}
	listenAndServe = func(addr string, handler http.Handler) error {
		if addr != "127.0.0.1:0" {
			t.Fatalf("unexpected non-TLS addr: %s", addr)
		}
		return nil
	}
	defer func() { listenAndServeTLS = oldListenTLS; listenAndServe = oldListen }()

	dir := t.TempDir()
	t.Setenv("PASTEBIN_HOST", "127.0.0.1")
	t.Setenv("PASTEBIN_PORT", "0")
	t.Setenv("PASTEBIN_SQLITE_PATH", dir+"/pastebin.db")
	if err := run(); err != nil {
		t.Fatalf("run failed: %v", err)
	}
}

func TestNewApp(t *testing.T) {
	cfg := &Settings{MaxParallelUploads: 2}
	store := &stubStorage{get: func(string) (*PasteData, error) { return nil, nil }, peekMeta: func(string) (*PasteData, error) { return nil, nil }, getDel: func(string) (*PasteData, error) { return nil, nil }}
	lim := newIPRateLimiter(1, 1, 5*time.Second)
	mgr := plugins.NewManager(plugins.DefaultBase(""), nil)
	tmpl := template.New("app")
	app := newApp(cfg, store, nil, tmpl, mgr, lim)

	if app.cfg != cfg || app.storage != store || app.tmpl != tmpl || app.limiter != lim {
		t.Fatal("newApp did not populate fields correctly")
	}
	if cap(app.uploadSem) != 2 {
		t.Fatalf("expected uploadSem cap 2, got %d", cap(app.uploadSem))
	}
}

func TestParseIndexTemplate(t *testing.T) {
	if _, err := parseIndexTemplate(); err != nil {
		t.Fatalf("parseIndexTemplate failed: %v", err)
	}
}

func TestNewPluginManager(t *testing.T) {
	mgr := newPluginManager(&Settings{PathPrefix: "/pastebin"})
	if mgr == nil {
		t.Fatal("expected plugin manager")
	}
}

func TestBuildFinalHandler(t *testing.T) {
	cfg := &Settings{PathPrefix: "/pastebin", MaxParallelUploads: 1}
	store := &stubStorage{get: func(string) (*PasteData, error) { return nil, nil }, peekMeta: func(string) (*PasteData, error) { return nil, nil }, getDel: func(string) (*PasteData, error) { return nil, nil }}
	lim := newIPRateLimiter(1, 1, 5*time.Second)
	mgr := newPluginManager(cfg)
	tmpl, err := parseIndexTemplate()
	if err != nil {
		t.Fatalf("parseIndexTemplate: %v", err)
	}
	app := newApp(cfg, store, nil, tmpl, mgr, lim)

	handler, err := buildFinalHandler(cfg, app)
	if err != nil {
		t.Fatalf("buildFinalHandler failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/pastebin/static/prism.js", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for static file, got %d", res.Code)
	}
	if res.Body.Len() == 0 {
		t.Fatal("expected static file response body")
	}
}

func TestMainWithTLSExit(t *testing.T) {
	if os.Getenv("RUN_MAIN") == "1" {
		main()
		return
	}

	dir := t.TempDir()
	path := dir + "/pastebin.db"

	cmd := exec.Command(os.Args[0], "-test.run=TestMainWithTLSExit")
	cmd.Env = append(os.Environ(),
		"RUN_MAIN=1",
		"PASTEBIN_HOST=127.0.0.1",
		"PASTEBIN_PORT=0",
		"PASTEBIN_SQLITE_PATH="+path,
		"PASTEBIN_TLS_KEY=missing.key",
		"PASTEBIN_TLS_CERT=missing.crt",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected subprocess to exit with error, got output: %s", out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 0 {
			t.Fatalf("expected non-zero exit code")
		}
		return
	}
	t.Fatalf("expected exec.ExitError, got %T: %v", err, err)
}

func TestMainStartsServer(t *testing.T) {
	if os.Getenv("RUN_MAIN") == "1" {
		main()
		return
	}

	dir := t.TempDir()
	path := dir + "/pastebin.db"

	cmd := exec.Command(os.Args[0], "-test.run=TestMainStartsServer")
	cmd.Env = append(os.Environ(),
		"RUN_MAIN=1",
		"PASTEBIN_HOST=127.0.0.1",
		"PASTEBIN_PORT=0",
		"PASTEBIN_SQLITE_PATH="+path,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start subprocess: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill subprocess: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected subprocess to exit with error after kill")
	}
}
