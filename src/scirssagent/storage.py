from __future__ import annotations

import json
import sqlite3
from collections.abc import Iterable
from datetime import UTC, date, datetime
from pathlib import Path

from scirssagent.metadata import paper_key
from scirssagent.models import (
    AbstractImage,
    Classification,
    ClassificationProfile,
    FeedbackRecord,
    FeedbackState,
    FeedbackStatus,
    Paper,
    ProfileProposal,
    ProfileProposalDelta,
    ProfileProposalState,
    Relevance,
    ReportPaper,
    ZoteroSaveState,
    ZoteroStatus,
)
from scirssagent.profile_compact import persisted_profile

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
  read_at TEXT,
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

CREATE TABLE IF NOT EXISTS feedback (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL,
  original_relevance TEXT NOT NULL,
  corrected_relevance TEXT NOT NULL,
  note TEXT,
  state TEXT NOT NULL DEFAULT 'open',
  used_in_prompt INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  FOREIGN KEY (paper_id) REFERENCES papers(id)
);

CREATE TABLE IF NOT EXISTS profile_proposals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  summary TEXT NOT NULL,
  proposed_profile_json TEXT NOT NULL,
  rule_delta_json TEXT,
  source_feedback_ids_json TEXT NOT NULL,
  model TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL,
  applied_at TEXT,
  rejected_at TEXT,
  applied_version INTEGER
);

CREATE TABLE IF NOT EXISTS zotero_saves (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL UNIQUE,
  state TEXT NOT NULL,
  item_key TEXT,
  error_message TEXT,
  attempted_at TEXT NOT NULL,
  saved_at TEXT,
  FOREIGN KEY (paper_id) REFERENCES papers(id)
);

CREATE INDEX IF NOT EXISTS idx_papers_first_seen_at ON papers(first_seen_at);
CREATE INDEX IF NOT EXISTS idx_classifications_paper_id ON classifications(paper_id);
CREATE INDEX IF NOT EXISTS idx_feedback_paper_id ON feedback(paper_id);
CREATE INDEX IF NOT EXISTS idx_feedback_created_at ON feedback(created_at);
CREATE INDEX IF NOT EXISTS idx_profile_proposals_created_at ON profile_proposals(created_at);
"""


def connect(path: Path | str) -> sqlite3.Connection:
    if isinstance(path, Path):
        path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(path)
    conn.row_factory = sqlite3.Row
    conn.executescript(SCHEMA)
    paper_columns = {row["name"] for row in conn.execute("PRAGMA table_info(papers)").fetchall()}
    if "read_at" not in paper_columns:
        conn.execute("ALTER TABLE papers ADD COLUMN read_at TEXT")
    columns = {
        row["name"] for row in conn.execute("PRAGMA table_info(classifications)").fetchall()
    }
    if "translated_title_zh" not in columns:
        conn.execute("ALTER TABLE classifications ADD COLUMN translated_title_zh TEXT")
    proposal_columns = {
        row["name"] for row in conn.execute("PRAGMA table_info(profile_proposals)").fetchall()
    }
    if "rule_delta_json" not in proposal_columns:
        conn.execute("ALTER TABLE profile_proposals ADD COLUMN rule_delta_json TEXT")
    return conn


def upsert_paper(conn: sqlite3.Connection, paper: Paper) -> tuple[int, bool]:
    key = paper_key(paper)
    now = datetime.now(UTC).isoformat()
    raw_payload = {
        **paper.raw,
        "_abstract_html": paper.abstract_html,
        "_abstract_images": [item.model_dump(mode="json") for item in paper.abstract_images],
    }
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
                json.dumps(raw_payload, ensure_ascii=False),
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
            json.dumps(raw_payload, ensure_ascii=False),
        ),
    )
    return int(cursor.lastrowid), True


def update_paper_abstract_fields(
    conn: sqlite3.Connection,
    paper_id: int,
    *,
    journal: str | None = None,
    abstract: str | None,
    abstract_html: str | None,
    abstract_images: list[AbstractImage],
) -> None:
    row = paper_by_id(conn, paper_id)
    if row is None:
        raise ValueError(f"Paper {paper_id} not found.")
    raw_payload = json.loads(row["raw_json"])
    raw_payload["_abstract_html"] = abstract_html
    raw_payload["_abstract_images"] = [
        item.model_dump(mode="json") for item in abstract_images
    ]
    conn.execute(
        """
        UPDATE papers
        SET journal = COALESCE(?, journal),
            abstract = ?,
            raw_json = ?,
            last_checked_at = ?
        WHERE id = ?
        """,
        (
            journal,
            abstract,
            json.dumps(raw_payload, ensure_ascii=False),
            datetime.now(UTC).isoformat(),
            paper_id,
        ),
    )


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


def mark_paper_read(conn: sqlite3.Connection, paper_id: int) -> datetime:
    now = datetime.now(UTC).isoformat()
    conn.execute(
        """
        UPDATE papers
        SET read_at = COALESCE(read_at, ?)
        WHERE id = ?
        """,
        (now, paper_id),
    )
    row = conn.execute("SELECT read_at FROM papers WHERE id = ?", (paper_id,)).fetchone()
    if row is None or row["read_at"] is None:
        raise ValueError("Failed to persist read status.")
    return datetime.fromisoformat(row["read_at"])


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


def save_feedback(
    conn: sqlite3.Connection,
    paper_id: int,
    original_relevance: Relevance,
    corrected_relevance: Relevance,
    note: str | None = None,
) -> FeedbackRecord:
    cursor = conn.execute(
        """
        INSERT INTO feedback (
          paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at
        )
        VALUES (?, ?, ?, ?, ?, ?, ?)
        """,
        (
            paper_id,
            original_relevance.value,
            corrected_relevance.value,
            note,
            FeedbackState.OPEN.value,
            0,
            datetime.now(UTC).isoformat(),
        ),
    )
    row = feedback_by_id(conn, int(cursor.lastrowid))
    if row is None:
        raise ValueError("Failed to reload saved feedback row.")
    return row


def feedback_by_id(conn: sqlite3.Connection, feedback_id: int) -> FeedbackRecord | None:
    row = conn.execute(
        """
        SELECT f.*, p.title AS paper_title
        FROM feedback f
        JOIN papers p ON p.id = f.paper_id
        WHERE f.id = ?
        """,
        (feedback_id,),
    ).fetchone()
    if row is None:
        return None
    return FeedbackRecord(
        id=int(row["id"]),
        paper_id=int(row["paper_id"]),
        paper_title=row["paper_title"],
        original_relevance=Relevance(row["original_relevance"]),
        corrected_relevance=Relevance(row["corrected_relevance"]),
        note=row["note"],
        state=FeedbackState(row["state"]),
        used_in_profile=bool(row["used_in_prompt"]),
        created_at=datetime.fromisoformat(row["created_at"]),
    )


def list_feedback(conn: sqlite3.Connection) -> list[FeedbackRecord]:
    rows = conn.execute(
        """
        SELECT f.*, p.title AS paper_title
        FROM feedback f
        JOIN papers p ON p.id = f.paper_id
        ORDER BY f.created_at DESC, f.id DESC
        """
    ).fetchall()
    return [
        FeedbackRecord(
            id=int(row["id"]),
            paper_id=int(row["paper_id"]),
            paper_title=row["paper_title"],
            original_relevance=Relevance(row["original_relevance"]),
            corrected_relevance=Relevance(row["corrected_relevance"]),
            note=row["note"],
            state=FeedbackState(row["state"]),
            used_in_profile=bool(row["used_in_prompt"]),
            created_at=datetime.fromisoformat(row["created_at"]),
        )
        for row in rows
    ]


def list_open_feedback(conn: sqlite3.Connection) -> list[FeedbackRecord]:
    return [item for item in list_feedback(conn) if item.state == FeedbackState.OPEN]


def delete_feedback(conn: sqlite3.Connection, feedback_id: int) -> None:
    conn.execute("DELETE FROM feedback WHERE id = ?", (feedback_id,))


def mark_feedback_used(
    conn: sqlite3.Connection,
    feedback_ids: Iterable[int],
    *,
    state: FeedbackState = FeedbackState.USED,
) -> None:
    for feedback_id in feedback_ids:
        conn.execute(
            """
            UPDATE feedback
            SET used_in_prompt = 1,
                state = ?
            WHERE id = ?
            """,
            (state.value, feedback_id),
        )


def latest_feedback_status(conn: sqlite3.Connection, paper_id: int) -> FeedbackStatus | None:
    row = conn.execute(
        """
        SELECT *
        FROM feedback
        WHERE paper_id = ? AND state = ?
        ORDER BY created_at DESC, id DESC
        LIMIT 1
        """,
        (paper_id, FeedbackState.OPEN.value),
    ).fetchone()
    if row is None:
        return None
    return FeedbackStatus(
        has_feedback=True,
        corrected_relevance=Relevance(row["corrected_relevance"]),
        note=row["note"],
        latest_feedback_at=datetime.fromisoformat(row["created_at"]),
        state=FeedbackState(row["state"]),
        used_in_profile=bool(row["used_in_prompt"]),
    )


def save_profile_proposal(
    conn: sqlite3.Connection,
    *,
    summary: str,
    proposed_profile: ClassificationProfile,
    rule_delta: ProfileProposalDelta,
    source_feedback_ids: list[int],
    model: str,
) -> ProfileProposal:
    compact_profile = persisted_profile(proposed_profile)
    cursor = conn.execute(
        """
        INSERT INTO profile_proposals (
          summary, proposed_profile_json, rule_delta_json,
          source_feedback_ids_json, model, state, created_at
        )
        VALUES (?, ?, ?, ?, ?, ?, ?)
        """,
        (
            summary,
            json.dumps(compact_profile.model_dump(mode="json"), ensure_ascii=False),
            json.dumps(rule_delta.model_dump(mode="json"), ensure_ascii=False),
            json.dumps(source_feedback_ids),
            model,
            ProfileProposalState.PENDING.value,
            datetime.now(UTC).isoformat(),
        ),
    )
    proposal = profile_proposal_by_id(conn, int(cursor.lastrowid))
    if proposal is None:
        raise ValueError("Failed to reload saved profile proposal.")
    return proposal


def profile_proposal_by_id(conn: sqlite3.Connection, proposal_id: int) -> ProfileProposal | None:
    row = conn.execute(
        "SELECT * FROM profile_proposals WHERE id = ?",
        (proposal_id,),
    ).fetchone()
    if row is None:
        return None
    return ProfileProposal(
        id=int(row["id"]),
        summary=row["summary"],
        proposed_profile=ClassificationProfile.model_validate(
            json.loads(row["proposed_profile_json"])
        ),
        rule_delta=ProfileProposalDelta.model_validate(json.loads(row["rule_delta_json"])),
        source_feedback_ids=[int(item) for item in json.loads(row["source_feedback_ids_json"])],
        model=row["model"],
        state=ProfileProposalState(row["state"]),
        created_at=datetime.fromisoformat(row["created_at"]),
        applied_at=datetime.fromisoformat(row["applied_at"]) if row["applied_at"] else None,
        rejected_at=datetime.fromisoformat(row["rejected_at"]) if row["rejected_at"] else None,
        applied_version=row["applied_version"],
    )


def list_profile_proposals(conn: sqlite3.Connection) -> list[ProfileProposal]:
    rows = conn.execute(
        "SELECT id FROM profile_proposals ORDER BY created_at DESC, id DESC"
    ).fetchall()
    proposals: list[ProfileProposal] = []
    for row in rows:
        proposal = profile_proposal_by_id(conn, int(row["id"]))
        if proposal is not None:
            proposals.append(proposal)
    return proposals


def apply_profile_proposal(conn: sqlite3.Connection, proposal_id: int, version: int) -> None:
    conn.execute(
        """
        UPDATE profile_proposals
        SET state = ?, applied_at = ?, applied_version = ?, rejected_at = NULL
        WHERE id = ?
        """,
        (
            ProfileProposalState.APPLIED.value,
            datetime.now(UTC).isoformat(),
            version,
            proposal_id,
        ),
    )


def reject_profile_proposal(conn: sqlite3.Connection, proposal_id: int) -> None:
    conn.execute(
        """
        UPDATE profile_proposals
        SET state = ?, rejected_at = ?
        WHERE id = ?
        """,
        (
            ProfileProposalState.REJECTED.value,
            datetime.now(UTC).isoformat(),
            proposal_id,
        ),
    )


def upsert_zotero_status(
    conn: sqlite3.Connection,
    paper_id: int,
    *,
    state: ZoteroSaveState,
    item_key: str | None = None,
    error_message: str | None = None,
) -> ZoteroStatus:
    now = datetime.now(UTC).isoformat()
    saved_at = now if state == ZoteroSaveState.SAVED else None
    conn.execute(
        """
        INSERT INTO zotero_saves (paper_id, state, item_key, error_message, attempted_at, saved_at)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(paper_id) DO UPDATE SET
          state = excluded.state,
          item_key = excluded.item_key,
          error_message = excluded.error_message,
          attempted_at = excluded.attempted_at,
          saved_at = excluded.saved_at
        """,
        (paper_id, state.value, item_key, error_message, now, saved_at),
    )
    status = latest_zotero_status(conn, paper_id)
    if status is None:
        raise ValueError("Failed to reload Zotero save status.")
    return status


def latest_zotero_status(conn: sqlite3.Connection, paper_id: int) -> ZoteroStatus | None:
    row = conn.execute(
        "SELECT * FROM zotero_saves WHERE paper_id = ?",
        (paper_id,),
    ).fetchone()
    if row is None:
        return None
    state = ZoteroSaveState(row["state"])
    return ZoteroStatus(
        state=state,
        saved=state == ZoteroSaveState.SAVED,
        item_key=row["item_key"],
        last_error=row["error_message"],
        attempted_at=datetime.fromisoformat(row["attempted_at"]) if row["attempted_at"] else None,
        saved_at=datetime.fromisoformat(row["saved_at"]) if row["saved_at"] else None,
    )


def paper_by_id(conn: sqlite3.Connection, paper_id: int) -> sqlite3.Row | None:
    return conn.execute("SELECT * FROM papers WHERE id = ?", (paper_id,)).fetchone()


def recent_paper_ids(conn: sqlite3.Connection, limit: int) -> list[int]:
    rows = conn.execute(
        "SELECT id FROM papers ORDER BY first_seen_at DESC LIMIT ?",
        (limit,),
    ).fetchall()
    return [int(row["id"]) for row in rows]


def all_paper_ids(conn: sqlite3.Connection) -> list[int]:
    rows = conn.execute("SELECT id FROM papers ORDER BY first_seen_at DESC").fetchall()
    return [int(row["id"]) for row in rows]


def feedback_paper_ids(conn: sqlite3.Connection) -> list[int]:
    rows = conn.execute(
        "SELECT DISTINCT paper_id FROM feedback ORDER BY created_at DESC, id DESC"
    ).fetchall()
    return [int(row["paper_id"]) for row in rows]


def paper_ids_for_feedback_ids(conn: sqlite3.Connection, feedback_ids: Iterable[int]) -> list[int]:
    ids = list(feedback_ids)
    if not ids:
        return []
    placeholders = ",".join("?" for _ in ids)
    rows = conn.execute(
        f"""
        SELECT DISTINCT paper_id
        FROM feedback
        WHERE id IN ({placeholders})
        ORDER BY created_at DESC, id DESC
        """,
        ids,
    ).fetchall()
    return [int(row["paper_id"]) for row in rows]


def paper_from_row(row: sqlite3.Row) -> Paper:
    raw_payload = json.loads(row["raw_json"])
    return Paper(
        source_url=row["source_url"],
        feed_title=row["feed_title"],
        title=row["title"],
        url=row["url"],
        doi=row["doi"],
        journal=row["journal"],
        authors=json.loads(row["authors_json"]),
        abstract=row["abstract"],
        abstract_html=raw_payload.get("_abstract_html"),
        abstract_images=[
            AbstractImage.model_validate(item) for item in raw_payload.get("_abstract_images", [])
        ],
        published_date=date.fromisoformat(row["published_date"]) if row["published_date"] else None,
        first_seen_at=datetime.fromisoformat(row["first_seen_at"]),
        read_at=datetime.fromisoformat(row["read_at"]) if row["read_at"] else None,
        raw={
            key: value
            for key, value in raw_payload.items()
            if key not in {"_abstract_html", "_abstract_images"}
        },
    )


def papers_for_report(
    conn: sqlite3.Connection,
    report_date: date | None = None,
    limit: int | None = None,
    display_names_by_source: dict[str, str] | None = None,
) -> list[ReportPaper]:
    if report_date:
        rows = conn.execute(
            "SELECT * FROM papers WHERE date(first_seen_at) = ? ORDER BY first_seen_at DESC",
            (report_date.isoformat(),),
        ).fetchall()
    elif limit is None:
        rows = conn.execute("SELECT * FROM papers ORDER BY first_seen_at DESC").fetchall()
    else:
        rows = conn.execute(
            "SELECT * FROM papers ORDER BY first_seen_at DESC LIMIT ?",
            (limit,),
        ).fetchall()
    report_papers: list[ReportPaper] = []
    for row in rows:
        classification = latest_classification(conn, int(row["id"]))
        if not classification:
            continue
        paper = paper_from_row(row)
        display_journal = (
            display_names_by_source.get(paper.source_url)
            if display_names_by_source is not None
            else None
        )
        report_papers.append(
            ReportPaper(
                **paper.model_copy(
                    update={"journal": display_journal or paper.journal}
                ).model_dump(),
                id=int(row["id"]),
                classification=classification,
                seen_date=datetime.fromisoformat(row["first_seen_at"]).date(),
                feedback_status=latest_feedback_status(conn, int(row["id"])),
                zotero_status=latest_zotero_status(conn, int(row["id"])),
            )
        )
    return report_papers


def paper_ids_needing_classification(
    conn: sqlite3.Connection,
    paper_ids: Iterable[int],
) -> list[int]:
    pending: list[int] = []
    for paper_id in paper_ids:
        if latest_classification(conn, paper_id) is None:
            pending.append(paper_id)
    return pending
