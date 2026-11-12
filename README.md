# FloxFlix

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Go 1.26](https://img.shields.io/badge/Go-1.26-00add8?logo=go&logoColor=white)](https://go.dev/)
[![MongoDB](https://img.shields.io/badge/MongoDB-8-47a248?logo=mongodb&logoColor=white)](https://www.mongodb.com/)
[![FFmpeg](https://img.shields.io/badge/FFmpeg-HLS-007808?logo=ffmpeg&logoColor=white)](https://ffmpeg.org/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ed?logo=docker&logoColor=white)](#running-with-docker)
[![GHCR](https://img.shields.io/badge/GHCR-floxflix%2Ffloxflix-181717?logo=github&logoColor=white)](https://github.com/floxflix/floxflix/pkgs/container/floxflix)
![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)

Self-hosted video streaming server written in Go. It ingests source video files, transcodes them to HLS via FFmpeg in a background worker queue, and serves the resulting catalog (movies and TV shows) to authenticated users with per-account viewer profiles. A single all-in-one image bundles the React UI and this API behind one port.

## Features

- One image, one port: the Go server serves the API and the embedded React UI from the same origin
- JWT-based authentication with access/refresh tokens delivered as HTTP-only cookies
- User and viewer-profile management (Netflix-style multi-profile per account)
- Movie and TV show catalog with seasons, episodes and rich metadata
- Genre and cast collections, managed through their own CRUD endpoints
- Multipart upload for source files with automatic FFmpeg-driven HLS conversion
- Registration endpoints for files already placed in the upload folder, so an external ingester (such as a Telegram bot) can hand work to the queue without re-uploading
- Background conversion queue with status, start and cleanup endpoints
- HLS streaming endpoint serving `.m3u8`, `.ts`, `.m4s` and `.mp4`, plus a poster/background image endpoint
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
internal/handlers/   HTTP handlers (auth, users, viewers, videos, genres, cast,
                     health, runtime config, fallback)
internal/middleware/ JWT auth, refresh and CORS middleware
internal/models/     Domain models (User, Viewer, CatalogItem, Genre, Cast, ...)
internal/queue/      FFmpeg conversion queue
internal/storage/    Upload folder layout (paths relative to UPLOAD_FOLDER)
internal/utils/      JSON helpers, password hashing, etc.
internal/web/        Embedded SPA assets (go:embed) and static-file handler
  dist/                           Committed placeholder; the real build is
                                  baked in by the all-in-one Dockerfile
web/                 UI git submodule (React + Vite)
docs/                Design and roadmap notes (adaptive streaming, Telegram ingest, ...)
.devcontainer/       VS Code dev container definition
.github/workflows/   GHCR release-images pipeline (runs on v* tags)
Dockerfile           All-in-one image (UI + API)
Dockerfile.api       API-only image
docker-compose.yaml  API + MongoDB for local runs
openapi.yml          OpenAPI 3.0 specification
floxflix.postman_collection.json  Postman collection for the API
```

`web/` is a submodule, so it is empty until `git submodule update --init --recursive`. The upload folder (`UPLOAD_FOLDER`, `./uploads` by default) is created at runtime and is not part of the repository; its layout is described under [Uploads and conversion](#uploads-and-conversion).

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

### Published images (GHCR)

Released images are published to the GitHub Container Registry, built for `linux/amd64` and
`linux/arm64`:

| Image | Contents |
| --- | --- |
| `ghcr.io/floxflix/floxflix` | All-in-one (UI + API), from `Dockerfile` |
| `ghcr.io/floxflix/floxflix-api` | API only, from `Dockerfile.api` |

Run the published all-in-one image without cloning anything:

```bash
docker pull ghcr.io/floxflix/floxflix:latest

docker run --rm -p 7777:7777 \
  -e JWT_SECRET_KEY="$(openssl rand -base64 48)" \
  -e MONGO_URI=mongodb://host.docker.internal:27017/floxflix \
  -v floxflix-uploads:/app/uploads \
  ghcr.io/floxflix/floxflix:latest
```

To run them through compose instead of building locally, drop the service's `build:` block and point
`image:` at the registry:

```yaml
services:
  floxflix:
    image: ghcr.io/floxflix/floxflix:latest   # or :1.2.3
    # no build: — compose pulls the published image
```

The packages are public, so `docker pull` needs no login. Pulling from a private fork does:
`echo "$GITHUB_TOKEN" | docker login ghcr.io -u <user> --password-stdin`.

**Tags.** Pushing a `v*` tag runs [`.github/workflows/release-images.yml`](./.github/workflows/release-images.yml), which builds and pushes both images. A `v1.2.3` tag produces `1.2.3`, `1.2`, `v1.2.3`, `sha-<short>` and `latest`; prerelease tags such as `v1.2.3-rc1` do not move `latest`. Pin a full version for anything you care about — `latest` moves under you on the next release.

In a fork the names follow the repository: `ghcr.io/<owner>/<repo>` and `ghcr.io/<owner>/<repo>-api`.

### First run

On first run there are no users: open `/register` (or `POST /v1/api/auth/register`) to create the first
account, then log in.

The container runs as non-root UID/GID `10001`. The named `uploads` volume inherits that ownership on
first start; if you bind-mount an existing host directory instead, make it writable by that UID
(`sudo chown -R 10001:10001 ./uploads`).

## API

All routes are mounted under the `/v1/api` prefix, except for the health probes which live under `/healthz`. The UI shell is served from `/`, hashed assets from `/assets/*`, and runtime UI configuration from `/config.js`.

| Group     | Prefix                | Auth                     |
| --------- | --------------------- | ------------------------ |
| Health    | `/healthz`            | Public                   |
| Auth      | `/v1/api/auth`        | Mixed (login/register public, others require JWT) |
| Users     | `/v1/api/users`       | JWT                      |
| Viewers   | `/v1/api/viewers`     | JWT, scoped to caller    |
| Videos    | `/v1/api/videos`      | Read public, write JWT   |
| Genres    | `/v1/api/genres`      | JWT                      |
| Cast      | `/v1/api/cast`        | JWT                      |
| Bot       | `/v1/api/bot`         | **None** — see [Bot ingest endpoints](#bot-ingest-endpoints) |
| UI        | `/`, `/assets/*`, `/config.js` | Public          |

[`openapi.yml`](./openapi.yml) is the machine-readable contract and covers every route listed above. Open it with any OpenAPI viewer (Swagger UI, Redoc, Stoplight, etc.) to explore endpoints, request bodies and response schemas, or import [`floxflix.postman_collection.json`](./floxflix.postman_collection.json) into Postman.

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

`POST /v1/api/auth/token/refresh` requires a valid refresh token. Logout (`DELETE /v1/api/auth/logout`) blacklists the current token's `jti`. `GET /v1/api/auth/check` validates the current access token and returns the caller's account.

Access tokens are rejected on the refresh route and refresh tokens are rejected on protected routes, so the two are not interchangeable.

### Streaming

`GET /v1/api/videos/stream/<path>` serves files from inside `UPLOAD_FOLDER`. The handler sets the appropriate `Content-Type` for HLS playlists and segments and rejects paths that escape the upload directory.

`GET /v1/api/videos/image/<path>` serves posters and background images from the same root, with the same path checks. Both take a path **relative to the upload root** — the value stored in the catalog's `file_path` and `bg_image` fields. Only `.jpg`, `.jpeg` and `.png` get an explicit image `Content-Type`.

### Catalog reads

| Endpoint                                      | Returns                                   |
| --------------------------------------------- | ----------------------------------------- |
| `GET /v1/api/videos`                           | The whole catalog                         |
| `GET /v1/api/videos/{vtype}/list`              | Catalog filtered by type (`movie`, `tvshow`) |
| `GET /v1/api/videos/{id}/details`              | One item with its full metadata           |
| `GET /v1/api/videos/{id}/season/{season}`      | One season of a show, with its episodes   |

### Uploads and conversion

There are two ways to get content into the catalog: **upload** the file through the API, or **register** a file that is already inside `UPLOAD_FOLDER`. Either way the files land in a fixed layout, and the paths stored in MongoDB (`file_path`, `bg_image`) are relative to the upload root. After conversion the queue rewrites `file_path` to the generated `.m3u8` playlist:

```
<UPLOAD_FOLDER>/
  movies/<title>/                 Movie source, poster, playlist and segments
  tv-shows/<title>/               Show poster
  tv-shows/<title>/S01/E02/       Episode source, playlist and segments
```

#### Upload

`POST /v1/api/videos/upload` accepts a `multipart/form-data` body with:

- `file` — the source video (`.mp4`, `.avi`, `.flv`, `.mkv`, `.mov`, `.wmv`, `.webm`)
- `type` — `movie` or `tvshow`
- `values` — JSON string with title, release year, rating, description, genre, cast
- `metadata` — JSON string with `season`, `episode`, `title` (TV shows only)

The file is saved to disk, a catalog entry is created, and a job is pushed onto the conversion queue. `PUT /v1/api/videos/{id}/new-episode` adds another episode to an existing show the same way, as multipart.

#### Register an existing file

`POST /v1/api/videos` takes a JSON catalog item whose source paths are **relative to the upload root** and whose files are already on disk. The paths are resolved before anything is written, so a bad path fails the request instead of leaving an entry stuck `In-Progress`. The response carries the new item's `uuid` and the number of jobs queued:

```json
{ "status": "success", "id": "6f1c…", "queued": 1 }
```

`POST /v1/api/videos/{id}/new-episode` is the same thing for one episode of an existing show: an episode body plus the `season` it belongs to and a `file_path` under the upload root.

#### Editing episode metadata

`PUT /v1/api/videos/{uuid}/season/{season}/episode/{episode}` patches a single episode in place — title, intro skip markers and the rest — without touching its media.

#### Queue control

Queue control endpoints (all require JWT):

- `GET  /v1/api/videos/queue/info` — current queue snapshot, with job paths reduced to their base filename and error messages truncated
- `POST /v1/api/videos/queue/start` — kick the worker if idle
- `POST /v1/api/videos/queue/cleanup` — drop completed/failed jobs

### Bot ingest endpoints

`/v1/api/bot/*` exists for an external ingester — currently the Telegram bot (see [`docs/TELEGRAM_INGEST_ROADMAP.md`](./docs/TELEGRAM_INGEST_ROADMAP.md)) — that drops files straight into `UPLOAD_FOLDER` and then tells the API about them. They are the same handlers as the JWT-protected catalog routes, registered a second time:

| Endpoint                                       | Equivalent protected route              |
| ---------------------------------------------- | --------------------------------------- |
| `POST /v1/api/bot/videos`                       | `POST /v1/api/videos`                   |
| `POST /v1/api/bot/videos/{id}/new-episode`      | `POST /v1/api/videos/{id}/new-episode`  |
| `GET  /v1/api/bot/titles?title=<q>`             | — (case-insensitive title search, used to find the show to append to) |
| `GET  /v1/api/bot/genres`                       | `GET /v1/api/genres`                    |

> **These routes carry no authentication.** Anyone who can reach them can write to the catalog and queue FFmpeg work. Keep `/v1/api/bot/*` on a private network, or block it at the reverse proxy and let only the bot through.

## Contributing

Contributions are welcome! Please read the [Contributing Guide](./CONTRIBUTING.md) for setup instructions, conventions, and how to submit changes. Bug reports and feature requests use the issue templates, and security issues should be reported privately per the [Security Policy](./SECURITY.md).

## License

Released under the [MIT License](./LICENSE).
