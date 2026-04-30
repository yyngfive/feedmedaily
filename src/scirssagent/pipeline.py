from __future__ import annotations

import logging
from collections.abc import Iterable
from datetime import UTC, date, datetime

from scirssagent.classifier import LlmConfig, classify_papers
from scirssagent.config import Settings
from scirssagent.feeds import fetch_all_feeds, read_feed_urls
from scirssagent.metadata import enrich_paper
from scirssagent.models import ClassificationProfile, Paper
from scirssagent.profiles import ensure_profile
from scirssagent.reporting import build_report, publish_static_app, write_report_json
from scirssagent.storage import (
    connect,
    paper_from_row,
    paper_ids_needing_classification,
    papers_for_report,
    save_classification,
    upsert_paper,
)


def configure_logging(settings: Settings) -> None:
    settings.logs_dir.mkdir(parents=True, exist_ok=True)
    log_path = settings.logs_dir / f"{date.today().isoformat()}.log"
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(message)s",
        handlers=[logging.FileHandler(log_path, encoding="utf-8"), logging.StreamHandler()],
    )


def run_once(
    settings: Settings,
    max_papers: int | None = None,
    reclassify: bool = False,
) -> str:
    configure_logging(settings)
    urls = read_feed_urls(settings.feeds_file)
    logging.info("Fetching %s feeds", len(urls))
    papers, errors = fetch_all_feeds(urls)
    if max_papers is not None:
        papers = papers[:max_papers]
        logging.info("Limiting run to %s papers", len(papers))
    logging.info("Fetched %s candidate papers", len(papers))
    conn = connect(settings.database_path)
    touched_ids: list[int] = []
    new_count = 0
    llm_config = build_classifier_config(settings)
    profile = ensure_profile(settings.profile_path)
    try:
        for paper in papers:
            paper_id, is_new = upsert_paper(conn, paper)
            touched_ids.append(paper_id)
            if is_new:
                new_count += 1
        conn.commit()
        pending_ids = (
            touched_ids if reclassify else paper_ids_needing_classification(conn, touched_ids)
        )
        logging.info(
            "Inserted %s new papers; %s papers need classification",
            new_count,
            len(pending_ids),
        )
        enriched_pairs: list[tuple[int, Paper]] = []
        for paper_id in pending_ids:
            row = conn.execute("SELECT * FROM papers WHERE id = ?", (paper_id,)).fetchone()
            if not row:
                continue
            paper = paper_from_row(row)
            enriched = enrich_paper(paper)
            upsert_paper(conn, enriched)
            enriched_pairs.append((paper_id, enriched))
        conn.commit()
        classify_enriched_pairs(
            conn,
            enriched_pairs,
            llm_config,
            profile,
            settings.classifier_batch_size,
        )
        report_date = datetime.now(UTC).date()
        report_papers = papers_for_report(conn, report_date=None, limit=settings.report_limit)
        report = build_report(report_papers, report_date, errors)
        write_report_json(report, settings.reports_dir)
        index = publish_static_app(settings.root / "web" / "dist", settings.reports_dir, report)
        logging.info("Report written to %s", index)
        return str(index)
    finally:
        conn.close()


def regenerate_latest_report(settings: Settings) -> str:
    configure_logging(settings)
    conn = connect(settings.database_path)
    try:
        report_date = datetime.now(UTC).date()
        report_papers = papers_for_report(conn, report_date=None, limit=settings.report_limit)
        report = build_report(report_papers, report_date, [])
        write_report_json(report, settings.reports_dir)
        index = publish_static_app(settings.root / "web" / "dist", settings.reports_dir, report)
        logging.info("Report regenerated at %s", index)
        return str(index)
    finally:
        conn.close()


def build_classifier_config(settings: Settings) -> LlmConfig:
    if not settings.deepseek_api_key:
        raise ValueError("SCIRSS_DEEPSEEK_API_KEY is required for classification.")
    return LlmConfig(
        api_key=settings.deepseek_api_key,
        model=settings.classifier_model,
        base_url=settings.deepseek_base_url,
        thinking=settings.classifier_thinking,
    )


def classify_enriched_pairs(
    conn,
    enriched_pairs: list[tuple[int, Paper]],
    llm_config: LlmConfig,
    profile: ClassificationProfile,
    batch_size: int,
) -> int:
    classified = 0
    for start in range(0, len(enriched_pairs), batch_size):
        batch = enriched_pairs[start : start + batch_size]
        batch_papers = [paper for _, paper in batch]
        classifications = classify_papers(batch_papers, profile=profile, config=llm_config)
        for (paper_id, _paper), classification in zip(batch, classifications, strict=True):
            save_classification(conn, paper_id, classification)
            classified += 1
        conn.commit()
    return classified


def reclassify_paper_ids(settings: Settings, paper_ids: Iterable[int]) -> int:
    configure_logging(settings)
    conn = connect(settings.database_path)
    llm_config = build_classifier_config(settings)
    profile = ensure_profile(settings.profile_path)
    try:
        enriched_pairs: list[tuple[int, Paper]] = []
        for paper_id in paper_ids:
            row = conn.execute("SELECT * FROM papers WHERE id = ?", (paper_id,)).fetchone()
            if row is None:
                continue
            paper = paper_from_row(row)
            enriched = enrich_paper(paper)
            upsert_paper(conn, enriched)
            enriched_pairs.append((paper_id, enriched))
        conn.commit()
        if not enriched_pairs:
            return 0
        return classify_enriched_pairs(
            conn,
            enriched_pairs,
            llm_config,
            profile,
            settings.classifier_batch_size,
        )
    finally:
        conn.close()
