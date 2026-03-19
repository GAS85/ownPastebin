FROM python:3.14-slim

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
      org.opencontainers.image.revision=$VCS_REF \
      org.opencontainers.image.version=$VERSION

ENV VERSION=$VERSION
ENV TZ=Europe/Zurich
ENV DATE_FORMAT="%Y-%m-%d %H:%M:%S"
ENV UVICORN_HOST="0.0.0.0"
ENV UVICORN_PORT="8080"

COPY requirements.txt .
COPY app ./app
COPY --chmod=555 entrypoint.sh /entrypoint.sh

RUN pip install --no-cache-dir -r requirements.txt && \
    chown nobody /app/app/logging.yml

USER nobody

EXPOSE 8080

ENTRYPOINT /entrypoint.sh
