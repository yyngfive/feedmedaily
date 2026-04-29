from __future__ import annotations

import logging
from datetime import UTC, date, datetime

from scirssagent.classifier import classify_paper
from scirssagent.config import Settings
from scirssagent.feeds import fetch_all_feeds, read_feed_urls
from scirssagent.metadata import enrich_paper
from scirssagent.reporting import build_report, publish_static_app, write_report_json
from scirssagent.storage import (
    connect,
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


def run_once(settings: Settings) -> str:
    configure_logging(settings)
    urls = read_feed_urls(settings.feeds_file)
    logging.info("Fetching %s feeds", len(urls))
    papers, errors = fetch_all_feeds(urls)
    logging.info("Fetched %s candidate papers", len(papers))
    conn = connect(settings.database_path)
    new_ids: list[int] = []
    try:
        for paper in papers:
            paper_id, is_new = upsert_paper(conn, paper)
            if is_new:
                new_ids.append(paper_id)
        conn.commit()
        logging.info("Inserted %s new papers", len(new_ids))
        for paper_id in new_ids:
            row = conn.execute("SELECT * FROM papers WHERE id = ?", (paper_id,)).fetchone()
            if not row:
                continue
            paper = paper_from_row(row)
            enriched = enrich_paper(paper)
            upsert_paper(conn, enriched)
            paper = enriched
            classification = classify_paper(
                paper,
                api_key=settings.openai_api_key,
                model=settings.openai_model,
            )
            save_classification(conn, paper_id, classification)
            conn.commit()
        report_date = datetime.now(UTC).date()
        report_papers = papers_for_report(conn, report_date=None)
        report = build_report(report_papers, report_date, errors)
        write_report_json(report, settings.reports_dir)
        index = publish_static_app(settings.root / "web" / "dist", settings.reports_dir)
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
        index = publish_static_app(settings.root / "web" / "dist", settings.reports_dir)
        logging.info("Report regenerated at %s", index)
        return str(index)
    finally:
        conn.close()
