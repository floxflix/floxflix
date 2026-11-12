# FloxFlix

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Go 1.26](https://img.shields.io/badge/Go-1.26-00add8?logo=go&logoColor=white)](https://go.dev/)
[![MongoDB](https://img.shields.io/badge/MongoDB-8-47a248?logo=mongodb&logoColor=white)](https://www.mongodb.com/)
[![FFmpeg](https://img.shields.io/badge/FFmpeg-HLS-007808?logo=ffmpeg&logoColor=white)](https://ffmpeg.org/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ed?logo=docker&logoColor=white)](#running-with-docker)
![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)

Self-hosted video streaming server written in Go. It ingests source video files, transcodes them to HLS via FFmpeg in a background worker queue, and serves the resulting catalog (movies and TV shows) to authenticated users with per-account viewer profiles. A single all-in-one image bundles the React UI and this API behind one port.

## Features

- One image, one port: the Go server serves the API and the embedded React UI from the same origin
- JWT-based authentication with access/refresh tokens delivered as HTTP-only cookies
- User and viewer-profile management (Netflix-style multi-profile per account)
- Movie and TV show catalog with seasons, episodes and rich metadata
- Multipart upload for source files with automatic FFmpeg-driven HLS conversion
- Background conversion queue with status, start and cleanup endpoints
- HLS streaming endpoint serving `.m3u8`, `.ts`, `.m4s` and `.mp4`
- Health, ping and readiness probes
- Consistent JSON error responses for unknown paths and unsupported methods
- MongoDB persistence

## Tech stack

- Go 1.26
- [chi](https://github.com/go-chi/chi) router
- MongoDB (driver v2)
- [golang-jwt](https://github.com/golang-jwt/jwt) for JWT
- FFmpeg (system binary, used by the conversion worker)
- React + Vite + Ant Design UI (bundled from the `web/` submodule)

## Project layout

```
cmd/server/          Application entrypoint
internal/config/     Environment configuration loader
internal/database/   MongoDB client and index setup
internal/handlers/   HTTP handlers (auth, users, viewers, videos, health, fallback)
internal/middleware/ JWT auth, refresh and CORS middleware
internal/models/     Domain models (User, Viewer, CatalogItem, ...)
internal/queue/      FFmpeg conversion queue
internal/storage/    Upload folder layout (paths relative to UPLOAD_FOLDER)
internal/utils/      JSON helpers, password hashing, etc.
internal/web/        Embedded SPA assets and static-file handler
web/                 UI git submodule (React + Vite)
uploads/             Default upload + HLS output directory
  movies/<title>/                 Movie source, poster, playlist and segments
  tv-shows/<title>/               Show poster
  tv-shows/<title>/S01/E02/       Episode source, playlist and segments
openapi.yml          OpenAPI 3.0 specification
```

## Configuration

Configuration is loaded from environment variables. A starting point is provided in `.env.example`.

| Variable           | Default                                  | Description                                      |
| ------------------ | ---------------------------------------- | ------------------------------------------------ |
| `MONGO_URI`        | `mongodb://localhost:27017/floxflix`     | MongoDB connection string                        |
| `JWT_SECRET_KEY`   | _(required)_                             | Secret used to sign JWTs. No default: the server refuses to start unless it is set and at least 32 bytes long. Generate one with `openssl rand -base64 48`. |
| `CORS_ORIGIN`      | _(empty)_                                | Allowed CORS origin(s), comma-separated. Empty disables the middleware; the all-in-one image is same-origin so it needs nothing here. |
| `UPLOAD_FOLDER`    | `./uploads`                              | Directory for uploaded files and HLS output      |
| `PORT`             | `7777`                                   | HTTP listen port                                 |
| `HLS_SEGMENT_TIME` | `10`                                     | HLS segment duration in seconds                  |
| `HLS_LIST_SIZE`    | `0`                                      | HLS playlist size (0 = unlimited)                |
| `HLS_SEGMENT_TYPE` | `fmp4`                                   | HLS segment type (`fmp4` or `mpegts`)            |
| `MAX_FILE_SIZE`    | `2147483648`                             | Max upload size in bytes (default 2 GiB)         |
| `COOKIE_SECURE`    | `true`                                   | Auth cookie `Secure` flag. Set `false` for plain-HTTP LAN deploys (browsers drop `Secure` cookies over `http://` except localhost). TLS in front of the API is the better answer. |
| `COOKIE_SAMESITE`  | `lax`                                    | Auth cookie `SameSite` (`lax`, `strict`, or `none`). `none` requires `COOKIE_SECURE=true`. |
| `SERVE_UI`         | `true`                                   | Serve the embedded UI. Set `false` for API-only mode from the same image. |
| `UI_DIR`           | _(unset)_                                | Serve the SPA from this directory instead of the embedded build (local UI iteration). |
| `VITE_STREAMAPI_URL` | _(empty)_                              | API origin injected into `/config.js`. Empty means same origin; set only to point the UI at another host. |
| `VITE_STREAMAPI_PREFIX` | `/v1/api`                            | API path prefix injected into `/config.js`.      |
| `VITE_STREAMAPI_PREFIX_ADMIN` | `/dashboard`                     | Admin route prefix injected into `/config.js`.   |
| `VITE_DEFAULT_NEXT_EPISODE_OFFSET` | `15`                          | Next-episode countdown in seconds.               |

## Running locally

Prerequisites: Go 1.26+, MongoDB, FFmpeg available on `PATH`.

```bash
cp .env.example .env
# Nothing in the app parses .env — the config loader reads the process
# environment, so export the variables into your shell first.
set -a && source .env && set +a
export JWT_SECRET_KEY=$(openssl rand -base64 48)
go mod download
go run ./cmd/server
```

The API will listen on `http://localhost:7777`. If you run the API from source, the UI is the committed
placeholder until you build it into `internal/web/dist` (or point `UI_DIR` at `web/dist`):

```bash
git submodule update --init --recursive
(cd web && npm ci && npm run build)
UI_DIR=./web/dist go run ./cmd/server
```

## Running with Docker

The default image is all-in-one: the Go binary serves the API, the UI and `/config.js` from one port.
Two build files are provided: `Dockerfile` (all-in-one) and `Dockerfile.api` (API only).

```bash
# The image refuses to start without a strong signing key.
export JWT_SECRET_KEY=$(openssl rand -base64 48)
docker compose up --build
```

Then open <http://localhost:7777>. MongoDB stays a separate service (the compose file starts it), with
persistent volumes for uploads and database data.

The UI source is a git submodule at `web/`, so clone with `--recursive` (or run
`git submodule update --init` before `docker build`). The submodule commit pins the exact UI version
baked into the image.

### Deployment options

| Option | What it runs | When to use |
| --- | --- | --- |
| All-in-one (default) | One container: UI + API (`Dockerfile`) | Normal self-hosting. One URL, no CORS, no version skew. |
| API-only | One container: API only (`Dockerfile.api`) | Smallest artifact when the UI is served elsewhere. No Node/UI build stage, `SERVE_UI=false` baked in. |
| Split images | `floxflix` API + the standalone `floxflix-ui` image | Scale or CDN the UI separately. Set `CORS_ORIGIN` and `VITE_STREAMAPI_URL` on the UI. |
| `SERVE_UI=false` | Same all-in-one image, API only at runtime | When you want one artifact but the UI is served elsewhere. |

Build the images directly without compose:

```bash
# All-in-one (UI + API)
docker build -t floxflix:all-in-one .

# API only
docker build -t floxflix:api -f Dockerfile.api .

docker run --rm -p 7777:7777 \
  -e JWT_SECRET_KEY="$(openssl rand -base64 48)" \
  -e MONGO_URI=mongodb://host.docker.internal:27017/floxflix \
  -v floxflix-uploads:/app/uploads \
  floxflix:all-in-one
```

The API-only image serves no UI: `/` and unknown non-API paths return JSON `404`, and `/config.js` is
not registered. To run it with compose (on port 7778, so it can coexist with the default service):

```bash
export JWT_SECRET_KEY=$(openssl rand -base64 48)
docker compose --profile api-only up api
```

### Published images

Pushing a `v*` tag runs [`.github/workflows/release-images.yml`](./.github/workflows/release-images.yml), which builds both images for `linux/amd64` and `linux/arm64` and pushes them to GHCR:

- `ghcr.io/<owner>/<repo>:<tag>` — all-in-one
- `ghcr.io/<owner>/<repo>-api:<tag>` — API only

For the `floxflix/floxflix` repository those are `ghcr.io/floxflix/floxflix` and `ghcr.io/floxflix/floxflix-api`. A `v1.2.3` tag produces `1.2.3`, `1.2`, `v1.2.3`, `sha-<short>` and `latest`; prerelease tags such as `v1.2.3-rc1` do not move `latest`.

On first run there are no users: open `/register` (or `POST /v1/api/auth/register`) to create the first
account, then log in.

The container runs as non-root UID/GID `10001`. The named `uploads` volume inherits that ownership on
first start; if you bind-mount an existing host directory instead, make it writable by that UID
(`sudo chown -R 10001:10001 ./uploads`). Kubernetes deployments need a matching `fsGroup: 10001`.

## API

All routes are mounted under the `/v1/api` prefix, except for the health probes which live under `/healthz`. The UI shell is served from `/`, hashed assets from `/assets/*`, and runtime UI configuration from `/config.js`.

| Group     | Prefix                | Auth                     |
| --------- | --------------------- | ------------------------ |
| Health    | `/healthz`            | Public                   |
| Auth      | `/v1/api/auth`        | Mixed (login/register public, others require JWT) |
| Users     | `/v1/api/users`       | JWT                      |
| Viewers   | `/v1/api/viewers`     | JWT, scoped to caller    |
| Videos    | `/v1/api/videos`      | Read public, write JWT   |
| UI        | `/`, `/assets/*`, `/config.js` | Public          |

The full machine-readable contract is in [`openapi.yml`](./openapi.yml). Open it with any OpenAPI viewer (Swagger UI, Redoc, Stoplight, etc.) to explore endpoints, request bodies and response schemas.

### Error responses

Errors are always JSON, never HTML, so clients can parse a single shape for every failure:

```json
{ "status": "failed", "message": "Not found" }
```

Requests that do not match a route are handled by a router-level fallback instead of chi's plain-text defaults. API paths (`/v1/api/*` and `/healthz*`) always keep the JSON shape below; other paths with `Accept: text/html` are served the SPA shell so client-side routes survive a reload.

| Case                                            | Status | Message              |
| ----------------------------------------------- | ------ | -------------------- |
| Unknown API path                                 | `404`  | `Not found`          |
| Known API path, method not registered            | `405`  | `Method not allowed` |
| Unknown UI path with `Accept: text/html`         | `200`  | SPA shell (`index.html`) |
| Missing hashed asset (`/assets/*`)               | `404`  | plain 404            |

### Authentication

`POST /v1/api/auth/login` and `POST /v1/api/auth/register` are public. They set `access_token` and `refresh_token` HTTP-only cookies and also return the tokens in the JSON body so non-browser clients can use the `Authorization: Bearer <token>` header.

`POST /v1/api/auth/token/refresh` requires a valid refresh token. Logout (`DELETE /v1/api/auth/logout`) blacklists the current token's `jti`.

### Streaming

`GET /v1/api/videos/stream/<path>` serves files from inside `UPLOAD_FOLDER`. The handler sets the appropriate `Content-Type` for HLS playlists and segments and rejects paths that escape the upload directory.

### Uploads and conversion

`POST /v1/api/videos/upload` accepts a `multipart/form-data` body with:

- `file` — the source video (`.mp4`, `.avi`, `.flv`, `.mkv`, `.mov`, `.wmv`, `.webm`)
- `type` — `movie` or `tvshow`
- `values` — JSON string with title, release year, rating, description, genre, cast
- `metadata` — JSON string with `season`, `episode`, `title` (TV shows only)

The file is saved to disk, a catalog entry is created, and a job is pushed onto the conversion queue. Adding more episodes to an existing TV show uses `PUT /v1/api/videos/{id}/new-episode`.

Queue control endpoints (all require JWT):

- `GET  /v1/api/videos/queue/info` — current queue snapshot, with job paths reduced to their base filename and error messages truncated
- `POST /v1/api/videos/queue/start` — kick the worker if idle
- `POST /v1/api/videos/queue/cleanup` — drop completed/failed jobs

## Contributing

Contributions are welcome! Please read the [Contributing Guide](./CONTRIBUTING.md) for setup instructions, conventions, and how to submit changes. Bug reports and feature requests use the issue templates, and security issues should be reported privately per the [Security Policy](./SECURITY.md).

## License

Released under the [MIT License](./LICENSE).
