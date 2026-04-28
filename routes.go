package main

import (
	"encoding/json"
	"html/template"
	"io"
	"os"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	gonanoid "github.com/matoous/go-nanoid/v2"

	"github.com/GAS85/ownPastebin/plugins"
)

// App holds all shared dependencies for handlers.
type App struct {
	cfg       *Settings
	storage   Storage
	crypto    *Crypto // nil if encryption disabled
	tmpl      *template.Template
	plugins   *plugins.Manager
	uploadSem chan struct{}
	limiter   *ipRateLimiter
}

// TemplateData is passed to index.html for every render.
type TemplateData struct {
	IsEditable    bool
	IsCreated     bool
	IsBurned      bool
	IsBurn        bool       // true = this paste is configured as burn-on-read
	IsError       bool
	IsEncrypted   bool
	IsClone       bool
	IsProtected   bool       // true = DELETE is blocked; hides/disables delete button in UI
	PastebinCode  string
	PastebinID    string
	PastebinCls   string
	Version       string
	ExpireAt      *time.Time // nil = never expires
	CSSImports    []string   // plugin CSS — loaded before custom.css
	TailCSSImports []string  // loaded last — custom.css always wins the cascade
	JSImports     []string
	JSInits       []string
	ExpiryTimes   []ExpiryOption
	URIPrefix     string
	DefaultExpiry string
	DefaultBurn   bool

	// Flash / redirect params (mirroring Python ?level=&msg=&glyph=&url=)
	Level    string
	Msg      string
	Glyph    string
	FlashURL string
}

type ExpiryOption struct {
	Label string
	Value string
}

var defaultExpiryTimes = []ExpiryOption{
	{"Never", "0"},
	{"5 min", "300"},
	{"10 min", "600"},
	{"1 hour", "3600"},
	{"1 day", "86400"},
	{"1 week", "604800"},
	{"1 month", "2592000"},
	{"1 year", "31536000"},
}

func (a *App) baseData(r *http.Request) TemplateData {
	// New-paste page: no language known yet — exclude conditional plugins (Mermaid).
	css, js, inits := a.plugins.BuildFor("")
	return TemplateData{
		Version:        os.Getenv("VERSION"),
		URIPrefix:      a.cfg.PathPrefix,
		CSSImports:     css,
		TailCSSImports: a.plugins.TailCSSImports(),
		JSImports:      js,
		JSInits:        inits,
		ExpiryTimes:    defaultExpiryTimes,
		DefaultExpiry:  strconv.FormatInt(int64(a.cfg.DefaultTTL.Seconds()), 10),
		DefaultBurn:    a.cfg.DefaultBurn,
		Level:          r.URL.Query().Get("level"),
		Msg:            r.URL.Query().Get("msg"),
		Glyph:          r.URL.Query().Get("glyph"),
		FlashURL:       r.URL.Query().Get("url"),
	}
}

func toJSON(v any) template.JS {
	b, err := json.Marshal(v)
	if err != nil {
		return template.JS("[]") // fallback to empty array
	}
	return template.JS(b)
}

// ---- routes -----------------------------------------------------------------

func (a *App) router() http.Handler {
	r := chi.NewRouter()

	// Access log — wraps every route including swagger and config.
	r.Use(a.accessLogMiddleware)
	// Per-IP rate limiting on all routes.
	r.Use(a.rateLimitMiddleware)

	// Paste content — must never be cached: content can be burned/deleted at
	// any moment, and serving a stale copy after deletion would be a data leak.
	r.With(noCacheMiddleware).Get("/raw/{id}", a.handleRaw)
	r.With(noCacheMiddleware).Get("/download/{id}", a.handleDownload)

	// Long-lived cacheable endpoints — safe to cache for 6 months.
	r.With(longCacheMiddleware).Get("/config", a.handleConfig)
	r.With(longCacheMiddleware).Get("/openapi.json", a.handleOpenAPISpec)
	r.With(longCacheMiddleware).Get("/swagger-ui", a.handleSwaggerUI)

	r.Get("/", a.handleNewPaste)
	r.Post("/", a.handleCreatePaste)

	// /{id} must be last — it is a catch-all wildcard.
	// Paste view shares the no-cache policy: burn-on-read pastes vanish after
	// the first fetch and must not be replayed from any cache.
	r.With(noCacheMiddleware).Get("/{id}", a.handleView)
	r.Delete("/{id}", a.handleDelete)

	return r
}

// GET /
func (a *App) handleNewPaste(w http.ResponseWriter, r *http.Request) {
	d := a.baseData(r)
	d.IsEditable = true
	a.render(w, d, http.StatusOK)
}

// POST /
func (a *App) handleCreatePaste(w http.ResponseWriter, r *http.Request) {
	// Read body first, before acquiring a semaphore slot.
	// This prevents a slow client upload from holding a slot for the entire
	// transfer duration and blocking other concurrent uploads unnecessarily.
	if r.ContentLength > a.cfg.MaxPasteSize {
		http.Error(w, "paste too large", http.StatusRequestEntityTooLarge)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, a.cfg.MaxPasteSize))
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	if len(raw) == 0 {
		http.Error(w, "empty paste", http.StatusBadRequest)
		return
	}

	// Acquire a slot only for the CPU/memory-intensive work (encrypt + save).
	// If the channel is full, reject immediately.
	select {
	case a.uploadSem <- struct{}{}:
		defer func() {
			<-a.uploadSem
			// When the last slot drains (burst is over), hint the GC to reclaim
			// heap. FreeOSMemory is intentionally NOT called here — it forces a
			// stop-the-world double-GC plus madvise on every free span, which can
			// pause the server for tens of milliseconds. GOMEMLIMIT is the right
			// knob for controlling RSS. A single GC() is enough to let the runtime
			// schedule memory return at its own pace.
			if len(a.uploadSem) == 0 {
				runtime.GC()
			}
		}()
	default:
		http.Error(w, "server busy, try again", http.StatusServiceUnavailable)
		return
	}

	// Apply server-side encryption if enabled.
	// Nil out raw immediately after use so the original plaintext slice is eligible for GC while content (the encrypted copy) is still live.
	// Without this, both slices are held in memory until the function returns, doubling peak memory per concurrent upload.
	content := raw
	encryptedFlag := false
	if a.cfg.ServerSideEncryptionEnabled && a.crypto != nil {
		encrypted, err := a.crypto.Encrypt(raw)
		// release the plaintext — don't hold both copies simultaneously
		raw = nil
		if err != nil {
			slog.Error("encrypt error", "err", err)
			http.Error(w, "encryption error", http.StatusInternalServerError)
			return
		}
		content = encrypted
		encryptedFlag = true
	}

	q := r.URL.Query()

	// use default Burn values
	burnParam := q.Get("burn")

	var burn bool
	if burnParam == "" {
		burn = a.cfg.DefaultBurn
	} else {
		burn = burnParam == "true"
	}

	var ttl time.Duration
	if s := q.Get("ttl"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 0 {
			http.Error(w, "invalid ttl", http.StatusBadRequest)
			return
		}
		ttl = time.Duration(n) * time.Second
	}
	ttl = a.cfg.resolveTTL(ttl)

	lang := q.Get("lang")
	if lang == "" {
		lang = "text"
	}

	// Only honour ?protected=true when the feature is enabled in config.
	protected := a.cfg.ProtectedPasteEnabled && q.Get("protected") == "true"

	paste := &PasteData{
		Content:      content,
		Burn:         burn,
		Encrypted:    encryptedFlag,
		E2EEncrypted: q.Get("encrypted") == "true",
		Protected:    protected,
		Lang:         lang,
	}

	// Generate a unique slug and save. Retry on the extremely unlikely collision.
	// INSERT OR IGNORE is used in storage so a conflict returns ErrSlugConflict
	// rather than silently overwriting an existing paste.
	const maxSlugRetries = 3
	var id string
	for i := 0; i < maxSlugRetries; i++ {
		id, err = gonanoid.New(a.cfg.SlugLen)
		if err != nil {
			http.Error(w, "id generation failed", http.StatusInternalServerError)
			return
		}
		saveErr := a.storage.Save(id, paste, ttl)
		if saveErr == nil {
			break
		}
		if saveErr == ErrSlugConflict && i < maxSlugRetries-1 {
			slog.Warn("slug collision, retrying", "id", id, "attempt", i+1)
			continue
		}
		slog.Error("storage save error", "err", saveErr)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	// cfg.BaseURL already contains the full base including any path prefix,
	// e.g. "http://localhost:8080/pastebin" — so no extra joining is needed.
	url := a.cfg.BaseURL + "/" + id
	w.Header().Set("Location", url)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(`{"url":"` + url + `","id":"` + id + `","lang":"` + lang + `","protected":` + strconv.FormatBool(protected) + `}`))
}

// GET /config
func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	maxTTL := int64(0)
	if a.cfg.MaxTTL > 0 {
		maxTTL = int64(a.cfg.MaxTTL.Seconds())
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{` +
		`"max_ttl":` + strconv.FormatInt(maxTTL, 10) + `,` +
		`"default_ttl":` + strconv.FormatInt(int64(a.cfg.DefaultTTL.Seconds()), 10) + `,` +
		`"max_paste_size":` + strconv.FormatInt(a.cfg.MaxPasteSize, 10) + `,` +
		`"server_side_encryption":` + strconv.FormatBool(a.cfg.ServerSideEncryptionEnabled) + `,` +
		`"protected_paste_enabled":` + strconv.FormatBool(a.cfg.ProtectedPasteEnabled) +
		`}`))
}

// GET /raw/{id}
func (a *App) handleRaw(w http.ResponseWriter, r *http.Request) {
	paste, err := a.fetchPaste(chi.URLParam(r, "id"))
	if err != nil || paste == nil {
		http.NotFound(w, r)
		return
	}
	content, err := a.decryptIfNeeded(paste)
	if err != nil {
		http.Error(w, "decryption error", http.StatusInternalServerError)
		return
	}
	if utf8.Valid(content) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(content)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename="+chi.URLParam(r, "id"))
		w.Write(content)
	}
}

// GET /download/{id}
func (a *App) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	paste, err := a.fetchPaste(id)
	if err != nil || paste == nil {
		http.NotFound(w, r)
		return
	}
	content, err := a.decryptIfNeeded(paste)
	if err != nil {
		http.Error(w, "decryption error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+id)
	w.Write(content)
}

// GET /{id}
func (a *App) handleView(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	paste, err := a.fetchPaste(id)
	if err != nil || paste == nil {
		d := a.baseData(r)
		d.IsError = true
		a.render(w, d, http.StatusNotFound)
		return
	}
	content, err := a.decryptIfNeeded(paste)
	if err != nil {
		http.Error(w, "decryption error", http.StatusInternalServerError)
		return
	}
	text := "[binary data]"
	if utf8.Valid(content) {
		text = string(content)
	}

	// Build CSS/JS imports for the actual language of this paste.
	// Conditional plugins (e.g. Mermaid) are included only when their language
	// is active, so mermaid.min.js is never loaded for non-mermaid pastes.
	css, js, inits := a.plugins.BuildFor(paste.Lang)

	d := a.baseData(r)
	d.CSSImports     = css
	d.JSImports      = js
	d.JSInits        = inits
	d.IsCreated      = true
	d.IsBurned       = paste.Burn
	d.IsBurn         = paste.Burn
	d.IsEncrypted    = paste.E2EEncrypted
	d.IsProtected    = paste.Protected
	d.PastebinCode   = text
	d.PastebinID     = id
	d.PastebinCls    = "language-" + paste.Lang
	d.ExpireAt       = paste.ExpireAt
	a.render(w, d, http.StatusOK)
}

// DELETE /{id}
func (a *App) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Guard: when the protected-paste feature is active, peek at metadata before
	// deleting. Protected pastes return 403 — all other features (TTL, burn) are
	// unaffected. The check is skipped entirely when the feature is disabled so
	// there is zero overhead for deployments that never use it.
	if a.cfg.ProtectedPasteEnabled {
		meta, err := a.storage.PeekMeta(id)
		if err != nil {
			http.Error(w, "delete failed", http.StatusInternalServerError)
			return
		}
		if meta != nil && meta.Protected {
			http.Error(w, "paste is protected", http.StatusForbidden)
			return
		}
	}
	if err := a.storage.Delete(id); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"url":"/?level=info&msg=Paste deleted successfully"}`))
}

// ---- helpers ----------------------------------------------------------------

// fetchPaste handles burn-on-read: for burn pastes we go directly to GetAndDelete so the content is loaded exactly once and atomically removed.
// The previous pattern (Get - check Burn - GetAndDelete) loaded the full content blob twice, doubling memory usage for every burn-paste read.
func (a *App) fetchPaste(id string) (*PasteData, error) {
	// Use a metadata-only peek to check the burn flag without loading the content blob. If the storage backend supports PeekMeta, we avoid the double-load entirely. Otherwise fall back to the single Get path.
	if meta, err := a.storage.PeekMeta(id); err == nil && meta != nil {
		if meta.Burn {
			return a.storage.GetAndDelete(id)
		}
		return a.storage.Get(id)
	}
	paste, err := a.storage.Get(id)
	if err != nil || paste == nil {
		return nil, err
	}
	if paste.Burn {
		return a.storage.GetAndDelete(id)
	}
	return paste, nil
}

// decryptIfNeeded returns the plaintext content if server-side encryption is enabled, otherwise returns the stored content as-is.
func (a *App) decryptIfNeeded(paste *PasteData) ([]byte, error) {
	if paste.Encrypted && a.crypto != nil {
		return a.crypto.Decrypt(paste.Content)
	}
	return paste.Content, nil
}

func (a *App) render(w http.ResponseWriter, d TemplateData, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := a.tmpl.Execute(w, d); err != nil {
		slog.Error("template render error", "err", err)
	}
}
