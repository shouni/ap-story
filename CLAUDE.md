# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

**ap-story** is an MCP-capable manga/comic generation orchestrator service (Go, Cloud Run + Cloud Tasks). It turns a manuscript (URL or text) into chapters → script → character design sheets → panels → pages as async jobs, persisting artifacts and a state document (`comic_state.json`) to GCS. All actual generation logic (script, design sheets, panels, pages) is delegated to the external [`go-comic-kit`](https://github.com/shouni/go-comic-kit) library — this repo's job is job management, async execution, history, and API/MCP exposure, nothing else.

Two callers hit the same JSON API: browsers via Google OAuth session, and AI agents (e.g. Claude) via service-account OIDC (M2M).

## Common commands

```bash
go build ./...                    # build
go vet ./...                      # vet
gofmt -l .                        # check formatting (CI fails if non-empty)
go test -race ./...               # run all tests (race detector, as CI does)
go test -race ./internal/pipeline/...   # run tests for one package
go test -race -run TestNewJobIDFormat ./internal/domain/...  # run a single test
golangci-lint run                 # lint (config: .golangci.yml; CI pins golangci-lint v2.12.2)
govulncheck ./...                 # vulnerability scan (as CI does)
```

There is no Makefile — use `go` and `golangci-lint` directly. CI (`.github/workflows/ci.yml`) runs build, vet, gofmt check, `go test -race`, golangci-lint, and govulncheck as separate jobs on push/PR to `main`/`develop`.

## Architecture

### Layering

```
main.go → internal/config → internal/server → internal/builder → internal/app.Container
                                                      │
                        assets (embedded HTML templates, CSS/JS, prompt templates)
                        internal/adapters (external SDKs: Gemini, Slack, Cloud Tasks, characters.json)
                        internal/pipeline (Worker: Task → Steps → go-comic-kit Ops)
                        internal/repository (GCS comic_state.json history: list/get/delete)
                        internal/domain (framework-free core: Task, JobID, ports/interfaces)
                        internal/server/handlers (HTTP handlers, shared by JSON API and HTML views)
```

- **`internal/domain`** is dependency-free core logic: `Task` (job submission/validation), job ID generation/validation (`job_id.go` — job IDs are the single point HTTP input and GCS paths are sanitized through; always route new ID-consuming code through `ValidateJobID`/`SanitizeJobID`. Minting delegates to `jobid.New("c")` from `go-utils`, producing `c-{date}-{time}-{hex12}`; IDs minted before that change used `c{date}-{time}-{hex8}` and stay in GCS forever, so **never sort job IDs lexically** — `-` sorts below `2`, which would park every new job behind every legacy one. Use `jobid.SortKey`, as `history_listing.go` and `character_history.go` do), and the `TaskQueue`/`ComicRepository`/`Notifier` interfaces (`ports.go`) that other layers implement/consume. Add new cross-cutting interfaces here, not in `app`.
- **`internal/app.Container`** is the DI container — a plain struct of already-constructed dependencies (RemoteIO, `go-comic-kit` `Ops`, pipeline `Runner`, `TaskQueue`, `ComicRepository`, character definitions, HTTP client). It holds no construction logic itself.
- **`internal/builder`** contains the factory functions that build `Container` (`app.go`: `BuildContainer`) and the HTTP handler bundle (`handlers.go`: `BuildHandlers` → `AppHandlers{Auth, Web, Worker, M2M}`). This is where config values turn into live clients (GCS, Gemini, Slack, Cloud Tasks). If a resource fails to init partway through `BuildContainer`, already-created closers are cleaned up via the deferred error path — follow that pattern when adding a new external resource.
- **`internal/pipeline`** is the Worker side: `Runner.Run` takes a `domain.Task`, resolves output directories (job's `comics/{jobID}`, `design-jobs/{jobID}`, shared `character/`), asks a `Planner` for the ordered `Step` list for the task's command, and executes steps sequentially against `go-comic-kit` `Operations`. Steps live in `step_*.go` (one per pipeline stage: script, design, panel, page, state). `Runner.New` validates required deps at construction time (fail fast at startup, not mid-job). Notification (Slack) failures never fail the job — they're logged only.
- **`internal/repository`** reads/writes `comic_state.json` on GCS directly (no database). History listing enumerates GCS prefixes without reading full state for unselected pages, and uses a TTL cache (`ttlcache`, `NewHistoryCache`) for history summaries.
- **`internal/server`**: `router.go` wires chi routes. Two parallel auth paths share one middleware (`protectedAccessMiddleware`): it first tries M2M (OIDC Bearer) verification, and falls back to browser session (cookie + CSRF) if that's not attempted. Worker routes (`/tasks/generate`) are OIDC-verified separately as Cloud Tasks calls, not user-facing.
- **`internal/server/handlers`**: JSON API handlers and HTML view handlers share the same underlying logic — only the response format differs. Files are one concern each and named for it: `{resource}_handler.go` for HTML screens, `{resource}_api_handler.go` for JSON. When adding a capability that needs both a page and an endpoint, follow the existing split (e.g. `comics_compose_handler.go` renders and submits the form while `comics_handler.go` serves the same submission as JSON, both going through `newComposeTask`). The package stays a single package on purpose — ap-comp and ap-mv organise theirs the same way, and every handler shares the eight dependencies held by `Handler`.
- **`internal/adapters`**: thin wrappers around external SDKs — `ai.go` (Gemini/`go-comic-kit` operations wiring), `characters.go` (loads `go-character-kit` characters.json from GCS or local, falls back to the kit's embedded defaults), `slack.go` (Notifier impl), `tasks.go` (Cloud Tasks enqueuer). `adapters/prompts` implements the kit's prompt ports and parses the templates; the template files themselves live in `assets/prompts/`.
- **`assets`**: every embedded resource that is edited on a different cycle from the code — HTML templates, CSS/JS, prompt templates — matching ap-comp and ap-mv. It holds embeds and path constants only; loading and parsing belong to the consumer (`internal/server/handlers`, `internal/adapters/prompts`). Keep it that way: ap-comp's equivalent package has accumulated six files of logic and stopped being a single-purpose location. Co-locating an embed with its consumer is still right for something one package owns privately — `go-comic-kit`'s `internal/prompts/templates` is the contrast.

### Async job model

Generation is multi-stage and long-running (minutes to ~10+ min), so everything runs as a Cloud Tasks job, never inline in a request:

1. `POST /api/comics` (or `/api/design-sheets`) mints a job ID and enqueues a Cloud Tasks task, returning the job ID immediately.
2. The worker (`POST /tasks/generate`) receives the task and runs `pipeline.Runner`, which executes `go-comic-kit` operations and saves state (`comic_state.json`) to GCS.
3. Clients poll `GET /api/comics/{jobID}` to read state/progress.

Idempotency: Cloud Tasks task names include job ID + stage to dedupe enqueues; state saves are always full overwrites (`store.Save` semantics from `go-comic-kit`), so re-running a step is always safe.

`render_comic` is the deliberate exception — its task name carries the submission time instead. Resuming means re-submitting the same job, and Cloud Tasks keeps rejecting a name for a while after the task finishes, so a deterministic name would swallow exactly the retry the command exists for. Re-running is cheap because generated panels are skipped.

State is saved at every phase boundary, and `Runner.savePartialResults` saves whatever exists before reporting a step failure, on a context detached from the caller's — being cut off by `PIPELINE_TIMEOUT` is precisely when the results are worth keeping. Without it a run that died on panel 40 of 64 left those images in GCS referenced by nothing.

A Cloud Tasks redelivery therefore resumes rather than restarts: `compose_comic` begins with `LoadStateIfExistsStep`, and each phase skips what is already done — `OutlineStep` returns early when chapters exist, `AllChapterScriptsStep` skips chapters that already have panels (regenerating one would replace them and drop their generation records), and the batch steps run with `SkipGenerated`. This is safe because `compose_comic` is only ever submitted with a freshly minted job ID, so an existing state means a re-run. Keep that property when adding a phase: a step in this plan must be a no-op when its output is already present.

Pipeline commands (`Task.command`, dispatched by `Planner`): `compose_comic` (full run: outline → per-chapter script → all panels → all pages; does **not** include design sheets — those are generated separately via `generate_design_sheet`), `render_comic`, `regenerate_chapter_script`, `generate_design_sheet`, `regenerate_panel`, `regenerate_page`.

`compose_comic` with `stop_after_script` ends after the script so the result can be reviewed before paying for an image per panel; `render_comic` then fills in the panels and pages that are missing. Because it skips what already exists, the same command also serves as the resume path, and the detail page offers it under one button whose wording follows which case applies.

Panels and pages go through the kit's batch operations (`Ops.PanelBatch` / `Ops.PageBatch`), which run at `MAX_CONCURRENCY` (default 1, i.e. serial). Those return partial successes alongside an error, so `AllPanelsStep` / `AllPagesStep` assign the returned state even when the call fails — dropping it would strip the images that were just generated and paid for.

### GCS layout

```
gs://{STORY_BUCKET}/
├── comics/{jobID}/                    # per-work-job prefix
│   ├── comic_state.json               # MangaState — single source of truth for a job
│   └── images/
│       ├── panel_{panelID}.png
│       └── comic_page_{N}.png
├── design-jobs/{jobID}/
│   └── comic_state.json               # state for standalone design-sheet-only jobs (images live under character/)
├── character/{tag}/{jobID}.png        # design sheets — shared, work-independent assets
└── character-reference/{characterID}/... # human-curated master reference images (separate from AI-generated character/)
```

Character design sheets are stored outside `comics/{jobID}/` because a character can be reused across multiple works; `{tag}` is the character ID (or concatenated IDs for a composite sheet), and `{jobID}` is unique per generation call so regenerating a character never overwrites prior results. History listing enumerates `comics/*/comic_state.json`; design-sheet-only jobs live under `design-jobs/` instead and are surfaced via `/characters/{characterID}` rather than the main history list. Images are never served directly by the app — all image routes 302-redirect to a GCS signed URL. **Any code that turns a job ID into an HTTP path or GCS path must go through `domain.ValidateJobID`/`SanitizeJobID`** (see `internal/domain/job_id.go`).

### Config

`internal/config` loads all settings from environment variables via `caarlos0/env`, grouped into `ServerConfig`, `GCPConfig`, `TasksConfig`, `StorageConfig`, `NotificationConfig`, `AIConfig` (mapped to `go-comic-kit`'s `ports.Config` via `AIConfig.KitConfig()`), and `AuthConfig` (Google OAuth for browser + allowed M2M service accounts for agent calls). Everything Cloud Tasks touches lives in `TasksConfig` (queue, worker URL, audience, allowed issuers) — the same grouping ap-mv and ap-comp use. Two settings there are easily confused: `GCPConfig.ServiceAccountEmail` is the SA that **signs** an outgoing task (an enqueuer-side setting, so web-only), while `TasksConfig.AllowedServiceAccounts` is the receiver's allowlist of **issuers**; read it through `Config.TaskIssuers()`, which falls back to the signer when the allowlist is empty. `cfg.ValidateEssentialConfig()` is checked at startup in `main.go` before the server runs — extend it, don't add ad-hoc runtime checks, when a new required setting is introduced. `cfg.WarnOnContradictoryGenerationSettings()` runs alongside it for combinations that are legal but self-defeating: throughput is bounded by `1/RATE_INTERVAL` regardless of `MAX_CONCURRENCY`, and a `REQUEST_TIMEOUT` shorter than one image aborts generation rather than protecting it.

## Conventions worth knowing

- Comments and many identifiers/docstrings in this codebase are in Japanese; match the existing language when editing a file that's already documented in Japanese.
- Interfaces are defined where they're consumed (`internal/domain/ports.go`), and implementations assert conformance with `var _ domain.X = (*Impl)(nil)` (see `repository.go`). Follow this when adding a new adapter/repository implementation.
- Constructors that assemble multi-dependency structs (`pipeline.New`, `app.BuildContainer`) validate required fields at construction and return an error immediately rather than letting a nil dependency panic later at request time.
