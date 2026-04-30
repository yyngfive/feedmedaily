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

## Repository Conventions

- README should stay focused on project structure and usage.
- Put internal workflow notes, safety guidance, and agent-facing conventions here.
- Keep generated caches, databases, logs, and reports out of Git.
- Prefer deterministic pipeline logic over free-form agent behavior for RSS ingestion, classification, and reporting.
- For larger changes, create a new Git branch first and merge it back only after the work is complete and verified.

## Current Architecture

- Classification is profile-driven. The active rules live in `data/classification_profile.json`, which is user-local and Git-ignored.
- There are two DeepSeek model roles:
  - model A: paper classification
  - model B: profile generation and profile revision
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
