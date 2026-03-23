# Pastebin

A minimal, RAM-friendly paste service with support for raw uploads, TTL, burn-after-read, optional encryption, and **pluggable storage backends**.

## ✨ Features

* ⚡ Fast and lightweight
* 🔌 Pluggable storage:

  * Redis (optional, in-memory)
  * PostgreSQL (optional, persistent)
  * SQLite (default fallback)

* 🔥 Burn-after-read pastes
* ⏳ TTL (expiration support)
* 🔐 Optional server-side encryption
* 📦 Binary-safe uploads/downloads
* 🧠 Designed to be memory efficient (no caching layer)

## ⚙️ Configuration

All configuration is done via environment variables.

### 🗄️ Storage Backends (Priority Order)

The application automatically selects the first available backend:

1. `REDIS_URL`
2. `POSTGRES_URL`
3. SQLite (fallback)

### Variables

* `REDIS_URL` - Redis connection string. **No default** → if not set, Redis is disabled. Example:

  ```plain
  redis://redis:6379/0
  ```

* `POSTGRES_URL` - PostgreSQL connection string. Used if Redis is not configured. Example:

   ```plain
   postgresql://user:pass@postgres:5432/pastebin
   ```

* `SQLITE_PATH` - Path to SQLite database file. Default:

  ```plain
  /app/data/pastes.db
  ```

## 🌐 Application Settings

* `BASE_URL`- Public base URL of your service. Default:

  ```plain
  http://localhost:8080
  ```

* `UVICORN_HOST` - Bind address. Default: `0.0.0.0`
* `UVICORN_PORT` - Port. Default: `8080`
* `TLS_KEY` - Provide path to TLS Key to enable TLS Support directly on a service.
* `TLS_CERT` - Provide path to TLS Certificate to enable TLS Support directly on a service.

## ⏳ TTL Settings

* `DEFAULT_TTL` - Default expiration if none provided. Default: `0` (no expiration)
* `MAX_TTL` -  Maximum allowed TTL. It is recommended to set this value for internet accessible sites. If set:
  * caps user-provided TTL
  * used when no TTL is provided

### Supported Formats

| Format       | Example |
|:-------------|:-------:|
| Seconds      | `3600`  |
| Minutes      | `30M`   |
| Hours        | `1h`    |
| Days         | `1d`    |
| Months (30d) | `1m`    |

## 📏 Limits

* `MAX_PASTE_SIZE` - Max upload size. Default: `5MB`

### Supported Formats

| Format    | Example |
|:----------|:-------:|
| bytes     | `3600`  |
| Kilobytes | `1kb`   |
| Megabytes | `1mb`   |
| Gigabytes | `1gb`   |

## 🔐 Security

* `SERVER_SIDE_ENCRYPTION_ENABLED`- Enable encryption before storage. Default disabled.
* `SERVER_SIDE_ENCRYPTION_KEY`- 32-byte base64 key (required if encryption enabled). You can generate Key with openssl, or directly with this container

```bash
openssl rand -base64 32
```

Or:

```bash
docker run -e GENERATE_KEY=true gas85/ownpastebin:latest
```

## 🕒 Misc

* `SLUG_LEN` - Uniq URL Length. Default to `20`. It is not recommended to go below this value to avoid collision and Link guessing attack.
* `DATE_FORMAT` - Log timestamp format. Default: `%Y-%m-%d %H:%M:%S`
* `TZ` - Timezone. Default `Europe/Zurich`

## 🧠 Storage Behavior

| Config                  | Backend Used |
| ----------------------- | ------------ |
| `REDIS_URL` set         | Redis        |
| Only `POSTGRES_URL` set | PostgreSQL   |
| None set                | SQLite       |

### Notes

* Redis = fastest, but memory-based storage. Fits good for Local network usage.
* PostgreSQL = persistent, scalable. Fits good for Local network and Internet usage.
* SQLite = zero-config, minimal RAM usage. Default simple storage fits to all.

## 🚀 Run

```bash
docker compose up -d
```

## 📦 Pastebin API

### 🚀 Create Paste - `POST /`

Create a new paste.

#### Supported Content Types

* `application/json`
* `application/x-www-form-urlencoded`
* `multipart/form-data`
* raw body (`--data-binary`)

#### Query Parameters

| Name        | Type | Description                          |
| ----------- | ---- | ------------------------------------ |
| `ttl`       | int  | Time to live (seconds)               |
| `burn`      | bool | Delete after first read              |
| `encrypted` | bool | Marks paste as client-side encrypted |

### Examples

#### Raw upload

```bash
curl "http://localhost:8080" --data-binary "@file.txt"
```

#### Burn after read + TTL

```bash
curl "http://localhost:8080?burn=true&ttl=60" --data-binary "@file.txt"
```

#### Response

As Response you will get JSON with URL to the webUI for this paste and paste ID that you can use e.g. to call `/raw` endpoint.

```json
{
  "url": "http://localhost:8080/abc123",
  "id": "abc123",
  "lang": "text"
}
```

Headers:

```plain
Location: http://localhost:8080/abc123
```

### 📖 View Paste - `GET /{paste_id}`

Returns HTML view.

```bash
curl http://localhost:8080/abc123
```

### 📄 View Raw Paste - `GET /raw/{paste_id}`

Returns plain text or binary-safe response.

```bash
curl http://localhost:8080/raw/abc123
```

### ⬇️ Download Paste - `GET /download/{paste_id}`

Forces file download.

```bash
curl http://localhost:8080/download/abc123
```

### ❌ Delete Paste - `DELETE /{paste_id}`

```bash
curl -X DELETE http://localhost:8080/abc123
```

### 🔥 Burn After Read

If `burn=true`:

* First request → returns content
* Second request → `404 Not Found`

### ⚠️ Limits & Errors

### Paste too large

```plain
413 Paste too large
```

## 🧾 Logging

Example:

```plain
2026-03-18 21:30:21 - INFO - uvicorn.access - 192.168.1.1:36854 - "POST / HTTP/1.1" 201
```

## 🛠️ Notes

* Works with curl, browsers, API clients
* Binary-safe storage
* No in-memory caching → RAM efficient
* SQLite uses WAL mode for better concurrency
* Expired pastes are cleaned on access (lazy cleanup)

## 📌 Summary

This pastebin is designed to be:

* ⚡ Fast
* 🧠 Memory-efficient
* 🔌 Flexible (multi-backend)
* 🔐 Secure (optional encryption)

Inspired by [Pastebin](https://github.com/mkaczanowski/pastebin).
