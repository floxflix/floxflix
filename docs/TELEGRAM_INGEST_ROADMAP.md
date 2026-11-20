# Roadmap — Telegram ingestion for FloxFlix

**Goal:** send a video file (or a link) to a Telegram bot, and have it land in the catalog, transcoded to
HLS by the existing conversion queue, with progress and completion reported back in the chat — without
touching the server by hand.

Status: **partially implemented, across two repositories.** The bot is a standalone service
([`floxflix-telegram-bot`](https://github.com/floxflix/floxflix-telegram-bot)) that writes into the shared
`uploads` volume and registers what it saved through the `/v1/api/bot/*` routes. Uploading a file,
collecting metadata and publishing it to the catalog work end to end. Queue persistence, duplicate
protection, progress reporting and the hardening in §3 do not. Owner: @elvus.

---

## 1. What exists today

| Piece | Where | State |
| --- | --- | --- |
| Bot service: wizard, disk layout, de-duplicating file names | `floxflix-telegram-bot` (`internal/bot`, `internal/storage`) | working |
| Local Bot API server (2 GB, `getFile` → local path, `os.Rename`) | `docker-compose.yml` in the bot repo, `aiogram/telegram-bot-api` | working |
| Catalog ingest endpoints | `internal/handlers/videos.go:167-169`, `internal/handlers/genres.go:28` | working, **unauthenticated** |
| Shared storage | `videos-pvc` mounted in both deployments (`k8s/deployment.yaml`, bot `k8s/deployment-telegram-bot.yaml`) | working |
| Bot API token / allowlist | `FLOXFLIX_ALLOWED_USER_IDS` (numeric Telegram IDs) | working, all-or-nothing |
| Edit / replace / delete existing items | — | **not implemented**, create-only — tracked in the bot repo's [`ROADMAP.md`](https://github.com/floxflix/floxflix-telegram-bot/blob/main/ROADMAP.md) |
| Persisted conversion queue | `internal/queue/queue.go` (`jobs []Job`) | **not done**, in memory |
| Duplicate protection, free-space check, progress, `/status` `/queue` `/cancel` | — | **not done** |

The flow is no longer "the API reaches into Telegram". It is two processes and one volume:

```
Telegram client ──sends file──▶ local Bot API server ──writes──▶ telegram-data volume
                                       ▲                              │ os.Rename (same host)
                                       │ getFile → absolute path      ▼
                              floxflix-telegram-bot ──▶ uploads/<type>/<title>/[S01/E01/]<file>
                                       │
                                       │ POST /v1/api/bot/videos[/{uuid}/new-episode]
                                       ▼
                              floxflix API ──▶ catalog row (In-Progress) ──▶ queue ──▶ HLS + thumbnail
```

The bot owns the conversation and the download; the API owns the catalog and the transcode. Neither
imports the other, so the contract between them is the JSON of `catalog.go` and the response of the bot
routes — and that contract has no test on the API side (the drift risk is in §7).

## 2. Where this diverged from the original plan

The first version of this document proposed an in-process `internal/telegram` + `internal/ingest`
package. The split below is the better fit for a bot that must keep working while the API restarts, so
this section exists to stop the old plan from being re-litigated.

| Original plan | Shipped | Consequence |
| --- | --- | --- |
| `internal/ingest.FromFile()` shared with `handleUploadVideo` | Bot writes the file itself and calls `POST /v1/api/bot/videos` | No refactor of the 240-line upload handler; the HTTP body is the contract |
| `bg_image` required, blocking Telegram | Optional in `POST /v1/api/bot/videos`; `queue.go:336` falls back to the generated `thumbnail.jpg` | Resolved — the wizard still asks for one, but it accepts a URL |
| Filename parser (`Show.S01E02.1080p…`), caption overrides, inline confirmation, TMDB | Guided wizard asking title, description, year, genre, rating, backdrop, season/episode, skip-intro times | No inference and no TMDB key, but every field is user-supplied and validated; no parser regressions to chase |
| `go-telegram/bot` with a custom API base URL | `go-telegram-bot-api/telegram-bot-api/v5` with `NewBotAPIWithAPIEndpoint` | Same requirement satisfied; no reason to swap |
| Sessions in Mongo with a TTL index ("must survive a restart") | In-memory map, 1 h TTL, swept on write (`internal/bot/session.go`) | A restart mid-wizard loses the conversation. Acceptable for a single-admin tool, but it contradicts the old rule — see §3.8 |
| `/link` codes + `telegram_links` collection | Static `FLOXFLIX_ALLOWED_USER_IDS` | No per-user attribution: every upload is "the admin's". Fine while the bot is single-tenant |
| Read-timeout fix as a blocking prerequisite | Done: `main.go:89` has `ReadHeaderTimeout` and no `ReadTimeout` | Closed |
| Progress edits, `/download`, quotas, webhooks | — | Still ahead (§4) |

## 3. Open issues, in the order they should be fixed

### 3.1 `/v1/api/bot/*` is unauthenticated and publicly routable — fix first

The routes are registered outside the JWT group and the ingress publishes `/` on `api.floxflix.io`
(`k8s/ingress.yaml`), so anyone on the internet can `POST /v1/api/bot/videos` with a `file_path` under
`UPLOAD_FOLDER`, write catalog rows, and enqueue ffmpeg runs. That is both a data-integrity hole and a
cheap way to fill the disk.

`README.md` already warns about this; a warning is not a control. Recommended: a shared secret header
(`X-Bot-Token`) compared in constant time by a middleware on the `/v1/api/bot/*` group, configured as
`BOT_API_TOKEN` in the API and in the bot's config. It costs one env var per side, works over the
in-cluster HTTP the bot already uses, and fails closed when unset. Network policy or an ingress location
block is a second layer, not the first one.

### 3.2 The bot token sits in plaintext in two files

`FLOXFLIX_BOT_TOKEN` is written literally in `.env` and `k8s/secret-telegram-bot.yaml`. Both are
gitignored and were never committed (`git log --all -- k8s .env` is empty), so there is no history to
purge and no forced rotation. Treat it as sensitive anyway: rotate via BotFather if either file has ever
been copied off the machine, and keep the token out of the manifest — a SealedSecret, SOPS, or an
externally managed Secret. The same file also hard-codes the numeric admin ID.

### 3.3 Skip-intro and next-episode times all come from the start time

`internal/bot/catalog.go:136-138` fills `IntroStartTime`, `IntroEndTime` and `NextEpisodeTime` from
`sess.SkipIntroStartTime`. `sess.SkipIntroEndTime` and `sess.NextEpisode` are collected by the wizard
(`handlers.go:450-465`) and then discarded, so every episode is published with a zero-length skip-intro
window and the next-episode button appears at the intro start.

The repo's own test already catches it — `go test ./...` in the bot repo is red:

```
--- FAIL: TestBuildCatalogItemPutsTheEpisodeInsideTheSeason
    times = "00:00:30"/"00:00:30"/"00:00:30", want hh:mm:ss
--- FAIL: TestFlowsEndOnTheFileStep
    the tv-show flow ends on stage 16, want the file
```

The second failure is a stale expectation, not a bug: `tvShowFlow` now ends on
`stageAwaitingNextEpisodeFile` so a season can be sent in a row, and the test still asserts
`stageAwaitingFile`. Fix the three field assignments and update that assertion.

### 3.4 An empty `FLOXFLIX_API_URL` silently turns the bot into a disk-only tool

Publishing is best-effort by design: a failed catalog call is logged and reported in the chat so the file
is never lost. But when `FLOXFLIX_API_URL` is empty the bot returns early without telling anyone
(`internal/bot/catalog.go:157-160` and `:180-183`): the file is saved, no catalog row is created, and the
user only sees "✅ Saved to: …".

`.env.example` and the bot README now document the variable and the `FLOXFLIX_` / `TELEGRAM_API_*`
split in one file (both used to show bare `BOT_TOKEN`, `ALLOWED_USER_IDS`, `TELEGRAM_API_ENDPOINT`, so
copying the example ended in `BOT_TOKEN is empty`). What remains is the silent mode itself: warn at
startup, or refuse to start unless `FLOXFLIX_API_URL` is set.

### 3.5 The bot image declares a user that does not exist

`Dockerfile:28` ends with `USER floxflix`, but the runtime stage never creates the account (only
`apk add ca-certificates tzdata`). Kubernetes masks it with `runAsUser: 101`; a plain `docker run` of the
published image fails at startup. Add `addgroup -S floxflix && adduser -S -G floxflix floxflix`, or drop
the `USER` line and document the UID.

### 3.6 No duplicate protection

`update_id` is ignored and `FileUniqueID` is only used to invent a fallback filename
(`internal/bot/media.go`). A redelivered update, or the same file sent twice, downloads the media again,
lands it as `name (1).mkv` via `storage.UniquePath`, and publishes a second catalog row (and a second
ffmpeg job). Add an `ingested_files` collection keyed by `file_unique_id` (TTL or permanent), check it
before acknowledging, and reply "already in your library" with the existing item.

### 3.7 No size or free-space preflight

The bot never looks at `Document.FileSize` / `Video.FileSize`, and never checks free space on
`DOWNLOAD_DIR`. A 1.4 GB episode plus its HLS output plus the local server's copy is roughly 3 GB for one
upload; `telegram-data` is a 20 Gi PVC and `videos` is shared with the streaming server. Compare the
declared size against a `FLOXFLIX_MAX_FILE_SIZE`, require ~2.5× free space before starting, and refuse
with a clear message instead of failing halfway through.

### 3.8 Nothing watches the transcode

The bot reports "Saved to: …" and "Catalog updated", then goes quiet; the item stays `In-Progress` in the
UI until ffmpeg finishes. The queue itself is still the in-memory `[]Job` from
`internal/queue/queue.go`, so a restart silently drops pending work and leaves those rows `In-Progress`
forever. Doing progress properly needs, in order: the persisted `jobs` collection (§6), a read endpoint
the bot can poll (`GET /v1/api/bot/jobs/{id}`, or reuse `queue/info`), `-progress pipe:1` parsing in the
worker, and throttled message edits on the bot side. Until then, a `/status <uuid>` that reads the
catalog row is a cheap interim.

### 3.9 The web upload path still parses the request body twice

Independent of Telegram: `internal/handlers/videos.go:697-713` calls `ParseMultipartForm` twice, the second
time with a 2 MB `MaxBytesReader` on a body that is already consumed (and a comment that says 5 MB). Any
multipart upload that reaches it behaves unpredictably. The bot avoids this route entirely, which is why
it has not been noticed. Delete the second block and validate the poster from its `*multipart.FileHeader`.

### 3.10 Two sanitizers with different rules

`internal/bot.SanitizeFilename` (dots → `_`, lowercased) and `internal/storage.SanitizeName` (dots and
case kept) apply different rules to the same input. Folder and file names agree today because only the
first one builds paths; anything that later uses the second one to build a path gets a surprise.

## 4. Remaining phases

**Phase A — close the holes.** §3.1 bot-route auth, §3.2 secret handling, §3.3 the time fields and the
red tests, §3.4 the silent no-publish mode, §3.5 image user. No new features; this is what makes the
existing feature safe to leave running.

**Phase B — make it trustworthy.** §3.6 duplicate protection, §3.7 size/space preflight, persisted queue
(§6), `/status` and `/cancel`. A failed ingest should be explainable and retryable from the chat, not
from `kubectl logs`.

**Phase C — feedback.** Job progress (`-progress pipe:1` + `ffprobe` duration), message edits throttled to
one every 10–15 s with an edit skipped when the percentage has not moved, 429/`retry_after` backoff,
completion message with thumbnail and deep link, failure message with the last ~15 lines of ffmpeg stderr
and a [Retry] button. Rate limits are the constraint here, not ffmpeg.

**Phase D — optional.** `/download <url>` with the SSRF rules below; webhook mode; a metadata provider as
a *prefill* for the wizard (the wizard stays authoritative, so a provider outage can never block an
upload); the MTProto route (option B below) only if pulling existing channel archives becomes a goal.

### Editing: the bot is create-only

Creating works (movie, show + first episode, append episode); changing anything afterwards does not. The
gap, the API pieces missing to close it and the ordering constraint (bot-route auth first, §3.1) are
tracked in the bot repository's [`ROADMAP.md`](https://github.com/floxflix/floxflix-telegram-bot/blob/main/ROADMAP.md)
rather than duplicated here. The one part that matters on this side: an episode PATCH twin under
`/v1/api/bot/*` is the minimum needed to repair the skip-intro values already published by §3.3, and it
carries the same authentication requirement as every other bot write route.

### `/download <url>` is an SSRF sink — treat it as hostile input

- scheme allowlist (`https` only by default), resolve the hostname and reject the request if any resolved
  IP is loopback / link-local / private / CGNAT / IPv6 ULA / `169.254.169.254`;
- re-validate after **every** redirect (cap at 3) and pin the connection to the validated IP;
- hard caps on `Content-Length`, bytes written and wall clock; stream to disk, never to memory;
- optional host allowlist for a paranoid deployment; off by default behind `URL_INGEST_ENABLED`.

## 5. The 20 MB constraint (why the local server is not optional)

The public Bot API lets a bot download **at most 20 MB** via `getFile`; there is no chunking and no
ranges. The options have not changed:

| Option | Max size | Cost | Status |
| --- | --- | --- | --- |
| **A. Self-hosted local Bot API server** (`--local`) | **no download limit**, 2000 MB upload | one container + `api_id`/`api_hash` | **shipped** |
| B. MTProto user client (`gotd/td`) | 2 GB (4 GB Premium) | a user session to protect | escape hatch, not built |
| C. Bot receives a URL, server fetches it | n/a | least code, most abuse surface | Phase D, not built |

For A the two plumbing details still matter and are satisfied today: both deployments mount
`telegram-data` at the **same absolute path** (`/var/lib/telegram-bot-api`), and `videos-pvc` reaches the
API at `/app/videos` (`UPLOAD_FOLDER=videos/`) and the bot at `/app/videos` (`FLOXFLIX_DOWNLOAD_DIR`).
Both claims are `local-path`, so the rename stays on one filesystem — a storage class change would turn
every ingest into a 2 GB copy. When the path does not resolve — cloud API, or a volume that is not
mounted — the bot falls back to an HTTP download and logs it; that path leaves a second copy in the local
server's cache.

## 6. Data model additions

- `jobs` — the persisted conversion queue: `status`, `attempts`, `last_error`, `progress`, `source`
  (`http`\|`telegram`\|`url`), `worker_id`, `claimed_at`; claim with
  `FindOneAndUpdate({status: pending}, {$set: {status: processing, …}})` and reset orphaned `processing`
  rows on boot. This is the dependency for Phase C and is useful with or without Telegram.
- `ingested_files` — `{file_unique_id (unique), content_uuid, season, episode, ingested_at}` for §3.6.
- `catalog` — `source` / `source_ref`, so a row can say it came from Telegram (or a URL) and be traced
  back to the chat.
- `telegram_links` / `telegram_sessions` — only if the bot ever grows per-user accounts or restarts
  mid-conversation matter enough to persist the wizard. Not needed for the single-admin deployment.

## 7. Risks and open decisions

| Risk | Mitigation |
| --- | --- |
| Open `/v1/api/bot/*` on a public ingress (§3.1) | Shared-secret middleware before anything else; network policy as a second layer |
| Bot and API drift apart (two repos, one JSON contract) | A contract test in the API for the exact body the bot sends; version the route if it must change |
| Disk exhaustion: source + HLS + local server cache | Preflight (§3.7), delete the source after a successful transcode (already done by `processJob`), orphan sweeper |
| A 40-minute transcode lost to a restart | Persisted queue (§6); today a restart loses the job and the row stays `In-Progress` |
| Redelivered updates creating duplicates | `file_unique_id` ledger (§3.6) |
| Telegram rate limits muting the bot | Only relevant once edits exist: throttle, backoff on 429/`retry_after` |
| Secrets in `.env` / k8s manifests | External secret management; rotate if either file left the machine |
| Clients recompress video sent "as video" | Bot should say "send as file"; `.mkv` always arrives as a Document |

**Open decisions:** does the bot stay single-admin (static allowlist) or become per-user with `/link`
codes and `telegram_links`? Is the queue's persistence a prerequisite of this roadmap or of the API's own
reliability work (it probably belongs to both)? Should the bot keep owning disk layout, or should the API
expose an upload endpoint so the layout rules live in `internal/storage` only?

## 8. Suggested sequencing

Phase A is one PR in each repo and is worth doing before Phase B precisely because it is small: the bot
is already running against a public ingress with an unauthenticated write path.

1. **A** — bot-route auth, secret handling, §3.3 fix, §3.4 warning, image user. *No new features.*
2. **B** — dedupe ledger, size/space preflight, `/status` + `/cancel`, persisted queue.
3. **C** — progress, completion/failure messages, retry.
4. **D** — `/download`, webhook mode, metadata prefill, MTProto.

Keep `internal/telegram` free of Mongo and ffmpeg if the package ever moves back into this repo; the bot
translating Telegram updates into `POST /v1/api/bot/videos` bodies is the same boundary, one process
further away.

---

### References

- [`floxflix-telegram-bot`](https://github.com/floxflix/floxflix-telegram-bot) — the bot service
- [`ALL_IN_ONE_IMAGE_ROADMAP.md`](ALL_IN_ONE_IMAGE_ROADMAP.md) — the shared `uploads` volume and image layout
- [`EMBEDDED_DATABASE_ROADMAP.md`](EMBEDDED_DATABASE_ROADMAP.md) — `jobs` is one of the collections the
  storage port has to accommodate
- [Bot API — getFile](https://core.telegram.org/bots/api#getfile) · [Bots FAQ](https://core.telegram.org/bots/faq)
- [tdlib/telegram-bot-api](https://github.com/tdlib/telegram-bot-api) — local server, `--local`
- [Telegram — Uploading and Downloading Files](https://core.telegram.org/api/files) (MTProto path)
