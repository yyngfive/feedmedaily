import httpx

from scirssagent.metadata import enrich_paper, normalize_doi, paper_key
from scirssagent.models import AbstractSource, Paper


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


def test_enrich_paper_prefers_openalex_abstract(monkeypatch) -> None:
    paper = Paper(
        source_url="https://example.com/rss",
        title="A paper",
        url="https://example.com/a",
        doi="10.1000/abc",
        abstract="RSS abstract",
    )

    def fake_get(self, url, **kwargs):
        if "openalex" in url:
            return httpx.Response(
                200,
                json={
                    "doi": "https://doi.org/10.1000/abc",
                    "abstract_inverted_index": {"OpenAlex": [0], "abstract": [1]},
                    "primary_location": {"source": {"display_name": "OpenAlex Journal"}},
                },
            )
        return httpx.Response(
            200,
            json={
                "message": {
                    "container-title": ["Crossref Journal"],
                    "abstract": "Crossref abstract",
                }
            },
        )

    monkeypatch.setattr(httpx.Client, "get", fake_get)

    enriched = enrich_paper(paper)

    assert enriched.abstract == "OpenAlex abstract"
    assert enriched.journal == "OpenAlex Journal"
    assert enriched.abstract_source == AbstractSource.OPENALEX


def test_enrich_paper_falls_back_to_crossref_then_rss(monkeypatch) -> None:
    paper = Paper(
        source_url="https://example.com/rss",
        title="A paper",
        url="https://example.com/a",
        doi="10.1000/abc",
        abstract="RSS abstract",
        abstract_html="<p>RSS abstract</p>",
    )

    def fake_get(self, url, **kwargs):
        if "openalex" in url:
            return httpx.Response(
                200,
                json={
                    "doi": "https://doi.org/10.1000/abc",
                    "abstract_inverted_index": None,
                    "primary_location": {"source": {"display_name": "OpenAlex Journal"}},
                },
            )
        return httpx.Response(
            200,
            json={
                "message": {
                    "container-title": ["Crossref Journal"],
                    "abstract": "Crossref abstract",
                }
            },
        )

    monkeypatch.setattr(httpx.Client, "get", fake_get)

    enriched = enrich_paper(paper)

    assert enriched.abstract == "Crossref abstract"
    assert enriched.abstract_html is None
    assert enriched.abstract_source == AbstractSource.CROSSREF


def test_enrich_paper_keeps_rss_abstract_when_external_missing(monkeypatch) -> None:
    paper = Paper(
        source_url="https://example.com/rss",
        title="A paper",
        url="https://example.com/a",
        doi="10.1000/abc",
        abstract="RSS abstract",
        abstract_html="<p>RSS abstract</p>",
    )

    def fake_get(self, url, **kwargs):
        if "openalex" in url:
            return httpx.Response(200, json={"results": [{}]})
        return httpx.Response(200, json={"message": {"container-title": ["Crossref Journal"]}})

    monkeypatch.setattr(httpx.Client, "get", fake_get)

    enriched = enrich_paper(paper)

    assert enriched.abstract == "RSS abstract"
    assert enriched.abstract_html == "<p>RSS abstract</p>"
    assert enriched.abstract_source == AbstractSource.RSS


def test_enrich_paper_marks_none_when_no_abstract_exists(monkeypatch) -> None:
    paper = Paper(
        source_url="https://example.com/rss",
        title="A paper",
        url="https://example.com/a",
    )

    def fake_get(self, url, **kwargs):
        if "openalex" in url:
            return httpx.Response(200, json={"results": [{}]})
        return httpx.Response(200, json={"message": {}})

    monkeypatch.setattr(httpx.Client, "get", fake_get)

    enriched = enrich_paper(paper)

    assert enriched.abstract is None
    assert enriched.abstract_source == AbstractSource.NONE
