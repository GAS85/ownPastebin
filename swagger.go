package main

import (
	"bytes"
	"embed"
	"net/http"
	"strconv"
	"text/template"
)

//go:embed templates/swagger_ui.html templates/openapi.json.tmpl
var swaggerFS embed.FS

// openAPIData holds the values interpolated into openapi.json.tmpl.
type openAPIData struct {
	SSEEnabled string
	MaxSize    string
	MaxTTL     int64
	Version    string
	BaseURL    string
	SlugLen    int
}

// openAPISpec renders the OpenAPI 3.0 JSON spec from the embedded template,
// using live config values so max_paste_size, server_side_encryption, etc.
// are always accurate.
func (a *App) openAPISpec() (string, error) {
	tmplBytes, err := swaggerFS.ReadFile("templates/openapi.json.tmpl")
	if err != nil {
		return "", err
	}

	t, err := template.New("openapi").Parse(string(tmplBytes))
	if err != nil {
		return "", err
	}

	maxTTL := int64(0)
	if a.cfg.MaxTTL > 0 {
		maxTTL = int64(a.cfg.MaxTTL.Seconds())
	}

	data := openAPIData{
		SSEEnabled: strconv.FormatBool(a.cfg.ServerSideEncryptionEnabled),
		MaxSize:    strconv.FormatInt(a.cfg.MaxPasteSize, 10),
		MaxTTL:     maxTTL,
		Version:    a.cfg.Version,
		BaseURL:    a.cfg.BaseURL,
		SlugLen:    a.cfg.SlugLen,
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// handleOpenAPISpec serves the raw OpenAPI 3.0 JSON spec.
func (a *App) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	spec, err := a.openAPISpec()
	if err != nil {
		http.Error(w, "failed to render OpenAPI spec: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(spec))
}

// handleSwaggerUI serves the Swagger UI HTML page from the embedded template.
func (a *App) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	html, err := swaggerFS.ReadFile("templates/swagger_ui.html")
	if err != nil {
		http.Error(w, "swagger UI not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html)
}
