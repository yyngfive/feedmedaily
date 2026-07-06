# SciRSSAgent Agents Guide

## Scope

This file holds project-specific agent guidance that should stay out of the user-facing README.

## Secrets

- Keep real API keys in `.env` or user-level environment variables.
- Never commit real tokens, cookies, or local credential files.
- `.env` is Git-ignored and stays local to the machine.
- Before committing, scan staged changes for API keys or accidental secret material.

## Environment

- Preferred backend workflow: Go from `go.mod`.
- Preferred frontend workflow: `pnpm` with Node from `.nvmrc`.
- Standard source-mode setup:

```powershell
Copy-Item .env.example .env
corepack pnpm --dir web install
corepack pnpm --dir web build
go run .\cmd\feedmedaily-tray --root .
```

- To run the local backend service directly:

```powershell
go run .\cmd\feedmedailyd --root . --host 127.0.0.1 --port 8000
```

- In source mode, `--root .` means runtime state stays under the project root rather than `%LOCALAPPDATA%`.
  - Source-mode logs are written under `.\logs\`
  - Source-mode user/runtime data is written under `.\data\`
  - When debugging source mode, check the repo-local `logs/` and `data/` folders first
- In installed/release mode, logs and runtime data live under `%LOCALAPPDATA%\FeedMeDaily\` instead.

- `.env.example` documents the current recommended environment variables and should be kept in sync with README when configuration changes.

## Repository Conventions

- README should stay focused on product usage and developer setup.
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
- If the user asks to make a git commit, do not limit the commit to agent-authored edits only; include the user's local modifications in the same commit unless the user says otherwise.

## Changelog

- `CHANGELOG.md` is the canonical versioned changelog for user-facing product changes.
- Maintain changelog entries grouped by version number, starting from `0.2.0`.
- Each version section should describe changes relative to the previous released version, not as an all-time cumulative list.
- The latest released version is `0.4.0`; the current rolling unreleased section should therefore be `0.4.1` until `0.4.1` is shipped.
- When a version is released, convert that section from unreleased to dated release notes and open a new unreleased section for the next planned version.
- Add behavior changes, bug fixes, runtime changes, packaging changes, and notable UX changes that matter to users or release notes readers.
- Avoid filling the changelog with pure planning-only edits, internal note reshuffles, or agent-policy-only changes unless they have a user-visible release impact.
- Release draft files under `docs/release-notes-v*.md` should stay very short and use a simple bullet list style like `docs/release-notes-v0.2.1-draft.md`.
- Treat `CHANGELOG.md` as the detailed canonical source and keep per-version release drafts as compact human-facing summaries.

## Current Architecture

- Classification is profile-driven. The active rules live in `data/classification_profile.json`, which is user-local and Git-ignored.
- There are two configurable LLM roles:
  - classifier model: paper classification
  - profile model: profile generation and profile revision
- Each role can use its own API key and base URL:
  - `SCIRSS_CLASSIFIER_API_KEY` / `SCIRSS_CLASSIFIER_BASE_URL`
  - `SCIRSS_PROFILE_API_KEY` / `SCIRSS_PROFILE_BASE_URL`
- The code owns the task shell and output schema, but user interest boundaries, topic taxonomy, few-shots, and classification notes come from the profile file.
- The Go backend is the primary local app surface:
  - serves `web/dist`
  - exposes `/api/report/latest`
  - exposes feedback, profile proposal, Zotero, admin job, and protected-feed verification APIs
- Profile lifecycle:
  1. User describes interests in onboarding
  2. the profile model generates an initial profile proposal
  3. the user applies the proposal
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
- When investigating logs or runtime state, distinguish source mode from release mode before drawing conclusions: source mode uses repo-local `logs/` and `data/`, while installed/release builds use `%LOCALAPPDATA%\FeedMeDaily\`.
- When changing the profile proposal UI, optimize for reviewability rather than raw JSON visibility.
- Keep long-running LLM actions as background jobs with visible status in the UI.
- Keep review-critical loading paths narrow: the paper list should not wait on app-update, scheduler, settings, proposal, or feedback hydration unless the task explicitly redesigns that behavior.
- Keep admin/settings surfaces compact and low-chrome; avoid stacked form cards when a denser table or document layout communicates better.
- Prefer one primary profile-review document card over multiple small cards when showing the current profile or a pending proposal.
- For onboarding, profile, and proposal review surfaces, avoid card-inside-card or box-inside-box compositions; prefer one primary document card with lightweight sections.
- Keep onboarding/profile review pages short when possible by compressing explanatory chrome and using document-style sections instead of nested modules.
- When a user has already removed specific UI helper text, badges, or decorative labels during the current iteration, do not reintroduce them unless the user explicitly asks for them back.
- Keep paper cards summary-only and move richer context or rendering into the right detail panel.
- Favor simplified wording and fewer redundant labels or duplicate actions when refining the UI.

## UI Baseline

- The main app currently uses a three-column layout: left filters, center paper list, right detail panel.
- If no profile exists, the app shows onboarding first. If a profile exists, the three-column shell should render immediately and let the paper list load independently from admin/status data. If no RSS feeds are configured, the app switches into a feed-initialization empty state only after feed loading resolves and opens admin feed settings by default.
- The center list defaults to `Unread + Last 30 days`, uses virtualized card rendering, and keeps paper cards as read-only previews without inline action buttons.
- The admin panel owns feed subscription editing, manual jobs, feedback review, and profile proposal review.
- The right detail panel owns the paper action buttons (`DOI link`, `Mark as read`, `Save to Zotero`, `Mark wrong`).
- Zotero saving is implemented through the Zotero Web API with an in-app collection picker, not through the browser connector.
- `/api/report/latest` is a performance-critical path. Keep report reads batched and avoid reintroducing request-level SQLite reopen/close behavior or first-screen blocking dependencies from non-critical admin requests.
- In `web/src/App.tsx` and `web/src/main.tsx`, top-level helpers and components should keep short Chinese comments that explain each function or component's responsibility.
