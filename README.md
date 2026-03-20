# Pastebin

A minimal paste service with support for raw uploads, TTL, burn-after-read, and optional encryption.

## Configuration

Following Variables are supported:

- `REDIS_URL` - redis URL, default to `redis://redis:6379/0`.
- `BASE_URL` - Base URL, default to `http://localhost:8080`. Base URL will be used in API answers, provide your real host, protocol and port here. E.g if your host is `myhost.com` with https and default ports set it to `https://myhost.com`.
- `UVICORN_HOST` - Web Server interface to bind, default `0.0.0.0`.
- `UVICORN_PORT` - Web Server port to bind, default `8080`.
- `DEFAULT_TTL` - Default TTL if no TTL was provided by paste creation, default to `0` (no expiration). Supported values:
  - Seconds: `3600`
  - Hours: `1h`
  - Days: `1d`
  - Month: `1m`
- `MAX_TTL` - Maximum TTL, default is not set. This value will define Maximum allowed TTL to set. If no TTL was provided, `MAX_TTL` Value will apply. Supported values are the same as for `DEFAULT_TTL`. It is recommended to set `MAX_TTL` e.g. to 1 year (15768000) otherwise most paste's will be stored forever.
- `SLUG_LEN` - Uniq URL Length, default to `20`. It is not recommended to go below this value to avoid collision and Link guessing.
- `MAX_PASTE_SIZE` - Max payload size to be pasted, default to `5MB`.
- `SERVER_SIDE_ENCRYPTION_ENABLED` - Enable Server Side encryption, default to `false`. It is strongly recommended to enable Server Side encryption, especially on a shared redis instances.
- `SERVER_SIDE_ENCRYPTION_KEY` - Server side 32-byte encryption key, there is no default. You can generate one by command `openssl rand -base64 32` or using this container directly by setting `GENERATE_KEY` to `true`, e.g.:

```shell
docker run -e GENERATE_KEY=true gas85/ownpastebin:latest
```

- `TLS_KEY` - Provide path to TLS Key to enable TLS Support directly on a service.
- `TLS_CERT` - Provide path to TLS Certificate to enable TLS Support directly on a service.
- `DATE_FORMAT` - You can modify logs date format, default value is `%Y-%m-%d %H:%M:%S`.
- `TZ` - Time Zone.

## Run it

## 📦 Pastebin API

## 🚀 Create Paste

### POST `/`

Create a new paste.

#### Supported Content Types

Basically any, but you can set it explicitly.

- `application/json`
- `application/x-www-form-urlencoded`
- `multipart/form-data`
- raw body (`--data-binary`)

#### Query Parameters

| Name      | Type | Description                    |
|-----------|------|--------------------------------|
| `ttl`     | int  | Time to live (seconds)         |
| `burn`    | bool | Delete after first read        |
| `encrypt` | bool | Content was e2e encrypted. This helps UI to offer password prompt on content request. |

#### Examples

##### Raw upload

```shell
curl "http://localhost:8000" --data-binary "@file.txt"
```

##### With burn + TTL 60 Seconds

```shell
curl "http://localhost:8000?burn=true&ttl=60" --data-binary "@file.txt"
```

#### Response

As Response you will get JSON with URL to the webUI for this paste and paste ID that you can use e.g. to call `/raw` endpoint.

```json
{
  "url": "http://localhost:8000/abc123",
  "id": "abc123"
}
```

Additionally `Location` Header will be set:

```plain
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

If query `burn=true` was set upon paste creation:

- First request returns content
- Second request will show `404 Not Found` as content is deleted

## ⚠️ Limits

- Max paste size: configurable (`MAX_PASTE_SIZE`)
- Large uploads return:

```plain
413 Paste too large
```

You can also limit POST request size on your nginx or Apache2 in front of the service.

## 🧾 Logging

All requests are logged in following format:

```plain
2026-03-18 21:30:21 - INFO - uvicorn.access - 192.168.1.1:36854 - "POST / HTTP/1.1" 201
```

## 🛠️ Notes

- Supports `curl`, browsers, and API clients
- Designed for simplicity and speed
- Backed by Redis
- Inspired by [Pastebin](https://github.com/mkaczanowski/pastebin)
