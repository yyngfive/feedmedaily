from scirssagent.models import Paper
from scirssagent.storage import connect, upsert_paper


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
