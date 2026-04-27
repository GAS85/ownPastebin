# Pastebin

[![Dev Build](https://github.com/GAS85/ownPastebin/actions/workflows/docker-dev.yml/badge.svg?branch=dev)](https://github.com/GAS85/ownPastebin/actions/workflows/docker-dev.yml)
[![Release Build and Push to Dockerhub](https://github.com/GAS85/ownPastebin/actions/workflows/docker-release.yml/badge.svg)](https://github.com/GAS85/ownPastebin/actions/workflows/docker-release.yml)
![Release](https://img.shields.io/github/actions/workflow/status/GAS85/ownPastebin/docker-release.yml?label=release&logo=github)
[![Docker hub](https://img.shields.io/badge/Docker--hub-grey?logo=docker)][docker-hub]
[![Docker Pulls][docker-pulls]][docker-hub]
[![Docker Image Size][docker-size]][docker-hub]

[docker-hub]: https://hub.docker.com/r/gas85/ownpastebin
[docker-pulls]: https://img.shields.io/docker/pulls/gas85/ownpastebin
[docker-size]: https://img.shields.io/docker/image-size/gas85/ownpastebin/latest

----

A minimal, RAM-friendly paste service with support for raw uploads, TTL, burn-after-read, optional encryption, and **pluggable storage backends**.

## Demo

<https://sitnikov.eu/pastebin/>

## ✨ Features

* ⚡ Fast and lightweight
* 🔌 Pluggable storage:

  * Redis (optional, in-memory)
  * PostgreSQL (optional, persistent)
  * SQLite (default)

* 🔥 Burn-after-read pastes
* ⏳ TTL (expiration support)
* 🔐 Optional server-side encryption with AES-GCM
* 🔐 Optional end-to-end encryption with AES-GCM
* 📦 Binary-safe uploads/downloads
* 🧠 Designed to be memory efficient

## ⚙️ Configuration

All configuration is done via environment variables.

### 🗄️ Storage Backends (Priority Order)

The application automatically selects the first available backend:

1. [Redis](https://github.com/redis/redis) - `PASTEBIN_REDIS_URL`
2. [Postgres](https://github.com/postgres/postgres) - `PASTEBIN_POSTGRES_URL`
3. [SQLite](https://github.com/sqlite/sqlite) default if none above was set - `PASTEBIN_SQLITE_PATH`

### Variables

* `PASTEBIN_REDIS_URL` - Redis connection string. No default - if not set, Redis is disabled. Example:

  ```plain
  redis://redis:6379/0
  ```

* `PASTEBIN_POSTGRES_URL` - PostgreSQL connection string. Used if Redis is not configured. No default - if not set, PostgreSQL is disabled. Example:

   ```plain
   postgresql://user:pass@postgres:5432/pastebin
   ```

* `PASTEBIN_SQLITE_PATH` - Path to SQLite database file. Default:

  ```plain
  /app/data/pastes.db
  ```

* `PASTEBIN_SQLITE_PAGE_SIZE` - You can set SQLite Page size for a new table. Valid values are power of 2 from `512` to `65536`. You can calculate it roughly on following basis:
  * `4096` is default — good for typical text pastes (< 100 KB).
  * `8192` or `16384`  — better when pastes are regularly several MB, because each paste fits in fewer pages, reducing I/O and B-tree depth.

## 🌐 Application Settings

* `PASTEBIN_BASE_URL`- Public base URL of your service. Default:

  ```plain
  http://localhost:8080
  ```

  Following prefixes are supported:

  * No prefix `PASTEBIN_BASE_URL=http://localhost:8080` or `PASTEBIN_BASE_URL=https://pastebin.myserver.com`
  * Behind nginx at `/pastebin` - `PASTEBIN_BASE_URL=https://myserver.com/pastebin`
  * Behind nginx at `/tools/paste` - `PASTEBIN_BASE_URL=https://myserver.com/tools/paste`

* `PASTEBIN_HOST` - Bind address. Default: `0.0.0.0`
* `PASTEBIN_PORT` - Port to listen to. Default: `8080`
* `PASTEBIN_TLS_KEY` - Provide path to TLS Key to enable TLS Support directly on a service.
* `PASTEBIN_TLS_CERT` - Provide path to TLS Certificate to enable TLS Support directly on a service.
* `PASTEBIN_TRUSTED_PROXY` - Provide IP or CIDR of trusted proxies, so that X-Forwarded-For header will be used.
* `PASTEBIN_LOG_LEVEL` - Set log level. Default: `Info`.
* `PASTEBIN_LOG_FORMAT` - Set it to `json`, to have JSON logs output. Default: `text`.
* `PASTEBIN_FILE_LOG` - Set log file location to log all App output. Default is not set, it is logged to stdout. If you need log file, simply provide a path writable by user "nobody". Recommended is `/app/data/pastebin.log`.

## ⏳ TTL Settings

* `PASTEBIN_DEFAULT_TTL` - Default expiration if none provided. Default: `0` (no expiration)
* `PASTEBIN_MAX_TTL` -  Maximum allowed TTL. It is recommended to set this value for internet accessible sites. If set:
  * caps user-provided TTL
  * used when no TTL is provided
* `PASTEBIN_DEFAULT_BURN` - If enabled all pastes without `burn=false` will be saved to be viewed only once. You can still set `burn=false` via UI or CLI. Default: `false`.

Supported Formats:

| Format       | Example |
|:-------------|:-------:|
| Seconds      | `3600`  |
| Hours        | `1h`    |
| Days         | `1d`    |
| Months (30d) | `1mo`   |

## 📏 Limits

* `PASTEBIN_MAX_PARALLEL_UPLOADS` - Max amount of parallel POST requests. Default `20`. Be aware that each requests needs memory. E.g. if `PASTEBIN_MAX_PASTE_SIZE=5MB` and `PASTEBIN_MAX_PARALLEL_UPLOADS=20`, that needs around 5 *20* 3,3 (roughly amount of modifications) = 400 Mb of RAM and with `PASTEBIN_MAX_PASTE_SIZE=30MB` around 2 GB of RAM.
* `PASTEBIN_MAX_PASTE_SIZE` - Max upload size. Default: `5MB`

Supported Formats:

| Format    | Example |
|:----------|:-------:|
| bytes     | `3600`  |
| Kilobytes | `1kb`   |
| Megabytes | `1mb`   |
| Gigabytes | `1gb`   |

## 🔐 Security

* `PASTEBIN_SERVER_SIDE_ENCRYPTION_ENABLED`- Enable encryption before storage. Default `false` - disabled.
* `PASTEBIN_SERVER_SIDE_ENCRYPTION_KEY`- 32-byte base64 key (required if encryption enabled). You can generate Key with openssl, or directly with this container.

```bash
openssl rand -base64 32
```

Or:

```bash
docker run -e GENERATE_KEY=true gas85/ownpastebin:latest
```

⚠️ If you ever rotate the key or loose it, **old pastes become permanently unreadable.** ⚠️

## 🕒 Misc

* `PASTEBIN_SLUG_LEN` - Uniq URL Length. Default to `20`. It is not recommended to go below this value to avoid possible collision and Link guessing attack.
* `PASTEBIN_DATE_FORMAT` - Log timestamp format. Default: `%Y-%m-%d %H:%M:%S`
* `TZ` - Timezone. Default `Europe/Zurich`

## 🧠 Storage Behavior

| Config                  | Backend Used |
| ----------------------- | ------------ |
| `PASTEBIN_REDIS_URL`    | Redis        |
| `PASTEBIN_POSTGRES_URL` | PostgreSQL   |
| None set                | SQLite       |

 You can still set custom SQLite DB location via `PASTEBIN_SQLITE_PATH`.

### Notes

* Redis = fastest, but memory-based storage. Fits good for Local network usage.
* PostgreSQL = persistent, scalable. Fits good for Local network and Internet usage, distributed, high performance.
* SQLite = zero-config, minimal RAM usage. Default, simple fast storage fits to all.

## 🚀 Run

### Docker

```bash
docker run -d \
  --name pastebin \
  -p 8080:8080 \
  -v ./data:/app/data \
  -e PASTEBIN_MAX_TTL=360d \
  -e PASTEBIN_BASE_URL=http://localhost:8080 \
  --restart always \
  gas85/ownpastebin:latest
```

### Docker Compose

Please refer to [docker-compose.yml](https://github.com/GAS85/ownPastebin/blob/main/docker-compose.yml) as example.

```bash
docker compose up -d
```

### Kubernetes / Openshift

Please refer to [k8s.yml](https://github.com/GAS85/ownPastebin/blob/main/k8s.yml) as example.

```bash
kubectl apply -f k8s.yml
```

## Build

You can build it with following commands:

```bash
go mod download
CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o pastebin .
```

Or use docker

```bash
docker build -t ownpastebin:latest .
```

## 📦 Pastebin API

You can find API documentation under `/swagger-ui` e.g. <https://sitnikov.eu/pastebin/swagger-ui>

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
| `encrypted` | bool | Marks paste as client-side encrypted. It is only being used to give UI a trigger to offer Decryption directly in Browser. Otherwise encrypted data will be shown. |

### Examples

#### Raw upload

```bash
curl "http://localhost:8080" --data-binary "@file.txt"
```

E.g. you can push all docker logs to the pastebin:

```bash
# As per https://wiki.sitnikov.eu/doku.php?id=howto:docker#push_all_docker_logs_to_the_pastebin
docker ps --format '{{.Names}}' | xargs -I {} sh -c 'docker logs --timestamps -tail 500 {} 2>&1 | sed "s/^/[{}] /"' | curl http://localhost.eu:8080 --data-binary @-
```

#### Burn after read + TTL

```bash
curl "http://localhost:8080?burn=true&ttl=60" --data-binary "@file.txt"
```

#### Response

As Response you will get JSON with URL to the webUI for this paste and paste ID that you can use e.g. to call `/raw` or `/download` endpoint.

```json
{
  "url": "http://localhost:8080/abc123",
  "id": "abc123",
  "lang": "text"
}
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
2026-03-25 09:55:57 - INFO - pastebin - access method=GET path=/ status=200 duration=1.077ms bytes=15044 ip=192.168.65.1:57801
```

```json
{"level":"INFO","msg":{"bytes":14562,"duration":"710µs","ip":"192.168.65.1:59435","message":"access","method":"GET","path":"/","status":200},"time":"2026-04-27T20:13:34.986291506+02:00"}
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
