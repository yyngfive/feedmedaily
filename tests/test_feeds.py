import json
from pathlib import Path
from uuid import uuid4
from xml.etree import ElementTree

from scirssagent.feeds import parse_rss, read_feed_subscriptions, read_feed_urls


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


def test_parse_rss_prefers_encoded_html_abstract_and_extracts_images() -> None:
    root = ElementTree.fromstring(
        """
        <rss xmlns:content="http://purl.org/rss/1.0/modules/content/">
          <channel>
            <title>Angew</title>
            <item>
              <title>Paper A</title>
              <link>https://example.com/a</link>
              <description>Vol. 62, Issue 10, Pages 1-4</description>
              <content:encoded><![CDATA[
                <p>Real abstract paragraph.</p>
                <p><img src="/figure.png" alt="Figure 1"></p>
              ]]></content:encoded>
            </item>
          </channel>
        </rss>
        """
    )

    papers = parse_rss(root, "https://example.com/rss")

    assert papers[0].abstract == "Real abstract paragraph."
    assert "Real abstract paragraph." in (papers[0].abstract_html or "")
    assert papers[0].abstract_images[0].src == "https://example.com/figure.png"


def test_parse_rss_drops_metadata_only_description() -> None:
    root = ElementTree.fromstring(
        """
        <rss>
          <channel>
            <title>Science</title>
            <item>
              <title>Paper B</title>
              <link>https://example.com/b</link>
              <description>Vol. 388, Issue 6742, pp. 120-124</description>
            </item>
          </channel>
        </rss>
        """
    )

    papers = parse_rss(root, "https://example.com/rss")

    assert papers[0].abstract is None
    assert papers[0].abstract_html is None
    assert papers[0].abstract_images == []


def test_parse_rss_strips_nature_preamble_from_abstract() -> None:
    root = ElementTree.fromstring(
        """
        <rss xmlns:content="http://purl.org/rss/1.0/modules/content/"
             xmlns:prism="http://prismstandard.org/namespaces/basic/2.0/">
          <channel>
            <title>Nature</title>
            <item>
              <title>Paper C</title>
              <link>https://example.com/c</link>
              <content:encoded><![CDATA[
                <p>Nature, Published online: 29 April 2026; <a href="https://example.com/c">doi:10.1038/test</a></p>
                A real abstract paragraph.
              ]]></content:encoded>
              <prism:publicationName>Nature</prism:publicationName>
            </item>
          </channel>
        </rss>
        """
    )

    papers = parse_rss(root, "https://example.com/rss")

    assert papers[0].journal == "Nature"
    assert papers[0].abstract == "A real abstract paragraph."
    assert "Published online" not in (papers[0].abstract_html or "")


def test_parse_rss_extracts_jacs_journal_name_from_cite() -> None:
    root = ElementTree.fromstring(
        """
        <rss xmlns:dc="http://purl.org/dc/elements/1.1/">
          <channel>
            <title>JACS</title>
            <item>
              <title>Paper D</title>
              <link>https://example.com/d</link>
              <description><![CDATA[
                <p><img src="https://example.com/graphic.gif" alt="TOC Graphic" /></p>
                <div><cite>Journal of the American Chemical Society</cite></div>
                <div>DOI: 10.1021/test</div>
              ]]></description>
            </item>
          </channel>
        </rss>
        """
    )

    papers = parse_rss(root, "https://example.com/rss")

    assert papers[0].journal == "Journal of the American Chemical Society"
    assert papers[0].abstract is None


def _tmp_root(name: str) -> Path:
    path = (Path(".tmp") / "feed-tests" / f"{name}-{uuid4().hex}").resolve()
    path.mkdir(parents=True, exist_ok=True)
    return path
