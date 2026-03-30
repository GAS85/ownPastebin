#!/bin/sh
set -e

# ── Defaults ──────────────────────────────────────────────────────────────────
export PASTEBIN_BASE_URL="${PASTEBIN_BASE_URL:-http://localhost:8080}"
export PASTEBIN_SQLITE_PATH="${PASTEBIN_SQLITE_PATH:-/app/data/pastes.db}"
export PASTEBIN_DEFAULT_TTL="${PASTEBIN_DEFAULT_TTL:-0}"
export PASTEBIN_SLUG_LEN="${PASTEBIN_SLUG_LEN:-20}"
export PASTEBIN_MAX_PARALLEL_UPLOADS="${PASTEBIN_MAX_PARALLEL_UPLOADS:-20}"
export PASTEBIN_MAX_PASTE_SIZE="${PASTEBIN_MAX_PASTE_SIZE:-5MB}"
export PASTEBIN_SERVER_SIDE_ENCRYPTION_ENABLED="${PASTEBIN_SERVER_SIDE_ENCRYPTION_ENABLED:-false}"
export PASTEBIN_HOST="${PASTEBIN_HOST:-0.0.0.0}"
export PASTEBIN_PORT="${PASTEBIN_PORT:-8080}"
export PASTEBIN_SHELL_DATE_FORMAT="${PASTEBIN_SHELL_DATE_FORMAT:-%Y-%m-%d %H:%M:%S}"
export PASTEBIN_LOG_LEVEL="${PASTEBIN_LOG_LEVEL:-INFO}"

ts() { date +"$PASTEBIN_SHELL_DATE_FORMAT"; }
log() { echo "$(ts) - $1 - $(basename "$0") - $2"; }

# ── Key generator ─────────────────────────────────────────────────────────────
if [ "$GENERATE_KEY" = "true" ]; then
    KEY=$(openssl rand -base64 32)
    log INFO "Generated AES-256 key: $KEY"
    log INFO "Set PASTEBIN_SERVER_SIDE_ENCRYPTION_KEY=$KEY"
    exit 0
fi

# ── TLS validation ────────────────────────────────────────────────────────────
if [ -n "${PASTEBIN_TLS_KEY+x}" ]; then
    [ -f "$PASTEBIN_TLS_KEY" ] || {
        log ERROR "PASTEBIN_TLS_KEY file not found: $PASTEBIN_TLS_KEY"
        exit 1
    }
    if [ -n "${PASTEBIN_TLS_CERT+x}" ]; then
        [ -f "$PASTEBIN_TLS_CERT" ] || {
            log ERROR "PASTEBIN_TLS_CERT file not found: $PASTEBIN_TLS_CERT"
            exit 1
        }
    else
        log ERROR "PASTEBIN_TLS_CERT is not set"
        exit 1
    fi
fi

# ── Storage selection (informational — Go binary selects at runtime) ──────────
if [ -n "${PASTEBIN_REDIS_URL+x}" ]; then
    DB_INFO="Redis ($PASTEBIN_REDIS_URL)"
elif [ -n "${PASTEBIN_POSTGRES_URL+x}" ]; then
    DB_INFO="PostgreSQL"
else
    DB_INFO="SQLite ($PASTEBIN_SQLITE_PATH)"
    DB_SIZE="$(du -h $PASTEBIN_SQLITE_PATH | awk '{ print $1 }' 2>/dev/null)"
    echo $DB_SIZE
    SQLITE_DIR=$(dirname "$PASTEBIN_SQLITE_PATH")
    if [ ! -w "$SQLITE_DIR" ]; then
        log ERROR "$SQLITE_DIR is not writable by UID $(id -u). Exiting."
        exit 1
    fi
fi

# ── Encryption sanity check ───────────────────────────────────────────────────
if [ "$PASTEBIN_SERVER_SIDE_ENCRYPTION_ENABLED" = "true" ] && [ -z "$PASTEBIN_SERVER_SIDE_ENCRYPTION_KEY" ]; then
    log ERROR "PASTEBIN_SERVER_SIDE_ENCRYPTION_ENABLED=true but PASTEBIN_SERVER_SIDE_ENCRYPTION_KEY is not set."
    log ERROR "Run with GENERATE_KEY=true to create one."
    exit 1
fi

# ── Startup summary ───────────────────────────────────────────────────────────
log INFO "Welcome to own Pastebin $VERSION"
log INFO "Storage:                $DB_INFO"
if [ ! -z "${DB_SIZE+x}" ]; then
    log INFO "Storage size:           ${DB_SIZE}"
fi
log INFO "Listen:                 ${PASTEBIN_HOST}:${PASTEBIN_PORT}"
log INFO "Base URL:               $PASTEBIN_BASE_URL"
log INFO "Server side Encryption: $PASTEBIN_SERVER_SIDE_ENCRYPTION_ENABLED"
log INFO "Max TTL:                ${PASTEBIN_MAX_TTL:-unlimited}"
log INFO "Default TTL:            $PASTEBIN_DEFAULT_TTL"
log INFO "Max paste:              $PASTEBIN_MAX_PASTE_SIZE"
log INFO "Max Parallel Uploads:   $PASTEBIN_MAX_PARALLEL_UPLOADS"
log INFO "Uniq URL Length:        $PASTEBIN_SLUG_LEN"
log INFO "TLS key:                ${PASTEBIN_TLS_KEY:-not set}"
log INFO "TLS cert:               ${PASTEBIN_TLS_CERT:-not set}"
log INFO "Timezone:               ${TZ:-not set}"
log INFO "Log level:              $PASTEBIN_LOG_LEVEL"
log INFO "Date format:            ${PASTEBIN_DATE_FORMAT:-not set}"
log INFO "Shell Date format:      $PASTEBIN_SHELL_DATE_FORMAT"

exec /app/pastebin
