from __future__ import annotations

import json
from textwrap import dedent
from typing import Any

import httpx
from openai import OpenAI

from scirssagent.config import Settings
from scirssagent.models import (
    Classification,
    ClassificationProfile,
    FeedbackRecord,
    Paper,
    ProfileMeta,
    RelevanceRules,
    ZoteroCollectionOption,
    ZoteroCollectionsResponse,
)
from scirssagent.profiles import validate_profile_json, write_profile


def profile_model_client(settings: Settings) -> OpenAI:
    if not settings.profile_api_key:
        raise ValueError(
            "SCIRSS_PROFILE_API_KEY is required for profile generation and prompt revision."
        )
    return OpenAI(api_key=settings.profile_api_key, base_url=settings.profile_base_url)


def _profile_contract() -> str:
    return json.dumps(
        {
            "meta": {
                "name": "short profile name",
                "version": 1,
                "created_at": "ISO-8601 datetime",
                "updated_at": "ISO-8601 datetime",
                "source_description": "original user interest description",
            },
            "scope": "one paragraph describing the reader's research interests",
            "relevance_rules": {
                "direct": ["rule string"],
                "indirect": ["rule string"],
                "unrelated": ["rule string"],
            },
            "topic_taxonomy": [
                {
                    "id": "snake_case_tag",
                    "label": "Display Label",
                    "description": "what this tag means",
                    "examples": ["optional example"],
                }
            ],
            "few_shots": [
                {
                    "title": "paper title",
                    "relevance": "direct",
                    "tags": ["snake_case_tag"],
                    "rationale": "why this label is correct",
                }
            ],
            "classification_notes": ["special boundary or exclusion rule"],
        },
        ensure_ascii=False,
        indent=2,
    )


def _chat_completion_content(response: Any) -> str:
    return response.choices[0].message.content or ""


def _request_profile_json(
    client: OpenAI,
    settings: Settings,
    *,
    system_prompt: str,
    user_prompt: str,
    max_tokens: int,
) -> str:
    response = client.chat.completions.create(
        model=settings.profile_model,
        messages=[
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_prompt},
        ],
        temperature=0,
        max_tokens=max_tokens,
        response_format={"type": "json_object"},
        extra_body={"thinking": {"type": settings.profile_thinking}},
    )
    return _chat_completion_content(response)


def _repair_profile_json(
    client: OpenAI,
    settings: Settings,
    *,
    malformed_content: str,
) -> ClassificationProfile:
    prompt = dedent(
        f"""
        Repair the malformed scientific-literature classification profile below.

        Requirements:
        - Return valid JSON only.
        - Return a complete profile object.
        - Follow the required schema exactly.
        - Keep the repaired content faithful to the original intent.
        - If the draft was truncated, infer the smallest sensible completion.

        Required JSON shape:
        {_profile_contract()}

        Malformed draft:
        {malformed_content}
        """
    ).strip()
    repaired = _request_profile_json(
        client,
        settings,
        system_prompt="You repair malformed JSON classification profiles.",
        user_prompt=prompt,
        max_tokens=4200,
    )
    return validate_profile_json(repaired)


def _coerce_profile_json(
    client: OpenAI,
    settings: Settings,
    *,
    content: str,
) -> ClassificationProfile:
    try:
        return validate_profile_json(content)
    except ValueError as first_error:
        try:
            return _repair_profile_json(client, settings, malformed_content=content)
        except ValueError as second_error:
            raise ValueError(
                "Model returned invalid classification profile JSON. "
                f"First parse failed: {first_error} Repair attempt failed: {second_error}"
            ) from second_error


def generate_initial_profile_payload(
    settings: Settings,
    *,
    interest_description: str,
    name: str | None = None,
) -> dict[str, object]:
    client = profile_model_client(settings)
    prompt = dedent(
        f"""
        Build a complete scientific-literature classification profile from the user's
        research interests.

        Requirements:
        - Return valid JSON only.
        - Use exactly the schema shape shown below.
        - The fixed relevance labels are direct, indirect, unrelated.
        - topic_taxonomy must define the only allowed topic tags.
        - Write practical, compact, reusable rules.
        - Include enough few-shot examples to teach the profile's boundaries.

        User profile name hint:
        {name or "Default profile"}

        User interest description:
        {interest_description}

        Required JSON shape:
        {_profile_contract()}
        """
    ).strip()
    content = _request_profile_json(
        client,
        settings,
        system_prompt="You design structured classification profiles.",
        user_prompt=prompt,
        max_tokens=4200,
    )
    profile = _coerce_profile_json(client, settings, content=content)
    summary = (
        f"Initial profile for {profile.meta.name} with "
        f"{len(profile.topic_taxonomy)} topic tags and {len(profile.few_shots)} few-shot examples."
    )
    return {"summary": summary, "proposed_profile": profile}


def generate_profile_proposal_payload(
    settings: Settings,
    current_profile: ClassificationProfile,
    feedback_items: list[FeedbackRecord],
) -> dict[str, object]:
    if not feedback_items:
        raise ValueError("No feedback is available for profile proposal generation.")
    client = profile_model_client(settings)
    feedback_payload = [
        {
            "feedback_id": item.id,
            "paper_id": item.paper_id,
            "paper_title": item.paper_title,
            "original_relevance": item.original_relevance.value,
            "corrected_relevance": item.corrected_relevance.value,
            "note": item.note,
        }
        for item in feedback_items
    ]
    prompt = dedent(
        f"""
        Revise the current scientific-literature classification profile using human feedback.

        Requirements:
        - Return valid JSON only.
        - Return a complete new profile, not a patch.
        - Preserve useful existing rules and tags unless the feedback implies they should change.
        - Keep direct/indirect/unrelated as the only relevance labels.
        - topic_taxonomy defines the only allowed topic tags.
        - Update few-shot examples and notes when they help future classification.
        - Use the feedback to sharpen boundaries and reduce future mistakes.

        Current profile:
        {json.dumps(current_profile.model_dump(mode="json"), ensure_ascii=False, indent=2)}

        Human feedback:
        {json.dumps(feedback_payload, ensure_ascii=False, indent=2)}

        Required JSON shape:
        {_profile_contract()}
        """
    ).strip()
    content = _request_profile_json(
        client,
        settings,
        system_prompt="You revise structured classification profiles.",
        user_prompt=prompt,
        max_tokens=4600,
    )
    proposed_profile = _coerce_profile_json(client, settings, content=content)
    summary = (
        f"Updated profile from {len(feedback_items)} feedback item(s); "
        f"{len(proposed_profile.topic_taxonomy)} topic tags, "
        f"{len(proposed_profile.few_shots)} few-shot examples."
    )
    return {
        "summary": summary,
        "proposed_profile": proposed_profile,
        "model": settings.profile_model,
        "source_feedback_ids": [item.id for item in feedback_items],
    }


def write_current_profile(settings: Settings, profile: ClassificationProfile) -> None:
    write_profile(settings.profile_path, profile)


def default_profile(
    *,
    source_description: str,
    name: str = "Default profile",
) -> ClassificationProfile:
    from datetime import UTC, datetime

    now = datetime.now(UTC)
    return ClassificationProfile(
        meta=ProfileMeta(
            name=name,
            version=1,
            created_at=now,
            updated_at=now,
            source_description=source_description,
        ),
        scope=source_description,
        relevance_rules=RelevanceRules(),
        topic_taxonomy=[],
        few_shots=[],
        classification_notes=[],
    )


def zotero_item_payload(
    paper: Paper,
    classification: Classification,
    collection_key: str | None,
) -> dict:
    creators = []
    for author in paper.authors:
        parts = [part for part in author.replace(",", " ").split() if part]
        if not parts:
            continue
        creators.append(
            {
                "creatorType": "author",
                "firstName": " ".join(parts[:-1]),
                "lastName": parts[-1],
            }
        )
    tags = [
        {"tag": "scirssagent"},
        {"tag": classification.relevance.value},
        *({"tag": tag} for tag in classification.topic_tags[:8]),
    ]
    payload = {
        "itemType": "journalArticle",
        "title": paper.title,
        "creators": creators,
        "abstractNote": paper.abstract or "",
        "publicationTitle": paper.journal or paper.feed_title or "",
        "date": paper.published_date.isoformat() if paper.published_date else "",
        "DOI": paper.doi or "",
        "url": paper.url,
        "tags": tags,
    }
    if collection_key:
        payload["collections"] = [collection_key]
    return payload


def _zotero_library_prefix(settings: Settings) -> str:
    if not settings.zotero_api_key:
        raise ValueError(
            "SCIRSS_ZOTERO_API_KEY is not configured. Add it to your .env file first."
        )
    if not settings.zotero_library_id:
        raise ValueError(
            "SCIRSS_ZOTERO_LIBRARY_ID is not configured. "
            "Add your Zotero user or group library ID to the .env file."
        )
    library_type = settings.zotero_library_type
    if library_type not in {"user", "group"}:
        raise ValueError(
            "SCIRSS_ZOTERO_LIBRARY_TYPE must be 'user' or 'group'. "
            "Check the .env setting before saving to Zotero."
        )
    return f"{library_type}s/{settings.zotero_library_id}"


def list_zotero_collections(settings: Settings) -> ZoteroCollectionsResponse:
    prefix = _zotero_library_prefix(settings)
    response = httpx.get(
        f"https://api.zotero.org/{prefix}/collections",
        headers={
            "Zotero-API-Version": "3",
            "Zotero-API-Key": settings.zotero_api_key or "",
        },
        params={"format": "json", "limit": 1000},
        timeout=30,
    )
    if response.status_code >= 400:
        raise ValueError(f"Zotero API error {response.status_code}: {response.text}")
    payload = response.json()
    if not isinstance(payload, list):
        raise ValueError("Unexpected Zotero collections response.")

    by_key: dict[str, dict[str, Any]] = {}
    for item in payload:
        if not isinstance(item, dict):
            continue
        data = item.get("data")
        key = item.get("key")
        if isinstance(data, dict) and isinstance(key, str):
            by_key[key] = data

    def build_path_label(collection_key: str) -> str:
        names: list[str] = []
        current_key: str | None = collection_key
        while current_key:
            current = by_key.get(current_key)
            if current is None:
                break
            names.append(str(current.get("name") or current_key))
            parent_key = current.get("parentCollection")
            current_key = str(parent_key) if isinstance(parent_key, str) and parent_key else None
        return " / ".join(reversed(names))

    default_key = settings.zotero_collection_key or None
    collections = [
        ZoteroCollectionOption(
            key=collection_key,
            name=str(data.get("name") or collection_key),
            path_label=build_path_label(collection_key),
            parent_key=(
                str(data.get("parentCollection"))
                if isinstance(data.get("parentCollection"), str) and data.get("parentCollection")
                else None
            ),
            is_default=collection_key == default_key,
        )
        for collection_key, data in sorted(
            by_key.items(),
            key=lambda item: build_path_label(item[0]).lower(),
        )
    ]
    return ZoteroCollectionsResponse(
        collections=collections,
        default_collection_key=default_key,
    )


def save_paper_to_zotero(
    settings: Settings,
    paper: Paper,
    classification: Classification,
    collection_key: str | None = None,
) -> tuple[str, str | None]:
    prefix = _zotero_library_prefix(settings)
    url = f"https://api.zotero.org/{prefix}/items"
    target_collection_key = (
        collection_key if collection_key is not None else settings.zotero_collection_key
    )
    if target_collection_key == "":
        target_collection_key = None
    response = httpx.post(
        url,
        headers={
            "Zotero-API-Version": "3",
            "Zotero-API-Key": settings.zotero_api_key or "",
            "Content-Type": "application/json",
        },
        content=json.dumps(
            [zotero_item_payload(paper, classification, target_collection_key)],
            ensure_ascii=False,
        ).encode("utf-8"),
        timeout=30,
    )
    if response.status_code >= 400:
        raise ValueError(f"Zotero API error {response.status_code}: {response.text}")
    payload = response.json()
    successful = payload.get("successful") or {}
    item_info = next(iter(successful.values()), None)
    item_key = item_info.get("key") if isinstance(item_info, dict) else None
    return response.text, item_key
