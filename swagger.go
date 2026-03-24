package main

import (
	"fmt"
	"net/http"
	"strconv"
)

// openAPISpec returns the OpenAPI 3.0 JSON spec built from live config values
// so max_paste_size, server_side_encryption etc. are always accurate.
func (a *App) openAPISpec() string {
	maxSize := strconv.FormatInt(a.cfg.MaxPasteSize, 10)
	sseEnabled := strconv.FormatBool(a.cfg.ServerSideEncryptionEnabled)
	maxTTL := int64(0)
	if a.cfg.MaxTTL > 0 {
		maxTTL = int64(a.cfg.MaxTTL.Seconds())
	}

	return fmt.Sprintf(`{
  "openapi": "3.0.3",
  "info": {
    "title": "Pastebin API",
    "description": "Simple, fast and standalone pastebin service.\n\n**Server-side encryption:** %s\n**Max paste size:** %s bytes\n**Max TTL:** %d seconds (0 = unlimited)",
    "version": "%s"
  },
  "servers": [
    { "url": "%s", "description": "Current server" }
  ],
  "paths": {
    "/": {
      "post": {
        "summary": "Create a paste",
        "description": "Upload raw content (text or binary). Content-Type is not enforced — send raw bytes.",
        "operationId": "createPaste",
        "tags": ["Paste"],
        "parameters": [
          {
            "name": "ttl",
            "in": "query",
            "description": "Time-to-live in seconds. 0 = never expires. Clamped to max_ttl if set.",
            "schema": { "type": "integer", "minimum": 0, "example": 86400 }
          },
          {
            "name": "burn",
            "in": "query",
            "description": "Delete the paste after the first read.",
            "schema": { "type": "boolean", "default": false }
          },
          {
            "name": "lang",
            "in": "query",
            "description": "Prism.js language identifier for syntax highlighting.",
            "schema": { "type": "string", "default": "text", "example": "python" }
          },
          {
            "name": "encrypted",
            "in": "query",
            "description": "Mark paste as client-side (E2E) encrypted. The server stores the ciphertext as-is.",
            "schema": { "type": "boolean", "default": false }
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/octet-stream": {
              "schema": {
                "type": "string",
                "format": "binary",
                "maxLength": %s
              }
            },
            "text/plain": {
              "schema": { "type": "string", "maxLength": %s }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Paste created successfully",
            "headers": {
              "Location": {
                "description": "Full URL of the created paste",
                "schema": { "type": "string", "format": "uri" }
              }
            },
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/CreateResponse" }
              }
            }
          },
          "400": { "description": "Empty paste or invalid TTL" },
          "413": { "description": "Paste exceeds max_paste_size" },
          "500": { "description": "Encryption or storage error" }
        }
      }
    },
    "/{id}": {
      "get": {
        "summary": "View a paste (HTML)",
        "operationId": "viewPaste",
        "tags": ["Paste"],
        "parameters": [
          { "$ref": "#/components/parameters/PasteID" }
        ],
        "responses": {
          "200": { "description": "HTML page with paste content" },
          "404": { "description": "Paste not found or expired" }
        }
      },
      "delete": {
        "summary": "Delete a paste",
        "operationId": "deletePaste",
        "tags": ["Paste"],
        "parameters": [
          { "$ref": "#/components/parameters/PasteID" }
        ],
        "responses": {
          "200": {
            "description": "Paste deleted",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/DeleteResponse" }
              }
            }
          },
          "500": { "description": "Storage error" }
        }
      }
    },
    "/raw/{id}": {
      "get": {
        "summary": "Get raw paste content",
        "description": "Returns plain text in-browser for UTF-8 content, or forces download for binary.",
        "operationId": "rawPaste",
        "tags": ["Paste"],
        "parameters": [
          { "$ref": "#/components/parameters/PasteID" }
        ],
        "responses": {
          "200": {
            "description": "Raw content",
            "content": {
              "text/plain": { "schema": { "type": "string" } },
              "application/octet-stream": { "schema": { "type": "string", "format": "binary" } }
            }
          },
          "404": { "description": "Paste not found or expired" },
          "500": { "description": "Decode error" }
        }
      }
    },
    "/download/{id}": {
      "get": {
        "summary": "Download paste as file",
        "description": "Always responds with Content-Disposition: attachment, forcing a file download.",
        "operationId": "downloadPaste",
        "tags": ["Paste"],
        "parameters": [
          { "$ref": "#/components/parameters/PasteID" }
        ],
        "responses": {
          "200": {
            "description": "File download",
            "content": {
              "application/octet-stream": { "schema": { "type": "string", "format": "binary" } }
            }
          },
          "404": { "description": "Paste not found or expired" }
        }
      }
    },
    "/config": {
      "get": {
        "summary": "Get server configuration",
        "description": "Returns runtime limits and feature flags. Useful for clients to adapt UI (e.g. hide TTL selector if max_ttl is 0).",
        "operationId": "getConfig",
        "tags": ["Meta"],
        "responses": {
          "200": {
            "description": "Server configuration",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ConfigResponse" }
              }
            }
          }
        }
      }
    }
  },
  "components": {
    "parameters": {
      "PasteID": {
        "name": "id",
        "in": "path",
        "required": true,
        "description": "Paste identifier (nanoid, %d characters)",
        "schema": { "type": "string", "example": "V1StGXR8_Z5jdHi6B-myT" }
      }
    },
    "schemas": {
      "CreateResponse": {
        "type": "object",
        "properties": {
          "url":  { "type": "string", "format": "uri", "example": "%s/V1StGXR8_Z5jdHi6B-myT" },
          "id":   { "type": "string", "example": "V1StGXR8_Z5jdHi6B-myT" },
          "lang": { "type": "string", "example": "python" }
        }
      },
      "DeleteResponse": {
        "type": "object",
        "properties": {
          "url": { "type": "string", "example": "/?level=info&msg=Paste deleted successfully" }
        }
      },
      "ConfigResponse": {
        "type": "object",
        "properties": {
          "max_ttl":               { "type": "integer", "description": "Maximum TTL in seconds. 0 = unlimited.", "example": 0 },
          "default_ttl":           { "type": "integer", "description": "Default TTL in seconds.", "example": 0 },
          "max_paste_size":        { "type": "integer", "description": "Maximum paste size in bytes.", "example": %s },
          "server_side_encryption":{ "type": "boolean", "description": "Whether server-side AES-256-GCM encryption is active.", "example": %s }
        }
      }
    }
  }
}`,
		// info.description interpolations
		sseEnabled, maxSize, maxTTL,
		// version, server url
		a.cfg.Version, a.cfg.BaseURL,
		// requestBody maxLength (x2)
		maxSize, maxSize,
		// PasteID slug length
		a.cfg.SlugLen,
		// CreateResponse example url
		a.cfg.BaseURL,
		// ConfigResponse example values
		maxSize, sseEnabled,
	)
}

// swaggerUIHTML returns a self-contained Swagger UI page that loads the spec
// from /openapi.json — no npm, no binary embedding, just a CDN script tag.
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Pastebin API — Swagger UI</title>
  <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/5.17.14/swagger-ui.min.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/5.17.14/swagger-ui-bundle.min.js"></script>
  <script>
    SwaggerUIBundle({
      url: "openapi.json",
      dom_id: "#swagger-ui",
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
      layout: "BaseLayout",
      deepLinking: true,
      tryItOutEnabled: true,
    });
  </script>
</body>
</html>`

// handleOpenAPISpec serves the raw OpenAPI 3.0 JSON spec.
func (a *App) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(a.openAPISpec()))
}

// handleSwaggerUI serves the Swagger UI HTML page.
func (a *App) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(swaggerUIHTML))
}
