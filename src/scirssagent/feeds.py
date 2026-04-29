from __future__ import annotations

from collections.abc import Iterable
from datetime import date, datetime
from email.utils import parsedate_to_datetime
from pathlib import Path
from xml.etree import ElementTree

import httpx

from scirssagent.models import Paper

ATOM = "{http://www.w3.org/2005/Atom}"
DC = "{http://purl.org/dc/elements/1.1/}"
PRISM = "{http://prismstandard.org/namespaces/basic/2.0/}"
CONTENT = "{http://purl.org/rss/1.0/modules/content/}"


def read_feed_urls(path: Path) -> list[str]:
    if not path.exists():
        raise FileNotFoundError(f"RSS feed file not found: {path}")
    urls: list[str] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        clean = line.strip()
        if clean and not clean.startswith("#"):
            urls.append(clean)
    return urls


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


def text(node: ElementTree.Element | None) -> str | None:
    if node is None or node.text is None:
        return None
    value = node.text.strip()
    return value or None


def child_text(node: ElementTree.Element, *names: str) -> str | None:
    for name in names:
        value = text(node.find(name))
        if value:
            return value
    return None


def parse_rss(root: ElementTree.Element, source_url: str) -> list[Paper]:
    channel = root.find("channel")
    if channel is None:
        return []
    feed_title = child_text(channel, "title")
    papers: list[Paper] = []
    for item in channel.findall("item"):
        title = child_text(item, "title")
        link = child_text(item, "link", "guid")
        if not title or not link:
            continue
        authors = [
            value
            for value in [
                child_text(item, f"{DC}creator"),
                child_text(item, "author"),
            ]
            if value
        ]
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
    for entry in root.findall(f"{ATOM}entry"):
        title = child_text(entry, f"{ATOM}title")
        link_node = entry.find(f"{ATOM}link[@href]")
        link = link_node.get("href") if link_node is not None else child_text(entry, f"{ATOM}id")
        if not title or not link:
            continue
        authors = [
            name
            for author in entry.findall(f"{ATOM}author")
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
    if root.tag == "rss" or root.find("channel") is not None:
        return parse_rss(root, url)
    if root.tag == f"{ATOM}feed":
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
