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
export PASTEBIN_DATE_FORMAT="${PASTEBIN_DATE_FORMAT:-%Y-%m-%d %H:%M:%S}"
# Keep date format for the shell
export PASTEBIN_SHELL_DATE_FORMAT="${PASTEBIN_DATE_FORMAT}"
# Rewrite to Go format
export PASTEBIN_DATE_FORMAT="$(date -D "%Y-%m-%dT%H:%M:%S" -d "2006-01-02T15:04:05" +"$PASTEBIN_DATE_FORMAT")"
export PASTEBIN_LOG_LEVEL="${PASTEBIN_LOG_LEVEL:-INFO}"
export PASTEBIN_LOG_FORMAT="${PASTEBIN_LOG_FORMAT:-plain}"

PASTEBIN_LOG_FORMAT=json

ts() { date +"$PASTEBIN_SHELL_DATE_FORMAT"; }
log() {
    if [ ${PASTEBIN_LOG_FORMAT} = "json" ]; then
        echo "{\"ts\":\"$(ts)\",\"level\":\"$1\",\"component\":\"$(basename "$0")\",\"msg\":\"$(echo $2 | sed 's/[[:space:]]\+/ /g; s/\\t//g')\"}"
    else
        echo -e "$(ts) - $1 - $(basename "$0") - $(echo "$2" | grep -v '^$')"
    fi
}

# ── Key generator ─────────────────────────────────────────────────────────────
if [ "${GENERATE_KEY}" = "true" ]; then
    KEY=$(openssl rand -base64 32)
    log INFO "Generated AES-256 key: $KEY"
    log INFO "Set variable PASTEBIN_SERVER_SIDE_ENCRYPTION_KEY=$KEY"
    exit 0
fi

# ── TLS validation ────────────────────────────────────────────────────────────
if [ -n "${PASTEBIN_TLS_KEY+x}" ]; then
    [ -f "${PASTEBIN_TLS_KEY}" ] || {
        log ERROR "PASTEBIN_TLS_KEY file not found: $PASTEBIN_TLS_KEY"
        exit 1
    }
    if [ -n "${PASTEBIN_TLS_CERT+x}" ]; then
        [ -f "${PASTEBIN_TLS_CERT}" ] || {
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
    # We can only check SQlite DB as it is mounted to the container
    DB_SIZE="$(du -h $PASTEBIN_SQLITE_PATH | awk '{ print $1 }' 2>/dev/null)"
    SQLITE_DIR=$(dirname "$PASTEBIN_SQLITE_PATH")
    if [ ! -w "$SQLITE_DIR" ]; then
        log ERROR "$SQLITE_DIR is not writable by UID $(id -u). Exiting."
        exit 1
    fi
    # Check if PASTEBIN_SQLITE_PAGE_SIZE set and is between 512 and 65536 and power of 2
    if [ -n "${PASTEBIN_SQLITE_PAGE_SIZE}" ]; then
        # Check if it's a number
        case "${PASTEBIN_SQLITE_PAGE_SIZE}" in
        *[!0-9]*)
            log ERROR "$PASTEBIN_SQLITE_PAGE_SIZE is not a valid number. Exiting."
            exit 1
            ;;
        *)
            # Valid number, continue
            ;;
        esac
        # Check range and power of 2
        if [ "${PASTEBIN_SQLITE_PAGE_SIZE}" -ge 512 ] &&
            [ "${PASTEBIN_SQLITE_PAGE_SIZE}" -le 65536 ] &&
            [ $((PASTEBIN_SQLITE_PAGE_SIZE & (PASTEBIN_SQLITE_PAGE_SIZE - 1))) -eq 0 ]; then
            # Valid value
            :
        else
            log ERROR "$PASTEBIN_SQLITE_PAGE_SIZE has not valid value. Valid values are from 512 to 65536, power of 2. Exiting."
            exit 1
        fi
    fi
fi

# ── Encryption sanity check ───────────────────────────────────────────────────
if [ "$PASTEBIN_SERVER_SIDE_ENCRYPTION_ENABLED" = "true" ] && [ -z "$PASTEBIN_SERVER_SIDE_ENCRYPTION_KEY" ]; then
    log ERROR "PASTEBIN_SERVER_SIDE_ENCRYPTION_ENABLED=true but PASTEBIN_SERVER_SIDE_ENCRYPTION_KEY is not set."
    log ERROR "Run with GENERATE_KEY=true to create one."
    exit 1
fi

# ── Startup summary ───────────────────────────────────────────────────────────

log INFO "Welcome to own Pastebin $VERSION, build $VCS_REF.
\t\tStorage:                ${DB_INFO},
$([ -n "${DB_SIZE}" ] && echo "\t\tStorage size:           ${DB_SIZE},";)
$([ -n "${PASTEBIN_SQLITE_PAGE_SIZE}" ] && echo "\t\tCustom SQLite Page size:${PASTEBIN_SQLITE_PAGE_SIZE},";)
\t\tListen:                 ${PASTEBIN_HOST}:${PASTEBIN_PORT},
\t\tBase URL:               ${PASTEBIN_BASE_URL},
\t\tServer side Encryption: ${PASTEBIN_SERVER_SIDE_ENCRYPTION_ENABLED},
\t\tMax TTL:                ${PASTEBIN_MAX_TTL:-unlimited},
\t\tDefault TTL:            ${PASTEBIN_DEFAULT_TTL:-not set},
\t\tBurn by default         ${PASTEBIN_DEFAULT_BURN:-false},
\t\tMax paste:              ${PASTEBIN_MAX_PASTE_SIZE},
\t\tMax Parallel Uploads:   ${PASTEBIN_MAX_PARALLEL_UPLOADS},
\t\tUniq URL Length:        ${PASTEBIN_SLUG_LEN},
\t\tTLS key:                ${PASTEBIN_TLS_KEY:-not set},
\t\tTLS cert:               ${PASTEBIN_TLS_CERT:-not set},
\t\tTrusted proxy:          ${PASTEBIN_TRUSTED_PROXY:-not set (XFF ignored)},
\t\tTimezone:               ${TZ:-not set},
\t\tLog level:              ${PASTEBIN_LOG_LEVEL},
\t\tDate format:            ${PASTEBIN_SHELL_DATE_FORMAT}"

# ── File Logging  ─────────────────────────────────────────────────────────────
if [ -n "${PASTEBIN_FILE_LOG+x}" ]; then
    touch "$PASTEBIN_FILE_LOG" 2>/dev/null || {
        log ERROR "Cannot create log file under ${PASTEBIN_FILE_LOG} as UID $(id -u). Check permissions or disable file logging. Exiting."
        exit 1
    }
    if [ -w "$PASTEBIN_FILE_LOG" ]; then
        log INFO "Logging to file:        ${PASTEBIN_FILE_LOG}"
        exec >>"$PASTEBIN_FILE_LOG" 2>&1
        # After this point everything will be logged to the file.
    else
        log ERROR "Log file not writable: ${PASTEBIN_FILE_LOG}"
        exit 1
    fi
fi

# ── Start the app  ────────────────────────────────────────────────────────────
exec /app/pastebin

log INFO "Shutdown."
