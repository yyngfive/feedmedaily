import json
from datetime import UTC, datetime
from pathlib import Path
from uuid import uuid4
from xml.etree import ElementTree

from scirssagent.config import load_settings
from scirssagent.feeds import extract_rss_entry_abstract
from scirssagent.models import Classification, Paper, Relevance
from scirssagent.profiles import write_profile
from scirssagent.repair import (
    build_feed_match_index,
    match_feed_paper,
    normalized_journal_for_repair,
    repair_all_abstracts,
)
from scirssagent.storage import (
    connect,
    paper_by_id,
    papers_for_report,
    save_classification,
    upsert_paper,
)


def test_extract_rss_entry_abstract_matches_parse_path() -> None:
    root = ElementTree.fromstring(
        """
        <item xmlns:content="http://purl.org/rss/1.0/modules/content/">
          <title>Paper A</title>
          <link>https://example.com/a</link>
          <description>Vol. 62, Issue 10, Pages 1-4</description>
          <content:encoded><![CDATA[
            <p>Real abstract paragraph.</p>
            <p><img src="/figure.png" alt="Figure 1"></p>
          ]]></content:encoded>
        </item>
        """
    )

    abstract, abstract_html, images = extract_rss_entry_abstract(
        root,
        base_url="https://example.com/a",
    )

    assert abstract == "Real abstract paragraph."
    assert "Real abstract paragraph." in (abstract_html or "")
    assert images[0].src == "https://example.com/figure.png"


def test_match_feed_paper_prefers_doi_then_identifier_then_title() -> None:
    source_url = "https://example.com/rss"
    candidates = [
        Paper(
            source_url=source_url,
            title="Alpha",
            url="https://example.com/a",
            doi="10.1000/alpha",
        ),
        Paper(
            source_url=source_url,
            title="Beta",
            url="https://example.com/b",
            raw={"guid": "guid-beta"},
        ),
        Paper(
            source_url=source_url,
            title="Gamma title",
            url="https://example.com/c",
        ),
    ]
    index = build_feed_match_index(candidates)

    matched, basis = match_feed_paper(
        Paper(
            source_url=source_url,
            title="Other title",
            url="https://db.example.com/1",
            doi="10.1000/alpha",
        ),
        index,
    )
    assert matched is not None
    assert matched.title == "Alpha"
    assert basis == "doi"

    matched, basis = match_feed_paper(
        Paper(
            source_url=source_url,
            title="Other title",
            url="https://db.example.com/2",
            raw={"guid": "guid-beta"},
        ),
        index,
    )
    assert matched is not None
    assert matched.title == "Beta"
    assert basis == "url_or_guid"

    matched, basis = match_feed_paper(
        Paper(
            source_url=source_url,
            title="Gamma   title",
            url="https://db.example.com/3",
        ),
        index,
    )
    assert matched is not None
    assert matched.title == "Gamma title"
    assert basis == "title"


def test_normalized_journal_for_repair_uses_matched_or_normalized_legacy_value() -> None:
    paper = Paper(
        source_url="https://example.com/science.rss",
        feed_title="AAAS: Science: Table of Contents",
        title="Science paper",
        url="https://example.com/science-paper",
        journal="AAAS: Science: Table of Contents",
    )
    assert normalized_journal_for_repair(paper, None) == "Science"

    matched = Paper(
        source_url=paper.source_url,
        feed_title=paper.feed_title,
        title=paper.title,
        url=paper.url,
        journal="Journal of the American Chemical Society",
    )
    assert (
        normalized_journal_for_repair(
            Paper(
                source_url="https://example.com/jacs.rss",
                feed_title="JACS",
                title="JACS paper",
                url="https://example.com/jacs-paper",
                journal="JACS",
            ),
            matched,
        )
        == "Journal of the American Chemical Society"
    )


def test_repair_all_abstracts_updates_feed_matches_and_clears_metadata(monkeypatch) -> None:
    root = _project_root("repair")
    _bootstrap_root(root)
    _write_profile(root)
    (root / "data" / "rss_feeds.json").write_text(
        json.dumps(
            [
                {"journal": "Nature", "url": "https://example.com/nature.rss"},
                {"journal": "Science", "url": "https://example.com/science.rss"},
            ]
        ),
        encoding="utf-8",
    )

    conn = connect(root / "data" / "literature.sqlite")
    matched_id, _ = upsert_paper(
        conn,
        Paper(
            source_url="https://example.com/nature.rss",
            title="Nature old html",
            url="https://example.com/nature-1",
            doi="10.1000/nature1",
            abstract="<p>Nature Methods, Published online; old snippet.</p>",
        ),
    )
    cleared_id, _ = upsert_paper(
        conn,
        Paper(
            source_url="https://example.com/science.rss",
            title="Science metadata only",
            url="https://example.com/science-1",
            abstract="Science, Volume 392, Issue 6797, Page 478-480, April 2026.",
        ),
    )
    fallback_id, _ = upsert_paper(
        conn,
        Paper(
            source_url="https://example.com/missing.rss",
            title="Legacy html abstract",
            url="https://example.com/legacy-1",
            abstract=(
                "<p>Legacy abstract paragraph.</p>"
                '<p><img src="/legacy.png" alt="Legacy"></p>'
            ),
        ),
    )
    for paper_id in (matched_id, cleared_id, fallback_id):
        save_classification(
            conn,
            paper_id,
            Classification(
                relevance=Relevance.INDIRECT,
                confidence=0.8,
                reason="Fixture",
                model="test",
            ),
        )
    conn.commit()
    conn.close()

    monkeypatch.chdir(root)
    import scirssagent.repair as repair_module

    def fake_fetch_feed(url: str) -> list[Paper]:
        if url == "https://example.com/nature.rss":
            return [
                Paper(
                    source_url=url,
                    title="Nature old html",
                    url="https://example.com/nature-1",
                    doi="10.1000/nature1",
                    abstract="Real repaired abstract.",
                    abstract_html="<p>Real repaired abstract.</p>",
                )
            ]
        if url == "https://example.com/science.rss":
            return [
                Paper(
                    source_url=url,
                    title="Science metadata only",
                    url="https://example.com/science-1",
                    abstract="Science, Volume 392, Issue 6797, Page 478-480, April 2026.",
                )
            ]
        return []

    monkeypatch.setattr(repair_module, "fetch_feed", fake_fetch_feed)
    result = repair_all_abstracts(load_settings(root))

    assert result["summary"]["updated"] == 1
    assert result["summary"]["cleared_metadata_only"] == 1
    assert result["summary"]["fallback_from_db_html"] == 1

    conn = connect(root / "data" / "literature.sqlite")
    matched = paper_by_id(conn, matched_id)
    cleared = paper_by_id(conn, cleared_id)
    fallback = paper_by_id(conn, fallback_id)
    report_papers = papers_for_report(conn)
    conn.close()

    assert matched is not None
    matched_payload = json.loads(matched["raw_json"])
    assert matched["abstract"] == "Real repaired abstract."
    assert matched_payload["_abstract_html"] == "<p>Real repaired abstract.</p>"

    assert cleared is not None
    cleared_payload = json.loads(cleared["raw_json"])
    assert cleared["abstract"] is None
    assert cleared_payload["_abstract_html"] is None
    assert cleared_payload["_abstract_images"] == []

    assert fallback is not None
    fallback_payload = json.loads(fallback["raw_json"])
    assert fallback["abstract"] == "Legacy abstract paragraph."
    assert "Legacy abstract paragraph." in fallback_payload["_abstract_html"]
    assert fallback_payload["_abstract_images"][0]["src"] == "https://example.com/legacy.png"

    assert any(
        paper.title == "Legacy html abstract" and paper.abstract_html
        for paper in report_papers
    )
    assert Path(result["database_backup_path"]).exists()
    assert Path(result["repair_report_path"]).exists()


def _write_profile(root: Path) -> None:
    from scirssagent.models import (
        ClassificationProfile,
        ProfileMeta,
        RelevanceRules,
        TopicDefinition,
    )

    now = datetime(2026, 5, 1, tzinfo=UTC)
    write_profile(
        root / "data" / "classification_profile.json",
        ClassificationProfile(
            meta=ProfileMeta(
                name="Repair profile",
                version=1,
                created_at=now,
                updated_at=now,
                source_description="Repair test profile.",
            ),
            scope="Repair test profile.",
            relevance_rules=RelevanceRules(
                direct=["Direct."],
                indirect=["Indirect."],
                unrelated=["Unrelated."],
            ),
            topic_taxonomy=[TopicDefinition(id="test", label="Test")],
            few_shots=[],
        ),
    )


def _bootstrap_root(root: Path) -> None:
    (root / "data").mkdir(parents=True, exist_ok=True)
    (root / "reports").mkdir(parents=True, exist_ok=True)
    (root / "logs").mkdir(parents=True, exist_ok=True)


def _project_root(name: str) -> Path:
    path = (Path(".tmp") / "repair-tests" / f"{name}-{uuid4().hex}").resolve()
    path.mkdir(parents=True, exist_ok=True)
    return path
