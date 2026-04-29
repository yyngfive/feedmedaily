from __future__ import annotations

import logging
from datetime import UTC, date, datetime

from scirssagent.classifier import LlmConfig, classify_papers
from scirssagent.config import Settings
from scirssagent.feeds import fetch_all_feeds, read_feed_urls
from scirssagent.metadata import enrich_paper
from scirssagent.models import Paper
from scirssagent.reporting import build_report, publish_static_app, write_report_json
from scirssagent.storage import (
    connect,
    paper_ids_needing_classification,
    paper_from_row,
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
    llm_config = LlmConfig(
        provider=settings.llm_provider,
        api_key=settings.llm_api_key,
        model=settings.llm_model,
        base_url=settings.llm_base_url,
        thinking=settings.llm_thinking,
    )
    try:
        for paper in papers:
            paper_id, is_new = upsert_paper(conn, paper)
            touched_ids.append(paper_id)
            if is_new:
                new_count += 1
        conn.commit()
        pending_ids = touched_ids if reclassify else paper_ids_needing_classification(conn, touched_ids)
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
        batch_size = settings.llm_batch_size
        for start in range(0, len(enriched_pairs), batch_size):
            batch = enriched_pairs[start : start + batch_size]
            batch_papers = [paper for _, paper in batch]
            classifications = classify_papers(batch_papers, config=llm_config)
            for (paper_id, _paper), classification in zip(batch, classifications, strict=True):
                save_classification(conn, paper_id, classification)
            conn.commit()
        report_date = datetime.now(UTC).date()
        report_papers = papers_for_report(conn, report_date=None)
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
        report_papers = papers_for_report(conn, report_date=None)
        report = build_report(report_papers, report_date, [])
        write_report_json(report, settings.reports_dir)
        index = publish_static_app(settings.root / "web" / "dist", settings.reports_dir, report)
        logging.info("Report regenerated at %s", index)
        return str(index)
    finally:
        conn.close()
