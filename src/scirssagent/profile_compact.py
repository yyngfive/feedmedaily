from __future__ import annotations

from scirssagent.models import (
    ClassificationProfile,
    ProfileFewShot,
    RelevanceRules,
    TopicDefinition,
)

FEW_SHOT_LIMIT = 2


def normalize_text(value: str) -> str:
    return " ".join(value.split()).strip()


def normalize_rule_list(items: list[str]) -> list[str]:
    normalized: list[str] = []
    seen: set[str] = set()
    for item in items:
        clean = normalize_text(item)
        if clean and clean not in seen:
            seen.add(clean)
            normalized.append(clean)
    return normalized


def merge_rule_list(existing: list[str], additions: list[str]) -> list[str]:
    return normalize_rule_list([*existing, *additions])


def slim_topic_taxonomy(items: list[TopicDefinition]) -> list[TopicDefinition]:
    slimmed: list[TopicDefinition] = []
    seen: set[str] = set()
    for item in items:
        topic = TopicDefinition(id=item.id, label=item.label)
        if topic.id in seen:
            continue
        seen.add(topic.id)
        slimmed.append(topic)
    return slimmed


def trim_few_shots(
    items: list[ProfileFewShot],
    limit: int = FEW_SHOT_LIMIT,
) -> list[ProfileFewShot]:
    trimmed: list[ProfileFewShot] = []
    for item in items[:limit]:
        trimmed.append(
            ProfileFewShot(
                title=normalize_text(item.title),
                relevance=item.relevance,
                tags=item.tags,
                rationale=normalize_text(item.rationale),
            )
        )
    return trimmed


def compact_rules(rules: RelevanceRules) -> RelevanceRules:
    return RelevanceRules(
        direct=normalize_rule_list(rules.direct),
        indirect=normalize_rule_list(rules.indirect),
        unrelated=normalize_rule_list(rules.unrelated),
    )


def compact_profile(
    profile: ClassificationProfile,
    *,
    include_few_shots: bool,
    few_shot_limit: int = FEW_SHOT_LIMIT,
) -> ClassificationProfile:
    return profile.model_copy(
        update={
            "scope": normalize_text(profile.scope),
            "relevance_rules": compact_rules(profile.relevance_rules),
            "topic_taxonomy": slim_topic_taxonomy(profile.topic_taxonomy),
            "few_shots": trim_few_shots(profile.few_shots, limit=few_shot_limit)
            if include_few_shots
            else [],
        }
    )


def persisted_profile(profile: ClassificationProfile) -> ClassificationProfile:
    return compact_profile(profile, include_few_shots=True)


def profile_prompt_payload(
    profile: ClassificationProfile,
    *,
    include_few_shots: bool,
    few_shot_limit: int = FEW_SHOT_LIMIT,
) -> dict[str, object]:
    compact = compact_profile(
        profile,
        include_few_shots=include_few_shots,
        few_shot_limit=few_shot_limit,
    )
    payload: dict[str, object] = {
        "scope": compact.scope,
        "relevance_rules": compact.relevance_rules.model_dump(mode="json"),
        "topic_taxonomy": [
            {"id": item.id, "label": item.label} for item in compact.topic_taxonomy
        ],
    }
    if compact.few_shots:
        payload["few_shots"] = [
            {
                "title": item.title,
                "relevance": item.relevance.value,
                "tags": item.tags,
                "rationale": item.rationale,
            }
            for item in compact.few_shots
        ]
    return payload
