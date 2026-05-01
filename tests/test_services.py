from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path

import pytest

from scirssagent.config import Settings
from scirssagent.models import (
    ClassificationProfile,
    FeedbackProposalContext,
    ProfileFewShot,
    ProfileMeta,
    ProfileProposalDelta,
    Relevance,
    RelevanceRules,
    TopicDefinition,
)
from scirssagent.services import generate_profile_proposal_payload


def test_generate_profile_proposal_payload_preserves_profile_identity(monkeypatch) -> None:
    settings = _settings()
    current = _current_profile()
    generated = ProfileProposalDelta(
        summary="Add a direct rule for engineered XNA polymerases.",
        direct_rule_additions=[
            "Papers engineering XNA-active polymerases should be treated as directly relevant."
        ],
    )

    monkeypatch.setattr("scirssagent.services.profile_model_client", lambda settings: object())
    monkeypatch.setattr(
        "scirssagent.services._request_profile_json",
        lambda client, settings, **kwargs: generated.model_dump_json(),
    )

    payload = generate_profile_proposal_payload(settings, current, [_feedback()])
    proposal = payload["proposed_profile"]

    assert proposal.meta.name == current.meta.name
    assert proposal.meta.version == current.meta.version + 1
    assert proposal.meta.created_at == current.meta.created_at
    assert proposal.meta.source_description == current.meta.source_description
    assert payload["rule_delta"].summary == generated.summary
    assert proposal.relevance_rules.direct[-1].startswith("Papers engineering XNA-active")


def test_generate_profile_proposal_payload_rejects_generic_collapse(monkeypatch) -> None:
    settings = _settings()
    current = _current_profile()
    generated = ProfileProposalDelta(
        summary="Collapse to a generic broad-science profile.",
        scope_rewrite="General scientific literature covering a broad range of topics.",
        direct_rule_additions=["Article directly addresses core research question"],
        indirect_rule_additions=["Article provides background or related methodology"],
        unrelated_rule_additions=["Article is outside the scope of research interests"],
        tag_removals=[
            "nucleic_acid_chemistry",
            "xna",
            "aptamer_selex",
        ],
        tag_additions=[TopicDefinition(id="general_science", label="General Science")],
    )

    monkeypatch.setattr("scirssagent.services.profile_model_client", lambda settings: object())
    monkeypatch.setattr(
        "scirssagent.services._request_profile_json",
        lambda client, settings, **kwargs: generated.model_dump_json(),
        )

    with pytest.raises(ValueError, match="unsafe profile proposal"):
        generate_profile_proposal_payload(settings, current, [_feedback()])


def _settings() -> Settings:
    root = Path(".").resolve()
    return Settings(
        mode="source",
        root=root,
        app_dir=root,
        user_data_dir=root,
        config_dir=root,
        settings_store_path=None,
        secrets_store_path=None,
        runtime_state_path=root / "runtime.json",
        web_dist_dir=root / "web" / "dist",
        feeds_path=root / "data" / "rss_feeds.json",
        data_dir=root / "data",
        reports_dir=root / "reports",
        logs_dir=root / "logs",
        database_path=root / "data" / "literature.sqlite",
        profile_path=root / "data" / "classification_profile.json",
        launch_command_path=root / "FeedMeDaily.exe",
        update_manifest_url=None,
        classifier_api_key="classifier-key",
        classifier_base_url="https://example.com",
        classifier_model="classifier-model",
        classifier_thinking="disabled",
        classifier_batch_size=10,
        profile_api_key="profile-key",
        profile_base_url="https://example.com",
        profile_model="profile-model",
        profile_thinking="enabled",
        zotero_api_key=None,
        zotero_library_type="user",
        zotero_library_id=None,
        zotero_collection_key=None,
        server_host="127.0.0.1",
        server_port=8000,
    )


def _current_profile() -> ClassificationProfile:
    now = datetime(2026, 5, 1, tzinfo=UTC)
    return ClassificationProfile(
        meta=ProfileMeta(
            name="XNA",
            version=2,
            created_at=datetime(2025, 4, 6, tzinfo=UTC),
            updated_at=now,
            source_description="Focused on nucleic acid chemistry and enzyme engineering.",
        ),
        scope="Focused on nucleic acid chemistry, XNA, and engineering of nucleic-acid enzymes.",
        relevance_rules=RelevanceRules(
            direct=["XNA chemistry and polymerase engineering"],
            indirect=["Related nucleic acid methods"],
            unrelated=["No overlap with nucleic acid chemistry"],
        ),
        topic_taxonomy=[
            TopicDefinition(
                id="nucleic_acid_chemistry",
                label="Nucleic Acid Chemistry",
            ),
            TopicDefinition(
                id="xna",
                label="XNA",
            ),
            TopicDefinition(
                id="aptamer_selex",
                label="Aptamer and SELEX",
            ),
            TopicDefinition(
                id="nucleic_acid_probes",
                label="Nucleic Acid Probes",
            ),
            TopicDefinition(
                id="nucleic_acid_enzyme_engineering",
                label="Nucleic Acid Enzyme Engineering",
            ),
            TopicDefinition(
                id="polymerase",
                label="Polymerase Engineering",
            ),
        ],
        few_shots=[
            ProfileFewShot(
                title="Paper A",
                relevance=Relevance.DIRECT,
                tags=["xna"],
                rationale="Direct XNA work.",
            ),
            ProfileFewShot(
                title="Paper B",
                relevance=Relevance.DIRECT,
                tags=["polymerase"],
                rationale="Direct polymerase engineering.",
            ),
        ],
    )


def _feedback() -> FeedbackProposalContext:
    return FeedbackProposalContext(
        feedback_id=1,
        paper_id=1,
        paper_title="Example feedback paper",
        journal="Nature Chemistry",
        abstract="Engineers an XNA polymerase to improve substrate acceptance.",
        original_relevance=Relevance.INDIRECT,
        corrected_relevance=Relevance.DIRECT,
        note="This should count as direct because it engineers an XNA polymerase.",
    )
