from __future__ import annotations

import json
from datetime import UTC, datetime
from json import JSONDecodeError
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


def _strip_code_fence(payload: str) -> str:
    stripped = payload.strip()
    if not stripped.startswith("```"):
        return stripped
    lines = stripped.splitlines()
    if not lines:
        return stripped
    if lines[0].startswith("```"):
        lines = lines[1:]
    if lines and lines[-1].strip() == "```":
        lines = lines[:-1]
    return "\n".join(lines).strip()


def _extract_json_object(payload: str) -> str | None:
    start = payload.find("{")
    if start < 0:
        return None
    depth = 0
    in_string = False
    escaped = False
    for index in range(start, len(payload)):
        char = payload[index]
        if in_string:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                in_string = False
            continue
        if char == '"':
            in_string = True
            continue
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return payload[start : index + 1]
    return None


def _load_profile_json(payload: str | bytes | dict) -> dict:
    if isinstance(payload, dict):
        return payload
    if isinstance(payload, bytes):
        text = payload.decode("utf-8")
    else:
        text = payload
    stripped = _strip_code_fence(text)
    try:
        loaded = json.loads(stripped)
        if not isinstance(loaded, dict):
            raise ValueError("Classification profile JSON must be an object.")
        return loaded
    except JSONDecodeError:
        extracted = _extract_json_object(stripped)
        if extracted and extracted != stripped:
            try:
                loaded = json.loads(extracted)
                if not isinstance(loaded, dict):
                    raise ValueError("Classification profile JSON must be an object.")
                return loaded
            except JSONDecodeError as extracted_exc:
                raise ValueError(
                    "Invalid classification profile JSON: "
                    f"{extracted_exc.msg} at line {extracted_exc.lineno}, "
                    f"column {extracted_exc.colno}."
                ) from extracted_exc
        raise ValueError(
            "Invalid classification profile JSON: could not find a complete JSON object."
        ) from None


def validate_profile_json(payload: str | bytes | dict) -> ClassificationProfile:
    data = _load_profile_json(payload)
    try:
        return ClassificationProfile.model_validate(data)
    except ValidationError as exc:
        raise ValueError(f"Invalid classification profile JSON: {exc}") from exc
