from __future__ import annotations

import json
from textwrap import dedent

from openai import OpenAI

from scirssagent.models import Classification, Paper, Relevance

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


def llm_classify(paper: Paper, api_key: str, model: str) -> Classification:
    client = OpenAI(api_key=api_key)
    response = client.chat.completions.create(
        model=model,
        messages=[
            {"role": "system", "content": "You are a careful scientific literature classifier."},
            {"role": "user", "content": classification_prompt(paper)},
        ],
        temperature=0,
        response_format={"type": "json_object"},
    )
    content = response.choices[0].message.content or "{}"
    payload = json.loads(content)
    return Classification.model_validate({**payload, "model": model})


def classify_paper(
    paper: Paper,
    api_key: str | None = None,
    model: str = "heuristic",
) -> Classification:
    if not api_key:
        return heuristic_classify(paper)
    try:
        return llm_classify(paper, api_key, model)
    except Exception as exc:
        fallback = heuristic_classify(paper)
        return fallback.model_copy(
            update={"reason": f"LLM classification failed ({exc}); {fallback.reason}"}
        )
