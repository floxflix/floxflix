# Roadmap — Pluggable persistence (MongoDB or embedded SQLite)

**Goal:** let FloxFlix run against either a MongoDB service or an embedded SQLite file, chosen with one
environment variable, without forking a single handler. This is the prerequisite for the
`floxflix:standalone` image in [`ALL_IN_ONE_IMAGE_ROADMAP.md`](ALL_IN_ONE_IMAGE_ROADMAP.md) §9 — the
persistence layer has to be swappable before a second engine can exist at all.

Status: proposal / not implemented.

---

## Problem or motivation

FloxFlix requires a MongoDB instance to do anything: `cmd/server/main.go` calls `database.Connect(cfg.MongoURI)`
and `log.Fatalf`s if it fails. `MONGO_URI` defaults to `mongodb://localhost:27017/floxflix`, so on a machine
without Mongo the server dies during startup — and `docker compose` runs Mongo as a second service.

That is the right default for a real deployment, but it is a hard wall for the audience this project actually
attracts:

- **Evaluating it** costs `docker compose up` plus a Mongo container before the login page renders.
- **Single-board / home-server / k8s-small installs** pay for a full document database to hold what is, today,
  a few thousand rows: users, viewers, catalog items, genres, cast.
- **Dev containers and CI** need a database service just to exercise handlers.
- **Backups** mean `mongodump` for anyone without Mongo tooling, when a single file copy would do.

The all-in-one image roadmap ([`ALL_IN_ONE_IMAGE_ROADMAP.md`](ALL_IN_ONE_IMAGE_ROADMAP.md) §9) deliberately
keeps Mongo out of the image and suggests, if a self-contained build is ever wanted, "a separate
`floxflix:standalone` tag with an embedded database and a loud *development and evaluation only* banner — and a
`MONGO_URI` that still overrides it." That promise is the reason the storage port has to come first.

Today it is not. The Mongo driver is the application's data layer:

| Where | Coupling |
| --- | --- |
| `internal/handlers/*.go`, `internal/middleware/jwt.go`, `internal/queue/queue.go` | **40** `database.Collection(client, "…")` call sites across 9 files |
| `cmd/server/main.go` | `*mongo.Client` is threaded into every `Register*Routes`, `queue.New`, `middleware.JWT` |
| `internal/handlers/health.go` | readiness is literally `client.Ping()` |
| `internal/database/mongo.go` | `EnsureIndexes` is Mongo-specific `IndexModel` setup |

So "just add SQLite" is not a driver swap; it is a missing abstraction.

## Proposed solution

Introduce a storage interface, then implement it twice — Mongo (current behaviour, unchanged) and SQLite
(embedded, zero-config). Keep Mongo as the default `DB_ENGINE` so nothing breaks on upgrade.

### 1. Define the port, not the adapter

Extract the operations the handlers actually perform — no more — into `internal/store`:

- `Catalog`, `Users`, `Viewers`, `Genres`, `Cast`, `TokenBlacklist` repositories
- methods mirroring today's queries: `List(filter, sort, skip, limit)`, `Count`, `GetByUUID`, `Create`,
  `UpdateByID`, `DeleteByUUID`, `ExistsUsernameOrEmail`, `AddEpisode(...)`
- keep the **`bson.M` filter/sort types out of the interface signature**; use plain Go structs
  (`ListOptions`, `CatalogFilter`) so SQLite is not forced to emulate Mongo query syntax.

`internal/database/mongo.go` becomes the Mongo adapter; handlers take a `store.Store` instead of
`*mongo.Client`. The readiness probe becomes `store.Ping(ctx)`.

### 2. SQLite adapter

- Driver: **`modernc.org/sqlite`** (pure Go). Both `Dockerfile` and `Dockerfile.api` build with
  `CGO_ENABLED=0`; `mattn/go-sqlite3` would force cgo back into the image and break the multi-arch buildx
  build (`platforms: linux/amd64,linux/arm64` in `release-images.yml`, with `TARGETOS`/`TARGETARCH` passed to
  `go build`).
- Schema in versioned migrations (`internal/store/sqlite/migrations`), one table per collection, nested
  `seasons[].episodes[]` stored as JSON columns (they are always read and written as a whole document — see
  `internal/handlers/videos.go`, which already updates them with `$push` + `arrayFilters`).
- Pragmas at open: `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=on`, `synchronous=NORMAL`.
  One writer (the ffmpeg queue worker) and many readers is exactly WAL's shape.
- `DB_ENGINE=sqlite`, `SQLITE_PATH=./data/floxflix.db` (created with `0750`, alongside `UPLOAD_FOLDER`).
  `DB_ENGINE=mongo` is the default and still reaches Mongo only through `MONGO_URI`.

### 3. Prove both adapters behave the same

A single table-driven **conformance suite** parameterised by `store.Store`, run against SQLite in normal `go test`,
and against Mongo only when `MONGO_TEST_URI` is set (CI gets a service container). This is the only thing that
keeps two adapters honest long term, and today there is no DB-level test at all.

### 4. Documentation and migration

- `README` configuration table: `DB_ENGINE`, `SQLITE_PATH`, and `MONGO_URI` marked "only when `DB_ENGINE=mongo`".
- `floxflix migrate --from mongo --to ./data/floxflix.db` (or a small `cmd/` tool) so an existing library
  moves without hand-export, and the reverse for anyone who outgrows SQLite.
- When **not** to use SQLite, documented here: multiple API replicas sharing one file (network filesystems +
  SQLite locking), or a library large enough that scans stop fitting the page cache. Mongo stays the answer
  in both cases.

### Acceptance criteria

- [ ] `DB_ENGINE=mongo` builds and behaves exactly as today; existing tests pass unchanged.
- [ ] `DB_ENGINE=sqlite go run ./cmd/server` starts, migrates, serves the full API and streams HLS from an
      empty `./data` directory with no Mongo present.
- [ ] Conformance suite green against both backends.
- [ ] `/healthz/ready` reflects the configured backend.
- [ ] Import/export tool round-trips a real catalog, users, viewers and genres.
- [ ] No handler imports `go.mongodb.org/mongo-driver` anymore (grep-enforced in CI).
- [ ] Docs updated; `docker compose` still documents Mongo as the production path.

## Alternatives considered

**TinyDB** — the shape the original request had in mind: a file on disk, no server, no daemon. The problem is
that it is a Python library with a Python object API, so nothing in it is portable to Go. Its *spirit* (a file,
zero services) is what the SQLite adapter delivers, but a document/JSON store is the wrong shape for the queries
already in the codebase: pagination with `sort` + `skip`/`limit`, `$regex` title search
(`internal/handlers/videos.go`), unique-index enforcement on `uuid`/`username`/`email`, and conditional array
updates. Doing those in application code over a JSON blob means rewriting the same logic with worse durability
guarantees.

**`bbolt` / `Badger`** — genuinely Go-embedded and faster for key/value, but they are not query engines: every
filter, sort and count becomes hand-written index code. SQLite gets those from SQL, has a real query planner,
and the schema doubles as documentation.

**FerretDB (MongoDB wire protocol)** — the closest thing to "MongoDB-compatible" and the cheapest code change
(zero: point `MONGO_URI` at it). But it does not remove the second service, which is the entire motivation.
As of FerretDB 2.x the SQLite backend is gone and it runs on PostgreSQL + the DocumentDB extension — verify the
current version before relying on this. Worth documenting as a deployment option for people who want the Mongo
protocol without MongoDB, not as an embedded mode.

**Do nothing / document "use compose"** — already the position in
[`ALL_IN_ONE_IMAGE_ROADMAP.md`](ALL_IN_ONE_IMAGE_ROADMAP.md) §9, and it is defensible for production. It just
doesn't fix evaluation, CI or the standalone tag, and §9 already commits to the standalone path in principle.

## Additional context

### What the queries actually need (today's surface)

`Find` with `bson.M` filters, `FindOne`, `FindOneAndUpdate`, `UpdateOne` with `$set`, `$push` into
`seasons.$[s].episodes` (array filters), `$or` over `username`/`email`, `$regex` on titles, `CountDocuments`,
`DeleteOne`/`DeleteMany`, `skip`/`limit` + sort, unique indexes, `_id` as `ObjectID` looked up from `uuid`.

Nothing here needs Mongo-specific features (no transactions, no sessions, no change streams, no aggregation
pipelines) — which is why a second adapter is realistic rather than a rewrite.

### Two problems found while surveying (fix in this work, not after)

1. `internal/database/mongo.go` creates indexes for a `categories` collection that no handler uses, while
   **`genres` (5 call sites) has no unique index on `uuid`**. Either rename the index block or drop it; the
   SQLite schema should make the intent explicit.
2. [`TELEGRAM_INGEST_ROADMAP.md`](TELEGRAM_INGEST_ROADMAP.md) §3.8 wants the conversion queue moved into a
   `jobs` collection (`FindOneAndUpdate` claim, attempts, progress). Design `store.Store` so that collection
   can land without a second refactor — otherwise this interface gets written twice.

### Rough sequencing (one PR each)

1. Port + Mongo adapter + conformance suite. No behaviour change, no new dependency. *Reviewable on its own.*
2. SQLite adapter + migrations + `DB_ENGINE`/`SQLITE_PATH` config + tests.
3. `migrate` tool, docs, and (follow-up issue) the `floxflix:standalone` image tag from §9.

### Open questions

- Default for `DB_ENGINE`: keep `mongo` (safe, no surprise) or flip to `sqlite` for source runs only?
- One file per install, or a directory (`./data/db/`) so WAL sidecars and future attachments stay grouped?
- Does the `standalone` image ship in the same `Dockerfile` as a build arg, or as its own tag built in
  `release-images.yml`?
- Is a Postgres adapter wanted later, or does "Mongo for scale, SQLite for single-node" cover it?

### References

- [`ALL_IN_ONE_IMAGE_ROADMAP.md`](ALL_IN_ONE_IMAGE_ROADMAP.md) §9 — why Mongo is not bundled, and the
  standalone-tag escape hatch
- [`TELEGRAM_INGEST_ROADMAP.md`](TELEGRAM_INGEST_ROADMAP.md) §6 — the `jobs` collection this abstraction
  should anticipate
- [`AUTH_AND_MEDIA_SECURITY_ROADMAP.md`](AUTH_AND_MEDIA_SECURITY_ROADMAP.md) — owner/account model that any
  schema has to preserve across a migration
- [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) — cgo-free SQLite driver
- [SQLite WAL mode](https://sqlite.org/wal.html)
