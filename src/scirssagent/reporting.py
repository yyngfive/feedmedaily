from __future__ import annotations

import json
import shutil
from collections import Counter
from datetime import UTC, date, datetime
from pathlib import Path

from scirssagent.models import Report, ReportPaper


def build_report(papers: list[ReportPaper], report_date: date, errors: list[str]) -> Report:
    counts = Counter(paper.classification.relevance.value for paper in papers)
    totals = {
        "total": len(papers),
        "direct": counts.get("direct", 0),
        "indirect": counts.get("indirect", 0),
        "unrelated": counts.get("unrelated", 0),
    }
    return Report(
        generated_at=datetime.now(UTC),
        report_date=report_date,
        totals=totals,
        papers=papers,
        errors=errors,
    )


def write_report_json(report: Report, reports_dir: Path) -> Path:
    data_dir = reports_dir / "data"
    data_dir.mkdir(parents=True, exist_ok=True)
    payload = report.model_dump(mode="json")
    latest = data_dir / "latest.json"
    dated = data_dir / f"{report.report_date.isoformat()}.json"
    text = json.dumps(payload, ensure_ascii=False, indent=2)
    latest.write_text(text, encoding="utf-8")
    dated.write_text(text, encoding="utf-8")
    return latest


def publish_static_app(web_dist: Path, reports_dir: Path) -> Path:
    target = reports_dir / "latest"
    if web_dist.exists():
        if target.exists():
            shutil.rmtree(target)
        shutil.copytree(web_dist, target)
    else:
        target.mkdir(parents=True, exist_ok=True)
        (target / "index.html").write_text(
            """
            <!doctype html>
            <meta charset="utf-8">
            <title>SciRSSAgent</title>
            <body>
              <h1>SciRSSAgent report data generated</h1>
              <p>Run <code>pnpm --dir web build</code> to build the React dashboard.</p>
              <p>Data: <a href="../data/latest.json">../data/latest.json</a></p>
            </body>
            """.strip(),
            encoding="utf-8",
        )
    return target / "index.html"

