from __future__ import annotations

import re
from typing import Any

import httpx

from scirssagent.models import Paper

DOI_RE = re.compile(r"10\.\d{4,9}/[-._;()/:A-Z0-9]+", re.IGNORECASE)


def normalize_doi(value: str | None) -> str | None:
    if not value:
        return None
    match = DOI_RE.search(value)
    if not match:
        return value.strip().removeprefix("doi:") or None
    return match.group(0).rstrip(".").lower()


def paper_key(paper: Paper) -> str:
    doi = normalize_doi(paper.doi)
    if doi:
        return f"doi:{doi}"
    if paper.url:
        return f"url:{paper.url.strip().lower()}"
    return f"title:{paper.title.strip().lower()}"


def abstract_from_openalex_inverted_index(index: dict[str, list[int]] | None) -> str | None:
    if not index:
        return None
    words: list[tuple[int, str]] = []
    for word, positions in index.items():
        words.extend((position, word) for position in positions)
    return " ".join(word for _, word in sorted(words)) or None


def enrich_with_crossref(paper: Paper, client: httpx.Client) -> Paper:
    doi = normalize_doi(paper.doi)
    if not doi:
        return paper
    response = client.get(f"https://api.crossref.org/works/{doi}", timeout=15)
    if response.status_code != 200:
        return paper
    message: dict[str, Any] = response.json().get("message", {})
    journal = paper.journal or (message.get("container-title") or [None])[0]
    abstract = paper.abstract or message.get("abstract")
    return paper.model_copy(update={"doi": doi, "journal": journal, "abstract": abstract})


def enrich_with_openalex(paper: Paper, client: httpx.Client) -> Paper:
    doi = normalize_doi(paper.doi)
    if doi:
        url = f"https://api.openalex.org/works/https://doi.org/{doi}"
        response = client.get(url, timeout=15)
    else:
        response = client.get(
            "https://api.openalex.org/works",
            params={"search": paper.title, "per-page": 1},
            timeout=15,
        )
    if response.status_code != 200:
        return paper
    payload = response.json()
    work = payload.get("results", [{}])[0] if "results" in payload else payload
    abstract = paper.abstract or abstract_from_openalex_inverted_index(
        work.get("abstract_inverted_index")
    )
    doi_value = normalize_doi(paper.doi or work.get("doi"))
    journal = paper.journal
    primary_location = work.get("primary_location") or {}
    source = primary_location.get("source") or {}
    if source.get("display_name"):
        journal = journal or source["display_name"]
    return paper.model_copy(update={"doi": doi_value, "journal": journal, "abstract": abstract})


def enrich_paper(paper: Paper) -> Paper:
    doi = normalize_doi(paper.doi)
    if doi and paper.abstract and paper.journal:
        return paper.model_copy(update={"doi": doi})
    with httpx.Client(headers={"User-Agent": "SciRSSAgent/0.1"}) as client:
        enriched = paper.model_copy(update={"doi": doi or paper.doi})
        try:
            enriched = enrich_with_crossref(enriched, client)
        except httpx.HTTPError:
            pass
        if not enriched.abstract:
            try:
                enriched = enrich_with_openalex(enriched, client)
            except httpx.HTTPError:
                pass
        return enriched
