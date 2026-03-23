FROM python:3.12-slim

WORKDIR /app

ARG VERSION=dev
ARG VCS_REF=dev
ARG BUILD_DATE=unknown

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
ENV DATE_FORMAT="%Y-%m-%d %H:%M:%S"
ENV UVICORN_HOST="0.0.0.0"
ENV UVICORN_PORT="8080"

COPY requirements.txt .
COPY --chmod=555 entrypoint.sh /entrypoint.sh
COPY app ./app

# Download static data to host it locally
ADD https://code.jquery.com/jquery-3.6.0.min.js ./app/static
ADD https://cdn.jsdelivr.net/npm/bootstrap@4.6.2/dist/css/bootstrap.min.css ./app/static
ADD https://cdn.jsdelivr.net/npm/bootstrap@4.6.2/dist/js/bootstrap.bundle.min.js ./app/static
ADD https://cdnjs.cloudflare.com/ajax/libs/crypto-js/4.1.1/crypto-js.min.js ./app/static

RUN pip install --no-cache-dir -r requirements.txt && \
    mkdir /app/data && \
    chown nobody /app/app/logging.yml /app/data && \
    chmod -R 555 /app/app/static/

USER nobody

EXPOSE 8080

ENTRYPOINT /entrypoint.sh

HEALTHCHECK --interval=5m \
            --timeout=5s \
            --retries=1 \
            CMD python -c "import urllib.request, sys; sys.exit(0 if urllib.request.urlopen('http://$UVICORN_HOST:$UVICORN_PORT/config').status == 200 else 1)"
