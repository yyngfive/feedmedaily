# FeedMeDaily Architecture

## Overview

FeedMeDaily is a local-first literature triage app for journal RSS feeds. The current product is built around a Go backend service, a Windows tray runtime, a React review UI, a local SQLite database, and profile-driven LLM classification.

## Repository Structure

- `cmd/feedmedailyd/`: production Go backend entrypoint
- `cmd/feedmedaily-tray/`: Windows tray runtime entrypoint
- `cmd/feedmedaily-verifier/`: Windows-only protected-feed verifier window
- `internal/api/`: HTTP API handlers, job endpoints, verification endpoints, and static asset serving
- `internal/classifier/`: Go classifier client, prompt shaping, and thinking-fallback handling
- `internal/config/`: settings schema, local config editing, and path resolution
- `internal/feeds/`: feed fetch client, generic RSS/Atom/RDF parser, and publisher-specific extractors
- `internal/jobs/`: sync pipeline, reclassify flows, and background job orchestration
- `internal/metadata/`: conditional metadata enrichment via DOI/OpenAlex/Crossref lookups
- `internal/profile/`: profile validation, generation, and persistence helpers
- `internal/runtime/`: shared runtime paths, version, mode, process, and app metadata helpers
- `internal/store/sqlite/`: SQLite persistence for papers, classifications, feedback, proposals, and Zotero status
- `internal/trayapp/`: tray lifecycle, scheduling, backend supervision, and autostart
- `internal/zotero/`: Zotero Web API integration
- `web/`: Vite + React + TypeScript frontend

Legacy reference files still remain in the repository for comparison and regression work, but they are not part of the current product workflow.

## Production Flow

1. Feed subscriptions are stored in `data/rss_feeds.json`.
2. `feedmedailyd` fetches feed content through a layered Go pipeline: HTTP client, generic RSS/Atom/RDF parser, and publisher-specific extractors.
3. Papers are deduplicated and upserted into `data/literature.sqlite`.
4. Metadata enrichment runs only when core fields such as DOI, authors, journal, or usable abstract content are missing.
5. The classifier evaluates papers against the active `data/classification_profile.json`.
6. Classifications, feedback, profile proposals, and Zotero save status are persisted in SQLite.
7. `feedmedailyd` serves `web/dist` and exposes the local JSON API surface.
8. The API service keeps a long-lived SQLite handle open for request reuse, and the UI reads the latest report through `/api/report/latest`, rebuilt from SQLite-backed state rather than replayed from disk report snapshots.

## Runtime Surfaces

### Go backend service

`cmd/feedmedailyd/` is the supported local backend for source mode and packaged builds. It owns:

- app/runtime endpoints such as `/api/app/health`, `/api/app/meta`, `/api/app/update`, `/api/app/open`, and `/api/app/exit`
- settings endpoints for config, feeds, and scheduler
- report, feedback, paper-read, and profile proposal APIs
- profile bootstrap, proposal generation, proposal apply/reject, and reclassify flows
- Zotero collection listing and save flows
- admin job endpoints including sync and reclassify
- protected-feed verification start, callback, and completion endpoints
- structured job-progress payloads for fetch, metadata, classification, report refresh, and profile-generation status updates
- static React asset serving and SPA fallback

`Run Sync Now` is fully owned by Go end-to-end: feed fetch, ingest, conditional metadata enrichment, classification, report refresh, and background job state all run through `feedmedailyd`.

The job polling endpoints expose both human-readable messages and structured progress fields so the UI can show stage-aware status such as current feed `i/N`, metadata/classification completion percentages, and step-based profile generation progress.

The API service reuses its SQLite store across requests instead of reopening it per handler. The `/api/report/latest` read path batch-selects each paper's latest classification, latest open feedback, and latest Zotero save state with SQL windowing rather than issuing per-paper follow-up lookups. `/api/app/update` also keeps a short-lived in-memory status cache so remote manifest fetch latency does not get multiplied by repeated UI polling or page initialization.

### Protected-feed verification

Cloudflare-protected or challenge-gated feeds use a Windows-only verification assist flow:

- a fetch job can move to `waiting_for_user` when the backend detects a challenge page or a Cloudflare-style `403`
- the UI opens `feedmedaily-verifier`, a dedicated verifier window with its own temporary WebView2 session
- once the protected page resolves to RSS/Atom/RDF content, the verifier POSTs the captured XML back to `/api/feeds/verification/callback`
- the backend resumes the paused job by injecting that XML into the normal Go feed parser

This verifier session is intentionally temporary and non-persistent. Its WebView2 user-data directory is scoped to the current verification step and removed on exit.

### Windows tray runtime

`cmd/feedmedaily-tray/` is the Windows app shell. It is responsible for:

- single-instance tray lifecycle
- ensuring the local backend is available while the tray is active
- opening the browser UI
- triggering `Run Sync Now`
- launch-at-login
- tray-owned local daily scheduling

Packaged builds ship the tray executable as the primary desktop entrypoint. In source mode, the tray builds or launches the local Go backend as needed.

## Data Model And State

### User-local state

These files are local machine state and must not be committed:

- `.env`
- `data/classification_profile.json`
- `data/literature.sqlite`
- `data/rss_feeds.json`

### LLM roles

There are two configurable model roles:

- classifier model: paper relevance classification
- profile model: onboarding profile generation and feedback-driven profile revision

Each role can use its own API key and base URL:

- `SCIRSS_CLASSIFIER_API_KEY` / `SCIRSS_CLASSIFIER_BASE_URL`
- `SCIRSS_PROFILE_API_KEY` / `SCIRSS_PROFILE_BASE_URL`

The code owns the prompt shell and response schema. User interest boundaries, topic taxonomy, notes, and few-shot guidance live in the profile file.

The current classification path stores relevance, confidence, reason, recommended action, and translated title. It does not emit paper-level topic tags.

Both model roles can request provider thinking mode. If a request fails with timeout, gateway-style, or reasoning-mode errors, the runtime retries once with `thinking=disabled`.

### Configuration handling

Editable local configuration is exposed through the UI:

- secret values are written only to local config storage
- secrets are never echoed back to the frontend in plain text
- each field reports whether its value comes from local config, the system environment, or a built-in default

## UI Architecture

The main app uses a three-column layout:

- left: filters and navigation context
- center: virtualized paper list
- right: paper detail panel and actions

Behavioral baseline:

- if no profile exists, onboarding is shown first and includes local configuration editing
- if a profile exists, the three-column review shell renders immediately and the paper list begins loading as soon as the report request starts
- if a profile exists but no feeds exist, the app switches into a feed-initialization empty state once feed loading resolves
- the default review view is `Unread + Last 30 days`
- paper cards stay summary-only
- paper actions live in the detail panel
- admin owns configuration editing, feed editing, manual jobs, feedback review, and profile proposal review
- app-update, scheduler, settings, proposal, and feedback hydration are non-critical background loads and must not block the card list from appearing

## Profile Lifecycle

1. The user describes research interests during onboarding.
2. The profile model generates an initial profile proposal.
3. The user applies the proposal.
4. The applied profile is written to `data/classification_profile.json`.
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
