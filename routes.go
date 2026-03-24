package main

import (
	"encoding/base64"
	"html/template"
	"io"
	"os"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	gonanoid "github.com/matoous/go-nanoid/v2"

	"github.com/GAS85/ownPastebin/plugins"
)

// App holds all shared dependencies for handlers.
type App struct {
	cfg     *Settings
	storage Storage
	crypto  *Crypto // nil if encryption disabled
	tmpl    *template.Template
	plugins *plugins.Manager
}

// TemplateData is passed to index.html for every render.
type TemplateData struct {
	IsEditable   bool
	IsCreated    bool
	IsBurned     bool
	IsBurn       bool       // true = this paste is configured as burn-on-read
	IsError      bool
	IsEncrypted  bool
	IsClone      bool
	PastebinCode string
	PastebinID   string
	PastebinCls  string
	Version      string
	ExpireAt     *time.Time // nil = never expires
	CSSImports   []string
	JSImports    []string
	JSInits      []string
	ExpiryTimes  []ExpiryOption
	URIPrefix    string

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
	{"1 month", "18144000"},
	{"1 year", "220752000"},
}

func (a *App) baseData(r *http.Request) TemplateData {
	return TemplateData{
		Version:     os.Getenv("VERSION"),
		URIPrefix:   a.cfg.PathPrefix,
		CSSImports:  a.plugins.CSSImports,
		JSImports:   a.plugins.JSImports,
		JSInits:     a.plugins.JSInits,
		ExpiryTimes: defaultExpiryTimes,
		Level:       r.URL.Query().Get("level"),
		Msg:         r.URL.Query().Get("msg"),
		Glyph:       r.URL.Query().Get("glyph"),
		FlashURL:    r.URL.Query().Get("url"),
	}
}

// ---- encode / decode --------------------------------------------------------

func (a *App) encodeForStorage(raw []byte) (string, error) {
	if a.cfg.ServerSideEncryptionEnabled && a.crypto != nil {
		return a.crypto.encrypt(raw)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func (a *App) decodeFromStorage(stored string) ([]byte, error) {
	if a.cfg.ServerSideEncryptionEnabled && a.crypto != nil {
		return a.crypto.decrypt(stored)
	}
	return base64.StdEncoding.DecodeString(stored)
}

// ---- routes -----------------------------------------------------------------

func (a *App) router() http.Handler {
	r := chi.NewRouter()

	// Access log — wraps every route including swagger and config.
	r.Use(accessLogMiddleware)

	r.Get("/", a.handleNewPaste)
	r.Post("/", a.handleCreatePaste)
	r.Get("/config", a.handleConfig)
	r.Get("/raw/{id}", a.handleRaw)
	r.Get("/download/{id}", a.handleDownload)

	// API documentation
	r.Get("/openapi.json", a.handleOpenAPISpec)
	r.Get("/swagger-ui", a.handleSwaggerUI)

	// /{id} must be last — it is a catch-all wildcard.
	r.Get("/{id}", a.handleView)
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
	raw, err := io.ReadAll(io.LimitReader(r.Body, a.cfg.MaxPasteSize+1))
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	if int64(len(raw)) > a.cfg.MaxPasteSize {
		http.Error(w, "paste too large", http.StatusRequestEntityTooLarge)
		return
	}
	if len(raw) == 0 {
		http.Error(w, "empty paste", http.StatusBadRequest)
		return
	}

	content, err := a.encodeForStorage(raw)
	if err != nil {
		slog.Error("encrypt error", "err", err)
		http.Error(w, "encryption error", http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()

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

	id, err := gonanoid.New(a.cfg.SlugLen)
	if err != nil {
		http.Error(w, "id generation failed", http.StatusInternalServerError)
		return
	}

	lang := q.Get("lang")
	if lang == "" {
		lang = "text"
	}

	paste := &PasteData{
		Content:      content,
		Burn:         q.Get("burn") == "true",
		Encrypted:    a.cfg.ServerSideEncryptionEnabled,
		E2EEncrypted: q.Get("encrypted") == "true",
		Lang:         lang,
	}

	if err := a.storage.Save(id, paste, ttl); err != nil {
		slog.Error("storage save error", "err", err)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	// cfg.BaseURL already contains the full base including any path prefix,
	// e.g. "http://localhost:8080/pastebin" — so no extra joining is needed.
	url := a.cfg.BaseURL + "/" + id
	w.Header().Set("Location", url)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(`{"url":"` + url + `","id":"` + id + `","lang":"` + lang + `"}`))
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
		`"server_side_encryption":` + strconv.FormatBool(a.cfg.ServerSideEncryptionEnabled) +
		`}`))
}

// GET /raw/{id}
func (a *App) handleRaw(w http.ResponseWriter, r *http.Request) {
	paste, err := a.fetchPaste(chi.URLParam(r, "id"))
	if err != nil || paste == nil {
		http.NotFound(w, r)
		return
	}
	data, err := a.decodeFromStorage(paste.Content)
	if err != nil {
		http.Error(w, "decode error", http.StatusInternalServerError)
		return
	}
	if utf8.Valid(data) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(data)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename="+chi.URLParam(r, "id"))
		w.Write(data)
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
	data, err := a.decodeFromStorage(paste.Content)
	if err != nil {
		http.Error(w, "decode error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+id)
	w.Write(data)
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

	data, err := a.decodeFromStorage(paste.Content)
	if err != nil {
		http.Error(w, "decode error", http.StatusInternalServerError)
		return
	}

	text := "[binary data]"
	if utf8.Valid(data) {
		text = string(data)
	}

	d := a.baseData(r)
	d.IsCreated = true
	d.IsBurned = paste.Burn
	d.IsBurn = paste.Burn
	d.IsEncrypted = paste.E2EEncrypted
	d.PastebinCode = text
	d.PastebinID = id
	d.PastebinCls = "language-" + paste.Lang
	d.ExpireAt = paste.ExpireAt
	a.render(w, d, http.StatusOK)
}

// DELETE /{id}
func (a *App) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.storage.Delete(chi.URLParam(r, "id")); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"url":"/?level=info&msg=Paste deleted successfully"}`))
}

// ---- helpers ----------------------------------------------------------------

// fetchPaste handles burn-on-read: if burn=true the paste is atomically
// deleted on the first read so no second caller can ever retrieve it.
func (a *App) fetchPaste(id string) (*PasteData, error) {
	paste, err := a.storage.Get(id)
	if err != nil || paste == nil {
		return nil, err
	}
	if paste.Burn {
		return a.storage.GetAndDelete(id)
	}
	return paste, nil
}

func (a *App) render(w http.ResponseWriter, d TemplateData, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := a.tmpl.Execute(w, d); err != nil {
		slog.Error("template render error", "err", err)
	}
}

// sanitizeLang prevents path traversal or XSS in the CSS class name.
func sanitizeLang(lang string) string {
	lang = strings.ToLower(lang)
	for _, c := range lang {
		if !('a' <= c && c <= 'z') && !('0' <= c && c <= '9') && c != '-' {
			return "text"
		}
	}
	return lang
}
