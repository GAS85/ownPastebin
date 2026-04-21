# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

# CGO is required for go-sqlite3
RUN apk add --no-cache gcc musl-dev

WORKDIR /build

COPY . .

# Download static data to host it locally
# Will be used in a plugin.go
# Check for updates under https://cdnjs.com
# CSS
ADD https://www.w3schools.com/w3css/5/w3.css ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.7.2/css/all.min.css ./static
# Fonts
ADD https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.7.2/webfonts/fa-solid-900.woff2 ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.7.2/webfonts/fa-regular-400.woff2 ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.7.2/webfonts/fa-brands-400.woff2 ./static
# JS
ADD https://cdnjs.cloudflare.com/ajax/libs/clipboard.js/2.0.11/clipboard.min.js ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/mermaid/11.12.0/mermaid.min.js ./static
# Replace relative links to static
RUN sed -i 's|../webfonts/||g' ./static/all.min.css

# Install needed packages
RUN apk add --no-cache \
        openssl \
        minify

# Add local script hashes to the CSP
RUN export InternalHashes=$(grep -oE 'on[a-zA-Z]+="[^"]+"' ./templates/index.html \
    | sed 's/^on[a-zA-Z]*="//; s/"$//' \
    | sort -u \
    | while IFS= read -r line; do \
        hash=$(printf "%s" "$line" | openssl dgst -sha256 -binary | openssl base64); \
        printf "'sha256-%s' " "$hash"; \
    done) && \
    sed -i "s|SHA-HASHES|$InternalHashes|g" ./templates/index.html

# Minify css, js, html, json and svg except "min" files
RUN find static/ -type f -name "*.css" ! -name "*.min.*" -exec minify -i "{}" \; && \
    find static/ -type f -name "*.js" ! -name "*.min.*" -exec minify -i "{}" \; && \
    minify -i "static/favicon.svg" && \
    minify -i "templates/swagger_ui.html" && \
    sed -i 's/{{.SSEEnabled}}/123456/g; \
        s/{{.MaxSize}}/345678/g; \
        s/{{.MaxTTL}}/567890/g; \
        s/{{.Version}}/789012/g; \
        s/{{.BaseURL}}/901234/g' "templates/openapi.json.tmpl" && \
    minify --type application/json -i "templates/openapi.json.tmpl" && \
    sed -i 's/123456/{{.SSEEnabled}}/g; \
         s/345678/{{.MaxSize}}/g; \
         s/567890/{{.MaxTTL}}/g; \
         s/789012/{{.Version}}/g; \
         s/901234/{{.BaseURL}}/g' "templates/openapi.json.tmpl"

RUN go mod download

RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o pastebin .

# ── Final stage ───────────────────────────────────────────────────────────────
FROM alpine:3.23

ARG VERSION=dev
ARG VCS_REF=dev
ARG BUILD_DATE=unknown
ARG USER=nobody

LABEL maintainer="Georgiy Sitnikov <g.own.pastebin@sitnikov.eu>" \
    org.opencontainers.image.title="ownpastebin" \
    org.opencontainers.image.description="A minimal paste service with support for raw uploads, TTL, burn-after-read, and optional encryption." \
    org.opencontainers.image.source="https://github.com/GAS85/ownPastebin" \
    org.opencontainers.image.url="https://hub.docker.com/r/gas85/ownpastebin" \
    org.opencontainers.image.documentation="https://github.com/GAS85/ownPastebin#" \
    org.opencontainers.image.licenses="MIT" \
    org.opencontainers.image.version=$VERSION \
    org.opencontainers.image.revision=$VCS_REF

ENV VERSION=$VERSION
ENV TZ=Europe/Zurich

RUN apk add --no-cache ca-certificates tzdata openssl

WORKDIR /app

COPY --from=builder --chmod=555 /build/pastebin /app/pastebin
COPY --chmod=555 entrypoint.sh /entrypoint.sh
COPY --chmod=444 LICENSE /app/LICENSE

RUN mkdir -p /app/data && \
    touch /app/data/pastebin.log && \
    chown -R $USER /app/data

USER $USER

EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]

HEALTHCHECK --interval=5m \
            --timeout=5s \
            --retries=3 \
            CMD wget -qO- "${PASTEBIN_BASE_URL:-http://localhost/8080}/config?healthcheck" > /dev/null || exit
