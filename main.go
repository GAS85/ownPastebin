package main

import (
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/time/rate"

	"github.com/GAS85/ownPastebin/plugins"
)

//go:embed templates/index.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// prismFS and mermaidFS are sub-trees of staticFS exposed to the plugin system.
// Prism files (prism.js, prism.css) live under static/ and are already covered
// by staticFS — we pass a sub-FS so the plugin can declare its own static routes.
var prismFS, _ = fs.Sub(staticFS, "static")

var Version string

// reapZombies runs for the lifetime of the process and reaps any zombie
// children whenever SIGCHLD is delivered.
//
// Why this is needed
// ──────────────────
// In Alpine-based containers the Go binary often runs as PID 1.  PID 1 is
// special: the kernel never auto-reaps its direct children, so every process
// that exits while still having Go as its parent becomes a zombie until PID 1
// calls wait(2).  Go's runtime does not do this automatically.
//
// The immediate culprit is the Docker / Kubernetes health-check command, which
// on Alpine is typically:
//
//	wget -q -O /dev/null https://localhost:8080/config
//
// Busybox wget forks an ssl_client helper process for each TLS connection.
// When wget exits it does not always reap ssl_client synchronously, leaving
// it as a zombie child of PID 1 (this Go process).  One zombie appears per
// health-check cycle — hence the steady accumulation every ~5 minutes.
//
// The fix: listen for SIGCHLD and call waitpid(-1, WNOHANG) in a tight loop
// until there are no more children to reap.
func reapZombies() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGCHLD)
	for range ch {
		for {
			var ws syscall.WaitStatus
			pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
			if pid <= 0 || err != nil {
				// No more children ready to be reaped right now.
				break
			}
			slog.Debug("reaped zombie child", "pid", pid, "status", ws)
		}
	}
}

func main() {
	// ── Zombie reaper ─────────────────────────────────────────────────────────
	// Must be started before any other goroutines so that SIGCHLD from health-
	// check helpers (ssl_client) is never missed.  Safe to run even when the
	// process is not PID 1: waitpid(-1, WNOHANG) on a process with no children
	// returns ECHILD immediately and the loop exits cleanly.
	go reapZombies()

	// ── Logger ────────────────────────────────────────────────────────────────
	// Must be first so all subsequent log calls use the configured handler.
	initLogger()

	cfg := loadSettings()

	// ── Crypto ────────────────────────────────────────────────────────────────
	var cry *Crypto
	if cfg.ServerSideEncryptionEnabled {
		var err error
		cry, err = newCrypto(cfg.ServerSideEncryptionKey)
		if err != nil {
			slog.Error("crypto init failed", "err", err)
			os.Exit(1)
		}
		slog.Info("AES-256-GCM server-side encryption enabled")
	}

	// ── Storage ───────────────────────────────────────────────────────────────
	store := newStorage(cfg)
	defer store.Close()

	// ── Plugins ───────────────────────────────────────────────────────────────
	activePlugins := []plugins.Plugin{
		&plugins.PrismPlugin{EmbeddedFS: prismFS},
		&plugins.MermaidPlugin{},
	}

	// Forward PathPrefix to the plugins via the Base struct.
	mgr := plugins.NewManager(plugins.DefaultBase(cfg.PathPrefix), activePlugins)

	// ── Templates ─────────────────────────────────────────────────────────────
	funcMap := template.FuncMap{
		// {{not .Bool}} — used for {{if not .IsBurned}} etc.
		"not": func(b bool) bool { return !b },
		// {{safeJS .}} — marks a string as safe for inline <script> injection
		"safeJS": func(s string) template.JS { return template.JS(s) },
		// {{formatTime .ExpireAt}} — formats *time.Time for display in the template.
		"formatTime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02 15:04:05 UTC")
		},
		// {{toJSON .JSInits}} — serialises a Go value to a JSON literal safe for
		// embedding inside <script type="application/json">. Defined here so the
		// template parser can resolve it at parse time; the implementation lives
		// in routes.go as toJSON().
		"toJSON": toJSON,
	}
	tmpl, err := template.New("index.html").Funcs(funcMap).ParseFS(templateFS, "templates/index.html")
	if err != nil {
		slog.Error("template parse failed", "err", err)
		os.Exit(1)
	}

	// ── Rate limiter ──────────────────────────────────────────────────────────
	// Both parameters are derived from MaxParallelUploads so they stay correct
	// when the operator changes PASTEBIN_MAX_PARALLEL_UPLOADS.
	//
	// Burst = MaxParallelUploads
	//   A single IP must be able to fire exactly MaxParallelUploads concurrent
	//   POSTs without being rate-limited, because the upload semaphore — not the
	//   rate limiter — is the intended binding constraint on upload concurrency.
	//   If burst < MaxParallelUploads the rate limiter would reject legitimate
	//   concurrent uploads before the semaphore even gets a chance to throttle.
	//
	// Sustained rate = MaxParallelUploads / 2  req/s
	//   Assumes each upload occupies the semaphore for ~2 s on average (network
	//   read + encrypt + SQLite write for a mid-sized paste).  This allows one
	//   IP to keep all slots busy continuously without triggering the limiter,
	//   while still blocking a scripted flood of tiny, fast requests.
	//   Floor of 1 req/s prevents a zero-rate if MaxParallelUploads is ever 1.
	uploadBurst := cfg.MaxParallelUploads
	uploadRate := rate.Limit(cfg.MaxParallelUploads) / 2
	if uploadRate < 1 {
		uploadRate = 1
	}
	lim := newIPRateLimiter(uploadRate, uploadBurst, 5*time.Minute)
	slog.Debug("rate limiter configured",
		"rate_per_sec", uploadRate,
		"burst", uploadBurst,
		"derived_from", cfg.MaxParallelUploads,
	)
	defer lim.Close()

	// ── App ───────────────────────────────────────────────────────────────────
	app := &App{
		cfg:       cfg,
		storage:   store,
		crypto:    cry,
		tmpl:      tmpl,
		plugins:   mgr,
		uploadSem: make(chan struct{}, cfg.MaxParallelUploads),
		limiter:   lim,
	}

	// ── Static file server ────────────────────────────────────────────────────
	// Serve the embedded static/ directory.
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		slog.Error("static fs setup failed", "err", err)
		os.Exit(1)
	}
	staticHandler := longCacheMiddleware(http.FileServer(http.FS(staticSub)))

	mux := app.router()

	// staticPrefix is e.g. "/pastebin/static" or "/static".
	staticPrefix := cfg.PathPrefix + "/static/"

	// prefixHandler strips PathPrefix before handing off to the Chi router,
	// so all route definitions stay as plain "/" paths regardless of deployment.
	// Static files are handled before stripping so their full path is intact.
	prefixHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, staticPrefix) {
			http.StripPrefix(staticPrefix, staticHandler).ServeHTTP(w, r)
			return
		}
		// Strip the path prefix so Chi sees e.g. "/" instead of "/pastebin/"
		if cfg.PathPrefix != "" {
			http.StripPrefix(cfg.PathPrefix, mux).ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})

	finalHandler := prefixHandler

	// ── Server ────────────────────────────────────────────────────────────────
	host := os.Getenv("PASTEBIN_HOST") // reuse existing env var names for drop-in compat
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("PASTEBIN_PORT")
	if port == "" {
		port = "8080"
	}
	addr := host + ":" + port

	Version = os.Getenv("VERSION")
	// TLS support (mirrors entrypoint.sh PASTEBIN_TLS_KEY / PASTEBIN_TLS_CERT vars)
	tlsKey := os.Getenv("PASTEBIN_TLS_KEY")
	tlsCert := os.Getenv("PASTEBIN_TLS_CERT")

	if tlsKey != "" && tlsCert != "" {
		slog.Debug("server starting with TLS", "addr", addr, "cert", tlsCert, "key", tlsKey)
		if err := http.ListenAndServeTLS(addr, tlsCert, tlsKey, finalHandler); err != nil {
			slog.Error("server stopped", "err", err)
			os.Exit(1)
		}
	} else {
		slog.Debug("server starting", "addr", addr)
		if err := http.ListenAndServe(addr, finalHandler); err != nil {
			slog.Error("server stopped", "err", err)
			os.Exit(1)
		}
	}
}
