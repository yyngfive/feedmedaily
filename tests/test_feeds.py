import json
from pathlib import Path
from uuid import uuid4

from scirssagent.feeds import read_feed_subscriptions, read_feed_urls


def test_read_feed_urls_reads_project_seed_file() -> None:
    urls = read_feed_urls(Path("RSS.txt"))

    assert "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=jacsat" in urls


def test_read_feed_subscriptions_reads_json_file() -> None:
    root = _tmp_root("json")
    path = root / "rss_feeds.json"
    path.write_text(
        json.dumps(
            [
                {"journal": "Nature", "url": "https://www.nature.com/nature.rss"},
                {"journal": "Science", "url": "https://www.science.org/rss"},
            ]
        ),
        encoding="utf-8",
    )

    subscriptions = read_feed_subscriptions(path)

    assert subscriptions[0].journal == "Nature"
    assert str(subscriptions[1].url) == "https://www.science.org/rss"


def test_read_feed_subscriptions_falls_back_to_legacy_file() -> None:
    root = _tmp_root("legacy")
    path = root / "rss_feeds.json"
    legacy = root / "RSS.txt"
    legacy.write_text("https://www.nature.com/nature.rss\n", encoding="utf-8")

    subscriptions = read_feed_subscriptions(path, legacy)

    assert len(subscriptions) == 1
    assert subscriptions[0].journal == "nature.com"


def _tmp_root(name: str) -> Path:
    path = (Path(".tmp") / "feed-tests" / f"{name}-{uuid4().hex}").resolve()
    path.mkdir(parents=True, exist_ok=True)
    return path
