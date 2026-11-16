# Changelog

All notable changes to FloxFlix are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `ACCESS_TOKEN_TTL` and `REFRESH_TOKEN_TTL` make token lifetimes configurable
  (Go durations; defaults unchanged at `1h` and `720h`). Malformed values fail
  startup instead of silently falling back to the defaults.

### Changed

- `POST /v1/api/auth/token/refresh` rotates the session: it issues a new refresh
  token alongside the access token and resets both cookies, so a device in
  regular use no longer has to log in 30 days after its first login. The
  response now also carries `refresh_token`. The token presented on the request
  is not revoked, so parallel tabs refreshing at the same time don't invalidate
  each other.
- Dev container builds the UI on creation
  (`go mod download && npm install --prefix web && npm run build --prefix web`).
  Together with the `SERVE_UI=true` and `UI_DIR=./web/dist` it already sets, the
  API serves the built SPA from its own port during development. The UI
  submodule is bumped.

## [1.0.2] - 2026-11-13

### Changed

- **The MongoDB database name comes from the connection string.** The hardcoded
  `floxflix` database is gone: the name is parsed from `MONGO_URI`, and a URI
  without one now fails startup with an explicit error instead of silently
  using a fixed database. Add the name to URIs that lack it, e.g.
  `mongodb://localhost:27017/floxflix`.

### Fixed

- `GET /v1/api/videos/{vtype}/list` and
  `GET /v1/api/videos/{id}/season/{season}` return seasons and episodes sorted
  by number, matching `GET /v1/api/videos/{id}/details`, so clients no longer
  depend on insertion order.

## [1.0.1] - 2026-11-13

### Added

- The dev container installs Node and npm and runs the API with
  `SERVE_UI=true` and `UI_DIR=./web/dist`, so the UI is served from the API port
  during development.

### Changed

- The SPA shell (`index.html`) is served from the same filesystem as the rest
  of the UI, so `UI_DIR` and the built submodule now cover `/` as well.
- `openapi.yml` documents the image, genre, cast, bot and `/config.js` routes,
  making it the complete contract for every mounted path.

### Fixed

- `GET /v1/api/videos/{id}/details` returns a show's seasons and episodes sorted
  by number instead of insertion order.

### Removed

- `floxflix.postman_collection.json`. `openapi.yml` is the single API contract,
  and the README and Contributing Guide no longer point at a collection.

## [1.0.0] - 2026-11-12

The upload folder gets a defined layout, and the paths stored in MongoDB become
relative to it. Existing installations need both a file move and a database
migration.

### Breaking

- **Upload folder layout changed.** Content used to be written to
  `<UPLOAD_FOLDER>/<title>/` for movies and `<UPLOAD_FOLDER>/<title>/S01/E02/`
  for episodes, with movies and shows sharing one namespace. They are now
  separated under `movies/<title>/` and `tv-shows/<title>/S01/E02/`. Files from
  an earlier version must be moved into the new tree.
- **Stored paths are now relative to `UPLOAD_FOLDER`.** `hls_path`, `bg_image`
  and the queue's `output_dir` no longer carry the upload folder prefix, so a
  playlist recorded as `uploads/the_movie/the_movie.m3u8` is now
  `movies/the_movie/the_movie.m3u8`. Catalog documents written by an earlier
  version keep the old absolute-style values and must be rewritten. The upload
  root is joined on only at the point where the filesystem is touched, so
  moving `UPLOAD_FOLDER` no longer invalidates the catalog.
- **`GET /v1/api/videos/stream/*` and `GET /v1/api/videos/image/*` take a path
  relative to the upload root.** The old handlers required the request path to
  begin with the upload folder name; that prefix is now rejected. Clients that
  build URLs from the catalog's `hls_path` and `bg_image` fields keep working,
  since those fields moved to the same relative form.
- **A missing file now returns net/http's plain-text 404** instead of the
  `{"status": "failed", "message": "File not found"}` body, because serving
  goes through `http.ServeFileFS` rather than an explicit `os.Stat` check.
- **`JWT_SECRET_KEY` is now required.** The server no longer falls back to the
  hardcoded `change-me-in-production` key; `config.Load()` returns an error and
  the process exits non-zero when the variable is unset or shorter than 32
  bytes. Generate one with `openssl rand -base64 48`. ([#1])
- **`GET /v1/api/videos/queue/info` now requires authentication.** The route
  moved inside the `JWTAuth` group, and the response is sanitized:
  `input_path` and `output_dir` are reduced to their base filename and `error`
  is truncated to 200 characters, so host filesystem layout and raw FFmpeg
  output no longer leak. ([#9])
- **Default port changed from `5000` to `7777`**, in `.env.example`,
  `docker-compose.yaml`, the Dockerfile, the devcontainer and `openapi.yml`.
- **Auth cookies default to `Secure` with `SameSite=Lax`.** Plain-HTTP
  deployments must now set `COOKIE_SECURE=false` explicitly, and
  `COOKIE_SAMESITE=none` is rejected unless `COOKIE_SECURE=true`.
- **MongoDB is no longer published on host port `27017`** by
  `docker-compose.yaml`; it is reachable only on the compose network.
- **`swagger.yaml` renamed to `openapi.yml`.**
- **Project renamed from FlixFlox to FloxFlix.** The Go module path changed to
  `github.com/elvis/floxflix`, the default MongoDB database from `flixflox` to
  `floxflix`, and the default `MONGO_URI` with it. A catalog in an existing
  `flixflox` database must be renamed or the URI pointed at the old name.

### Added

- JWT authentication with access and refresh tokens delivered as HTTP-only
  cookies.
- User accounts and Netflix-style viewer profiles.
- Movie and TV show catalog with seasons, episodes and metadata.
- Multipart upload of source files with automatic FFmpeg-driven HLS conversion
  in a background worker queue, plus queue status, start and cleanup endpoints.
- HLS streaming endpoint serving `.m3u8`, `.ts`, `.m4s` and `.mp4`, and a
  poster/background image endpoint.
- Health, ping and readiness probes.
- MongoDB persistence with index setup on startup.
- OpenAPI specification and a Postman collection.
- Dockerfile and `docker-compose.yaml` for local runs.
- Cast and genre resources, with their own handlers, models and MongoDB
  indexes.
- Background image upload and serving via `GET /v1/api/videos/image/*`.
  `bg_image` is optional on upload and falls back to the thumbnail generated by
  the conversion worker.
- Per-title custom message for the skip-intro button.
- `PUT /v1/api/videos/{uuid}/season/{season}/episode/{episode}` to update a
  single episode.
- `COOKIE_SECURE` and `COOKIE_SAMESITE` configuration, with tests covering
  parsing and the cookies the auth handlers set.
- `duration_seconds` is now probed with `ffprobe` and written to the catalog
  entry, alongside new playlist and probe helpers and their tests.
- JSON fallback responses for unknown paths and unsupported methods, instead of
  the router's plain-text defaults.
- Devcontainer setup and a `.dockerignore`.
- Project documentation: `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
  `SECURITY.md`, GitHub issue templates and the roadmap documents under
  `docs/`.
- `internal/storage`, a package that owns the on-disk layout of the upload
  folder: `MovieLayout`, `EpisodeLayout`, `PlaylistFile`, `SegmentPattern`,
  `Resolve` and `SanitizeFilename`. Handlers and the conversion queue now build
  every path through it instead of each assembling its own.
- Registration endpoints for files already placed in `UPLOAD_FOLDER`, so an
  external ingester can hand work to the queue without re-uploading:
  `POST /v1/api/videos` and `POST /v1/api/videos/{id}/new-episode` resolve the
  relative source paths before writing anything and return the number of jobs
  queued.
- Telegram bot ingest integration, with unauthenticated `/v1/api/bot/*` routes
  (`videos`, `videos/{id}/new-episode`, `titles`, `genres`) and a dedicated
  endpoint for adding an episode to an existing show.
- All-in-one image that serves the API, the embedded React UI and `/config.js`
  from one port, plus an API-only `Dockerfile.api`, a `SERVE_UI`/`UI_DIR` pair
  for UI serving, `VITE_*` runtime UI configuration and a GHCR release workflow
  for `v*` tags.

### Changed

- Path traversal in the streaming and background-image handlers is prevented by
  `os.OpenRoot`, which confines serving to the upload directory at the OS level
  and also blocks symlink escapes. It replaces a string-prefix comparison on
  the cleaned request path.
- Uploads are written through a shared `saveUploadedFile` helper using
  `io.Copy`, replacing two copies of a hand-rolled 32 KiB read/write loop that
  could leak a file handle on a mid-copy write error.
- Episode metadata parsing and the video extension check are shared between
  `handleUploadVideo` and `handleAddEpisode` instead of being duplicated.
- Poster images are stored against the show or movie directory rather than a
  path derived from the title separately from the rest of the layout, and a
  failed poster save now returns the standard JSON error shape instead of
  `http.Error`'s plain text.
- The conversion queue job pipeline was reworked around the new probe and
  playlist helpers.
- Catalog create and upload responses return the item's UUID in `id` instead of
  the MongoDB `ObjectID`, so callers get the identifier the rest of the API
  accepts.
- The `CORS_ORIGIN` list is trimmed and empty entries are dropped, so an unset
  origin disables the middleware instead of matching an empty `Origin` header.
- Upgraded Go module dependencies and the MongoDB image to `8.2.7`.
- README documents the upload folder layout, the `internal/storage` package,
  the one-image deployment and the bot ingest routes.

### Fixed

- **Refresh tokens no longer authenticate protected routes.** `JWTAuth` never
  checked `token_type` and fell back from the `access_token` cookie to
  `refresh_token`, so the long-lived refresh cookie kept working after the
  access cookie expired. Each middleware now reads only its own cookie and
  requires the matching token type. ([#2])
- A failed conversion stores only the last 15 lines (at most 2 KiB) of ffmpeg's
  output in the job's `error`, instead of the entire output. That error is
  served by `GET /v1/api/videos/queue/info`. The full output is still logged.
- Large uploads no longer time out: the HTTP server uses `ReadHeaderTimeout`
  instead of `ReadTimeout`, which capped the whole request body.
- MP3 audio is never stream-copied into HLS segments. fMP4/CMAF expects AAC, so
  copying MP3 produced silent playback in Safari; MP3 now always re-encodes to
  AAC. ([#10])
- Thumbnails are seeked at roughly 10% of the probed duration using an
  input-side `-ss`, replacing the hardcoded 5s offset that often landed on black
  frames or logos. ([#11])
- `handleAddEpisode` no longer mishandles episodes added to an existing show.
- `duration_seconds` was declared on the catalog model but never written.

[Unreleased]: https://github.com/elvis-segovia/floxflix/compare/v1.0.2...HEAD
[1.0.2]: https://github.com/elvis-segovia/floxflix/compare/v1.0.1...v1.0.2
[1.0.1]: https://github.com/elvis-segovia/floxflix/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/elvis-segovia/floxflix/releases/tag/v1.0.0
[#1]: https://github.com/elvis-segovia/floxflix/issues/1
[#2]: https://github.com/elvis-segovia/floxflix/issues/2
[#9]: https://github.com/elvis-segovia/floxflix/issues/9
[#10]: https://github.com/elvis-segovia/floxflix/issues/10
[#11]: https://github.com/elvis-segovia/floxflix/issues/11
