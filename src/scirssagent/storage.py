from __future__ import annotations

import json
import sqlite3
from collections.abc import Iterable
from datetime import UTC, date, datetime
from pathlib import Path

from scirssagent.metadata import paper_key
from scirssagent.models import Classification, Paper, ReportPaper

SCHEMA = """
CREATE TABLE IF NOT EXISTS papers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_key TEXT NOT NULL UNIQUE,
  doi TEXT,
  title TEXT NOT NULL,
  journal TEXT,
  url TEXT NOT NULL,
  source_url TEXT NOT NULL,
  feed_title TEXT,
  authors_json TEXT NOT NULL,
  abstract TEXT,
  published_date TEXT,
  first_seen_at TEXT NOT NULL,
  last_checked_at TEXT NOT NULL,
  raw_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS classifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL,
  relevance TEXT NOT NULL,
  confidence REAL NOT NULL,
  reason TEXT NOT NULL,
  topic_tags_json TEXT NOT NULL,
  recommended_action TEXT NOT NULL,
  model TEXT NOT NULL,
  translated_title_zh TEXT,
  classified_at TEXT NOT NULL,
  FOREIGN KEY (paper_id) REFERENCES papers(id)
);

CREATE INDEX IF NOT EXISTS idx_papers_first_seen_at ON papers(first_seen_at);
CREATE INDEX IF NOT EXISTS idx_classifications_paper_id ON classifications(paper_id);
"""


def connect(path: Path | str) -> sqlite3.Connection:
    if isinstance(path, Path):
        path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(path)
    conn.row_factory = sqlite3.Row
    conn.executescript(SCHEMA)
    columns = {
        row["name"] for row in conn.execute("PRAGMA table_info(classifications)").fetchall()
    }
    if "translated_title_zh" not in columns:
        conn.execute("ALTER TABLE classifications ADD COLUMN translated_title_zh TEXT")
    return conn


def upsert_paper(conn: sqlite3.Connection, paper: Paper) -> tuple[int, bool]:
    key = paper_key(paper)
    now = datetime.now(UTC).isoformat()
    existing = conn.execute("SELECT id FROM papers WHERE paper_key = ?", (key,)).fetchone()
    if existing:
        conn.execute(
            """
            UPDATE papers
            SET doi = COALESCE(?, doi),
                title = ?,
                journal = COALESCE(?, journal),
                url = ?,
                abstract = COALESCE(?, abstract),
                published_date = COALESCE(?, published_date),
                last_checked_at = ?,
                raw_json = ?
            WHERE id = ?
            """,
            (
                paper.doi,
                paper.title,
                paper.journal,
                paper.url,
                paper.abstract,
                paper.published_date.isoformat() if paper.published_date else None,
                now,
                json.dumps(paper.raw, ensure_ascii=False),
                existing["id"],
            ),
        )
        return int(existing["id"]), False
    cursor = conn.execute(
        """
        INSERT INTO papers (
          paper_key, doi, title, journal, url, source_url, feed_title, authors_json,
          abstract, published_date, first_seen_at, last_checked_at, raw_json
        )
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """,
        (
            key,
            paper.doi,
            paper.title,
            paper.journal,
            paper.url,
            paper.source_url,
            paper.feed_title,
            json.dumps(paper.authors, ensure_ascii=False),
            paper.abstract,
            paper.published_date.isoformat() if paper.published_date else None,
            paper.first_seen_at.isoformat(),
            now,
            json.dumps(paper.raw, ensure_ascii=False),
        ),
    )
    return int(cursor.lastrowid), True


def latest_classification(conn: sqlite3.Connection, paper_id: int) -> Classification | None:
    row = conn.execute(
        """
        SELECT * FROM classifications
        WHERE paper_id = ?
        ORDER BY classified_at DESC, id DESC
        LIMIT 1
        """,
        (paper_id,),
    ).fetchone()
    if not row:
        return None
    return Classification.model_validate(
        {
            "relevance": row["relevance"],
            "confidence": row["confidence"],
            "reason": row["reason"],
            "topic_tags": json.loads(row["topic_tags_json"]),
            "recommended_action": row["recommended_action"],
            "model": row["model"],
            "translated_title_zh": row["translated_title_zh"],
        }
    )


def save_classification(
    conn: sqlite3.Connection, paper_id: int, classification: Classification
) -> None:
    conn.execute(
        """
        INSERT INTO classifications (
          paper_id, relevance, confidence, reason, topic_tags_json,
          recommended_action, model, translated_title_zh, classified_at
        )
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
        """,
        (
            paper_id,
            classification.relevance.value,
            classification.confidence,
            classification.reason,
            json.dumps(classification.topic_tags, ensure_ascii=False),
            classification.recommended_action,
            classification.model,
            classification.translated_title_zh,
            datetime.now(UTC).isoformat(),
        ),
    )


def paper_from_row(row: sqlite3.Row) -> Paper:
    return Paper(
        source_url=row["source_url"],
        feed_title=row["feed_title"],
        title=row["title"],
        url=row["url"],
        doi=row["doi"],
        journal=row["journal"],
        authors=json.loads(row["authors_json"]),
        abstract=row["abstract"],
        published_date=date.fromisoformat(row["published_date"]) if row["published_date"] else None,
        first_seen_at=datetime.fromisoformat(row["first_seen_at"]),
        raw=json.loads(row["raw_json"]),
    )


def papers_for_report(
    conn: sqlite3.Connection,
    report_date: date | None = None,
) -> list[ReportPaper]:
    if report_date:
        rows = conn.execute(
            "SELECT * FROM papers WHERE date(first_seen_at) = ? ORDER BY first_seen_at DESC",
            (report_date.isoformat(),),
        ).fetchall()
    else:
        rows = conn.execute("SELECT * FROM papers ORDER BY first_seen_at DESC LIMIT 200").fetchall()
    report_papers: list[ReportPaper] = []
    for row in rows:
        classification = latest_classification(conn, int(row["id"]))
        if not classification:
            continue
        paper = paper_from_row(row)
        report_papers.append(
            ReportPaper(
                **paper.model_dump(),
                id=int(row["id"]),
                classification=classification,
                seen_date=datetime.fromisoformat(row["first_seen_at"]).date(),
            )
        )
    return report_papers


def unclassified_paper_ids(conn: sqlite3.Connection, ids: Iterable[int]) -> list[int]:
    unclassified: list[int] = []
    for paper_id in ids:
        if latest_classification(conn, paper_id) is None:
            unclassified.append(paper_id)
    return unclassified


def paper_ids_needing_classification(
    conn: sqlite3.Connection,
    paper_ids: Iterable[int],
) -> list[int]:
    pending: list[int] = []
    for paper_id in paper_ids:
        if latest_classification(conn, paper_id) is None:
            pending.append(paper_id)
    return pending
