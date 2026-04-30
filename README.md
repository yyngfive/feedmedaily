# SciRSSAgent

SciRSSAgent monitors journal RSS feeds, stores paper metadata in SQLite, classifies relevance with a user-owned `classification_profile.json`, and serves either a local static report or a single-user FastAPI app with feedback, profile updates, and Zotero export.

## Structure

- `src/scirssagent/`: Python backend pipeline, API, storage, and CLI
- `tests/`: backend tests
- `web/`: Vite + React + TypeScript UI
- `data/rss_feeds.json`: structured RSS feed subscriptions used by the app
- `data/literature.sqlite`: local paper database
- `data/classification_profile.json`: active user classification profile (local only, Git-ignored)
- `reports/data/latest.json`: latest report data
- `reports/latest/index.html`: static report entrypoint
- `logs/YYYY-MM-DD.log`: daily run logs

## Backend Architecture

- `config.py`: load runtime configuration from `.env`
- `profiles.py`: read, validate, and write `classification_profile.json`
- `classifier.py`: build profile-driven prompts and classify papers with model A
- `services.py`: generate initial and updated profiles with model B, plus Zotero Web API integration
- `storage.py`: persist papers, classifications, feedback, profile proposals, and Zotero save state
- `pipeline.py`: fetch, dedupe, enrich, classify, and publish reports
- `server.py`: FastAPI app for report APIs, onboarding, admin jobs, profile approval, and static hosting
- `cli.py`: expose `run`, `report`, `experiment`, `serve`, and scheduled-task commands

Current flow:
`data/rss_feeds.json` -> feed fetch -> paper dedupe/upsert -> metadata enrichment -> classifier model classification using `classification_profile.json` -> SQLite classifications -> report JSON -> static report or FastAPI app

## Web App Flow

The FastAPI-hosted app is the primary interface:

1. If no profile exists, onboarding asks for a natural-language description of research interests.
2. The profile model generates an initial profile proposal.
3. After approval, the profile is written to `data/classification_profile.json`.
4. The classifier model uses that profile for future classifications.
5. User feedback can trigger profile proposals; applying a proposal updates the profile and reclassifies the linked feedback papers.
6. Manual reclassification is available for recent papers, feedback-linked papers, or the full local library.
7. After a profile exists, the app enters a feed-setup state until at least one RSS subscription is saved.
8. The main paper list defaults to unread papers from the last 30 days; cards are read-only previews and actions live in the detail panel on the right.
9. Feed subscriptions can be edited from the admin panel and are saved as structured JSON in `data/rss_feeds.json`.

Static export is still supported:
`pnpm --dir web build` writes `web/dist/` -> Python publishes `reports/latest/` -> browser opens local `index.html`

## Quick Start

```powershell
uv sync
pnpm --dir web install
pnpm --dir web build
uv run scirssagent serve
```

Open `http://127.0.0.1:8000` for the interactive app.

Before the first run, create a local `.env` file. The easiest way is:

```powershell
Copy-Item .env.example .env
```

For a fresh clone, the guided setup is:

1. Copy `.env.example` to `.env` and fill in your model credentials.
2. Run `uv sync`.
3. Run `pnpm --dir web install`.
4. Run `pnpm --dir web build`.
5. Start the app with `uv run scirssagent serve`.
6. Open the app and create your classification profile.
7. Open Admin and add one or more RSS feeds.
8. Save feeds to create `data/rss_feeds.json`.
9. Run `Run fetch + classify`, or wait for your scheduled task.
10. Review new papers, use the right-side detail panel actions, and optionally save selected papers to Zotero.

To fetch feeds and publish a fresh report:

```powershell
uv run scirssagent run --once
```

## Configuration

Recommended `.env` settings:

```dotenv
SCIRSS_CLASSIFIER_API_KEY=...
SCIRSS_CLASSIFIER_BASE_URL=https://api.deepseek.com
SCIRSS_CLASSIFIER_MODEL=deepseek-v4-flash
SCIRSS_CLASSIFIER_THINKING=disabled
SCIRSS_CLASSIFIER_BATCH_SIZE=10

SCIRSS_PROFILE_API_KEY=...
SCIRSS_PROFILE_BASE_URL=https://api.deepseek.com
SCIRSS_PROFILE_MODEL=deepseek-v4-pro
SCIRSS_PROFILE_THINKING=enabled
SCIRSS_PROFILE_PATH=data/classification_profile.json

SCIRSS_ZOTERO_API_KEY=...
SCIRSS_ZOTERO_LIBRARY_TYPE=user
SCIRSS_ZOTERO_LIBRARY_ID=1234567
SCIRSS_ZOTERO_COLLECTION_KEY=

SCIRSS_SERVER_HOST=127.0.0.1
SCIRSS_SERVER_PORT=8000
```

Notes:

- `SCIRSS_CLASSIFIER_*` is used for paper classification.
- `SCIRSS_PROFILE_*` is used for onboarding, profile generation, and profile revision prompts.
- `data/rss_feeds.json` is the only feed configuration source.
- The file is user-local and can be created from the web settings screen.
- If Zotero is configured, the UI lets the user choose a target collection before saving a paper.
- If any model request fails because the key, base URL, or upstream provider is wrong, the app surfaces the backend error directly in the UI.

### Zotero Setup

Minimum Zotero settings:

```dotenv
SCIRSS_ZOTERO_API_KEY=...
SCIRSS_ZOTERO_LIBRARY_TYPE=user
SCIRSS_ZOTERO_LIBRARY_ID=1234567
```

How to find the library ID:

- Personal library: use your Zotero `userID` as `SCIRSS_ZOTERO_LIBRARY_ID`.
- Group library: set `SCIRSS_ZOTERO_LIBRARY_TYPE=group` and use the `groupID`.
- Zotero documents this in the Web API basics and API keys pages:
  - [Zotero Web API Basics](https://www.zotero.org/support/dev/web_api/v3/basics)
  - [Zotero API Keys](https://www.zotero.org/settings/keys)

### Daily Usage

After setup, the normal workflow is:

1. Open Admin and maintain your RSS feeds.
2. Run `Run fetch + classify` when you want an immediate refresh, or rely on a scheduled task.
3. Click a paper card to inspect it in the right-side detail panel.
4. Use `DOI link`, `Mark as read`, `Save to Zotero`, and `Mark wrong` from the detail panel.
5. Review feedback and profile proposals from Admin as your interests evolve.

## Commands

```powershell
uv run scirssagent run --once
uv run scirssagent run --once --max-papers 10
uv run scirssagent run --once --max-papers 10 --reclassify
uv run scirssagent report latest
uv run scirssagent experiment batch-size
uv run scirssagent serve
uv run scirssagent init-task
uv run pytest
uv run ruff check
pnpm --dir web test
pnpm --dir web build
```

In the admin panel, the interactive app supports:

- `Run fetch + classify`
- `Refresh report`
- `Reclassify recent 50`
- `Reclassify feedback papers`
- `Reclassify all`
- `Generate profile proposal`

## Batch Size Experiment

Use the batch-size experiment to find the cheapest classifier batch size that preserves accuracy against a manually labeled sample.

First run creates a gold-label CSV and does not call the model:

```powershell
uv run scirssagent experiment batch-size --sample-size 80 --batch-sizes 1,5,10,20,30
```

Fill `reports/experiments/batch-size-gold-sample.csv` column `gold_relevance` with `direct`, `indirect`, or `unrelated`, then rerun the same command. The experiment records classification plus title-translation tokens, latency, JSON/missing-item failures, Direct precision/recall/F1, false-direct counts, and the recommended batch size.

Results are written as JSON and CSV under `reports/experiments/`. The experiment reads papers from SQLite but never writes classifications back to the production database.

With nvm-windows:

```powershell
nvm use (Get-Content .nvmrc).Trim()
pnpm --dir web test
pnpm --dir web build
```

## Windows Scheduled Task

Use `uv run scirssagent init-task --print-only` to print a PowerShell command for creating a daily 10:00 task. Review it before running.

See `AGENTS.md` for environment notes, secret handling, and repository conventions.

## License

MIT. See [LICENSE](/D:/Codes/Projects/SciRSSAgent/LICENSE).
