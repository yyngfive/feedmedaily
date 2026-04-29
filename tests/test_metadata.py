from scirssagent.metadata import normalize_doi, paper_key
from scirssagent.models import Paper


def test_normalize_doi_from_url() -> None:
    assert normalize_doi("https://doi.org/10.1021/jacs.0c00000?x=1") == "10.1021/jacs.0c00000"


def test_paper_key_prefers_doi() -> None:
    paper = Paper(
        source_url="https://example.com/rss",
        title="A paper",
        url="https://example.com/a",
        doi="10.1000/ABC",
    )

    assert paper_key(paper) == "doi:10.1000/abc"

