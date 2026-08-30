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

## Quota Economy

- Before writing a new client or experiment runner, search the repository, ignored experiment directories, and relevant history for an existing production path or reusable harness. Extend or parameterize one existing path instead of duplicating prompts, request code, parsing, retries, or pricing logic.
- Make long experiments checkpointed and resumable. Preserve completed batches, avoid repeating successful paid calls, and keep automatic retries narrow and bounded.
- Record request counts, cache hits, input/output tokens, failures, and price snapshots for every condition so the cost of both successful and failed calls remains auditable.
- Do not spawn sub-agents, new ChatGPT tasks, or additional model-based judges for an experiment unless the user requests them or they are necessary to produce the requested result.

## Verification Economy

- Do not run `pytest`, `go test`, Playwright, or other broad test suites as a ritual closing step when the change is limited to experiments, reports, documentation, data summaries, or another scope those tests do not exercise.
- Prefer direct inspection, model judgment, existing runtime evidence, and the errors produced by the actual command or workflow being changed. Treat a successful real execution and its recorded output as the primary verification for experiment/report work.
- Run a targeted test only when the edited behavior has a concrete regression risk that the test can detect, or when the user explicitly requests it. Do not use unrelated repository-wide tests to create a false sense of confidence.

## Changelog

- `CHANGELOG.md` is the canonical versioned changelog for user-facing product changes.
- Maintain changelog entries grouped by version number, starting from `0.2.0`.
- Each version section should describe changes relative to the previous released version, not as an all-time cumulative list.
- The latest released version is `0.5.0`; the current rolling unreleased section should therefore be `0.5.1` until `0.5.1` is shipped.
- When a version is released, convert that section from unreleased to dated release notes and open a new unreleased section for the next planned version.
- Add behavior changes, bug fixes, runtime changes, packaging changes, and notable UX changes that matter to users or release notes readers.
- Avoid filling the changelog with pure planning-only edits, internal note reshuffles, or agent-policy-only changes unless they have a user-visible release impact.
- A temporary release draft under `docs/release-notes-v*.md` should stay very short and use a simple bullet list style.
- Treat `CHANGELOG.md` as the detailed canonical source and remove a temporary release draft after publishing that release.

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
  - `today`
  - `feedback`
  - `all`
  - `count` (0 through the current database paper count)
  - `unclassified` (papers without any classification record)
- Reclassify jobs (manual and apply-proposal launched) run as cancellable background jobs through the same cancel endpoint as sync; cancellation preserves completed batches and the partial job result.
- `Mark wrong` writes persistent feedback records to SQLite.
- `Save to Zotero` uses the Zotero Web API and stores save status in SQLite.

## Agent Notes

- Do not commit `data/classification_profile.json` or `data/*.sqlite`; they are per-user local state.
- When investigating logs or runtime state, distinguish source mode from release mode before drawing conclusions: source mode uses repo-local `logs/` and `data/`, while installed/release builds use `%LOCALAPPDATA%\FeedMeDaily\`.
- Keep long-running LLM actions as background jobs with visible status in the UI.
- Keep review-critical loading paths narrow: the paper list should not wait on app-update, scheduler, settings, proposal, or feedback hydration unless the task explicitly redesigns that behavior.

## UI Guidelines

- [`docs/ui-guidelines.zh-CN.md`](docs/ui-guidelines.zh-CN.md) is the canonical UI design, implementation, interaction, and visual-verification specification.
- Read and follow it before changing `web/`, including small refinements that affect colors, spacing, component states, copy, or default behavior.
- Keep architectural ownership in `ARCHITECTURE.md`; update the UI guidelines in the same change when the visual language, layout baseline, or interaction conventions change.
