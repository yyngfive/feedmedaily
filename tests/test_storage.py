from datetime import UTC, datetime

from scirssagent.models import (
    Classification,
    ClassificationProfile,
    Paper,
    ProfileMeta,
    ProfileProposalState,
    Relevance,
    RelevanceRules,
    TopicDefinition,
    ZoteroSaveState,
)
from scirssagent.storage import (
    connect,
    latest_zotero_status,
    mark_feedback_used,
    papers_for_report,
    save_classification,
    save_feedback,
    save_profile_proposal,
    upsert_paper,
    upsert_zotero_status,
)


def test_upsert_deduplicates_by_doi() -> None:
    conn = connect(":memory:")
    paper = Paper(
        source_url="https://example.com/rss",
        title="A paper",
        url="https://example.com/a",
        doi="10.1000/abc",
    )

    first_id, first_new = upsert_paper(conn, paper)
    second_id, second_new = upsert_paper(conn, paper)

    assert first_id == second_id
    assert first_new is True
    assert second_new is False


def test_papers_for_report_uses_configurable_limit() -> None:
    conn = connect(":memory:")
    classification = Classification(
        relevance=Relevance.UNRELATED,
        confidence=0.9,
        reason="Fixture",
        model="test",
    )
    for index in range(3):
        paper = Paper(
            source_url=f"https://example.com/{index}.rss",
            title=f"Paper {index}",
            url=f"https://example.com/{index}",
        )
        paper_id, _is_new = upsert_paper(conn, paper)
        save_classification(conn, paper_id, classification)
    conn.commit()

    assert len(papers_for_report(conn, limit=2)) == 2
    assert len(papers_for_report(conn, limit=3)) == 3


def test_report_includes_feedback_and_zotero_status() -> None:
    conn = connect(":memory:")
    paper = Paper(
        source_url="https://example.com/rss",
        title="A paper",
        url="https://example.com/a",
    )
    paper_id, _is_new = upsert_paper(conn, paper)
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
    save_feedback(conn, paper_id, Relevance.INDIRECT, Relevance.DIRECT, note="Should be direct.")
    upsert_zotero_status(conn, paper_id, state=ZoteroSaveState.SAVED, item_key="ABCD1234")
    conn.commit()

    report_paper = papers_for_report(conn, limit=1)[0]

    assert report_paper.feedback_status is not None
    assert report_paper.feedback_status.corrected_relevance == Relevance.DIRECT
    assert report_paper.feedback_status.used_in_profile is False
    assert report_paper.zotero_status is not None
    assert report_paper.zotero_status.saved is True


def test_report_hides_used_feedback_status() -> None:
    conn = connect(":memory:")
    paper = Paper(
        source_url="https://example.com/rss",
        title="Used feedback paper",
        url="https://example.com/used-feedback",
    )
    paper_id, _is_new = upsert_paper(conn, paper)
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
    feedback = save_feedback(
        conn,
        paper_id,
        Relevance.INDIRECT,
        Relevance.DIRECT,
        note="Should be direct.",
    )
    mark_feedback_used(conn, [feedback.id])
    conn.commit()

    report_paper = papers_for_report(conn, limit=1)[0]

    assert report_paper.feedback_status is None


def test_profile_proposal_round_trip() -> None:
    conn = connect(":memory:")

    proposal = save_profile_proposal(
        conn,
        summary="Use narrower rules for broad genomics.",
        proposed_profile=_profile(),
        source_feedback_ids=[1, 2],
        model="deepseek-v4-pro",
    )
    conn.commit()

    assert proposal.state == ProfileProposalState.PENDING
    assert proposal.source_feedback_ids == [1, 2]
    assert proposal.proposed_profile.topic_taxonomy[0].id == "genomics_boundary"
    assert latest_zotero_status(conn, 999) is None


def _profile() -> ClassificationProfile:
    now = datetime(2026, 4, 30, tzinfo=UTC)
    return ClassificationProfile(
        meta=ProfileMeta(
            name="Storage test",
            version=1,
            created_at=now,
            updated_at=now,
            source_description="Genomics and chemistry boundaries.",
        ),
        scope="Papers about chemistry-adjacent genomics boundaries.",
        relevance_rules=RelevanceRules(
            direct=["Chemistry-first genomics methods."],
            indirect=["Broad genomics without chemical method development."],
            unrelated=["No overlap."],
        ),
        topic_taxonomy=[
            TopicDefinition(
                id="genomics_boundary",
                label="Genomics Boundary",
                description="Boundary cases around genomics and chemistry.",
                examples=[],
            )
        ],
        few_shots=[],
        classification_notes=[],
    )
