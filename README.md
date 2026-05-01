# SciRSSAgent

SciRSSAgent monitors journal RSS feeds, stores paper metadata in SQLite, classifies relevance with a user-owned `classification_profile.json`, and serves either a local static report or a single-user FastAPI app with feedback, profile updates, and Zotero export.

Current system architecture is documented in [ARCHITECTURE.md](./ARCHITECTURE.md).

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
## Windows Scheduled Task

Use `uv run scirssagent init-task --print-only` to print a PowerShell command for creating a daily 10:00 task. Review it before running.

See `AGENTS.md` for environment notes, secret handling, and repository conventions. See `ARCHITECTURE.md` for the maintained architecture summary.

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
- `DeepSeek` is not necessary, other LLM can be used.
- 

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

## Daily Usage

After setup, the normal workflow is:

1. Open Admin and maintain your RSS feeds.
2. Run `Run fetch + classify` when you want an immediate refresh, or rely on a scheduled task.
3. Click a paper card to inspect it in the right-side detail panel.
4. Use `DOI link`, `Mark as read`, `Save to Zotero`, and `Mark wrong` from the detail panel.
5. Review feedback and profile proposals from Admin as your interests evolve.

## Commands

```powershell
uv run scirssagent run --once
uv run scirssagent report latest
pnpm --dir web build
uv run scirssagent serve
uv run scirssagent init-task
```
## License

MIT. See [LICENSE](/D:/Codes/Projects/SciRSSAgent/LICENSE).
