# FeedMeDaily

![FeedMeDaily banner](./assets/branding/feedmedaily-icon.svg)

FeedMeDaily is a local-first paper triage app for journal RSS feeds. It stores paper metadata in SQLite, classifies relevance with profile-driven LLM prompts, serves a local review UI, and lets you send selected papers to Zotero.

Current system architecture is documented in [ARCHITECTURE.md](./ARCHITECTURE.md).

## Source mode

If you want to run FeedMeDaily from source:

```powershell
pnpm --dir web install
pnpm --dir web build
go run .\cmd\feedmedaily-tray --root .
```

To build the experimental Windows tray and Go backend executables after Go is installed:

```powershell
pwsh -File .\tools\build_go_tray.ps1
```

To run the Go backend service directly:

```powershell
go run .\cmd\feedmedailyd --root . --host 127.0.0.1 --port 8000
```

The tray stores its local settings in `tray-settings.json` at the app config root. In source mode that file sits at the repository root. Backend command overrides are no longer part of the migration branch: the tray expects `feedmedailyd.exe` in release builds and builds a local cached backend binary in source mode.

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
- If a provider times out or fails in thinking mode, the Go classifier/profile flows retry once with `thinking=disabled`.

## Commands

```powershell
go run .\cmd\feedmedaily-tray --root .
go run .\cmd\feedmedailyd --root . --host 127.0.0.1 --port 8000
go run .\cmd\feedmedailyd --root . --run-once
go run .\cmd\feedmedailyd --root . --report-latest
go run .\cmd\feedmedailyd --root . --zotero-collections
go run .\cmd\feedmedailyd --root . --zotero-save --paper-id 1
```

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

## License

MIT. See [LICENSE](./LICENSE).
