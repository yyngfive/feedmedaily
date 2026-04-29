from __future__ import annotations

import json
from dataclasses import dataclass
from textwrap import dedent

from openai import OpenAI

from scirssagent.models import Classification, Paper, Relevance


@dataclass(frozen=True)
class LlmConfig:
    provider: str
    api_key: str | None
    model: str
    base_url: str | None = None
    thinking: str = "disabled"

DIRECT_TERMS = {
    "directed evolution": "directed_evolution",
    "evolution of enzymes": "directed_evolution",
    "polymerase": "polymerase_engineering",
    "nucleic acid": "nucleic_acid_chemistry",
    "oligonucleotide": "oligonucleotide_chemistry",
    "aptamer": "aptamer",
    "selex": "selex",
    "xna": "xna",
    "tna": "xna",
    "modified nucleotide": "modified_nucleotide",
    "genetic code expansion": "genetic_code_expansion",
}

PROXIMITY_METHOD_TERMS = {
    "proximity labeling": "proximity_labeling",
    "proximity labelling": "proximity_labeling",
    "turboid": "proximity_labeling",
    "miniturbo": "proximity_labeling",
    "bioid": "proximity_labeling",
    "apex": "proximity_labeling",
    "engineered peroxidase": "proximity_labeling",
}

INDIRECT_TERMS = {
    "protein design": "protein_design",
    "enzyme engineering": "enzyme_engineering",
    "dna repair": "dna_repair",
    "rna editing": "rna_editing",
    "bioorthogonal": "bioorthogonal_chemistry",
    "click chemistry": "click_chemistry",
    "synthetic biology": "synthetic_biology",
    "machine learning": "machine_learning",
}


def paper_text(paper: Paper) -> str:
    return "\n".join(
        part
        for part in [
            paper.title,
            paper.abstract or "",
            paper.journal or "",
        ]
        if part
    )


def heuristic_classify(paper: Paper) -> Classification:
    text = paper_text(paper).lower()
    tags: list[str] = []
    direct_hits: list[str] = []
    for term, tag in DIRECT_TERMS.items():
        if term in text:
            tags.append(tag)
            direct_hits.append(term)
    proximity_hits: list[str] = []
    for term, tag in PROXIMITY_METHOD_TERMS.items():
        if term in text:
            tags.append(tag)
            proximity_hits.append(term)
    indirect_hits: list[str] = []
    for term, tag in INDIRECT_TERMS.items():
        if term in text:
            tags.append(tag)
            indirect_hits.append(term)

    if direct_hits:
        return Classification(
            relevance=Relevance.DIRECT,
            confidence=0.72,
            topic_tags=tags,
            reason=f"Matched direct topic terms: {', '.join(sorted(set(direct_hits)))}.",
            recommended_action="read",
            model="heuristic",
        )
    if proximity_hits:
        # Without an LLM, method/application separation is uncertain; keep likely PL papers visible.
        return Classification(
            relevance=Relevance.DIRECT,
            confidence=0.64,
            topic_tags=tags,
            reason=(
                "Matched proximity labeling terms; inspect whether this is a method-development "
                "paper or a routine biological application."
            ),
            recommended_action="read",
            model="heuristic",
        )
    if indirect_hits:
        return Classification(
            relevance=Relevance.INDIRECT,
            confidence=0.6,
            topic_tags=tags,
            reason=f"Matched adjacent topic terms: {', '.join(sorted(set(indirect_hits)))}.",
            recommended_action="scan",
            model="heuristic",
        )
    return Classification(
        relevance=Relevance.UNRELATED,
        confidence=0.55,
        topic_tags=[],
        reason="No configured direct or adjacent topic terms were found in title/abstract.",
        recommended_action="skip",
        model="heuristic",
    )


def classification_prompt(paper: Paper) -> str:
    return dedent(
        f"""
        Classify this literature item for a reader interested in chemical biology,
        nucleic acid chemistry, protein directed evolution, and proximity-labeling methods.

        Labels:
        - direct: nucleic acid chemistry, modified nucleotides, aptamers/SELEX, polymerase
          engineering, directed evolution, enzyme/protein engineering central to the paper,
          genetic code expansion, or proximity-labeling method/tool development.
        - indirect: adjacent methods such as general protein design, bioorthogonal chemistry,
          RNA/DNA repair or editing, synthetic biology, or routine proximity-labeling biology
          applications without method development.
        - unrelated: no meaningful connection to the above interests.

        Important proximity-labeling rule:
        BioID/APEX/TurboID/miniTurbo/HRP papers are direct only when the paper develops,
        engineers, benchmarks, or materially improves the labeling method, enzyme, substrate,
        probe, reaction, or spatiotemporal strategy. Routine biological use is indirect.

        Return only JSON with:
        relevance: direct | indirect | unrelated
        confidence: number between 0 and 1
        topic_tags: array of snake_case tags
        reason: one concise sentence
        recommended_action: read | scan | skip

        Title: {paper.title}
        Journal: {paper.journal or paper.feed_title or "unknown"}
        Abstract: {paper.abstract or "No abstract available."}
        """
    ).strip()


def batch_classification_prompt(papers: list[tuple[str, Paper]]) -> str:
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
        Classify each literature item for a reader interested in chemical biology,
        nucleic acid chemistry, protein directed evolution, and proximity-labeling methods.

        Labels:
        - direct: nucleic acid chemistry, modified nucleotides, aptamers/SELEX, polymerase
          engineering, directed evolution, enzyme/protein engineering central to the paper,
          genetic code expansion, or proximity-labeling method/tool development.
        - indirect: adjacent methods such as general protein design, bioorthogonal chemistry,
          RNA/DNA repair or editing, synthetic biology, or routine proximity-labeling biology
          applications without method development.
        - unrelated: no meaningful connection to the above interests.

        Important proximity-labeling rule:
        BioID/APEX/TurboID/miniTurbo/HRP papers are direct only when the paper develops,
        engineers, benchmarks, or materially improves the labeling method, enzyme, substrate,
        probe, reaction, or spatiotemporal strategy. Routine biological use is indirect.

        Return valid JSON only, with this exact shape:
        {{
          "items": [
            {{
              "id": "string",
              "relevance": "direct | indirect | unrelated",
              "confidence": 0.0,
              "topic_tags": ["snake_case_tag"],
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


def llm_classify(paper: Paper, config: LlmConfig) -> Classification:
    client_kwargs: dict[str, str] = {"api_key": config.api_key or ""}
    if config.base_url:
        client_kwargs["base_url"] = config.base_url
    client = OpenAI(**client_kwargs)
    request_kwargs: dict[str, object] = {
        "model": config.model,
        "messages": [
            {"role": "system", "content": "You are a careful scientific literature classifier."},
            {"role": "user", "content": classification_prompt(paper)},
        ],
        "temperature": 0,
        "max_tokens": 400,
        "response_format": {"type": "json_object"},
    }
    if config.provider == "deepseek":
        request_kwargs["extra_body"] = {"thinking": {"type": config.thinking}}

    last_content = ""
    for _ in range(2):
        response = client.chat.completions.create(**request_kwargs)
        content = response.choices[0].message.content or ""
        if content.strip():
            payload = json.loads(content)
            return Classification.model_validate({**payload, "model": config.model})
        last_content = content

    raise ValueError(f"Model returned empty JSON content: {last_content!r}")


def llm_classify_batch(papers: list[Paper], config: LlmConfig) -> list[Classification]:
    client_kwargs: dict[str, str] = {"api_key": config.api_key or ""}
    if config.base_url:
        client_kwargs["base_url"] = config.base_url
    client = OpenAI(**client_kwargs)
    indexed_papers = [(str(index + 1), paper) for index, paper in enumerate(papers)]
    request_kwargs: dict[str, object] = {
        "model": config.model,
        "messages": [
            {"role": "system", "content": "You are a careful scientific literature classifier."},
            {"role": "user", "content": batch_classification_prompt(indexed_papers)},
        ],
        "temperature": 0,
        "max_tokens": max(600, 220 * len(papers)),
        "response_format": {"type": "json_object"},
    }
    if config.provider == "deepseek":
        request_kwargs["extra_body"] = {"thinking": {"type": config.thinking}}

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

    by_id = {}
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

    results: list[Classification] = []
    for item_id, paper in indexed_papers:
        classification = by_id.get(item_id)
        if classification is None:
            fallback = heuristic_classify(paper)
            classification = fallback.model_copy(
                update={
                    "reason": "Batch response did not contain this paper; using heuristic fallback."
                }
            )
        results.append(classification)
    return results


def llm_translate_titles_batch(
    papers: list[tuple[str, Paper]],
    config: LlmConfig,
) -> dict[str, str]:
    client_kwargs: dict[str, str] = {"api_key": config.api_key or ""}
    if config.base_url:
        client_kwargs["base_url"] = config.base_url
    client = OpenAI(**client_kwargs)
    request_kwargs: dict[str, object] = {
        "model": config.model,
        "messages": [
            {"role": "system", "content": "You translate scientific paper titles into concise Simplified Chinese."},
            {"role": "user", "content": title_translation_prompt(papers)},
        ],
        "temperature": 0,
        "max_tokens": max(300, 120 * len(papers)),
        "response_format": {"type": "json_object"},
    }
    if config.provider == "deepseek":
        request_kwargs["extra_body"] = {"thinking": {"type": "disabled"}}

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


def classify_paper(
    paper: Paper,
    config: LlmConfig | None = None,
) -> Classification:
    if not config or not config.api_key:
        return heuristic_classify(paper)
    try:
        return llm_classify(paper, config)
    except Exception as exc:
        fallback = heuristic_classify(paper)
        return fallback.model_copy(
            update={"reason": f"LLM classification failed ({exc}); {fallback.reason}"}
        )


def classify_papers(
    papers: list[Paper],
    config: LlmConfig | None = None,
) -> list[Classification]:
    if not config or not config.api_key:
        return [heuristic_classify(paper) for paper in papers]
    try:
        return llm_classify_batch(papers, config)
    except Exception as exc:
        results: list[Classification] = []
        for paper in papers:
            fallback = heuristic_classify(paper)
            results.append(
                fallback.model_copy(
                    update={"reason": f"LLM batch classification failed ({exc}); {fallback.reason}"}
                )
            )
        return results
