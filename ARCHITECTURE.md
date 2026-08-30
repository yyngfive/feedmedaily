# FeedMeDaily Architecture

## Overview

FeedMeDaily is a local-first literature triage app for journal RSS feeds. The current product is built around a Go backend service, a Windows tray runtime, a React review UI, a local SQLite database, and profile-driven LLM classification.

## Repository Structure

- `cmd/feedmedailyd/`: production Go backend entrypoint
- `cmd/feedmedaily-tray/`: Windows tray runtime entrypoint
- `cmd/feedmedaily-protected-verifier/`: Go native WebView2 helper for host-scoped protected-feed verification
- `internal/api/`: HTTP API handlers, job endpoints, verification endpoints, and static asset serving
- `internal/classifier/`: Go classifier client, provider adapter, prompt shaping, and bounded retry handling
- `internal/config/`: settings schema, fixed classifier model catalog, local config editing, and path resolution
- `internal/feeds/`: feed fetch client, generic RSS/Atom/RDF parser, and publisher-specific extractors
- `internal/jobs/`: sync pipeline, reclassify flows, and background job orchestration
- `internal/llmusage/`: thread-safe per-job LLM usage collection and immutable DeepSeek pricing snapshots
- `internal/metadata/`: conditional metadata enrichment via DOI/OpenAlex/Crossref lookups
- `internal/profile/`: profile validation, generation, and persistence helpers
- `internal/runtime/`: shared runtime paths, version, mode, process, and app metadata helpers
- `internal/store/sqlite/`: SQLite persistence for papers, classifications, feedback, proposals, Zotero status, and per-job LLM usage
- `internal/trayapp/`: tray lifecycle, scheduling, backend supervision, and autostart
- `internal/zotero/`: Zotero Web API integration
- `web/`: Vite + React + TypeScript frontend organized into app orchestration, feature modules, API/data adapters, and shared UI/types

## Production Flow

1. Feed subscriptions are stored in `data/rss_feeds.json`.
2. `feedmedailyd` fetches feed content through a layered Go pipeline: HTTP client, generic RSS/Atom/RDF parser, and publisher-specific extractors.
3. Papers are deduplicated and upserted into `data/literature.sqlite`.
4. Metadata enrichment runs only when core fields such as DOI, authors, journal, or usable abstract content are missing.
5. The classifier evaluates papers against the active `data/classification_profile.json`.
6. Classifications, feedback, profile proposals, Zotero save status, and completed-job LLM usage are persisted in SQLite.
7. `feedmedailyd` serves `web/dist` and exposes the local JSON API surface.
8. The API service keeps a long-lived SQLite handle open for request reuse, and the UI reads the latest report through `/api/report/latest`, rebuilt from SQLite-backed state rather than replayed from disk report snapshots.

## Runtime Surfaces

### Go backend service

`cmd/feedmedailyd/` is the supported local backend for source mode and packaged builds. It owns:

- app/runtime endpoints such as `/api/app/health`, `/api/app/meta`, `/api/app/update`, `/api/app/open`, and `/api/app/exit`
- settings endpoints for config, feeds, and scheduler
- report, feedback, paper-read, and profile proposal APIs
- profile bootstrap, proposal generation, proposal apply/reject, and reclassify flows
- Zotero recursive collection-tree listing and save flows
- admin job endpoints including sync and reclassify
- protected-feed verification start, callback, and completion endpoints
- structured job-progress payloads for fetch, metadata, classification, report refresh, and profile-generation status updates
- static React asset serving and SPA fallback

`Run Sync Now` is fully owned by Go end-to-end: feed fetch, ingest, conditional metadata enrichment, classification, report refresh, and background job state all run through `feedmedailyd`. The same `/api/admin/run` endpoint also accepts an optional saved-feed URL list so Dashboard can run a targeted manual sync without changing the stored subscriptions. Sync launch is single-flight across full and targeted runs: while a sync is queued, running, or waiting for verification, another launch request returns the existing job instead of starting a second pipeline. The Dashboard disables its Sync button while that active job is visible and exposes `Stop sync`; `POST /api/admin/jobs/{id}/cancel` propagates cancellation through the current feed/LLM context and verification wait, then records a terminal `cancelled` job without rolling back already persisted papers. The backend registry remains the concurrency authority for UI, tray, and concurrent API callers.

The job polling endpoints expose both human-readable messages and structured progress fields so the UI can show stage-aware status such as current feed `i/N`, metadata/classification completion percentages, step-based profile generation progress, and structured latest-job summaries. Sync warning details are read from the existing job result errors and matched back to the current feed list by URL.

Every LLM-backed job owns one explicit thread-safe usage collector and captures the current manual pricing settings when the job starts. Classifier batches, retries, single-paper degradation, title translation, profile generation, validation, JSON repair, and thinking fallbacks record each successful provider response exactly once, including its response timestamp. When the job reaches `completed`, `failed`, or `cancelled`, its token totals and pricing snapshot are copied into `JobInfo.llm_usage` and persisted as one `llm_usage_jobs` row. Ledger persistence failures are warnings and never change the business job outcome. Official DeepSeek requests use Beijing-time weekday peak/off-peak rates; official GLM-5.3-Flash requests use cached-input, ordinary-input, and output rates, with OpenAI-style cached-token details normalized by the adapter. Settings → Model exposes both providers in one compact pricing table; saved changes affect only later jobs because completed ledger rows keep their concrete rate snapshots. Unknown endpoints/models remain `unavailable`. Existing-database migrations and narrowly scoped historical DeepSeek pricing repairs run when the backend starts, before Dashboard opens its read-only store. Dashboard reads the last three days through `/api/admin/llm-usage`, while SQLite retains the full history.

The API service reuses long-lived SQLite stores across requests instead of reopening the database per handler. Read-heavy endpoints and mutation endpoints are split across separate store roles so the UI can keep `/api/report/latest` responsive while feedback or read-status writes are in flight. The SQLite runtime now enables WAL plus a busy-timeout-oriented connection string, and the `/api/report/latest` read path batch-selects each paper's latest classification, latest open feedback, and latest Zotero save state with SQL windowing rather than issuing per-paper follow-up lookups. `/api/app/update` keeps a short-lived in-memory status cache for routine polling and page initialization, but also accepts a force-refresh path so manual checks can bypass that cache immediately.

### Protected-feed verification

Cloudflare-protected or challenge-gated feeds use a Windows-first verification assist flow:

- a fetch job can move to `waiting_for_user` when the backend detects a challenge page or a Cloudflare-style `403`
- the UI opens a dedicated native WebView2 verifier helper with a persistent host-scoped profile under `data/verification-profiles/<host>`
- protected-feed requests are grouped by `verification_host`, so one verifier session can walk multiple feeds on the same host without reopening a fresh challenge flow for each feed
- once the protected page resolves to RSS/Atom/RDF content, the verifier POSTs the captured XML back to `/api/feeds/verification/callback`
- if the native helper cannot capture XML quickly, it posts a `needs_user` callback so the admin UI stays in explicit verification mode instead of appearing stuck on the last fetch item
- the backend resumes the paused fetch loop by injecting that XML into the normal Go feed parser without restarting already completed feeds
- if the verifier exits without captured XML, the job stays in `waiting_for_user` and the admin panel can switch that same verification into a system-browser fallback via `/api/feeds/verification/browser`
- browser fallback does not try to reuse browser cookies automatically; instead the user completes the challenge in their normal browser, copies the final raw RSS/Atom/RDF source, and submits it to `/api/feeds/verification/manual-submit`
- successful manual submissions are validated through the same feed parser path as normal fetches before the paused sync resumes

The backend also persists host-level verification session metadata in `data/verification-sessions.json`, including verifier kind, current session state, and the latest success/failure timestamps. That lets later sync runs try verified hosts optimistically first, then fall back to `needs_reverify` when a site challenges again.

The Go native helper in `cmd/feedmedaily-protected-verifier/` now serves as the default path for all protected hosts, not just ACS. The product treats verification as a host-scoped session rather than a single-feed callback, so the same verified WebView2 session can capture multiple same-host RSS responses and return those XML bodies to the active fetch loop. Feed fetching is internally sorted by host for execution only, preserving the user's saved feed list while allowing one host verification to cover the remaining same-host feeds; successful feeds are kept in memory for that sync and are not refetched after verification resumes. Helper diagnostics are written under `logs/protected-verifier/`, and the backend terminates a verifier process if its pending verification request times out so stale helper windows do not accumulate. Release builds package this helper as `{app}\FeedMeDailyProtectedVerifier\FeedMeDailyProtectedVerifier.exe`; source mode builds the same Go helper into `.tmp/runtime-bin/FeedMeDailyProtectedVerifier/FeedMeDailyProtectedVerifier.exe`.

Current limitation: even with a persistent verifier profile, some publisher challenges may still trust the user's full system browser more than an embedded WebView2 surface. The manual browser/XML fallback therefore remains part of the supported recovery path for protected feeds.

### Windows tray runtime

`cmd/feedmedaily-tray/` is the Windows app shell. It is responsible for:

- single-instance tray lifecycle
- ensuring the local backend is available while the tray is active
- opening the browser UI
- triggering `Run Sync Now`
- launch-at-login
- tray-owned local daily scheduling

Packaged builds ship the tray executable as the primary desktop entrypoint. In source mode, the tray builds or launches the local Go backend as needed.

New scheduler settings default to local time `12:30`, which falls in DeepSeek's current midday off-peak window when the machine uses China Standard Time; users in other time zones can override it. Existing saved scheduler settings remain authoritative. The Web scheduler form uses the same fallback so the tray, API, and UI do not present different first-run times.

## Data Model And State

### User-local state

These files are local machine state and must not be committed:

- `.env`
- `data/classification_profile.json`
- `data/literature.sqlite`
- `data/rss_feeds.json`

### LLM roles

There are two configurable model roles:

- classifier models: paper relevance classification (`deepseek-v4-flash`, `glm-5.3-flash`, `qwen3.8-flash`, and `mimo-v2.5`)
- profile model: onboarding profile generation and feedback-driven profile revision

The classifier catalog owns provider endpoints and supported thinking contracts. The global `SCIRSS_CLASSIFIER_THINKING` preference is exposed under Model → Advanced and can only toggle the lowest supported level: DeepSeek V4 Flash and Qwen3.8-Flash use `low`, MiMo-V2.5 uses the Chat API's lowest expressible `enabled` mode, and Zhipu GLM-5.3-Flash always remains `enabled` with `reasoning_effort=low` and deterministic sampling even when the preference is disabled. Disabled DeepSeek sends `thinking.type=disabled`; disabled Qwen sends `reasoning_effort=none`; disabled MiMo sends `thinking.type=disabled` with its documented `max_completion_tokens` field. DeepSeek and MiMo requests use a 4096 completion-token floor while thinking is enabled, without lowering a larger batch-derived limit. The shared adapter is used by batch classification, single-paper degradation, title translation, and connection tests; title translation remains forced to non-thinking mode. Managed providers never enter the generic disabled-thinking fallback, and no request automatically switches providers.

The classifier entries use their catalog-owned base URLs and their own keys:

- classifier keys: `SCIRSS_DEEPSEEK_API_KEY`, `SCIRSS_GLM_API_KEY`, `QWEN_API_KEY`, and `MIMO_API_KEY`
- Profile keeps its own `SCIRSS_PROFILE_API_KEY` / `SCIRSS_PROFILE_BASE_URL`

`SCIRSS_CLASSIFIER_ENABLED_MODELS` and `SCIRSS_CLASSIFIER_DEFAULT_MODEL` select enabled catalog entries. Legacy `SCIRSS_CLASSIFIER_API_KEY/BASE_URL/MODEL` values remain readable for migration and are materialized into the matching provider key on the first structured save; `SCIRSS_CLASSIFIER_THINKING` is the current global managed-classifier preference, and environment overrides stay authoritative.

The code owns the prompt shell and response schema. User interest boundaries, topic taxonomy, notes, and few-shot guidance live in the profile file. The classifier prompt applies profile rules in priority order: unrelated exclusions are checked first as a veto, then direct rules, then indirect rules. The compact model response contains relevance, confidence, a concise reason, and translated title; it no longer requests `decision_trace` or `recommended_action`.

The current classification path stores relevance, confidence, reason, recommended action, and translated title. Recommended action remains part of the local API/database compatibility shape but is derived deterministically by the application (`direct -> read`, `indirect -> scan`, `unrelated -> skip`) rather than generated by the model. It does not emit paper-level topic tags. Classifier requests use bounded retry handling for transient provider failures and malformed JSON responses; if a batch still fails, sync and reclassify jobs fall back to single-paper classification so successful papers can still be persisted.

Classifier defaults are DeepSeek V4 Flash with thinking disabled and batch size `5`. Advanced settings can enable only the lowest provider reasoning level; GLM cannot be disabled. Managed providers and the legacy generic path retain bounded same-model retries. Each sync/reclassify/model-test job captures its resolved model, key, effective provider controls, batch settings, thinking token floor, and pricing at queue time, so changing the default or thinking preference affects only later jobs.

### Configuration handling

Editable local configuration is exposed through the UI:

- secret values are written only to local config storage
- secrets are never echoed back to the frontend in plain text
- each field reports whether its value comes from local config, the system environment, or a built-in default
- first-run onboarding and Settings → Model share one classifier manager with multi-select, per-provider masked keys, connection tests, and a default restricted to enabled entries
- onboarding keeps an independent DeepSeek V4 Pro Profile key and offers a one-time copy of a configured DeepSeek classifier key only when the Profile key is empty
- connection tests run as `model-test` jobs, record token usage without saving unsaved keys, and warn that a small amount of provider quota is consumed
- saving local configuration reloads the running backend settings immediately, so follow-up jobs in the same session use the new API keys and model settings
- the active profile file path is fixed at `data/classification_profile.json` under the current runtime data directory and is not user-configurable

## UI Architecture

Frontend source ownership is feature-oriented:

- `web/src/app/`: application composition, loading gates, shared messages, and data-refresh orchestration
- `web/src/features/`: admin, onboarding, profile, and paper-review behavior and UI
- `web/src/api/` and `web/src/data/`: local HTTP client and generated feed catalog
- `web/src/shared/`: shared UI components and cross-feature types

The main app uses a three-column layout:

- left: filters and navigation context
- center: virtualized paper list
- right: paper detail panel and actions

Behavioral baseline:

- if no profile exists, onboarding is shown first with split basic/advanced settings, standalone settings saving, bootstrap job status, and an editable initial profile review card
- if a profile exists, the three-column review shell renders immediately and the paper list begins loading as soon as the report request starts
- if a profile exists but no feeds exist, the app switches into a feed-initialization empty state once feed loading resolves
- the default review view is `Unread + Last 30 days`
- paper cards stay summary-only
- paper actions live in the detail panel
- the settings drawer uses stable sidebar navigation for `Dashboard`, `Feeds`, `Profile`, `Model`, and `App`, with a horizontal fallback on narrow screens: Dashboard prioritizes active jobs and progressively discloses targeted sync, reclassification, and usage; Feeds edits an isolated local draft and opens catalog/custom additions as a focused secondary view; Profile keeps one primary review document and a secondary feedback queue; Model keeps credentials and defaults visible while tuning and pricing stay under Advanced with one save action; App owns visible update/runtime information followed by separate Zotero, scheduled-sync, and local-app panels. All single expandable settings use the same soft-surface HeroUI Disclosure pattern instead of native `details` elements.
- manual update checks are exposed in the upper `Settings -> App -> About and updates` section and in the footer status bar, and both routes trigger the same force-refresh update request
- app-update, scheduler, settings, proposal, and feedback hydration are non-critical background loads and must not block the card list from appearing
- `Mark as read` and feedback mutations commit their local UI result first and use a non-blocking report reconcile pass afterward, so the card list stays visible during background refreshes

### Packaged update distribution

- packaged builds still direct installer downloads and release notes to GitHub Releases
- update checks read the fixed DNS TXT record `feedmedaily-update.stassenger.top`
- the TXT record must contain `version=<semver>;url=<release-url>`, with a 600-second TTL on the current Aliyun DNS plan
- release builds still generate `dist/update.json`, and GitHub Releases must upload it as an asset for older installed clients that read `latest/download/update.json`
- update checks are not user-configurable through `.env` or the settings UI; release publishing updates GitHub Releases first, then runs `tools/update_release_dns.ps1` to update the DNS TXT record through Aliyun OpenAPI

## Profile Lifecycle

1. The user describes research interests during onboarding.
2. The profile model generates an initial profile proposal.
3. The user reviews and can edit the pending onboarding proposal before accepting it.
4. Accepting onboarding first applies the pending proposal and then saves the edited draft as the current profile.
5. Future classifications use that profile.
6. User feedback can generate new full-profile proposals.
7. Applying a proposal reclassifies only papers linked to that proposal feedback.

Supported admin reclassification scopes:

- `recent`
- `feedback`
- `all`

## Reporting

The supported report read path is the live `/api/report/latest` API backed by SQLite and current profile state.

That report path is optimized for the current UI by using batched SQLite reads and lightweight `raw_json` extraction for card-list fields such as abstract HTML and abstract images.

Legacy disk report artifacts under `reports/` remain compatibility or export leftovers. They are no longer the primary source of truth for the app UI or runtime verification.

## Maintenance Rule

When runtime boundaries, ownership between modules, or primary user workflows change, update this file in the same change.
