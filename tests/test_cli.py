from __future__ import annotations

import json
from types import SimpleNamespace

from typer.testing import CliRunner

from scirssagent.cli import app


class _DummyConn:
    def commit(self) -> None:
        return None

    def close(self) -> None:
        return None


class _JSONPayload:
    def __init__(self, payload: object) -> None:
        self._payload = payload

    def model_dump_json(self) -> str:
        return json.dumps(self._payload)


def test_reclassify_cli_uses_scope_selection(monkeypatch) -> None:
    captured: dict[str, object] = {}

    def fake_reclassify(settings, paper_ids):
        captured["paper_ids"] = list(paper_ids)
        return len(captured["paper_ids"])

    monkeypatch.setattr(
        "scirssagent.cli.load_settings",
        lambda root=None: SimpleNamespace(database_path="fixture.sqlite"),
    )
    monkeypatch.setattr("scirssagent.cli.connect", lambda path: _DummyConn())
    monkeypatch.setattr("scirssagent.cli.all_paper_ids", lambda conn: [1, 2, 3])
    monkeypatch.setattr("scirssagent.cli.run_reclassify_paper_ids", fake_reclassify)

    result = CliRunner().invoke(app, ["reclassify", "--scope", "all"])

    assert result.exit_code == 0
    assert captured["paper_ids"] == [1, 2, 3]


def test_reclassify_cli_uses_explicit_paper_ids(monkeypatch) -> None:
    captured: dict[str, object] = {}

    def fake_reclassify(settings, paper_ids):
        captured["paper_ids"] = list(paper_ids)
        return len(captured["paper_ids"])

    monkeypatch.setattr(
        "scirssagent.cli.load_settings",
        lambda root=None: SimpleNamespace(database_path="fixture.sqlite"),
    )
    monkeypatch.setattr("scirssagent.cli.run_reclassify_paper_ids", fake_reclassify)

    result = CliRunner().invoke(app, ["reclassify", "--paper-id", "8", "--paper-id", "9"])

    assert result.exit_code == 0
    assert captured["paper_ids"] == [8, 9]


def test_reclassify_cli_rejects_mixed_scope_and_paper_ids() -> None:
    result = CliRunner().invoke(app, ["reclassify", "--scope", "all", "--paper-id", "8"])

    assert result.exit_code != 0
    assert "--paper-id cannot be used together with --scope or --limit." in result.output


def test_profile_bootstrap_cli_outputs_json(monkeypatch) -> None:
    monkeypatch.setattr(
        "scirssagent.cli._bootstrap_profile_cli",
        lambda root, interest_description, name: {"proposal_id": 7, "name": name},
    )

    result = CliRunner().invoke(
        app,
        ["profile", "bootstrap", "--interest-description", "RNA biology", "--name", "Alice"],
    )

    assert result.exit_code == 0
    assert json.loads(result.output) == {"proposal_id": 7, "name": "Alice"}


def test_profile_proposal_generate_cli_outputs_json(monkeypatch) -> None:
    monkeypatch.setattr(
        "scirssagent.cli._generate_profile_proposal_cli",
        lambda root: {"proposal_id": 8, "state": "pending"},
    )

    result = CliRunner().invoke(app, ["profile", "proposal-generate"])

    assert result.exit_code == 0
    assert json.loads(result.output) == {"proposal_id": 8, "state": "pending"}


def test_run_cli_outputs_go_summary_json(monkeypatch) -> None:
    monkeypatch.setattr(
        "scirssagent.cli.load_settings",
        lambda root=None: SimpleNamespace(root=".", mode="source", app_dir="."),
    )
    monkeypatch.setattr(
        "scirssagent.cli._run_once_via_go",
        lambda settings, max_papers=None, reclassify=False: {
            "fetched": 4,
            "inserted": 2,
            "updated": 1,
            "classified": 3,
            "errors": [],
            "report_path": "reports/latest/index.html",
        },
    )

    result = CliRunner().invoke(app, ["run", "--once"])

    assert result.exit_code == 0
    assert json.loads(result.output) == {
        "fetched": 4,
        "inserted": 2,
        "updated": 1,
        "classified": 3,
        "errors": [],
        "report_path": "reports/latest/index.html",
    }


def test_report_latest_cli_outputs_go_payload(monkeypatch) -> None:
    monkeypatch.setattr(
        "scirssagent.cli.load_settings",
        lambda root=None: SimpleNamespace(root=".", mode="source", app_dir="."),
    )
    monkeypatch.setattr(
        "scirssagent.cli._report_latest_via_go",
        lambda settings: {"report_papers": 9},
    )

    result = CliRunner().invoke(app, ["report", "latest"])

    assert result.exit_code == 0
    assert json.loads(result.output) == {"report_papers": 9}


def test_zotero_collections_cli_outputs_json(monkeypatch) -> None:
    monkeypatch.setattr(
        "scirssagent.cli.load_settings",
        lambda root=None: SimpleNamespace(),
    )
    monkeypatch.setattr(
        "scirssagent.cli._zotero_collections_via_go",
        lambda settings: {"collections": [{"key": "A1", "name": "Inbox"}]},
    )

    result = CliRunner().invoke(app, ["zotero", "collections"])

    assert result.exit_code == 0
    assert json.loads(result.output) == {"collections": [{"key": "A1", "name": "Inbox"}]}


def test_zotero_save_cli_outputs_status_json(monkeypatch) -> None:
    monkeypatch.setattr(
        "scirssagent.cli.load_settings",
        lambda root=None: SimpleNamespace(),
    )
    monkeypatch.setattr(
        "scirssagent.cli._zotero_save_via_go",
        lambda settings, paper_id, collection_key=None: {
            "paper_id": paper_id,
            "state": "saved",
            "item_key": "ITEM-1",
        },
    )

    result = CliRunner().invoke(app, ["zotero", "save", "--paper-id", "5", "--collection-key", "ABC"])

    assert result.exit_code == 0
    assert json.loads(result.output) == {"paper_id": 5, "state": "saved", "item_key": "ITEM-1"}
