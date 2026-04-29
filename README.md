# SciRSSAgent

SciRSSAgent monitors journal RSS feeds, stores paper metadata in SQLite, classifies relevance to chemical biology / nucleic acid chemistry / directed evolution interests, and exports a local static report.

## Toolchain

- Python: `uv` with Python 3.12
- Frontend: Vite + React + TypeScript + Tailwind CSS
- Package manager: `pnpm`
- Version control: Git

`mamba` is kept as a future fallback for heavier scientific or GPU/local-model dependencies; the first version uses `uv` because the backend is mostly HTTP, RSS parsing, SQLite, JSON validation, and report export.

## Quick Start

```powershell
uv sync
pnpm --dir web install
uv run scirssagent run --once
pnpm --dir web build
```

Open `reports/latest/index.html` after a run. If `SCIRSS_OPENAI_API_KEY` is not set, SciRSSAgent uses a deterministic fallback classifier so the pipeline still works.

## Secrets

Keep real API keys in `.env` or user-level environment variables. `.env` is ignored by Git; only `.env.example` with blank placeholders should be committed.

If `python` is not on PATH, that is still fine: `uv` can install and manage the project Python itself.

```powershell
uv python install 3.12
uv sync
```

If uv cannot access its default user cache on Windows, keep its managed Python and cache inside this project:

```powershell
$env:UV_CACHE_DIR = ".uv-cache"
$env:UV_PYTHON_INSTALL_DIR = ".uv-python"
uv sync
```

If uv warns that hardlinking failed, use copy mode on Windows:

```powershell
$env:UV_LINK_MODE = "copy"
uv sync
```

Use mamba later if the project grows into GPU/local-model or heavier scientific dependencies.

## Commands

```powershell
uv run scirssagent run --once
uv run scirssagent report --latest
uv run scirssagent init-task
uv run pytest
uv run ruff check
pnpm --dir web test
pnpm --dir web build
```

With nvm-windows, use the installed Node 22 LTS line before frontend checks:

```powershell
nvm use (Get-Content .nvmrc).Trim()
pnpm --dir web test
pnpm --dir web build
```

## Inputs and Outputs

- `RSS.txt`: one RSS feed URL per line.
- `data/literature.sqlite`: local paper database.
- `reports/data/latest.json`: latest report data for the React app.
- `reports/latest/index.html`: static report entrypoint.
- `logs/YYYY-MM-DD.log`: daily run logs.

## Windows Scheduled Task

Use `uv run scirssagent init-task --print-only` to print a PowerShell command for creating a daily 10:00 task. Review it before running.
