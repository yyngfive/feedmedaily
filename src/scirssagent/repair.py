from __future__ import annotations

import json
import re
import shutil
from collections import Counter
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path

from scirssagent.config import Settings
from scirssagent.feeds import (
    fetch_feed,
    looks_like_metadata,
    normalize_feed_journal_title,
    normalize_text,
    read_feed_subscriptions,
    sanitize_abstract_html,
)
from scirssagent.metadata import normalize_doi
from scirssagent.models import AbstractImage, Paper
from scirssagent.pipeline import regenerate_latest_report
from scirssagent.storage import (
    connect,
    paper_from_row,
    update_paper_abstract_fields,
)

HTML_TAG_RE = re.compile(r"<[a-zA-Z][^>]*>")


@dataclass
class PaperMatchIndex:
    doi: dict[str, Paper]
    url_or_guid: dict[str, Paper]
    title: dict[str, Paper]


def normalize_title_key(value: str | None) -> str | None:
    clean = normalize_text(value).lower()
    return clean or None


def identifier_candidates(paper: Paper) -> list[str]:
    candidates = [paper.url, paper.raw.get("guid"), paper.raw.get("id")]
    normalized: list[str] = []
    seen: set[str] = set()
    for item in candidates:
        clean = normalize_text(str(item or ""))
        if clean and clean not in seen:
            seen.add(clean)
            normalized.append(clean)
    return normalized


def has_html_fragment(value: str | None) -> bool:
    if not value:
        return False
    return bool(HTML_TAG_RE.search(value))


def build_feed_match_index(papers: list[Paper]) -> PaperMatchIndex:
    doi_index: dict[str, Paper] = {}
    url_or_guid_index: dict[str, Paper] = {}
    title_index: dict[str, Paper] = {}
    for paper in papers:
        doi = normalize_doi(paper.doi)
        if doi and doi not in doi_index:
            doi_index[doi] = paper
        for candidate in identifier_candidates(paper):
            url_or_guid_index.setdefault(candidate, paper)
        title_key = normalize_title_key(paper.title)
        if title_key and title_key not in title_index:
            title_index[title_key] = paper
    return PaperMatchIndex(
        doi=doi_index,
        url_or_guid=url_or_guid_index,
        title=title_index,
    )


def match_feed_paper(paper: Paper, index: PaperMatchIndex) -> tuple[Paper | None, str | None]:
    doi = normalize_doi(paper.doi)
    if doi and doi in index.doi:
        return index.doi[doi], "doi"
    for candidate in identifier_candidates(paper):
        if candidate in index.url_or_guid:
            return index.url_or_guid[candidate], "url_or_guid"
    title_key = normalize_title_key(paper.title)
    if title_key and title_key in index.title:
        return index.title[title_key], "title"
    return None, None


def normalize_abstract_payload(
    abstract: str | None,
    abstract_html: str | None,
    abstract_images: list[AbstractImage],
    *,
    base_url: str,
) -> tuple[str | None, str | None, list[AbstractImage]]:
    source = abstract_html or abstract
    if not source:
        return None, None, []
    if abstract_html or has_html_fragment(source):
        html_value, images, plain_text = sanitize_abstract_html(source, base_url=base_url)
        if not plain_text or looks_like_metadata(plain_text):
            return None, None, []
        return plain_text, html_value, images or abstract_images
    plain_text = normalize_text(source)
    if not plain_text or looks_like_metadata(plain_text):
        return None, None, []
    return plain_text, None, []


def abstract_state(
    abstract: str | None,
    abstract_html: str | None,
    abstract_images: list[AbstractImage],
) -> tuple[str | None, str | None, tuple[tuple[str, str | None], ...]]:
    return (
        abstract,
        abstract_html,
        tuple((item.src, item.alt) for item in abstract_images),
    )


def backup_database(database_path: Path) -> Path:
    timestamp = datetime.now(UTC).strftime("%Y%m%d-%H%M%S")
    backup_path = database_path.with_name(
        f"{database_path.stem}-backup-{timestamp}{database_path.suffix}"
    )
    shutil.copy2(database_path, backup_path)
    return backup_path


def load_feed_indexes(settings: Settings) -> tuple[dict[str, PaperMatchIndex], list[str]]:
    indexes: dict[str, PaperMatchIndex] = {}
    errors: list[str] = []
    for subscription in read_feed_subscriptions(settings.feeds_path):
        source_url = str(subscription.url)
        try:
            indexes[source_url] = build_feed_match_index(fetch_feed(source_url))
        except Exception as exc:
            errors.append(f"{source_url}: {exc}")
    return indexes, errors


def write_repair_report(settings: Settings, payload: dict[str, object]) -> Path:
    timestamp = datetime.now(UTC).strftime("%Y%m%d-%H%M%S")
    target_dir = settings.reports_dir / "data"
    target_dir.mkdir(parents=True, exist_ok=True)
    path = target_dir / f"abstract-repair-{timestamp}.json"
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
    return path


def normalized_journal_for_repair(paper: Paper, matched_paper: Paper | None) -> str | None:
    if matched_paper and normalize_text(matched_paper.journal):
        return normalize_text(matched_paper.journal)
    for candidate in (paper.journal, paper.feed_title):
        normalized = normalize_feed_journal_title(candidate)
        if normalized:
            return normalized
    return None


def repair_all_abstracts(settings: Settings) -> dict[str, object]:
    backup_path = backup_database(settings.database_path)
    feed_indexes, feed_errors = load_feed_indexes(settings)
    conn = connect(settings.database_path)
    try:
        rows = conn.execute("SELECT * FROM papers ORDER BY id").fetchall()
        results: list[dict[str, object]] = []
        counts: Counter[str] = Counter()

        for row in rows:
            paper_id = int(row["id"])
            paper = paper_from_row(row)
            current_state = abstract_state(
                paper.abstract,
                paper.abstract_html,
                paper.abstract_images,
            )
            next_abstract = paper.abstract
            next_html = paper.abstract_html
            next_images = paper.abstract_images
            next_journal = paper.journal
            status = "unchanged"
            match_basis: str | None = None

            index = feed_indexes.get(paper.source_url)
            matched_paper: Paper | None = None
            if index is not None:
                matched_paper, match_basis = match_feed_paper(paper, index)
            if matched_paper is not None:
                next_abstract, next_html, next_images = normalize_abstract_payload(
                    matched_paper.abstract,
                    matched_paper.abstract_html,
                    matched_paper.abstract_images,
                    base_url=matched_paper.url,
                )
                next_journal = normalized_journal_for_repair(paper, matched_paper)
                status = "updated" if next_abstract else "cleared_metadata_only"
            elif has_html_fragment(paper.abstract) or paper.abstract_html:
                next_abstract, next_html, next_images = normalize_abstract_payload(
                    paper.abstract,
                    paper.abstract_html,
                    paper.abstract_images,
                    base_url=paper.url,
                )
                status = "fallback-from-db-html" if next_abstract else "cleared_metadata_only"
            elif looks_like_metadata(normalize_text(paper.abstract)):
                next_abstract, next_html, next_images = None, None, []
                status = "cleared_metadata_only"
            else:
                next_abstract, next_html, next_images = normalize_abstract_payload(
                    paper.abstract,
                    paper.abstract_html,
                    paper.abstract_images,
                    base_url=paper.url,
                )
                status = "unmatched"
                if abstract_state(next_abstract, next_html, next_images) == current_state:
                    status = "unchanged"

            next_journal = normalized_journal_for_repair(paper, matched_paper) or next_journal

            next_state = abstract_state(next_abstract, next_html, next_images)
            if next_state != current_state or next_journal != paper.journal:
                update_paper_abstract_fields(
                    conn,
                    paper_id,
                    journal=next_journal,
                    abstract=next_abstract,
                    abstract_html=next_html,
                    abstract_images=next_images,
                )
            elif status == "unmatched":
                status = "unchanged"

            counts[status] += 1
            results.append(
                {
                    "paper_id": paper_id,
                    "title": paper.title,
                    "source_url": paper.source_url,
                    "status": status,
                    "match_basis": match_basis,
                }
            )

        conn.commit()
    finally:
        conn.close()

    report_index = regenerate_latest_report(settings)
    repair_payload = {
        "generated_at": datetime.now(UTC).isoformat(),
        "database_backup_path": str(backup_path),
        "report_index": str(report_index),
        "feed_errors": feed_errors,
        "summary": {
            "total": sum(counts.values()),
            "updated": counts.get("updated", 0),
            "fallback_from_db_html": counts.get("fallback-from-db-html", 0),
            "cleared_metadata_only": counts.get("cleared_metadata_only", 0),
            "unmatched": counts.get("unmatched", 0),
            "unchanged": counts.get("unchanged", 0),
        },
        "results": results,
    }
    repair_report_path = write_repair_report(settings, repair_payload)
    return {
        "database_backup_path": str(backup_path),
        "repair_report_path": str(repair_report_path),
        "report_index": str(report_index),
        "summary": repair_payload["summary"],
        "feed_errors": feed_errors,
    }
