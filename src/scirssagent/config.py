from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

from dotenv import load_dotenv


@dataclass(frozen=True)
class Settings:
    root: Path
    feeds_file: Path
    data_dir: Path
    reports_dir: Path
    logs_dir: Path
    database_path: Path
    openai_api_key: str | None
    openai_model: str


def load_settings(root: Path | None = None) -> Settings:
    project_root = (root or Path.cwd()).resolve()
    load_dotenv(project_root / ".env")
    data_dir = project_root / "data"
    reports_dir = project_root / "reports"
    logs_dir = project_root / "logs"
    return Settings(
        root=project_root,
        feeds_file=project_root / "RSS.txt",
        data_dir=data_dir,
        reports_dir=reports_dir,
        logs_dir=logs_dir,
        database_path=data_dir / "literature.sqlite",
        openai_api_key=os.getenv("SCIRSS_OPENAI_API_KEY") or os.getenv("OPENAI_API_KEY"),
        openai_model=os.getenv("SCIRSS_OPENAI_MODEL", "gpt-4.1-mini"),
    )

