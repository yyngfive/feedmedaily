# FeedMeDaily Architecture

## Overview

FeedMeDaily is the public Windows release name for this single-user literature triage application. The internal Python package name remains `scirssagent`. The app is built around a local SQLite database, a profile-driven paper classifier, a Go tray manager for runtime control, and a Go backend that owns the production web/API surface. The Python package remains as a reference implementation and regression baseline.

## Repository Structure

- `src/scirssagent/`: Python reference pipeline, API, storage, and CLI helpers
- `cmd/feedmedailyd/`: Go backend service entrypoint
- `cmd/feedmedaily-tray/`: first-stage Go tray entrypoint for Windows runtime management
- `internal/api/`: Go HTTP compatibility layer for app/report metadata, tray-facing control routes, migrated storage-backed APIs, background jobs, and static UI serving
- `internal/classifier/`: Go-side OpenAI-compatible batch classifier and title-translation fallback
- `internal/config/`: early Go settings and path resolution layer
- `internal/jobs/`: Go-side run-once orchestration, reclassify orchestration, and background-job helpers
- `internal/metadata/`: Go-side metadata enrich logic for DOI/OpenAlex/Crossref lookups during reclassify
- `internal/profile/`: Go-side profile JSON validation, load, and persisted write helpers
- `internal/runtime/`: shared Go runtime helpers for app mode, paths, version, ports, and runtime state
- `internal/store/sqlite/`: Go-side SQLite access for reports, feedback, profile proposals, paper read-state, and local Zotero status writes
- `internal/trayapp/`: Go tray runtime, command orchestration, scheduling, and autostart code
- `tests/`: backend tests
- `web/`: Vite + React + TypeScript UI
- `data/rss_feeds.json`: structured RSS feed subscriptions used by the app
- `data/literature.sqlite`: local paper database
- `data/classification_profile.json`: active user classification profile (local only, Git-ignored)
- `reports/data/latest.json`: latest published report snapshot
- `reports/latest/index.html`: static report entrypoint
- `logs/YYYY-MM-DD.log`: daily run logs

The production path is:

1. RSS feed subscriptions are stored in `data/rss_feeds.json`.
2. The backend fetches feed entries and normalizes them into `Paper` records.
3. Papers are deduplicated and upserted into `data/literature.sqlite`.
4. Papers that need classification are enriched with metadata.
5. The classifier model scores papers against the active `data/classification_profile.json`.
6. Classifications, feedback, profile proposals, and Zotero save state are persisted in SQLite.
7. Report payloads are written to `reports/data/latest.json`.
8. `feedmedailyd` serves the built React app from `web/dist` and exposes JSON APIs for the UI.
9. On the Go migration path, read-only report/profile/feedback/proposal APIs can now be rebuilt directly from SQLite and the profile JSON instead of only replaying report snapshots.
10. On the current Go migration branch, report/profile/feedback/proposal reads, local-state writes, feed fetching/parsing, full `run --once`, reclassify/classifier execution, profile bootstrap/proposal generation, and Zotero collection/save flows all run inside Go.

## Runtime Surfaces

### Python reference app

`src/scirssagent/server.py` is retained as a legacy/reference local application surface.

It is still useful for:

- regression comparison against the Go service
- preserving the old Python behavior as a readable reference implementation
- targeted debugging when comparing Go and Python outputs

It is no longer the supported production backend for this branch.

### Go backend service

`cmd/feedmedailyd/` is the production Go backend entrypoint.

It currently owns the compatibility boundary, local-state write paths, and the first native decision-engine migration slice:

- `/api/report/latest` via Go-side live SQLite reads
- `/api/app/health`
- `/api/app/meta`
- `/api/app/update`
- `/api/app/open`
- `/api/app/exit`
- `/api/settings/config`
- `/api/settings/feeds`
- `/api/settings/scheduler`
- `/api/profile/current` via Go-side profile JSON validation and load
- `/api/profile/bootstrap` via Go-side profile generation and proposal persistence
- `/api/feedback` via Go-side SQLite read/write access
- `/api/feedback/{id}` via Go-side SQLite delete
- `/api/papers/{id}/read` via Go-side SQLite write
- `/api/profile/proposals` via Go-side SQLite read access
- `/api/profile/proposals/generate` via Go-side feedback-context collection and profile proposal generation
- `/api/profile/proposals/{id}` via Go-side SQLite read access
- `/api/profile/proposals/{id}/apply` via Go-side profile/SQLite writes plus Go-side reclassify/report rebuild
- `/api/profile/proposals/{id}/reject` via Go-side SQLite write
- `/api/zotero/collections` via Go-side Zotero Web API integration
- `/api/zotero/save/{paper_id}` via Go-side Zotero Web API integration plus SQLite status writes
- `/api/admin/run` via Go-side fetch -> ingest -> metadata -> classifier -> report pipeline
- `/api/admin/reclassify` via Go-side metadata enrich + classifier + report rebuild
- `/api/admin/report/latest`
- `/api/admin/jobs`
- React static asset serving and SPA fallback from `web/dist`

The Go service now owns `Run Sync Now` end-to-end, including feed fetching/parsing, SQLite ingest, metadata enrich, classifier execution, static report publishing, and Zotero collection/save integration. Python remains as a reference implementation and compatibility shell for regression comparison.

### CLI

`src/scirssagent/cli.py` is a thin operational entrypoint for:

- `run --once`
- `report latest`
- `zotero collections`
- `zotero save`
- `serve` as a legacy Python reference server
- scheduler commands
- `reclassify`

The CLI is intentionally kept smaller than the app surface and should only contain operational commands that are part of the maintained product workflow.

### Go tray manager

`cmd/feedmedaily-tray/` is the phase-1 Windows runtime shell.

It is responsible for:

- single-instance tray lifecycle
- starting and stopping the local backend service
- opening the browser UI
- triggering `Run Sync Now`
- launch-at-login
- a tray-owned local daily timer

On the Go migration branch, the tray is wired directly to the Go backend path. It expects `feedmedailyd.exe` in release builds and uses `go run ./cmd/feedmedailyd` in source mode.

The Windows release packaging path includes the tray executable and points installer shortcuts at it. The Go service is the supported backend runtime for release and source-mode product use.

## Core Backend Modules

- `src/scirssagent/config.py`
  Loads environment-based settings, resolves project-local paths, and manages editable local `.env` fields with source-aware precedence (`system environment` over project `.env` over built-in defaults).
- `src/scirssagent/feeds.py`
  Legacy reference implementation for feed subscriptions, RSS/Atom fetching, and feed-item normalization. The production `run --once` path is now owned by Go.
- `src/scirssagent/metadata.py`
  Enriches papers with DOI and metadata lookups.
- `src/scirssagent/profiles.py`
  Reads, validates, and writes the active classification profile.
- `src/scirssagent/classifier.py`
  Builds prompts from the profile and classifies papers with the classifier model.
- `src/scirssagent/services.py`
  Handles the Python-side reference implementations for profile-generation prompts and Zotero Web API integration.
- `src/scirssagent/storage.py`
  Owns SQLite persistence for papers, classifications, feedback, proposals, and Zotero status.
- `src/scirssagent/pipeline.py`
  Legacy reference implementation for the old Python pipeline. The production `run --once` path is now owned by Go-side jobs and report publishing.
- `src/scirssagent/reporting.py`
  Builds report payloads, writes JSON outputs, and publishes the static report bundle.
- `src/scirssagent/models.py`
  Defines shared Pydantic/domain models used across API, pipeline, and storage boundaries.

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

The code owns the task shell and response schema. User interest boundaries, taxonomy, notes, and few-shot guidance live in the profile file.

Editable configuration is exposed in the UI, but secret values are still handled as local-only state:

- secret fields are written only to the local project `.env`
- secret fields are never echoed back to the frontend in plain text
- the UI shows whether a value comes from the project `.env`, the system environment, or a built-in default

## UI Architecture

The main app uses a three-column layout:

- left: filters and navigation context
- center: virtualized paper list
- right: paper detail panel and actions

Behavioral baseline:

- if no profile exists, onboarding is shown first and also includes local configuration editing
- if a profile exists but no feeds exist, the app opens the feed-initialization empty state
- the default review view is `Unread + Last 30 days`
- paper cards remain summary-only
- paper actions live in the detail panel
- admin owns local configuration editing, feed editing, manual jobs, feedback review, and profile proposal review
- the settings/admin surface is split into tabs for configuration, feeds, and profile-plus-feedback review

## Profile Lifecycle

1. The user describes research interests during onboarding.
2. The profile model creates an initial profile proposal.
3. The user applies the proposal.
4. The applied profile is written to `data/classification_profile.json`.
5. Future classifications use that profile.
6. User feedback can trigger new full-profile proposals.
7. Applying a proposal reclassifies only papers linked to the proposal feedback.

Supported admin reclassification scopes:

- `recent`
- `feedback`
- `all`

## Reporting

Two report outputs are maintained:

- structured JSON in `reports/data/`
- a published static bundle in `reports/latest/`

Static publishing copies `web/dist`, injects the latest report payload for embedded viewing, and inlines built assets so the exported report remains self-contained.

## Maintenance Rule

When architecture, runtime boundaries, primary workflows, or ownership between modules changes, update this file in the same change.
