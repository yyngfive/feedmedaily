from __future__ import annotations

import json
from collections.abc import Iterable
from datetime import date, datetime
from email.utils import parsedate_to_datetime
from pathlib import Path
from urllib.parse import urlparse
from xml.etree import ElementTree

import httpx

from scirssagent.models import FeedSubscription, Paper

ATOM = "{http://www.w3.org/2005/Atom}"
DC = "{http://purl.org/dc/elements/1.1/}"
PRISM = "{http://prismstandard.org/namespaces/basic/2.0/}"
CONTENT = "{http://purl.org/rss/1.0/modules/content/}"


def infer_feed_journal_name(url: str) -> str:
    parsed = urlparse(url)
    host = parsed.netloc.removeprefix("www.")
    if host:
        return host
    return url


def _read_legacy_feed_urls(path: Path) -> list[str]:
    urls: list[str] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        clean = line.strip()
        if clean and not clean.startswith("#"):
            urls.append(clean)
    return urls


def read_feed_subscriptions(path: Path, legacy_path: Path | None = None) -> list[FeedSubscription]:
    source_path: Path | None = None
    if path.exists():
        source_path = path
    elif legacy_path is not None and legacy_path.exists():
        source_path = legacy_path
    if source_path is None:
        raise FileNotFoundError(f"RSS feed file not found: {path}")

    if source_path.suffix.lower() == ".json":
        payload = json.loads(source_path.read_text(encoding="utf-8"))
        if not isinstance(payload, list):
            raise ValueError("Feed subscriptions JSON must be an array.")
        return [FeedSubscription.model_validate(item) for item in payload]

    return [
        FeedSubscription(journal=infer_feed_journal_name(url), url=url)
        for url in _read_legacy_feed_urls(source_path)
    ]


def read_feed_urls(path: Path, legacy_path: Path | None = None) -> list[str]:
    return [str(item.url) for item in read_feed_subscriptions(path, legacy_path)]


def write_feed_subscriptions(path: Path, subscriptions: Iterable[FeedSubscription]) -> None:
    payload: list[dict[str, str]] = []
    seen: set[str] = set()
    for item in subscriptions:
        url = str(item.url)
        if url in seen:
            continue
        seen.add(url)
        payload.append({"journal": item.journal, "url": url})
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")


def parse_entry_date_text(*values: str | None) -> date | None:
    for value in values:
        if not value:
            continue
        try:
            return parsedate_to_datetime(value).date()
        except (TypeError, ValueError, IndexError):
            try:
                return datetime.fromisoformat(value.replace("Z", "+00:00")).date()
            except ValueError:
                continue
    return None


def entry_doi(*values: str | None) -> str | None:
    candidates: list[str] = []
    for value in values:
        if value:
            candidates.append(str(value))
    for candidate in candidates:
        lowered = candidate.lower()
        if "10." not in lowered:
            continue
        start = lowered.find("10.")
        doi = candidate[start:].strip().removeprefix("doi:")
        for sep in ("?", "#", "&"):
            doi = doi.split(sep, 1)[0]
        return doi.rstrip(".")
    return None


def local_name(tag: str) -> str:
    return tag.split("}", 1)[-1]


def children(node: ElementTree.Element, *names: str) -> list[ElementTree.Element]:
    wanted = {local_name(name) for name in names}
    return [child for child in list(node) if local_name(child.tag) in wanted]


def first_child(node: ElementTree.Element, *names: str) -> ElementTree.Element | None:
    matches = children(node, *names)
    return matches[0] if matches else None


def text(node: ElementTree.Element | None) -> str | None:
    if node is None or node.text is None:
        return None
    value = node.text.strip()
    return value or None


def child_text(node: ElementTree.Element, *names: str) -> str | None:
    for name in names:
        value = text(first_child(node, name))
        if value:
            return value
    return None


def parse_rss(root: ElementTree.Element, source_url: str) -> list[Paper]:
    channel = first_child(root, "channel")
    if channel is None:
        return []
    feed_title = child_text(channel, "title")
    papers: list[Paper] = []
    items = children(channel, "item")
    if not items:
        items = children(root, "item")
    for item in items:
        title = child_text(item, "title")
        link = child_text(item, "link", "guid")
        if not title or not link:
            continue
        authors = [
            value for value in (text(node) for node in children(item, f"{DC}creator")) if value
        ]
        fallback_author = child_text(item, "author")
        if fallback_author and fallback_author not in authors:
            authors.append(fallback_author)
        abstract = child_text(item, "description", f"{CONTENT}encoded")
        doi = entry_doi(
            child_text(item, f"{PRISM}doi"),
            child_text(item, f"{DC}identifier"),
            child_text(item, "guid"),
            link,
        )
        papers.append(
            Paper(
                source_url=source_url,
                feed_title=feed_title,
                title=title,
                url=link,
                doi=doi,
                journal=feed_title,
                authors=authors,
                abstract=abstract,
                published_date=parse_entry_date_text(
                    child_text(item, "pubDate"),
                    child_text(item, f"{DC}date"),
                ),
                raw={"guid": child_text(item, "guid")},
            )
        )
    return papers


def parse_atom(root: ElementTree.Element, source_url: str) -> list[Paper]:
    feed_title = child_text(root, f"{ATOM}title")
    papers: list[Paper] = []
    for entry in children(root, f"{ATOM}entry"):
        title = child_text(entry, f"{ATOM}title")
        link_node = next(
            (child for child in children(entry, f"{ATOM}link") if child.get("href")),
            None,
        )
        link = link_node.get("href") if link_node is not None else child_text(entry, f"{ATOM}id")
        if not title or not link:
            continue
        authors = [
            name
            for author in children(entry, f"{ATOM}author")
            for name in [child_text(author, f"{ATOM}name")]
            if name
        ]
        abstract = child_text(entry, f"{ATOM}summary", f"{ATOM}content")
        doi = entry_doi(child_text(entry, f"{ATOM}id"), link)
        papers.append(
            Paper(
                source_url=source_url,
                feed_title=feed_title,
                title=title,
                url=link,
                doi=doi,
                journal=feed_title,
                authors=authors,
                abstract=abstract,
                published_date=parse_entry_date_text(
                    child_text(entry, f"{ATOM}published"),
                    child_text(entry, f"{ATOM}updated"),
                ),
                raw={"id": child_text(entry, f"{ATOM}id")},
            )
        )
    return papers


def fetch_feed(url: str) -> list[Paper]:
    response = httpx.get(url, timeout=30, follow_redirects=True)
    response.raise_for_status()
    root = ElementTree.fromstring(response.content)
    if local_name(root.tag) == "rss" or first_child(root, "channel") is not None:
        return parse_rss(root, url)
    if local_name(root.tag) == "feed":
        return parse_atom(root, url)
    return []


def fetch_all_feeds(urls: Iterable[str]) -> tuple[list[Paper], list[str]]:
    papers: list[Paper] = []
    errors: list[str] = []
    for url in urls:
        try:
            papers.extend(fetch_feed(url))
        except Exception as exc:  # feedparser may surface different parser/network exceptions.
            errors.append(f"{url}: {exc}")
    return papers, errors
