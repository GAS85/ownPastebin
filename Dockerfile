# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder
# 1.26

# CGO is required for go-sqlite3
RUN apk add --no-cache gcc musl-dev

WORKDIR /build

COPY . .

# Download static data to host it locally
# Will be used in a plugin.go
# CSS
ADD https://cdn.jsdelivr.net/npm/bootstrap@4.6.2/dist/css/bootstrap.min.css ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/font-awesome/5.15.4/css/all.min.css ./static
# Fonts
ADD https://cdnjs.cloudflare.com/ajax/libs/font-awesome/5.15.4/webfonts/fa-solid-900.woff2 ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/font-awesome/5.15.4/webfonts/fa-solid-900.woff ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/font-awesome/5.15.4/webfonts/fa-solid-900.ttf  ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/font-awesome/5.15.4/webfonts/fa-brands-400.woff2 ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/font-awesome/5.15.4/webfonts/fa-brands-400.woff ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/font-awesome/5.15.4/webfonts/fa-brands-400.ttf ./static
# JS
ADD https://cdnjs.cloudflare.com/ajax/libs/jquery/3.7.0/jquery.min.js ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/twitter-bootstrap/4.6.2/js/bootstrap.min.js ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/crypto-js/4.2.0/crypto-js.min.js ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/popper.js/1.16.1/umd/popper.min.js ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/clipboard.js/2.0.11/clipboard.min.js ./static
ADD https://cdnjs.cloudflare.com/ajax/libs/mermaid/10.6.1/mermaid.min.js ./static

RUN sed -i 's|../webfonts|/static|g' ./static/all.min.css
RUN mkdir -p /usr/local/go/src/pastebin/plugins && cp plugins/*.* /usr/local/go/src/pastebin/plugins
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
    org.opencontainers.image.version=$VERSION \
    org.opencontainers.image.revision=$VCS_REF

ENV VERSION=$VERSION
ENV TZ=Europe/Zurich

RUN apk add --no-cache ca-certificates tzdata openssl

WORKDIR /app

COPY --from=builder --chmod=555 /build/pastebin /app/pastebin
COPY --chmod=555 entrypoint.sh /entrypoint.sh

RUN mkdir -p /app/data && \
    chown -R $USER /app/data

USER $USER

EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]

HEALTHCHECK --interval=1m \
            --timeout=5s \
            --retries=1 \
            CMD "pgrep pastebin"
