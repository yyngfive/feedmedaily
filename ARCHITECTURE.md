# FeedMeDaily Architecture

## Overview

FeedMeDaily is the public Windows release name for this single-user literature triage application. The internal Python package name remains `scirssagent`. The app is built around a local SQLite database, a profile-driven paper classifier, a FastAPI-served React interface, and a new phase-1 Go tray manager for runtime control.

## Repository Structure

- `src/scirssagent/`: Python backend pipeline, API, storage, and CLI
- `cmd/feedmedaily-tray/`: first-stage Go tray entrypoint for Windows runtime management
- `internal/trayapp/`: Go tray runtime, command orchestration, scheduling, and autostart code
- `tests/`: backend tests
- `web/`: Vite + React + TypeScript UI
- `data/rss_feeds.json`: structured RSS feed subscriptions used by the app
- `data/literature.sqlite`: local paper database
- `data/classification_profile.json`: active user classification profile (local only, Git-ignored)
- `reports/data/latest.json`: latest report data
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
8. FastAPI serves the built React app from `web/dist` and exposes JSON APIs for the UI.

## Runtime Surfaces

### FastAPI app

`src/scirssagent/server.py` is the primary local application surface.

It is responsible for:

- serving the React build from `web/dist`
- exposing `/api/report/latest`
- exposing local configuration read/write APIs backed by the project `.env`
- exposing feed settings APIs
- exposing feedback APIs
- exposing profile bootstrap and proposal APIs
- exposing admin background-job APIs
- exposing Zotero collection lookup and save APIs

Long-running actions run in background threads and report status through the in-memory job registry.

### CLI

`src/scirssagent/cli.py` is a thin operational entrypoint for:

- `run --once`
- `report latest`
- `serve`
- `init-task`

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

In phase 1, the tray is intentionally backend-language-agnostic. It can launch the existing Python backend command today, and later switch to a Go backend command without changing tray UX.

The Windows release packaging path now includes this tray executable and points installer shortcuts at it, while the Python backend still provides the application API and pipeline runtime.

## Core Backend Modules

- `src/scirssagent/config.py`
  Loads environment-based settings, resolves project-local paths, and manages editable local `.env` fields with source-aware precedence (`system environment` over project `.env` over built-in defaults).
- `src/scirssagent/feeds.py`
  Reads feed subscriptions, fetches RSS/Atom feeds, and normalizes feed items.
- `src/scirssagent/metadata.py`
  Enriches papers with DOI and metadata lookups.
- `src/scirssagent/profiles.py`
  Reads, validates, and writes the active classification profile.
- `src/scirssagent/classifier.py`
  Builds prompts from the profile and classifies papers with the classifier model.
- `src/scirssagent/services.py`
  Handles profile-generation prompts and Zotero Web API integration.
- `src/scirssagent/storage.py`
  Owns SQLite persistence for papers, classifications, feedback, proposals, and Zotero status.
- `src/scirssagent/pipeline.py`
  Orchestrates fetch, upsert, enrichment, classification, report generation, and static publish steps.
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
