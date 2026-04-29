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
    llm_provider: str
    llm_api_key: str | None
    llm_base_url: str | None
    llm_model: str
    llm_thinking: str
    llm_batch_size: int


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
        llm_provider=os.getenv("SCIRSS_LLM_PROVIDER", "deepseek").strip().lower(),
        llm_api_key=(
            os.getenv("SCIRSS_LLM_API_KEY")
            or os.getenv("DEEPSEEK_API_KEY")
            or os.getenv("SCIRSS_OPENAI_API_KEY")
            or os.getenv("OPENAI_API_KEY")
        ),
        llm_base_url=os.getenv("SCIRSS_LLM_BASE_URL", "https://api.deepseek.com"),
        llm_model=os.getenv("SCIRSS_LLM_MODEL", "deepseek-v4-flash"),
        llm_thinking=os.getenv("SCIRSS_LLM_THINKING", "disabled").strip().lower(),
        llm_batch_size=max(1, int(os.getenv("SCIRSS_LLM_BATCH_SIZE", "10"))),
    )
