from datetime import UTC, datetime

import pytest

from scirssagent.models import (
    ClassificationProfile,
    ProfileFewShot,
    ProfileMeta,
    Relevance,
    RelevanceRules,
    TopicDefinition,
)
from scirssagent.profiles import validate_profile_json


def test_validate_profile_json_accepts_markdown_fence_and_extra_text() -> None:
    profile = _profile()
    payload = (
        "Here is the profile draft.\n"
        "```json\n"
        f"{profile.model_dump_json(indent=2)}\n"
        "```\n"
        "Use it as the new classification profile."
    )

    parsed = validate_profile_json(payload)

    assert parsed.meta.name == "Test profile"
    assert parsed.topic_taxonomy[0].id == "polymerase_engineering"


def test_validate_profile_json_rejects_legacy_tag_shape() -> None:
    profile = _profile().model_dump(mode="json")
    profile["topic_taxonomy"][0]["description"] = "Legacy field should be rejected."

    with pytest.raises(ValueError, match="topic_taxonomy.0.description"):
        validate_profile_json(profile)


def test_validate_profile_json_rejects_legacy_classification_notes() -> None:
    profile = _profile().model_dump(mode="json")
    profile["classification_notes"] = ["Legacy notes field should be rejected."]

    with pytest.raises(ValueError, match="classification_notes"):
        validate_profile_json(profile)


def _profile() -> ClassificationProfile:
    now = datetime(2026, 4, 30, tzinfo=UTC)
    return ClassificationProfile(
        meta=ProfileMeta(
            name="Test profile",
            version=1,
            created_at=now,
            updated_at=now,
            source_description="Nucleic acid chemistry and enzyme engineering.",
        ),
        scope="Papers about nucleic acid chemistry and engineered enzymes.",
        relevance_rules=RelevanceRules(
            direct=["Polymerase engineering and nucleic acid chemistry."],
            indirect=["General protein engineering without nucleic acid focus."],
            unrelated=["No meaningful overlap with the user's interests."],
        ),
        topic_taxonomy=[
            TopicDefinition(
                id="polymerase_engineering",
                label="Polymerase Engineering",
            )
        ],
        few_shots=[
            ProfileFewShot(
                title="Directed evolution of an XNA polymerase",
                relevance=Relevance.DIRECT,
                tags=["polymerase_engineering"],
                rationale="Matches direct polymerase interest.",
            )
        ],
    )
