package main

import (
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

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

func main() {
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

	// Forward PathPrefix to the plugins
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
	}
	tmpl, err := template.New("index.html").Funcs(funcMap).ParseFS(templateFS, "templates/index.html")
	if err != nil {
		slog.Error("template parse failed", "err", err)
		os.Exit(1)
	}

	// ── App ───────────────────────────────────────────────────────────────────
	app := &App{
		cfg:     cfg,
		storage: store,
		crypto:  cry,
		tmpl:    tmpl,
		plugins: mgr,
	}

	// ── Static file server ────────────────────────────────────────────────────
	// Serve the embedded static/ directory.
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		slog.Error("static fs setup failed", "err", err)
		os.Exit(1)
	}
	staticHandler := http.FileServer(http.FS(staticSub))

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
		slog.Info("server starting with TLS", "addr", addr, "cert", tlsCert, "key", tlsKey)
		if err := http.ListenAndServeTLS(addr, tlsCert, tlsKey, finalHandler); err != nil {
			slog.Error("server stopped", "err", err)
			os.Exit(1)
		}
	} else {
		slog.Info("server starting", "addr", addr)
		if err := http.ListenAndServe(addr, finalHandler); err != nil {
			slog.Error("server stopped", "err", err)
			os.Exit(1)
		}
	}
}