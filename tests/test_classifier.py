from datetime import UTC, datetime

from scirssagent.classifier import batch_classification_prompt, classification_prompt
from scirssagent.models import (
    ClassificationProfile,
    Paper,
    ProfileFewShot,
    ProfileMeta,
    Relevance,
    RelevanceRules,
    TopicDefinition,
)


def test_prompt_embeds_profile_rules_and_taxonomy() -> None:
    paper = Paper(
        source_url="https://example.com/rss",
        title="Directed evolution of an XNA polymerase",
        url="https://example.com/a",
    )

    prompt = classification_prompt(paper, _profile())

    assert "User classification profile" in prompt
    assert "Polymerase Engineering" in prompt
    assert "Only emit tag ids that exist in the profile topic taxonomy." in prompt
    assert "source_description" not in prompt
    assert "Engineered polymerases and related evolution studies." not in prompt


def test_batch_prompt_mentions_profile_tag_ids_and_trims_few_shots() -> None:
    paper = Paper(
        source_url="https://example.com/rss",
        title="Chemical ligation in nucleic acid systems",
        url="https://example.com/b",
        abstract="A nucleic acid ligation method.",
    )

    prompt = batch_classification_prompt([("1", paper)], _profile())

    assert "polymerase_engineering" in prompt
    assert "nucleic_acid_chemistry" in prompt
    assert "Chemical ligation in nucleic acid systems" in prompt
    assert "Directed evolution of an XNA polymerase" in prompt
    assert "Modified nucleotide ligation chemistry" in prompt


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
            ),
            TopicDefinition(
                id="nucleic_acid_chemistry",
                label="Nucleic Acid Chemistry",
            ),
        ],
        few_shots=[
            ProfileFewShot(
                title="Directed evolution of an XNA polymerase",
                relevance=Relevance.DIRECT,
                tags=["polymerase_engineering"],
                rationale="Matches direct polymerase interest.",
            ),
            ProfileFewShot(
                title="Modified nucleotide ligation chemistry",
                relevance=Relevance.DIRECT,
                tags=["nucleic_acid_chemistry"],
                rationale="Matches direct chemistry interest.",
            ),
        ],
    )
