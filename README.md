# Pastebin

## Configuration

Following Variables are supported:

- `REDIS_URL` - redis URL, default to `redis://redis:6379/0`
- `BASE_URL` - Base URL, default to `http://localhost:8000`. Base URL will be used in API answers, provide your real host, protocol and port here.
- `UVICORN_HOST` - Web Server interface to bind, default `0.0.0.0`
- `UVICORN_PORT` - Web Server port to bind, default `8080`
- `DEFAULT_TTL` - Default TTL if no TTL was provided by paste creation, default to `0` (no expiration). Supported values:
  - Seconds: `3600`
  - Hours: `1h`
  - Days: `1d`
  - Month: `1m`
- `MAX_TTL` - Maximum TTL, default is not set. This value will define Maximum allowed TTL to set. If no TTL was provided, `MAX_TTL` Value will apply. Supported values are the same as for `DEFAULT_TTL`.
- `SLUG_LEN` - Uniq URL Length, default to `20`
- `MAX_PASTE_SIZE` - Max payload size to be pasted, default to `5MB`
- `SERVER_SIDE_ENCRYPTION_ENABLED` - Enable Server Side encryption, default to `false`. It is strongly recommended to enable Server Side encryption, especially on a shared redis instances.
- `SERVER_SIDE_ENCRYPTION_KEY` - Server side 32-byte encryption key, there is no default. You can generate one by command `openssl rand -base64 32` or using this container directly by setting `GENERATE_KEY` to `true`, e.g.:

```shell
docker run -e GENERATE_KEY=true ownpastebin-pastebin:latest
```

## 📦 Pastebin API

A minimal paste service with support for raw uploads, TTL, burn-after-read, and optional encryption.

### 🔗 Base URL

```shell
http://localhost:8000
```

## 🚀 Create Paste

### POST `/`

Create a new paste.

#### Supported Content Types

* `application/json`
* `application/x-www-form-urlencoded`
* `multipart/form-data`
* raw body (`--data-binary`)

#### Query Parameters

| Name      | Type | Description                    |
|-----------|------|--------------------------------|
| `ttl`     | int  | Time to live (seconds)         |
| `burn`    | bool | Delete after first read        |
| `encrypt` | bool | Encrypt content before storing |

#### Examples

##### Raw upload

```shell
curl "http://localhost:8000" --data-binary "@file.txt"
```

##### With burn + TTL

```shell
curl "http://localhost:8000?burn=true&ttl=60" --data-binary "@file.txt"
```

#### Response

```json
{
  "url": "http://localhost:8000/abc123",
  "id": "abc123"
}
```

Headers:

```shell
Location: http://localhost:8000/abc123
```

## 📖 View Paste

### GET `/{paste_id}` - get results

Returns HTML view.

#### Example

```shell
curl http://localhost:8000/abc123
```

### GET `/raw/{paste_id}` - get raw results

Returns plain text.

```shell
curl http://localhost:8000/raw/abc123
```

## ⬇️ Download Paste

### GET `/download/{paste_id}`

Downloads paste as file.

## ❌ Delete Paste

### DELETE `/{paste_id}`

```shell
curl -X DELETE http://localhost:8000/abc123
```

## 🔥 Burn After Read

If `burn=true`:

* First request → returns content
* Second request → `404 Not Found`

## ⚠️ Limits

* Max paste size: configurable (`MAX_PASTE_SIZE`)
* Large uploads return:

```
413 Paste too large
```

## 🧾 Logging

All requests are logged in JSON format:

```plain
2026-03-18 21:30:21 - INFO - uvicorn.access - 192.168.65.1:36854 - "POST / HTTP/1.1" 201
```

## 🛠️ Notes

* Supports `curl`, browsers, and API clients
* Designed for simplicity and speed
* Backed by Redis
