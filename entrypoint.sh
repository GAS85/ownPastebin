#!/bin/bash

# Set defaults
export REDIS_URL="${REDIS_URL:-redis://redis:6379/0}"
export BASE_URL="${BASE_URL:-http://localhost:8000}"
export DEFAULT_TTL="${DEFAULT_TTL:-0}"
export SLUG_LEN="${SLUG_LEN:-20}"
export MAX_PASTE_SIZE="${MAX_PASTE_SIZE:-5MB}"
export SERVER_SIDE_ENCRYPTION_ENABLED="${SERVER_SIDE_ENCRYPTION_ENABLED:-false}"
export UVICORN_LOG_CONFIG="${UVICORN_LOG_CONFIG:-/app/app/logging.yml}"

if [[ "$GENERATE_KEY" == "true" ]]; then

    TEMP_KEY=$(openssl rand -base64 32)
    echo "$(date "+$DATE_FORMAT") - INFO - $(basename "$0") - Generating a random 32-byte key --> $TEMP_KEY <-- you can use it via SERVER_SIDE_ENCRYPTION_KEY"
    echo "$(date "+$DATE_FORMAT") - INFO - $(basename "$0") - Please set SERVER_SIDE_ENCRYPTION_KEY=$TEMP_KEY"
    exit 0

fi

if [[ ! -z ${TLS_KEY+x} ]]; then

    UVICORN_TLS_KEY="--ssl-keyfile=$TLS_KEY"
    test -f "$TLS_KEY" || {
        echo "$(date "+$DATE_FORMAT") - ERROR - $(basename "$0") - TLS_KEY file not found under $TLS_KEY"
        exit 1
    }

    if [[ ! -z ${TLS_CERT+x} ]]; then

        UVICORN_TLS_CERT="--certfile=$TLS_CERT"
        test -f "$TLS_CERT" || {
            echo "$(date "+$DATE_FORMAT") - ERROR - $(basename "$0") - TLS_CERT file not found under $TLS_CERT"
            exit 1
        }

    else

        echo "$(date "+$DATE_FORMAT") - ERROR - $(basename "$0") - TLS_CERT was not set"
        exit 1

    fi

fi

# Use central DATE_FORMAT in Uvicorn
CONFIG_UPDATE=$(sed "s/DATE_FORMAT/$DATE_FORMAT/g" $UVICORN_LOG_CONFIG)
echo "${CONFIG_UPDATE}" >${UVICORN_LOG_CONFIG}

echo "$(date "+$DATE_FORMAT") - INFO - $(basename "$0") - Welcome to your own Pastebin version $VERSION"
echo "$(date "+$DATE_FORMAT") - INFO - $(basename "$0") - Following variables are set:
                                UVICORN_HOST $UVICORN_HOST
                                UVICORN_PORT $UVICORN_PORT
                                BASE_URL $BASE_URL
                                SERVER_SIDE_ENCRYPTION_ENABLED $SERVER_SIDE_ENCRYPTION_ENABLED
                                MAX_TTL ${MAX_TTL:-Not set}
                                DEFAULT_TTL $DEFAULT_TTL
                                SLUG_LEN $SLUG_LEN
                                MAX_PASTE_SIZE $MAX_PASTE_SIZE
                                TLS_KEY ${TLS_KEY:-Not set}
                                TLS_CERT ${TLS_CERT:-Not set}"

uvicorn app.main:app --log-config "$UVICORN_LOG_CONFIG" $UVICORN_TLS_KEY $UVICORN_TLS_CERT
