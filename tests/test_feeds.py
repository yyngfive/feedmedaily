from pathlib import Path

from scirssagent.feeds import read_feed_urls


def test_read_feed_urls_reads_project_seed_file() -> None:
    urls = read_feed_urls(Path("RSS.txt"))

    assert "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=jacsat" in urls
