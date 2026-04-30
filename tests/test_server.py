from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path
from uuid import uuid4

from fastapi.testclient import TestClient

from scirssagent.models import (
    Classification,
    ClassificationProfile,
    Paper,
    ProfileMeta,
    Relevance,
    RelevanceRules,
    TopicDefinition,
    ZoteroSaveState,
)
from scirssagent.profiles import write_profile
from scirssagent.storage import (
    connect,
    latest_zotero_status,
    profile_proposal_by_id,
    save_classification,
    save_feedback,
    save_profile_proposal,
    upsert_paper,
)


def test_report_latest_and_feedback_api(monkeypatch) -> None:
    root = _project_root("server")
    _bootstrap_root(root)
    write_profile(root / "data" / "classification_profile.json", _profile("Server profile"))

    conn = connect(root / "data" / "literature.sqlite")
    paper_id, _is_new = upsert_paper(
        conn,
        Paper(
            source_url="https://example.com/rss",
            title="API paper",
            url="https://example.com/api-paper",
        ),
    )
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
    from scirssagent.server import create_app

    client = TestClient(create_app())

    report_response = client.get("/api/report/latest")
    assert report_response.status_code == 200
    report_payload = report_response.json()
    assert report_payload["papers"][0]["title"] == "API paper"

    feedback_response = client.post(
        "/api/feedback",
        json={
            "paper_id": paper_id,
            "corrected_relevance": "direct",
            "note": "Should be visible as direct.",
        },
    )
    assert feedback_response.status_code == 200
    feedback_payload = feedback_response.json()
    assert feedback_payload["corrected_relevance"] == "direct"


def test_profile_proposal_apply_updates_profile_and_feedback(monkeypatch) -> None:
    root = _project_root("proposal")
    _bootstrap_root(root)
    write_profile(root / "data" / "classification_profile.json", _profile("Current profile"))

    conn = connect(root / "data" / "literature.sqlite")
    paper_id, _is_new = upsert_paper(
        conn,
        Paper(
            source_url="https://example.com/rss",
            title="Prompt paper",
            url="https://example.com/prompt-paper",
        ),
    )
    save_classification(
        conn,
        paper_id,
        Classification(
            relevance=Relevance.INDIRECT,
            confidence=0.7,
            reason="Fixture",
            model="test",
        ),
    )
    feedback = save_feedback(conn, paper_id, Relevance.INDIRECT, Relevance.DIRECT, note="Direct.")
    proposal = save_profile_proposal(
        conn,
        summary="Tighten direct scope.",
        proposed_profile=_profile("Updated profile"),
        model="deepseek-v4-pro",
        source_feedback_ids=[feedback.id],
    )
    conn.commit()
    conn.close()

    monkeypatch.chdir(root)
    import scirssagent.server as server_module

    monkeypatch.setattr(
        server_module,
        "reclassify_paper_ids",
        lambda settings, paper_ids: len(paper_ids),
    )
    monkeypatch.setattr(
        server_module,
        "regenerate_latest_report",
        lambda settings: str(settings.reports_dir / "latest" / "index.html"),
    )
    client = TestClient(server_module.create_app())
    response = client.post(f"/api/profile/proposals/{proposal.id}/apply")
    assert response.status_code == 200
    assert response.json()["state"] == "applied"

    conn = connect(root / "data" / "literature.sqlite")
    updated = profile_proposal_by_id(conn, proposal.id)
    feedback_rows = client.get("/api/feedback").json()
    conn.close()

    assert updated is not None
    assert updated.applied_version == 2
    assert feedback_rows[0]["used_in_profile"] is True
    applied_profile = (root / "data" / "classification_profile.json").read_text(encoding="utf-8")
    assert "Updated profile" in applied_profile


def test_zotero_save_api_updates_status(monkeypatch) -> None:
    root = _project_root("zotero")
    _bootstrap_root(root)
    write_profile(root / "data" / "classification_profile.json", _profile("Zotero profile"))

    conn = connect(root / "data" / "literature.sqlite")
    paper_id, _is_new = upsert_paper(
        conn,
        Paper(
            source_url="https://example.com/rss",
            title="Zotero paper",
            url="https://example.com/zotero-paper",
        ),
    )
    save_classification(
        conn,
        paper_id,
        Classification(
            relevance=Relevance.DIRECT,
            confidence=0.95,
            reason="Fixture",
            model="test",
        ),
    )
    conn.commit()
    conn.close()

    monkeypatch.chdir(root)
    import scirssagent.server as server_module

    monkeypatch.setattr(
        server_module,
        "save_paper_to_zotero",
        lambda settings, paper, classification: ('{"ok":true}', "ITEM1234"),
    )
    client = TestClient(server_module.create_app())

    response = client.post(f"/api/zotero/save/{paper_id}")
    assert response.status_code == 200
    assert response.json()["state"] == "saved"
    assert response.json()["item_key"] == "ITEM1234"

    conn = connect(root / "data" / "literature.sqlite")
    saved = latest_zotero_status(conn, paper_id)
    conn.close()
    assert saved is not None
    assert saved.state == ZoteroSaveState.SAVED


def _bootstrap_root(root: Path) -> None:
    (root / "data").mkdir(parents=True, exist_ok=True)
    (root / "reports").mkdir(parents=True, exist_ok=True)
    (root / "logs").mkdir(parents=True, exist_ok=True)
    (root / "RSS.txt").write_text("", encoding="utf-8")


def _profile(name: str) -> ClassificationProfile:
    now = datetime(2026, 4, 30, tzinfo=UTC)
    return ClassificationProfile(
        meta=ProfileMeta(
            name=name,
            version=1,
            created_at=now,
            updated_at=now,
            source_description="Nucleic acid chemistry.",
        ),
        scope="Nucleic acid chemistry and engineered enzymes.",
        relevance_rules=RelevanceRules(
            direct=["Polymerase and nucleotide chemistry."],
            indirect=["General protein engineering."],
            unrelated=["No overlap."],
        ),
        topic_taxonomy=[
            TopicDefinition(
                id="polymerase_engineering",
                label="Polymerase Engineering",
                description="Engineered polymerases.",
                examples=[],
            )
        ],
        few_shots=[],
        classification_notes=[],
    )


def _project_root(name: str) -> Path:
    path = (Path(".tmp") / "server-tests" / f"{name}-{uuid4().hex}").resolve()
    path.mkdir(parents=True, exist_ok=True)
    return path
