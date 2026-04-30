from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

from dotenv import load_dotenv


@dataclass(frozen=True)
class Settings:
    root: Path
    feeds_path: Path
    data_dir: Path
    reports_dir: Path
    logs_dir: Path
    database_path: Path
    profile_path: Path
    classifier_api_key: str | None
    classifier_base_url: str
    classifier_model: str
    classifier_thinking: str
    classifier_batch_size: int
    profile_api_key: str | None
    profile_base_url: str
    profile_model: str
    profile_thinking: str
    zotero_api_key: str | None
    zotero_library_type: str
    zotero_library_id: str | None
    zotero_collection_key: str | None
    server_host: str
    server_port: int


def load_settings(root: Path | None = None) -> Settings:
    project_root = (root or Path.cwd()).resolve()
    load_dotenv(project_root / ".env")
    data_dir = project_root / "data"
    reports_dir = project_root / "reports"
    logs_dir = project_root / "logs"
    profile_value = os.getenv("SCIRSS_PROFILE_PATH")
    profile_path = (
        Path(profile_value) if profile_value else data_dir / "classification_profile.json"
    )
    if not profile_path.is_absolute():
        profile_path = (project_root / profile_path).resolve()
    return Settings(
        root=project_root,
        feeds_path=data_dir / "rss_feeds.json",
        data_dir=data_dir,
        reports_dir=reports_dir,
        logs_dir=logs_dir,
        database_path=data_dir / "literature.sqlite",
        profile_path=profile_path,
        classifier_api_key=os.getenv("SCIRSS_CLASSIFIER_API_KEY"),
        classifier_base_url=os.getenv(
            "SCIRSS_CLASSIFIER_BASE_URL",
            "https://api.deepseek.com",
        ),
        classifier_model=os.getenv("SCIRSS_CLASSIFIER_MODEL", "deepseek-v4-flash"),
        classifier_thinking=os.getenv("SCIRSS_CLASSIFIER_THINKING", "disabled").strip().lower(),
        classifier_batch_size=max(1, int(os.getenv("SCIRSS_CLASSIFIER_BATCH_SIZE", "10"))),
        profile_api_key=os.getenv("SCIRSS_PROFILE_API_KEY"),
        profile_base_url=os.getenv(
            "SCIRSS_PROFILE_BASE_URL",
            "https://api.deepseek.com",
        ),
        profile_model=os.getenv("SCIRSS_PROFILE_MODEL", "deepseek-v4-pro"),
        profile_thinking=os.getenv("SCIRSS_PROFILE_THINKING", "enabled").strip().lower(),
        zotero_api_key=os.getenv("SCIRSS_ZOTERO_API_KEY"),
        zotero_library_type=os.getenv("SCIRSS_ZOTERO_LIBRARY_TYPE", "user").strip().lower(),
        zotero_library_id=os.getenv("SCIRSS_ZOTERO_LIBRARY_ID"),
        zotero_collection_key=os.getenv("SCIRSS_ZOTERO_COLLECTION_KEY"),
        server_host=os.getenv("SCIRSS_SERVER_HOST", "127.0.0.1"),
        server_port=max(1, int(os.getenv("SCIRSS_SERVER_PORT", "8000"))),
    )
