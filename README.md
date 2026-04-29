# SciRSSAgent

SciRSSAgent monitors journal RSS feeds, stores paper metadata in SQLite, classifies relevance to chemical biology / nucleic acid chemistry / directed evolution interests, and exports a local static report.

## Structure

- `src/scirssagent/`: Python backend pipeline and CLI
- `tests/`: backend tests
- `web/`: Vite + React + TypeScript report UI
- `RSS.txt`: RSS feed list, one URL per line
- `data/literature.sqlite`: local paper database
- `reports/data/latest.json`: latest report data for the frontend
- `reports/latest/index.html`: static report entrypoint
- `logs/YYYY-MM-DD.log`: daily run logs

## Python Architecture

- `config.py`: load runtime configuration from `.env`
- `feeds.py`: fetch and parse RSS/Atom feeds
- `metadata.py`: enrich papers with DOI, journal, and abstract metadata
- `storage.py`: persist papers and classifications in SQLite
- `classifier.py`: run heuristic or LLM-based batch classification
- `pipeline.py`: orchestrate fetch, dedupe, classify, and report generation
- `reporting.py`: write JSON report artifacts and publish the static site bundle
- `cli.py`: expose `run`, `report`, and scheduled-task commands

Current flow:
`RSS.txt` -> feed fetch -> paper dedupe/upsert -> metadata enrichment -> pending classification queue -> SQLite classifications -> report JSON -> static report page

## Web Architecture

- `web/src/main.tsx`: app entrypoint
- `web/src/App.tsx`: HeroUI-based paper review dashboard
- `web/src/`: React UI, report data loader, and shared types for the local report reader
- `reports/latest/report-data.js`: embedded report payload written by the backend
- `reports/latest/index.html`: static page that reads embedded data and renders the report

Current flow:
`pnpm --dir web build` writes `web/dist/` -> Python copies built assets into `reports/latest/` and embeds `report-data.js` -> browser opens local `index.html` -> page renders the review dashboard with filters, relevance tabs, paper cards, and a detail panel

## Quick Start

```powershell
uv sync
pnpm --dir web install
pnpm --dir web build
uv run scirssagent run --once
```

Open `reports/latest/index.html` after a run. If `SCIRSS_LLM_API_KEY` is not set, SciRSSAgent uses a deterministic fallback classifier so the pipeline still works.

After changing frontend code, rebuild and republish the latest report:

```powershell
pnpm --dir web build
uv run scirssagent report latest
```

## Commands

```powershell
uv run scirssagent run --once
uv run scirssagent run --once --max-papers 10
uv run scirssagent run --once --max-papers 10 --reclassify
uv run scirssagent report latest
uv run scirssagent init-task
uv run pytest
uv run ruff check
pnpm --dir web test
pnpm --dir web build
uv run scirssagent report latest
```

With nvm-windows:

```powershell
nvm use (Get-Content .nvmrc).Trim()
pnpm --dir web test
pnpm --dir web build
```

## Windows Scheduled Task

Use `uv run scirssagent init-task --print-only` to print a PowerShell command for creating a daily 10:00 task. Review it before running.

See `AGENTS.md` for environment notes, secret handling, and repository conventions.
