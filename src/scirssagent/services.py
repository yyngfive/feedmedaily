from __future__ import annotations

import json
from datetime import UTC, datetime
from textwrap import dedent
from typing import Any

import httpx
from openai import OpenAI
from pydantic import ValidationError

from scirssagent.config import Settings
from scirssagent.models import (
    Classification,
    ClassificationProfile,
    FeedbackProposalContext,
    Paper,
    ProfileMeta,
    ProfileProposalDelta,
    ZoteroCollectionOption,
    ZoteroCollectionsResponse,
)
from scirssagent.profile_compact import (
    compact_profile,
    merge_rule_list,
    profile_prompt_payload,
    slim_topic_taxonomy,
)
from scirssagent.profiles import validate_profile_json, write_profile


def profile_model_client(settings: Settings) -> OpenAI:
    if not settings.profile_api_key:
        raise ValueError(
            "SCIRSS_PROFILE_API_KEY is required for profile generation and prompt revision."
        )
    return OpenAI(api_key=settings.profile_api_key, base_url=settings.profile_base_url)


def _compact_profile_contract() -> str:
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
        {_compact_profile_contract()}

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


def _profile_delta_contract() -> str:
    return json.dumps(
        {
            "summary": "short summary of what changed based on feedback",
            "direct_rule_additions": ["rule to append to direct relevance rules"],
            "indirect_rule_additions": ["rule to append to indirect relevance rules"],
            "unrelated_rule_additions": ["rule to append to unrelated relevance rules"],
            "scope_rewrite": "optional short rewritten scope summary",
            "tag_additions": [{"id": "snake_case_tag", "label": "Display Label"}],
            "tag_removals": ["old_tag_id"],
        },
        ensure_ascii=False,
        indent=2,
    )


def _load_json_object(payload: str) -> dict[str, Any]:
    loaded = json.loads(payload)
    if not isinstance(loaded, dict):
        raise ValueError("Model JSON response must be an object.")
    return loaded


def _repair_profile_delta_json(
    client: OpenAI,
    settings: Settings,
    *,
    malformed_content: str,
) -> ProfileProposalDelta:
    prompt = dedent(
        f"""
        Repair the malformed profile-update delta below.

        Requirements:
        - Return valid JSON only.
        - Return a complete delta object.
        - Follow the required schema exactly.
        - Keep the repaired content faithful to the original intent.
        - If the draft was truncated, infer the smallest sensible completion.

        Required JSON shape:
        {_profile_delta_contract()}

        Malformed draft:
        {malformed_content}
        """
    ).strip()
    repaired = _request_profile_json(
        client,
        settings,
        system_prompt="You repair malformed JSON profile update deltas.",
        user_prompt=prompt,
        max_tokens=2200,
    )
    try:
        return ProfileProposalDelta.model_validate(_load_json_object(repaired))
    except (ValidationError, ValueError) as exc:
        raise ValueError(f"Invalid profile delta JSON: {exc}") from exc


def _coerce_profile_delta_json(
    client: OpenAI,
    settings: Settings,
    *,
    content: str,
) -> ProfileProposalDelta:
    try:
        return ProfileProposalDelta.model_validate(_load_json_object(content))
    except (ValidationError, ValueError) as first_error:
        try:
            return _repair_profile_delta_json(client, settings, malformed_content=content)
        except ValueError as second_error:
            raise ValueError(
                "Model returned invalid profile delta JSON. "
                f"First parse failed: {first_error} Repair attempt failed: {second_error}"
            ) from second_error


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


def _normalized_profile_meta(current_profile: ClassificationProfile) -> ProfileMeta:
    return ProfileMeta(
        name=current_profile.meta.name,
        version=current_profile.meta.version + 1,
        created_at=current_profile.meta.created_at,
        updated_at=datetime.now(UTC),
        source_description=current_profile.meta.source_description,
    )


def _bounded_rule_delta(rule_delta: ProfileProposalDelta) -> ProfileProposalDelta:
    return rule_delta.model_copy(
        update={
            "direct_rule_additions": rule_delta.direct_rule_additions[:6],
            "indirect_rule_additions": rule_delta.indirect_rule_additions[:6],
            "unrelated_rule_additions": rule_delta.unrelated_rule_additions[:6],
            "tag_additions": slim_topic_taxonomy(rule_delta.tag_additions[:3]),
            "tag_removals": rule_delta.tag_removals[:3],
        }
    )


def _initial_profile_delta(
    profile: ClassificationProfile,
    *,
    summary: str,
) -> ProfileProposalDelta:
    return ProfileProposalDelta(
        summary=summary,
        direct_rule_additions=profile.relevance_rules.direct,
        indirect_rule_additions=profile.relevance_rules.indirect,
        unrelated_rule_additions=profile.relevance_rules.unrelated,
        scope_rewrite=profile.scope,
        tag_additions=profile.topic_taxonomy,
        tag_removals=[],
    )


def _merge_profile_delta(
    current_profile: ClassificationProfile,
    rule_delta: ProfileProposalDelta,
) -> ClassificationProfile:
    current_compact = compact_profile(
        current_profile,
        include_few_shots=bool(current_profile.few_shots),
    )
    bounded_delta = _bounded_rule_delta(rule_delta)
    removals = set(bounded_delta.tag_removals)
    merged_topics = [
        topic for topic in current_compact.topic_taxonomy if topic.id not in removals
    ]
    merged_ids = {topic.id for topic in merged_topics}
    for topic in bounded_delta.tag_additions:
        if topic.id in merged_ids:
            continue
        merged_topics.append(topic)
        merged_ids.add(topic.id)

    merged_few_shots = [
        item.model_copy(update={"tags": [tag for tag in item.tags if tag in merged_ids]})
        for item in current_compact.few_shots
    ]

    return current_compact.model_copy(
        update={
            "scope": bounded_delta.scope_rewrite or current_compact.scope,
            "relevance_rules": current_compact.relevance_rules.model_copy(
                update={
                    "direct": merge_rule_list(
                        current_compact.relevance_rules.direct,
                        bounded_delta.direct_rule_additions,
                    ),
                    "indirect": merge_rule_list(
                        current_compact.relevance_rules.indirect,
                        bounded_delta.indirect_rule_additions,
                    ),
                    "unrelated": merge_rule_list(
                        current_compact.relevance_rules.unrelated,
                        bounded_delta.unrelated_rule_additions,
                    ),
                }
            ),
            "topic_taxonomy": merged_topics,
            "few_shots": merged_few_shots,
        }
    )


def _destructive_revision_reason(
    current_profile: ClassificationProfile,
    proposed_profile: ClassificationProfile,
) -> str | None:
    current_tags = {item.id for item in current_profile.topic_taxonomy}
    proposed_tags = {item.id for item in proposed_profile.topic_taxonomy}
    overlap = current_tags & proposed_tags

    if current_tags and not proposed_tags:
        return "Generated proposal removed every existing topic tag."

    if (
        len(current_tags) >= 5
        and len(proposed_tags) <= max(2, len(current_tags) // 3)
        and len(overlap) < max(2, len(current_tags) // 3)
    ):
        return (
            "Generated proposal collapsed the topic taxonomy too aggressively "
            f"({len(current_tags)} -> {len(proposed_tags)} tags, overlap {len(overlap)})."
        )

    if (
        "general scientific literature" in proposed_profile.scope.lower()
        and current_profile.scope.strip() != proposed_profile.scope.strip()
    ):
        return "Generated proposal replaced the specific research scope with a generic scope."

    return None


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
        - topic_taxonomy must be lightweight and contain only id + label.
        - Write practical, compact, reusable rules.
        - few_shots are optional and must contain at most 2 examples total.
        - Do not include description/examples fields for tags.
        - Do not include a generic placeholder profile.

        User profile name hint:
        {name or "Default profile"}

        User interest description:
        {interest_description}

        Required JSON shape:
        {_compact_profile_contract()}
        """
    ).strip()
    content = _request_profile_json(
        client,
        settings,
        system_prompt="You design structured classification profiles.",
        user_prompt=prompt,
        max_tokens=4200,
    )
    profile = compact_profile(
        _coerce_profile_json(client, settings, content=content),
        include_few_shots=True,
    )
    summary = (
        f"Initial profile for {profile.meta.name} with "
        f"{len(profile.topic_taxonomy)} topic tags and {len(profile.few_shots)} few-shot examples."
    )
    return {
        "summary": summary,
        "proposed_profile": profile,
        "rule_delta": _initial_profile_delta(profile, summary=summary),
    }


def generate_profile_proposal_payload(
    settings: Settings,
    current_profile: ClassificationProfile,
    feedback_items: list[FeedbackProposalContext],
) -> dict[str, object]:
    if not feedback_items:
        raise ValueError("No feedback is available for profile proposal generation.")
    client = profile_model_client(settings)
    compact_profile_json = json.dumps(
        profile_prompt_payload(current_profile, include_few_shots=False),
        ensure_ascii=False,
        indent=2,
    )
    feedback_payload = [
        {
            "feedback_id": item.feedback_id,
            "paper_id": item.paper_id,
            "paper_title": item.paper_title,
            "journal": item.journal,
            "abstract": item.abstract,
            "original_relevance": item.original_relevance.value,
            "corrected_relevance": item.corrected_relevance.value,
            "note": item.note,
        }
        for item in feedback_items
    ]
    prompt = dedent(
        f"""
        Summarize the human feedback and propose compact rule updates for the current
        scientific-literature classification profile.

        Requirements:
        - Return valid JSON only.
        - Return a compact delta object, not a full profile.
        - First infer the shared patterns behind the corrected labels.
        - Then convert those patterns into reusable relevance-rule additions.
        - Keep the current profile as the base document.
        - Do not rewrite unrelated sections of the profile.
        - Do not replace a specific scientific-interest profile with a generic
          or placeholder profile.
        - Keep direct/indirect/unrelated as the only relevance labels.
        - Only propose small bounded tag edits when clearly necessary.
        - Do not generate few-shot examples.
        - Use the feedback to sharpen boundaries and reduce future mistakes.

        Current compact profile context:
        {compact_profile_json}

        Human feedback:
        {json.dumps(feedback_payload, ensure_ascii=False, indent=2)}

        Return:
        - a short summary of the shared patterns you found
        - direct_rule_additions for patterns that should be treated as directly relevant
        - indirect_rule_additions for patterns that should be treated as indirectly relevant
        - unrelated_rule_additions for patterns that should be treated as unrelated
        - optional scope_rewrite only if the current scope summary is misleading
        - optional bounded tag_additions and tag_removals

        Required JSON shape:
        {_profile_delta_contract()}
        """
    ).strip()
    content = _request_profile_json(
        client,
        settings,
        system_prompt="You summarize feedback into compact profile rule deltas.",
        user_prompt=prompt,
        max_tokens=2600,
    )
    rule_delta = _bounded_rule_delta(
        _coerce_profile_delta_json(client, settings, content=content)
    )
    merged_profile = _merge_profile_delta(current_profile, rule_delta)
    proposed_profile = merged_profile.model_copy(
        update={"meta": _normalized_profile_meta(current_profile)}
    )
    destructive_reason = _destructive_revision_reason(current_profile, proposed_profile)
    if destructive_reason:
        raise ValueError(
            "Model generated an unsafe profile proposal. "
            f"{destructive_reason} Current profile was kept unchanged."
        )
    return {
        "summary": rule_delta.summary,
        "proposed_profile": proposed_profile,
        "rule_delta": rule_delta,
        "model": settings.profile_model,
        "source_feedback_ids": [item.feedback_id for item in feedback_items],
    }


def write_current_profile(settings: Settings, profile: ClassificationProfile) -> None:
    write_profile(settings.profile_path, profile)


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
