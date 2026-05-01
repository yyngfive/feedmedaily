from __future__ import annotations

import json
import logging
import re
from typing import Any

import httpx

from scirssagent.models import AbstractSource, Paper

logger = logging.getLogger(__name__)

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


def has_abstract_content(paper: Paper) -> bool:
    return bool(paper.abstract or paper.abstract_html or paper.abstract_images)


def finalize_enriched_paper(
    original: Paper,
    candidate: Paper,
    *,
    source: AbstractSource,
) -> Paper:
    journal = candidate.journal or original.journal
    doi = normalize_doi(candidate.doi or original.doi)
    if source in {AbstractSource.OPENALEX, AbstractSource.CROSSREF}:
        return original.model_copy(
            update={
                "doi": doi,
                "journal": journal,
                "abstract": candidate.abstract,
                "abstract_html": None,
                "abstract_images": [],
                "abstract_source": source if candidate.abstract else AbstractSource.NONE,
            }
        )
    if source == AbstractSource.RSS and has_abstract_content(original):
        return original.model_copy(
            update={
                "doi": doi,
                "journal": journal,
                "abstract_source": AbstractSource.RSS,
            }
        )
    return original.model_copy(
        update={
            "doi": doi,
            "journal": journal,
            "abstract": None,
            "abstract_html": None,
            "abstract_images": [],
            "abstract_source": AbstractSource.NONE,
        }
    )


def enrich_with_crossref(paper: Paper, client: httpx.Client) -> Paper:
    doi = normalize_doi(paper.doi)
    if not doi:
        return paper
    response = client.get(f"https://api.crossref.org/works/{doi}", timeout=15)
    if response.status_code != 200:
        return paper
    message: dict[str, Any] = response.json().get("message", {})
    journal = paper.journal or (message.get("container-title") or [None])[0]
    abstract = message.get("abstract")
    return paper.model_copy(
        update={
            "doi": doi,
            "journal": journal,
            "abstract": abstract,
            "abstract_source": AbstractSource.CROSSREF if abstract else AbstractSource.NONE,
        }
    )


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
    abstract = abstract_from_openalex_inverted_index(work.get("abstract_inverted_index"))
    doi_value = normalize_doi(paper.doi or work.get("doi"))
    journal = paper.journal
    primary_location = work.get("primary_location") or {}
    source = primary_location.get("source") or {}
    if source.get("display_name"):
        journal = journal or source["display_name"]
    return paper.model_copy(
        update={
            "doi": doi_value,
            "journal": journal,
            "abstract": abstract,
            "abstract_source": AbstractSource.OPENALEX if abstract else AbstractSource.NONE,
        }
    )


def enrich_paper(paper: Paper) -> Paper:
    doi = normalize_doi(paper.doi)
    if (
        paper.abstract_source in {AbstractSource.OPENALEX, AbstractSource.CROSSREF}
        and paper.abstract
        and paper.journal
    ):
        return paper.model_copy(update={"doi": doi})
    external_errors: list[str] = []
    with httpx.Client(headers={"User-Agent": "SciRSSAgent/0.1"}) as client:
        enriched = paper.model_copy(update={"doi": doi or paper.doi})
        try:
            openalex = enrich_with_openalex(enriched, client)
            if openalex.abstract:
                result = finalize_enriched_paper(
                    paper,
                    openalex,
                    source=AbstractSource.OPENALEX,
                )
                log_enrichment_result(result, doi, external_errors)
                return result
        except httpx.HTTPError as exc:
            external_errors.append(f"openalex:{exc}")
        try:
            crossref = enrich_with_crossref(enriched, client)
            if crossref.abstract:
                result = finalize_enriched_paper(
                    paper,
                    crossref,
                    source=AbstractSource.CROSSREF,
                )
                log_enrichment_result(result, doi, external_errors)
                return result
        except httpx.HTTPError as exc:
            external_errors.append(f"crossref:{exc}")
        if (
            paper.abstract_source in {AbstractSource.OPENALEX, AbstractSource.CROSSREF}
            and paper.abstract
        ):
            result = paper.model_copy(update={"doi": doi or paper.doi})
            log_enrichment_result(result, doi, external_errors)
            return result
        if has_abstract_content(paper):
            result = finalize_enriched_paper(
                paper,
                enriched,
                source=AbstractSource.RSS,
            )
            log_enrichment_result(result, doi, external_errors)
            return result
        result = finalize_enriched_paper(
            paper,
            enriched,
            source=AbstractSource.NONE,
        )
        log_enrichment_result(result, doi, external_errors)
        return result


def log_enrichment_result(
    paper: Paper,
    normalized_doi: str | None,
    external_errors: list[str],
) -> None:
    payload = {
        "paper_key": paper_key(paper),
        "doi_found": bool(normalized_doi),
        "abstract_source": paper.abstract_source.value,
        "abstract_empty": not has_abstract_content(paper),
        "external_errors": external_errors,
    }
    logger.info("abstract_enrichment %s", json.dumps(payload, ensure_ascii=False, sort_keys=True))
