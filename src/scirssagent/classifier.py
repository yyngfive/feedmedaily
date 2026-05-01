from __future__ import annotations

import json
from dataclasses import dataclass
from textwrap import dedent

from openai import OpenAI

from scirssagent.models import Classification, ClassificationProfile, Paper
from scirssagent.profile_compact import profile_prompt_payload


@dataclass(frozen=True)
class LlmConfig:
    api_key: str
    model: str
    base_url: str | None = None
    thinking: str = "disabled"


BASE_CLASSIFICATION_INSTRUCTIONS = dedent(
    """
    You are a careful scientific literature classifier.

    Classify each paper using only the user-supplied classification profile.
    Do not invent interests, labels, tags, or rules that are not present in the profile.

    Labels are fixed:
    - direct
    - indirect
    - unrelated

    topic_tags rules:
    - Only emit tag ids that exist in the profile topic taxonomy.
    - Use an empty list when no tag clearly applies.

    Return concise, evidence-based reasoning grounded in the title and abstract.
    """
).strip()


def profile_prompt_block(profile: ClassificationProfile) -> str:
    return json.dumps(
        profile_prompt_payload(profile, include_few_shots=bool(profile.few_shots)),
        ensure_ascii=False,
        indent=2,
    )


def classification_prompt(paper: Paper, profile: ClassificationProfile) -> str:
    return dedent(
        f"""
        {BASE_CLASSIFICATION_INSTRUCTIONS}

        User classification profile:
        {profile_prompt_block(profile)}

        Return only JSON with:
        relevance: direct | indirect | unrelated
        confidence: number between 0 and 1
        topic_tags: array of topic ids defined in the profile
        reason: one concise sentence
        recommended_action: read | scan | skip

        Title: {paper.title}
        Journal: {paper.journal or paper.feed_title or "unknown"}
        Abstract: {paper.abstract or "No abstract available."}
        """
    ).strip()


def batch_classification_prompt(
    papers: list[tuple[str, Paper]],
    profile: ClassificationProfile,
) -> str:
    compact_profile_json = json.dumps(
        profile_prompt_payload(profile, include_few_shots=bool(profile.few_shots)),
        ensure_ascii=False,
        indent=2,
    )
    items = []
    for item_id, paper in papers:
        items.append(
            {
                "id": item_id,
                "title": paper.title,
                "journal": paper.journal or paper.feed_title or "unknown",
                "abstract": paper.abstract or "No abstract available.",
            }
        )
    return dedent(
        f"""
        {BASE_CLASSIFICATION_INSTRUCTIONS}

        User classification profile:
        {compact_profile_json}

        Return valid JSON only, with this exact shape:
        {{
          "items": [
            {{
              "id": "string",
              "relevance": "direct | indirect | unrelated",
              "confidence": 0.0,
              "topic_tags": ["profile_topic_id"],
              "reason": "one concise sentence",
              "recommended_action": "read | scan | skip",
              "translated_title_zh": "concise Chinese title translation"
            }}
          ]
        }}

        Items:
        {json.dumps(items, ensure_ascii=False)}
        """
    ).strip()


def title_translation_prompt(papers: list[tuple[str, Paper]]) -> str:
    items = [{"id": item_id, "title": paper.title} for item_id, paper in papers]
    return dedent(
        f"""
        Translate each paper title into concise Simplified Chinese.

        Return valid JSON only, with this exact shape:
        {{
          "items": [
            {{
              "id": "string",
              "translated_title_zh": "简体中文标题"
            }}
          ]
        }}

        Items:
        {json.dumps(items, ensure_ascii=False)}
        """
    ).strip()


def _openai_client(config: LlmConfig) -> OpenAI:
    client_kwargs: dict[str, str] = {"api_key": config.api_key}
    if config.base_url:
        client_kwargs["base_url"] = config.base_url
    return OpenAI(**client_kwargs)


def llm_classify_batch(
    papers: list[Paper],
    profile: ClassificationProfile,
    config: LlmConfig,
) -> list[Classification]:
    client = _openai_client(config)
    indexed_papers = [(str(index + 1), paper) for index, paper in enumerate(papers)]
    request_kwargs: dict[str, object] = {
        "model": config.model,
        "messages": [
            {"role": "system", "content": "You are a careful scientific literature classifier."},
            {
                "role": "user",
                "content": batch_classification_prompt(indexed_papers, profile),
            },
        ],
        "temperature": 0,
        "max_tokens": max(600, 220 * len(papers)),
        "response_format": {"type": "json_object"},
        "extra_body": {"thinking": {"type": config.thinking}},
    }

    last_content = ""
    payload: dict[str, object] | None = None
    for _ in range(2):
        response = client.chat.completions.create(**request_kwargs)
        content = response.choices[0].message.content or ""
        if content.strip():
            payload = json.loads(content)
            break
        last_content = content
    if payload is None:
        raise ValueError(f"Batch model returned empty JSON content: {last_content!r}")
    raw_items = payload.get("items")
    if not isinstance(raw_items, list):
        raise ValueError("Batch JSON response missing items list")

    by_id: dict[str, Classification] = {}
    for item in raw_items:
        if not isinstance(item, dict) or "id" not in item:
            continue
        item_id = str(item["id"])
        by_id[item_id] = Classification.model_validate({**item, "model": config.model})

    missing_translation = [
        (item_id, paper)
        for item_id, paper in indexed_papers
        if item_id in by_id and not by_id[item_id].translated_title_zh
    ]
    if missing_translation:
        translated = llm_translate_titles_batch(missing_translation, config)
        for item_id, translated_title in translated.items():
            if item_id in by_id and translated_title:
                by_id[item_id] = by_id[item_id].model_copy(
                    update={"translated_title_zh": translated_title}
                )

    missing_ids = [item_id for item_id, _paper in indexed_papers if item_id not in by_id]
    if missing_ids:
        joined = ", ".join(missing_ids)
        raise ValueError(f"Batch response missing papers: {joined}")

    return [by_id[item_id] for item_id, _paper in indexed_papers]


def llm_translate_titles_batch(
    papers: list[tuple[str, Paper]],
    config: LlmConfig,
) -> dict[str, str]:
    client = _openai_client(config)
    request_kwargs: dict[str, object] = {
        "model": config.model,
        "messages": [
            {
                "role": "system",
                "content": "You translate scientific paper titles into concise Simplified Chinese.",
            },
            {"role": "user", "content": title_translation_prompt(papers)},
        ],
        "temperature": 0,
        "max_tokens": max(300, 120 * len(papers)),
        "response_format": {"type": "json_object"},
        "extra_body": {"thinking": {"type": "disabled"}},
    }

    response = client.chat.completions.create(**request_kwargs)
    content = response.choices[0].message.content or ""
    payload = json.loads(content)
    raw_items = payload.get("items")
    if not isinstance(raw_items, list):
        return {}

    translations: dict[str, str] = {}
    for item in raw_items:
        if not isinstance(item, dict) or "id" not in item:
            continue
        translated_title = str(item.get("translated_title_zh", "")).strip()
        if translated_title:
            translations[str(item["id"])] = translated_title
    return translations


def classify_papers(
    papers: list[Paper],
    *,
    profile: ClassificationProfile,
    config: LlmConfig,
) -> list[Classification]:
    return llm_classify_batch(papers, profile, config)
