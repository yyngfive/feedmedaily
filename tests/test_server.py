from __future__ import annotations

import json
from datetime import UTC, datetime
from pathlib import Path
from uuid import uuid4

from fastapi.testclient import TestClient

from scirssagent.config import load_settings
from scirssagent.models import (
    Classification,
    ClassificationProfile,
    JobInfo,
    Paper,
    ProfileMeta,
    ProfileProposalDelta,
    Relevance,
    RelevanceRules,
    TopicDefinition,
    ZoteroCollectionOption,
    ZoteroCollectionsResponse,
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
    (root / "data" / "rss_feeds.json").write_text(
        json.dumps(
            [{"journal": "Custom Nature", "url": "https://example.com/rss"}],
            ensure_ascii=False,
        ),
        encoding="utf-8",
    )

    conn = connect(root / "data" / "literature.sqlite")
    paper_id, _is_new = upsert_paper(
        conn,
        Paper(
            source_url="https://example.com/rss",
            title="API paper",
            url="https://example.com/api-paper",
            journal="Original Feed Title",
            abstract="Plain abstract text.",
            abstract_html="<p>Plain abstract text.</p>",
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
    assert report_payload["papers"][0]["journal"] == "Original Feed Title"
    assert report_payload["papers"][0]["abstract_html"] == "<p>Plain abstract text.</p>"
    assert report_payload["papers"][0]["abstract_images"] == []

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

    read_response = client.post(f"/api/papers/{paper_id}/read")
    assert read_response.status_code == 200
    assert read_response.json()["paper_id"] == paper_id


def test_feed_settings_api_returns_empty_list_then_writes_json(monkeypatch) -> None:
    root = _project_root("feed-settings")
    _bootstrap_root(root)

    monkeypatch.chdir(root)
    from scirssagent.server import create_app

    client = TestClient(create_app())
    get_response = client.get("/api/settings/feeds")
    assert get_response.status_code == 200
    assert get_response.json() == []

    put_response = client.put(
        "/api/settings/feeds",
        json={
            "feeds": [
                {
                    "journal": "Nature",
                    "url": "https://www.nature.com/nature.rss",
                }
            ]
        },
    )
    assert put_response.status_code == 200
    payload = put_response.json()
    assert payload[0]["journal"] == "Nature"
    assert (root / "data" / "rss_feeds.json").exists()


def test_settings_config_api_masks_secrets_and_updates_local_env(monkeypatch) -> None:
    root = _project_root("settings-config")
    _bootstrap_root(root)
    (root / ".env").write_text(
        "\n".join(
            [
                "SCIRSS_CLASSIFIER_API_KEY=super-secret-classifier-key",
                "SCIRSS_CLASSIFIER_MODEL=deepseek-v4-flash",
                "SCIRSS_CLASSIFIER_BATCH_SIZE=10",
            ]
        ),
        encoding="utf-8",
    )

    monkeypatch.chdir(root)
    from scirssagent.server import create_app

    client = TestClient(create_app())
    get_response = client.get("/api/settings/config")

    assert get_response.status_code == 200
    assert "super-secret-classifier-key" not in get_response.text
    api_key_field = next(
        item for item in get_response.json()["fields"] if item["key"] == "SCIRSS_CLASSIFIER_API_KEY"
    )
    assert api_key_field["secret"] is True
    assert api_key_field["configured"] is True
    assert api_key_field["source"] == "dotenv"
    assert api_key_field["value"] is None

    put_response = client.put(
        "/api/settings/config",
        json={
            "fields": {
                "SCIRSS_CLASSIFIER_API_KEY": {"value": "replacement-secret"},
                "SCIRSS_CLASSIFIER_MODEL": {"value": "deepseek-v4-pro"},
                "SCIRSS_CLASSIFIER_BATCH_SIZE": {"value": "12"},
                "SCIRSS_ZOTERO_COLLECTION_KEY": {"value": "INBOX"},
            }
        },
    )

    assert put_response.status_code == 200
    assert "replacement-secret" not in put_response.text
    env_text = (root / ".env").read_text(encoding="utf-8")
    assert "SCIRSS_CLASSIFIER_API_KEY='replacement-secret'" in env_text
    assert "SCIRSS_CLASSIFIER_MODEL='deepseek-v4-pro'" in env_text
    assert "SCIRSS_CLASSIFIER_BATCH_SIZE='12'" in env_text
    assert "SCIRSS_ZOTERO_COLLECTION_KEY='INBOX'" in env_text

    settings = load_settings(root)
    assert settings.classifier_api_key == "replacement-secret"
    assert settings.classifier_model == "deepseek-v4-pro"
    assert settings.classifier_batch_size == 12
    assert settings.zotero_collection_key == "INBOX"


def test_settings_config_api_shows_environment_override_source(monkeypatch) -> None:
    root = _project_root("settings-config-env")
    _bootstrap_root(root)
    (root / ".env").write_text("SCIRSS_PROFILE_MODEL=local-profile-model", encoding="utf-8")
    monkeypatch.setenv("SCIRSS_PROFILE_MODEL", "system-profile-model")
    monkeypatch.chdir(root)

    from scirssagent.server import create_app

    client = TestClient(create_app())
    response = client.get("/api/settings/config")

    assert response.status_code == 200
    profile_model_field = next(
        item for item in response.json()["fields"] if item["key"] == "SCIRSS_PROFILE_MODEL"
    )
    assert profile_model_field["value"] == "system-profile-model"
    assert profile_model_field["source"] == "environment"
    assert profile_model_field["stored_in_dotenv"] is True


def test_release_mode_settings_are_written_to_user_data_store(monkeypatch) -> None:
    root = _project_root("release-settings")
    _bootstrap_root(root)
    data_root = (root / "user-data").resolve()
    monkeypatch.setenv("FEEDMEDAILY_RUNTIME_MODE", "release")
    monkeypatch.setenv("FEEDMEDAILY_DATA_ROOT", str(data_root))
    monkeypatch.chdir(root)

    from scirssagent.server import create_app

    client = TestClient(create_app())
    response = client.put(
        "/api/settings/config",
        json={
            "fields": {
                "SCIRSS_CLASSIFIER_API_KEY": {"value": "release-secret"},
                "SCIRSS_PROFILE_MODEL": {"value": "release-profile-model"},
                "SCIRSS_ZOTERO_LIBRARY_ID": {"value": "123456"},
            }
        },
    )

    assert response.status_code == 200
    settings_json = (data_root / "config" / "settings.json").read_text(encoding="utf-8")
    secrets_json = (data_root / "config" / "secrets.json").read_text(encoding="utf-8")
    assert "release-profile-model" in settings_json
    assert "123456" in settings_json
    assert "release-secret" not in settings_json
    assert "release-secret" not in secrets_json

    settings = load_settings(root)
    assert settings.mode == "release"
    assert settings.user_data_dir == data_root
    assert settings.classifier_api_key == "release-secret"
    assert settings.profile_model == "release-profile-model"


def test_app_meta_update_and_scheduler_apis(monkeypatch) -> None:
    root = _project_root("app-meta")
    _bootstrap_root(root)
    monkeypatch.chdir(root)
    import scirssagent.server as server_module

    monkeypatch.setattr(
        server_module,
        "scheduler_status",
        lambda settings: {
            "installed": True,
            "task_name": "FeedMeDaily Daily Sync",
            "mode": settings.mode,
            "scheduled_time": "09:30",
            "state": "Ready",
            "next_run_time": "2026-05-02T09:30:00+00:00",
            "last_run_time": "2026-05-01T09:30:00+00:00",
            "last_result": 0,
            "command": "FeedMeDaily.exe run --once",
        },
    )
    monkeypatch.setattr(
        server_module,
        "install_scheduler_task",
        lambda settings, daily_time: {
            "installed": True,
            "task_name": "FeedMeDaily Daily Sync",
            "mode": settings.mode,
            "scheduled_time": daily_time,
            "state": "Ready",
            "next_run_time": None,
            "last_run_time": None,
            "last_result": 0,
            "command": "FeedMeDaily.exe run --once",
        },
    )
    removed: dict[str, bool] = {"called": False}
    monkeypatch.setattr(
        server_module,
        "remove_scheduler_task",
        lambda: removed.update({"called": True}),
    )

    class DummyResponse:
        def raise_for_status(self) -> None:
            return None

        def json(self) -> dict[str, str]:
            return {
                "version": "0.2.0",
                "download_url": "https://example.com/feedmedaily-installer.exe",
                "release_notes_url": "https://example.com/release-notes",
            }

    monkeypatch.setattr(server_module.httpx, "get", lambda *args, **kwargs: DummyResponse())
    monkeypatch.setenv("FEEDMEDAILY_UPDATE_MANIFEST_URL", "https://example.com/update.json")

    client = TestClient(server_module.create_app())

    meta_response = client.get("/api/app/meta")
    assert meta_response.status_code == 200
    assert meta_response.json()["name"] == "FeedMeDaily"

    update_response = client.get("/api/app/update")
    assert update_response.status_code == 200
    assert update_response.json()["has_update"] is True

    scheduler_get = client.get("/api/settings/scheduler")
    assert scheduler_get.status_code == 200
    assert scheduler_get.json()["scheduled_time"] == "09:30"

    scheduler_put = client.put("/api/settings/scheduler", json={"daily_time": "08:15"})
    assert scheduler_put.status_code == 200
    assert scheduler_put.json()["scheduled_time"] == "08:15"

    scheduler_delete = client.delete("/api/settings/scheduler")
    assert scheduler_delete.status_code == 200
    assert removed["called"] is True


def test_profile_bootstrap_launches_job(monkeypatch) -> None:
    root = _project_root("bootstrap")
    _bootstrap_root(root)

    monkeypatch.chdir(root)
    import scirssagent.server as server_module

    launched: dict[str, object] = {}

    def fake_launch_job(job_type, target, *args, **kwargs):
        launched["job_type"] = job_type
        launched["args"] = args
        launched["queued_message"] = kwargs.get("queued_message")
        launched["running_message"] = kwargs.get("running_message")
        return JobInfo(id="job-1", job_type=job_type, status="queued")

    monkeypatch.setattr(server_module, "launch_job", fake_launch_job)
    client = TestClient(server_module.create_app())

    response = client.post(
        "/api/profile/bootstrap",
        json={
            "interest_description": "I study nucleic acid chemistry and polymerase engineering.",
            "name": "Test bootstrap profile",
        },
    )

    assert response.status_code == 200
    assert response.json()["job"]["job_type"] == "profile-bootstrap"
    assert launched["job_type"] == "profile-bootstrap"
    assert launched["queued_message"] == "Queued initial profile generation."
    assert launched["running_message"] == "Generating the initial classification profile proposal."


def test_feedback_delete_api_removes_feedback(monkeypatch) -> None:
    root = _project_root("feedback-delete")
    _bootstrap_root(root)
    write_profile(root / "data" / "classification_profile.json", _profile("Delete profile"))

    conn = connect(root / "data" / "literature.sqlite")
    paper_id, _is_new = upsert_paper(
        conn,
        Paper(
            source_url="https://example.com/rss",
            title="Delete feedback paper",
            url="https://example.com/delete-feedback",
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
    feedback = save_feedback(
        conn,
        paper_id,
        Relevance.INDIRECT,
        Relevance.DIRECT,
        note="Delete me.",
    )
    conn.commit()
    conn.close()

    monkeypatch.chdir(root)
    from scirssagent.server import create_app

    client = TestClient(create_app())
    response = client.delete(f"/api/feedback/{feedback.id}")

    assert response.status_code == 200
    assert response.json()["deleted"] is True
    assert client.get("/api/feedback").json() == []


def test_admin_reclassify_accepts_all_scope(monkeypatch) -> None:
    root = _project_root("reclassify-all")
    _bootstrap_root(root)
    write_profile(root / "data" / "classification_profile.json", _profile("All profile"))

    monkeypatch.chdir(root)
    import scirssagent.server as server_module

    launched: dict[str, object] = {}

    def fake_launch_job(job_type, target, *args, **kwargs):
        launched["job_type"] = job_type
        launched["args"] = args
        return JobInfo(id="job-all", job_type=job_type, status="queued")

    monkeypatch.setattr(server_module, "launch_job", fake_launch_job)
    client = TestClient(server_module.create_app())

    response = client.post("/api/admin/reclassify", json={"scope": "all", "limit": 50})

    assert response.status_code == 200
    assert response.json()["job"]["job_type"] == "reclassify"
    assert launched["job_type"] == "reclassify"
    assert len(launched["args"]) == 2
    assert launched["args"][1]["scope"] == "all"


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
        rule_delta=ProfileProposalDelta(
            summary="Initial compact proposal review.",
            direct_rule_additions=["Polymerase and nucleotide chemistry."],
            indirect_rule_additions=["General protein engineering."],
            unrelated_rule_additions=["No overlap."],
            scope_rewrite="Nucleic acid chemistry and engineered enzymes.",
            tag_additions=[
                TopicDefinition(
                    id="polymerase_engineering",
                    label="Polymerase Engineering",
                )
            ],
        ),
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

    captured: dict[str, object] = {}

    monkeypatch.setattr(
        server_module,
        "save_paper_to_zotero",
        lambda settings, paper, classification, collection_key=None: (
            captured.update({"collection_key": collection_key}) or '{"ok":true}',
            "ITEM1234",
        ),
    )
    client = TestClient(server_module.create_app())

    response = client.post(f"/api/zotero/save/{paper_id}", json={"collection_key": "COLL123"})
    assert response.status_code == 200
    assert response.json()["state"] == "saved"
    assert response.json()["item_key"] == "ITEM1234"
    assert captured["collection_key"] == "COLL123"

    conn = connect(root / "data" / "literature.sqlite")
    saved = latest_zotero_status(conn, paper_id)
    conn.close()
    assert saved is not None
    assert saved.state == ZoteroSaveState.SAVED


def test_zotero_collections_api_returns_flattened_options(monkeypatch) -> None:
    root = _project_root("zotero-collections")
    _bootstrap_root(root)

    monkeypatch.chdir(root)
    import scirssagent.server as server_module

    monkeypatch.setattr(
        server_module,
        "list_zotero_collections",
        lambda settings: ZoteroCollectionsResponse(
            collections=[
                ZoteroCollectionOption(
                    key="PARENT",
                    name="Top",
                    path_label="Top",
                    parent_key=None,
                    is_default=False,
                ),
                ZoteroCollectionOption(
                    key="CHILD",
                    name="Child",
                    path_label="Top / Child",
                    parent_key="PARENT",
                    is_default=True,
                ),
            ],
            default_collection_key="CHILD",
        ),
    )
    client = TestClient(server_module.create_app())

    response = client.get("/api/zotero/collections")

    assert response.status_code == 200
    payload = response.json()
    assert payload["default_collection_key"] == "CHILD"
    assert payload["collections"][1]["path_label"] == "Top / Child"


def test_zotero_collections_api_returns_400_when_not_configured(monkeypatch) -> None:
    root = _project_root("zotero-collections-error")
    _bootstrap_root(root)

    monkeypatch.chdir(root)
    import scirssagent.server as server_module

    monkeypatch.setattr(
        server_module,
        "list_zotero_collections",
        lambda settings: (_ for _ in ()).throw(
            ValueError("SCIRSS_ZOTERO_API_KEY is not configured.")
        ),
    )
    client = TestClient(server_module.create_app())

    response = client.get("/api/zotero/collections")

    assert response.status_code == 400
    assert response.json()["detail"] == "SCIRSS_ZOTERO_API_KEY is not configured."


def _bootstrap_root(root: Path) -> None:
    (root / "data").mkdir(parents=True, exist_ok=True)
    (root / "reports").mkdir(parents=True, exist_ok=True)
    (root / "logs").mkdir(parents=True, exist_ok=True)


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
            )
        ],
        few_shots=[],
    )


def _project_root(name: str) -> Path:
    path = (Path(".tmp") / "server-tests" / f"{name}-{uuid4().hex}").resolve()
    path.mkdir(parents=True, exist_ok=True)
    return path
