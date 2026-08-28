package main

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
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

func buildTemplateFuncMap() template.FuncMap {
	return template.FuncMap{
		"lower": strings.ToLower,
		"replace": strings.ReplaceAll,
		"split": strings.Split,
		"not": func(b bool) bool { return !b },
		"safeJS": func(s string) template.JS { return template.JS(s) },
		"formatTime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02 15:04:05 UTC")
		},
		"toJSON": toJSON,
	}
}

func computeLimiterParams(maxParallelUploads int) (rate.Limit, int) {
	uploadBurst := maxParallelUploads
	uploadRate := rate.Limit(maxParallelUploads) / 2
	if uploadRate < 1 {
		uploadRate = 1
	}
	return uploadRate, uploadBurst
}

func newPrefixHandler(cfg *Settings, mux http.Handler, staticHandler http.Handler) http.Handler {
	staticPrefix := cfg.PathPrefix + "/static/"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, staticPrefix) {
			http.StripPrefix(staticPrefix, staticHandler).ServeHTTP(w, r)
			return
		}
		if cfg.PathPrefix != "" {
			http.StripPrefix(cfg.PathPrefix, mux).ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func listenAddress() string {
	host := os.Getenv("PASTEBIN_HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("PASTEBIN_PORT")
	if port == "" {
		port = "8080"
	}
	return host + ":" + port
}

func newApp(cfg *Settings, store Storage, cry *Crypto, tmpl *template.Template, mgr *plugins.Manager, lim *ipRateLimiter) *App {
	return &App{
		cfg:       cfg,
		storage:   store,
		crypto:    cry,
		tmpl:      tmpl,
		plugins:   mgr,
		uploadSem: make(chan struct{}, cfg.MaxParallelUploads),
		limiter:   lim,
	}
}

func parseIndexTemplate() (*template.Template, error) {
	return template.New("index.html").Funcs(buildTemplateFuncMap()).ParseFS(templateFS, "templates/index.html")
}

func newPluginManager(cfg *Settings) *plugins.Manager {
	activePlugins := []plugins.Plugin{
		&plugins.PrismPlugin{EmbeddedFS: prismFS},
		&plugins.MermaidPlugin{},
	}
	return plugins.NewManager(plugins.DefaultBase(cfg.PathPrefix), activePlugins)
}

var listenAndServe = http.ListenAndServe
var listenAndServeTLS = http.ListenAndServeTLS

func buildFinalHandler(cfg *Settings, app *App) (http.Handler, error) {
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	staticHandler := longCacheMiddleware(http.FileServer(http.FS(staticSub)))
	mux := app.router()
	return newPrefixHandler(cfg, mux, staticHandler), nil
}

func run() error {
	cfg := loadSettings()

	var cry *Crypto
	if cfg.ServerSideEncryptionEnabled {
		var err error
		cry, err = newCrypto(cfg.ServerSideEncryptionKey)
		if err != nil {
			return fmt.Errorf("crypto init failed: %w", err)
		}
		slog.Info("AES-256-GCM server-side encryption enabled")
	}

	store := newStorage(cfg)
	defer store.Close()

	mgr := newPluginManager(cfg)

	tmpl, err := parseIndexTemplate()
	if err != nil {
		return fmt.Errorf("template parse failed: %w", err)
	}

	uploadRate, uploadBurst := computeLimiterParams(cfg.MaxParallelUploads)
	lim := newIPRateLimiter(uploadRate, uploadBurst, 5*time.Minute)
	defer lim.Close()

	app := newApp(cfg, store, cry, tmpl, mgr, lim)

	finalHandler, err := buildFinalHandler(cfg, app)
	if err != nil {
		return fmt.Errorf("static fs setup failed: %w", err)
	}

	addr := listenAddress()

	Version = os.Getenv("VERSION")
	tlsKey := os.Getenv("PASTEBIN_TLS_KEY")
	tlsCert := os.Getenv("PASTEBIN_TLS_CERT")
	if tlsKey != "" && tlsCert != "" {
		slog.Debug("server starting with TLS", "addr", addr, "cert", tlsCert, "key", tlsKey)
		return listenAndServeTLS(addr, tlsCert, tlsKey, finalHandler)
	}

	slog.Debug("server starting", "addr", addr)
	return listenAndServe(addr, finalHandler)
}

func main() {
	go reapZombies()
	initLogger()
	if err := run(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
