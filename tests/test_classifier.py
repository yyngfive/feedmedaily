from scirssagent.classifier import heuristic_classify
from scirssagent.models import Paper, Relevance


def test_proximity_labeling_method_is_visible_as_direct() -> None:
    paper = Paper(
        source_url="https://example.com/rss",
        title="Engineering TurboID variants for faster proximity labeling",
        url="https://example.com/turboid",
        abstract="We benchmark engineered enzymes and substrates for proximity labeling.",
    )

    result = heuristic_classify(paper)

    assert result.relevance == Relevance.DIRECT
    assert "proximity_labeling" in result.topic_tags


def test_unrelated_when_no_topic_terms_match() -> None:
    paper = Paper(
        source_url="https://example.com/rss",
        title="A cobalt electrocatalyst for water oxidation",
        url="https://example.com/cobalt",
    )

    assert heuristic_classify(paper).relevance == Relevance.UNRELATED

