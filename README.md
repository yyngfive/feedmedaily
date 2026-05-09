# FeedMeDaily

![FeedMeDaily banner](./docs/feedmedaily-banner.svg)

FeedMeDaily is a local-first paper triage app for journal RSS feeds. It stores paper metadata in SQLite, classifies relevance with profile-driven LLM prompts, serves a local review UI over FastAPI, and lets you send selected papers to Zotero.

Current system architecture is documented in [ARCHITECTURE.md](./ARCHITECTURE.md).

## Release-first usage

The preferred end-user distribution is a Windows installer build.

Release characteristics:

- Windows-first
- local service + browser UI
- no Node.js required at runtime
- no source checkout required
- user data stored under `%LOCALAPPDATA%\FeedMeDaily\`

After installation, the app launcher should run `FeedMeDailyTray.exe --root <install-dir>`, let the tray manage the local service, and open your browser automatically.

### Go tray phase 1

The repository now includes a first-stage Windows tray manager in Go under `cmd/feedmedaily-tray/`.

Current phase-1 scope:

- the tray can start and stop the existing local backend
- the tray can open the browser UI
- the tray can trigger `Run Sync Now`
- the tray owns launch-at-login and a local daily timer
- the backend command can still be Python in this phase

This means the tray is already the runtime shell, while the full Go backend rewrite remains a later step.

### User data location

FeedMeDaily stores local state in:

```text
%LOCALAPPDATA%\FeedMeDaily\
```

Typical files:

- `config/settings.json`
- `config/secrets.json`
- `data/literature.sqlite`
- `data/rss_feeds.json`
- `data/classification_profile.json`
- `logs\`
- `reports\`

If you already have data from a source checkout, you can copy it manually into the release data directory:

- copy `data/literature.sqlite`
- copy `data/rss_feeds.json`
- copy `data/classification_profile.json`

FeedMeDaily does not migrate these files automatically in v1.

### Updates and scheduling

- Updates are check-only in the app UI. The release can show a newer installer if `FEEDMEDAILY_UPDATE_MANIFEST_URL` is configured.
- Daily fetch/classify jobs use Windows Task Scheduler.
- The UI can create, update, and remove the scheduled task.

### Update manifest

`FEEDMEDAILY_UPDATE_MANIFEST_URL` should point to a public HTTPS JSON file like this:

```json
{
  "version": "0.1.0",
  "download_url": "https://github.com/yyngfive/feedmedaily/releases/download/v0.1.0/FeedMeDaily-Setup.exe",
  "release_notes_url": "https://github.com/yyngfive/feedmedaily/releases/tag/v0.1.0"
}
```

This repository includes a release-ready example at [release/update.json](./release/update.json).

For GitHub Releases, the simplest stable URL is:

```text
https://github.com/yyngfive/feedmedaily/releases/latest/download/update.json
```

If every release uploads an asset named `update.json`, installed apps can always check the latest version through that single URL.

## Source mode

If you want to run FeedMeDaily from source:

```powershell
uv sync
pnpm --dir web install
pnpm --dir web build
uv run scirssagent open
```

To build the experimental Windows tray executable after Go is installed:

```powershell
pwsh -File .\tools\build_go_tray.ps1
```

The tray stores its local settings in `tray-settings.json` at the app config root. In source mode that file sits at the repository root. You can override the backend launch command there with a tokenized command such as:

```json
{
  "backend_command": [
    ".venv\\Scripts\\python.exe",
    "-m",
    "scirssagent.cli",
    "serve",
    "--root",
    "{root}",
    "--host",
    "{host}",
    "--port",
    "{port}"
  ]
}
```

If `backend_command` is omitted, the tray auto-detects `.venv\\Scripts\\python.exe`, then `python`, then `uv`.

Before the first run, create a local `.env` file:

```powershell
Copy-Item .env.example .env
```

### Recommended configuration

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

FEEDMEDAILY_UPDATE_MANIFEST_URL=
```

Notes:

- `SCIRSS_CLASSIFIER_*` is used for paper classification.
- `SCIRSS_PROFILE_*` is used for onboarding, profile generation, and profile revision prompts.
- `SCIRSS_ZOTERO_*` is used for Zotero export.
- `FEEDMEDAILY_UPDATE_MANIFEST_URL` is optional and intended for packaged builds.

## Daily workflow

1. Open FeedMeDaily.
2. Create or review your classification profile.
3. Add RSS feeds from Settings.
4. Run `Run fetch + classify`, or let the Windows scheduled task do it.
5. Review papers from the main list and detail panel.
6. Use `Save to Zotero`, `Mark as read`, and `Mark wrong` as needed.
7. Review feedback-driven profile proposals over time.

In the tray-driven phase-1 runtime, users can also leave the tray running in the background and use it to reopen the app, start the backend, stop the backend, or trigger a sync manually.

## Commands

```powershell
uv run scirssagent open
uv run scirssagent serve
uv run scirssagent run --once
uv run scirssagent report latest
uv run scirssagent scheduler show
uv run scirssagent scheduler install --time 10:00
uv run scirssagent scheduler remove
uv run scirssagent version
```

## Building the Windows release

Release artifacts are built from the existing FastAPI backend, the Go tray shell, and the already-built web bundle.

Expected tooling:

- Python 3.12 via `uv`
- Node/pnpm for build-time web compilation only
- `pyinstaller`
- `go` for the tray executable
- `Inno Setup` (`ISCC.exe`) for the installer

Build flow:

```powershell
corepack pnpm --dir web build
pwsh -File .\tools\build_release.ps1
```

The build script:

- regenerates brand assets
- builds `web/dist`
- builds `FeedMeDailyTray.exe`
- packages the backend into `dist\FeedMeDaily\`
- copies the tray executable into `dist\FeedMeDaily\`
- copies the built web app into `dist\FeedMeDaily\web\dist`
- optionally compiles `installer\feedmedaily.iss`

## Branding assets

Primary assets included in the repo:

- app icon source: [assets/branding/feedmedaily-icon.svg](./assets/branding/feedmedaily-icon.svg)
- Windows icon: [assets/branding/feedmedaily.ico](./assets/branding/feedmedaily.ico)
- README banner: [docs/feedmedaily-banner.svg](./docs/feedmedaily-banner.svg)
- browser favicon: [web/public/favicon.svg](./web/public/favicon.svg)

## Zotero setup

Minimum Zotero settings:

```dotenv
SCIRSS_ZOTERO_API_KEY=...
SCIRSS_ZOTERO_LIBRARY_TYPE=user
SCIRSS_ZOTERO_LIBRARY_ID=1234567
```

How to find the library ID:

- Personal library: use your Zotero `userID` as `SCIRSS_ZOTERO_LIBRARY_ID`
- Group library: set `SCIRSS_ZOTERO_LIBRARY_TYPE=group` and use the `groupID`

Relevant Zotero docs:

- [Zotero Web API Basics](https://www.zotero.org/support/dev/web_api/v3/basics)
- [Zotero API Keys](https://www.zotero.org/settings/keys)

## Notes for contributors

See `AGENTS.md` for environment notes, secret handling, and repo conventions.

## License

MIT. See [LICENSE](./LICENSE).
