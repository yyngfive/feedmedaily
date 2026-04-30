from __future__ import annotations

import json
from datetime import UTC, datetime
from pathlib import Path

from pydantic import ValidationError

from scirssagent.models import ClassificationProfile, ProfileMeta


def read_profile(path: Path | None) -> ClassificationProfile | None:
    if path is None or not path.exists():
        return None
    payload = json.loads(path.read_text(encoding="utf-8"))
    return ClassificationProfile.model_validate(payload)


def write_profile(path: Path, profile: ClassificationProfile) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(profile.model_dump(mode="json"), ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


def ensure_profile(path: Path) -> ClassificationProfile:
    profile = read_profile(path)
    if profile is None:
        raise ValueError(f"Classification profile not found: {path}")
    return profile


def next_profile_version(profile: ClassificationProfile) -> int:
    return profile.meta.version + 1


def replace_profile_version(
    profile: ClassificationProfile,
    *,
    version: int,
    source_description: str | None = None,
) -> ClassificationProfile:
    now = datetime.now(UTC)
    meta = ProfileMeta(
        name=profile.meta.name,
        version=version,
        created_at=profile.meta.created_at,
        updated_at=now,
        source_description=source_description or profile.meta.source_description,
    )
    return profile.model_copy(update={"meta": meta})


def validate_profile_json(payload: str | bytes | dict) -> ClassificationProfile:
    if isinstance(payload, dict):
        data = payload
    else:
        data = json.loads(payload)
    try:
        return ClassificationProfile.model_validate(data)
    except ValidationError as exc:
        raise ValueError(f"Invalid classification profile JSON: {exc}") from exc
