from __future__ import annotations

import json
import re
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


def write_embedded_report_script(report: Report, target_dir: Path) -> None:
    payload = report.model_dump(mode="json")
    script = f"window.__SCIRSS_REPORT__ = {json.dumps(payload, ensure_ascii=False)};"
    (target_dir / "report-data.js").write_text(script, encoding="utf-8")


def patch_index_for_embedded_data(index_path: Path) -> None:
    text = index_path.read_text(encoding="utf-8")
    marker = '<script type="module"'
    if "report-data.js" in text:
        return
    if marker in text:
        text = text.replace(marker, '<script src="./report-data.js"></script>\n    <script type="module"', 1)
    else:
        text = text.replace("</head>", '  <script src="./report-data.js"></script>\n  </head>')
    index_path.write_text(text, encoding="utf-8")


def inline_static_assets(index_path: Path) -> None:
    text = index_path.read_text(encoding="utf-8")
    root = index_path.parent

    link_match = re.search(r'<link rel="stylesheet"[^>]*href="([^"]+)">', text)
    if link_match:
        css_path = (root / link_match.group(1)).resolve()
        css_text = css_path.read_text(encoding="utf-8")
        text = text.replace(link_match.group(0), f"<style>\n{css_text}\n</style>")

    script_match = re.search(
        r'<script type="module"[^>]*src="([^"]+)"></script>',
        text,
    )
    if script_match:
        js_path = (root / script_match.group(1)).resolve()
        js_text = js_path.read_text(encoding="utf-8")
        text = text.replace(
            script_match.group(0),
            f'<script type="module">\n{js_text}\n</script>',
        )

    report_data_path = root / "report-data.js"
    if report_data_path.exists():
        report_data_text = report_data_path.read_text(encoding="utf-8")
        text = text.replace(
            '<script src="./report-data.js"></script>',
            f"<script>\n{report_data_text}\n</script>",
        )

    index_path.write_text(text, encoding="utf-8")


def publish_static_app(web_dist: Path, reports_dir: Path, report: Report) -> Path:
    target = reports_dir / "latest"
    if web_dist.exists():
        if target.exists():
            shutil.rmtree(target)
        shutil.copytree(web_dist, target)
        write_embedded_report_script(report, target)
        patch_index_for_embedded_data(target / "index.html")
        inline_static_assets(target / "index.html")
    else:
        target.mkdir(parents=True, exist_ok=True)
        payload = report.model_dump(mode="json")
        (target / "index.html").write_text(
            f"""
            <!doctype html>
            <meta charset="utf-8">
            <title>SciRSSAgent</title>
            <body>
              <h1>SciRSSAgent report data generated</h1>
              <p>Run <code>pnpm --dir web build</code> to build the React dashboard.</p>
              <pre id="report-json">{json.dumps(payload, ensure_ascii=False, indent=2)}</pre>
            </body>
            """.strip(),
            encoding="utf-8",
        )
    return target / "index.html"
