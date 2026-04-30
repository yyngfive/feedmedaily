import json
from pathlib import Path
from uuid import uuid4

from scirssagent.feeds import read_feed_subscriptions, read_feed_urls


def test_read_feed_urls_returns_empty_list_when_json_file_is_missing() -> None:
    urls = read_feed_urls(_tmp_root("missing") / "rss_feeds.json")

    assert urls == []


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


def test_read_feed_subscriptions_returns_empty_list_when_file_is_missing() -> None:
    root = _tmp_root("missing-subscriptions")
    path = root / "rss_feeds.json"

    subscriptions = read_feed_subscriptions(path)

    assert subscriptions == []


def _tmp_root(name: str) -> Path:
    path = (Path(".tmp") / "feed-tests" / f"{name}-{uuid4().hex}").resolve()
    path.mkdir(parents=True, exist_ok=True)
    return path
