# SciRSSAgent Agents Guide

## Scope

This file holds project-specific agent guidance that should stay out of the user-facing README.

## Secrets

- Keep real API keys in `.env` or user-level environment variables.
- Never commit real tokens, cookies, or local credential files.
- `.env` is Git-ignored and stays local to the machine.
- Before committing, scan staged changes for API keys or accidental secret material.

## Environment

- Preferred Python workflow: `uv` with Python `3.12`.
- Preferred frontend workflow: `pnpm` with Node from `.nvmrc`.
- If `python` is not on PATH, `uv` can manage Python directly:

```powershell
uv python install 3.12
uv sync
```

- If uv cannot access the default Windows cache locations, keep managed Python and cache inside the project:

```powershell
$env:UV_CACHE_DIR = ".uv-cache"
$env:UV_PYTHON_INSTALL_DIR = ".uv-python"
uv sync
```

- If uv warns that hardlinking failed, use copy mode:

```powershell
$env:UV_LINK_MODE = "copy"
uv sync
```

- `mamba` is a fallback for future heavy scientific, GPU, or local-model dependencies. It is not the default workflow for v0.1.
- `.env.example` documents the current recommended environment variables and should be kept in sync with README when configuration changes.

## Repository Conventions

- README should stay focused on project structure and usage.
- `ARCHITECTURE.md` is the canonical current-architecture summary. If a change updates system boundaries, core flows, major modules, or UI ownership, update `ARCHITECTURE.md` in the same change.
- Put internal workflow notes, safety guidance, and agent-facing conventions here.
- Keep generated caches, databases, logs, and reports out of Git.
- Prefer deterministic pipeline logic over free-form agent behavior for RSS ingestion, classification, and reporting.
- For larger changes, create a new Git branch first and merge it back only after the work is complete and verified.

## Commit Hygiene

- Before staging or committing, review the current diff and group changes by feature, fix, or documentation scope.
- Do not bundle unrelated backend, frontend, tray, and planning changes into one large commit when they can be separated cleanly.
- If a task touches multiple independent scopes, prefer multiple focused commits with messages that describe the specific slice.
- Keep scratch files, temporary assets, and exploratory notes out of feature commits unless they are intentional deliverables.
- When a code change updates behavior, stage the directly affected docs in the same commit, but do not use doc updates as a reason to sweep unrelated work into that commit.
- If the working tree already contains unrelated edits, commit only the files that belong to the requested change and leave the rest unstaged.
- If the user asks to make a git commit, do not limit the commit to agent-authored edits only; include the user's in-scope local modifications in the same commit unless the user says otherwise.

## Current Architecture

- Classification is profile-driven. The active rules live in `data/classification_profile.json`, which is user-local and Git-ignored.
- There are two configurable LLM roles:
  - classifier model: paper classification
  - profile model: profile generation and profile revision
- Each role can use its own API key and base URL:
  - `SCIRSS_CLASSIFIER_API_KEY` / `SCIRSS_CLASSIFIER_BASE_URL`
  - `SCIRSS_PROFILE_API_KEY` / `SCIRSS_PROFILE_BASE_URL`
- The code owns the task shell and output schema, but user interest boundaries, topic taxonomy, few-shots, and classification notes come from the profile file.
- FastAPI is now the primary local app surface:
  - serves `web/dist`
  - exposes `/api/report/latest`
  - exposes feedback, profile proposal, Zotero, and admin job APIs
- Profile lifecycle:
  1. User describes interests in onboarding
  2. model B generates an initial profile proposal
  3. user applies the proposal
  4. future classifications use the applied profile
  5. feedback can generate new full-profile proposals
  6. applying a proposal reclassifies linked feedback papers only
- Reclassification scopes currently supported:
  - `recent`
  - `feedback`
  - `all`
- `Mark wrong` writes persistent feedback records to SQLite.
- `Save to Zotero` uses the Zotero Web API and stores save status in SQLite.

## Agent Notes

- Do not commit `data/classification_profile.json` or `data/*.sqlite`; they are per-user local state.
- When changing the profile proposal UI, optimize for reviewability rather than raw JSON visibility.
- Keep long-running LLM actions as background jobs with visible status in the UI.
- Keep admin/settings surfaces compact and low-chrome; avoid stacked form cards when a denser table or document layout communicates better.
- Prefer one primary profile-review document card over multiple small cards when showing the current profile or a pending proposal.
- Keep paper cards summary-only and move richer context or rendering into the right detail panel.
- Favor simplified wording and fewer redundant labels or duplicate actions when refining the UI.

## UI Baseline

- The main app currently uses a three-column layout: left filters, center paper list, right detail panel.
- If no profile exists, the app shows onboarding first. If a profile exists but no RSS feeds are configured, the app switches into a feed-initialization empty state and opens admin feed settings by default.
- The center list defaults to `Unread + Last 30 days`, uses virtualized card rendering, and keeps paper cards as read-only previews without inline action buttons.
- The admin panel owns feed subscription editing, manual jobs, feedback review, and profile proposal review.
- The right detail panel owns the paper action buttons (`DOI link`, `Mark as read`, `Save to Zotero`, `Mark wrong`).
- Zotero saving is implemented through the Zotero Web API with an in-app collection picker, not through the browser connector.
- In `web/src/App.tsx` and `web/src/main.tsx`, top-level helpers and components should keep short Chinese comments that explain each function or component's responsibility.
