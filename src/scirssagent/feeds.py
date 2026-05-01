from __future__ import annotations

import html
import json
import re
from collections.abc import Iterable
from datetime import date, datetime
from email.utils import parsedate_to_datetime
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import urljoin, urlparse
from xml.etree import ElementTree

import httpx

from scirssagent.models import AbstractImage, FeedSubscription, Paper

ATOM = "{http://www.w3.org/2005/Atom}"
DC = "{http://purl.org/dc/elements/1.1/}"
PRISM = "{http://prismstandard.org/namespaces/basic/2.0/}"
CONTENT = "{http://purl.org/rss/1.0/modules/content/}"
ALLOWED_TAGS = {"p", "br", "strong", "em", "b", "i", "u", "sub", "sup", "ul", "ol", "li", "a"}
WHITESPACE_RE = re.compile(r"\s+")
METADATA_RE = re.compile(
    r"\b(vol(?:ume)?|issue|pp?\.|pages?|doi|e?issn|published|online)\b",
    re.IGNORECASE,
)
ABSTRACT_HEADING_RE = re.compile(r"(?:^|\s)ABSTRACT[:\s-]*")
NATURE_PREFIX_RE = re.compile(
    r"^[^.]*?,\s*Published online:\s*.*?;\s*doi:\S+\s*",
    re.IGNORECASE,
)
CITE_RE = re.compile(r"<cite>(.*?)</cite>", re.IGNORECASE | re.DOTALL)


class FeedHtmlSanitizer(HTMLParser):
    def __init__(self, base_url: str):
        super().__init__(convert_charrefs=True)
        self.base_url = base_url
        self.fragments: list[str] = []
        self.images: list[AbstractImage] = []
        self.open_tags: list[str] = []
        self.text_parts: list[str] = []
        self._skip_depth = 0

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        tag = tag.lower()
        if tag in {"script", "style"}:
            self._skip_depth += 1
            return
        if self._skip_depth:
            return
        attr_map = {name.lower(): (value or "") for name, value in attrs}
        if tag == "img":
            src = resolve_image_src(attr_map.get("src"), self.base_url)
            if src:
                alt = normalize_text(attr_map.get("alt")) or None
                self.images.append(AbstractImage(src=src, alt=alt))
            return
        if tag not in ALLOWED_TAGS:
            return
        if tag == "a":
            href = resolve_link(attr_map.get("href"), self.base_url)
            if href:
                escaped_href = html.escape(href, quote=True)
                self.fragments.append(
                    f'<a href="{escaped_href}" target="_blank" rel="noreferrer">'
                )
                self.open_tags.append("a")
            return
        if tag == "br":
            self.fragments.append("<br>")
            self.text_parts.append("\n")
            return
        self.fragments.append(f"<{tag}>")
        self.open_tags.append(tag)
        if tag in {"p", "li"} and self.text_parts and not self.text_parts[-1].endswith("\n"):
            self.text_parts.append("\n")

    def handle_endtag(self, tag: str) -> None:
        tag = tag.lower()
        if tag in {"script", "style"} and self._skip_depth:
            self._skip_depth -= 1
            return
        if self._skip_depth or tag not in ALLOWED_TAGS or tag == "br":
            return
        if tag in self.open_tags:
            while self.open_tags:
                open_tag = self.open_tags.pop()
                self.fragments.append(f"</{open_tag}>")
                if open_tag == tag:
                    break
        if tag in {"p", "li"}:
            self.text_parts.append("\n")

    def handle_data(self, data: str) -> None:
        if self._skip_depth:
            return
        if not data:
            return
        self.fragments.append(html.escape(data))
        self.text_parts.append(data)

    def close_output(self) -> tuple[str | None, list[AbstractImage], str]:
        while self.open_tags:
            self.fragments.append(f"</{self.open_tags.pop()}>")
        html_text = "".join(self.fragments).strip() or None
        plain_text = normalize_text(
            " ".join(part.strip() for part in self.text_parts if part.strip())
        )
        return html_text, self.images, plain_text


def read_feed_subscriptions(path: Path) -> list[FeedSubscription]:
    if not path.exists():
        return []
    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, list):
        raise ValueError("Feed subscriptions JSON must be an array.")
    return [FeedSubscription.model_validate(item) for item in payload]


def read_feed_urls(path: Path) -> list[str]:
    return [str(item.url) for item in read_feed_subscriptions(path)]


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


def normalize_text(value: str | None) -> str:
    return WHITESPACE_RE.sub(" ", str(value or "").replace("\xa0", " ")).strip()


def element_inner_xml(node: ElementTree.Element | None) -> str | None:
    if node is None:
        return None
    parts: list[str] = []
    if node.text:
        parts.append(node.text)
    for child in list(node):
        parts.append(ElementTree.tostring(child, encoding="unicode", method="html"))
    payload = "".join(parts).strip()
    return payload or None


def looks_like_metadata(value: str) -> bool:
    lowered = value.lower()
    if len(value) < 120 and METADATA_RE.search(lowered):
        return True
    if len(value) < 80 and value.count(";") + value.count(",") >= 3 and not value.endswith("."):
        return True
    return False


def resolve_link(value: str | None, base_url: str) -> str | None:
    if not value:
        return None
    resolved = urljoin(base_url, value.strip())
    parsed = urlparse(resolved)
    if parsed.scheme in {"http", "https"}:
        return resolved
    return None


def resolve_image_src(value: str | None, base_url: str) -> str | None:
    return resolve_link(value, base_url)


def sanitize_abstract_html(
    value: str | None, *, base_url: str
) -> tuple[str | None, list[AbstractImage], str]:
    if not value:
        return None, [], ""
    parser = FeedHtmlSanitizer(base_url)
    parser.feed(value)
    html_value, images, plain_text = parser.close_output()
    return html_value, images, plain_text


def normalize_abstract_candidate(
    value: str | None, *, base_url: str
) -> tuple[str | None, str | None, list[AbstractImage]]:
    if not value:
        return None, None, []
    html_value, images, plain_text = sanitize_abstract_html(value, base_url=base_url)
    if not plain_text:
        return None, None, []
    return plain_text, html_value, images


def choose_best_abstract(
    candidates: list[str | None],
    *,
    base_url: str,
) -> tuple[str | None, str | None, list[AbstractImage]]:
    raw_candidates: list[tuple[str | None, str | None, list[AbstractImage]]] = [
        normalize_abstract_candidate(candidate, base_url=base_url) for candidate in candidates
    ]
    normalized = [normalize_extracted_abstract(*item) for item in raw_candidates]
    valid = [item for item in normalized if item[0]]
    if valid:
        return max(
            valid,
            key=lambda item: (len(item[0] or ""), len(item[2])),
            default=(None, None, []),
        )
    return None, None, []


def strip_abstract_heading(text: str) -> str:
    match = ABSTRACT_HEADING_RE.search(text)
    if not match:
        return text
    tail = text[match.end() :].strip()
    return tail or text


def strip_known_prefixes(text: str) -> str:
    stripped = NATURE_PREFIX_RE.sub("", text).strip()
    return stripped or text


def normalize_extracted_abstract(
    abstract: str | None,
    abstract_html: str | None,
    abstract_images: list[AbstractImage],
) -> tuple[str | None, str | None, list[AbstractImage]]:
    plain_text = normalize_text(abstract)
    html_text = abstract_html.strip() if abstract_html else None
    if plain_text:
        trimmed = strip_abstract_heading(strip_known_prefixes(plain_text))
        if trimmed:
            plain_text = trimmed
    if html_text:
        if "<h2>ABSTRACT</h2>" in html_text.upper():
            parts = re.split(r"<h2>\s*ABSTRACT\s*</h2>", html_text, maxsplit=1, flags=re.IGNORECASE)
            if len(parts) == 2 and parts[1].strip():
                html_text = parts[1].strip()
        if re.match(r"^<p>[^<]*,\s*Published online:.*?</p>", html_text, flags=re.IGNORECASE):
            html_text = re.sub(
                r"^<p>[^<]*,\s*Published online:.*?</p>",
                "",
                html_text,
                count=1,
                flags=re.IGNORECASE,
            ).strip() or None
    if plain_text and looks_like_metadata(plain_text):
        return None, None, []
    return plain_text or None, html_text, abstract_images


def cite_journal_from_html(value: str | None) -> str | None:
    if not value:
        return None
    match = CITE_RE.search(value)
    if not match:
        return None
    clean = normalize_text(html.unescape(match.group(1)))
    return clean or None


def normalize_feed_journal_title(value: str | None) -> str | None:
    if not value:
        return None
    clean = normalize_text(value)
    if not clean:
        return None
    replacements = [
        ("Wiley: ", ""),
        ("AAAS: ", ""),
        (": Table of Contents", ""),
        (": Latest Articles (ACS Publications)", ""),
        (" (ACS Publications)", ""),
    ]
    for old, new in replacements:
        clean = clean.replace(old, new)
    parts = [part.strip() for part in clean.split(":") if part.strip()]
    if len(parts) >= 2 and parts[0] == "Science":
        return "Science"
    return clean.strip() or None


def rss_item_journal(item: ElementTree.Element, feed_title: str | None) -> str | None:
    journal = child_text(item, f"{PRISM}publicationName")
    if journal:
        return normalize_text(journal)
    source = child_text(item, f"{DC}source")
    if source:
        return normalize_text(source.split(", Published online:", 1)[0])
    for candidate in (
        element_inner_xml(first_child(item, "description")),
        child_text(item, "description"),
    ):
        cite_value = cite_journal_from_html(candidate)
        if cite_value:
            return cite_value
    return normalize_feed_journal_title(feed_title)


def atom_entry_journal(entry: ElementTree.Element, feed_title: str | None) -> str | None:
    journal = child_text(entry, f"{PRISM}publicationName")
    if journal:
        return normalize_text(journal)
    source = child_text(entry, f"{DC}source")
    if source:
        return normalize_text(source.split(", Published online:", 1)[0])
    return normalize_feed_journal_title(feed_title)


def extract_rss_entry_abstract(
    item: ElementTree.Element, *, base_url: str
) -> tuple[str | None, str | None, list[AbstractImage]]:
    abstract, abstract_html, abstract_images = choose_best_abstract(
        [
            element_inner_xml(first_child(item, f"{CONTENT}encoded")),
            element_inner_xml(first_child(item, "description")),
            child_text(item, f"{DC}description"),
            child_text(item, f"{CONTENT}encoded"),
            child_text(item, "description"),
        ],
        base_url=base_url,
    )
    return normalize_extracted_abstract(abstract, abstract_html, abstract_images)


def extract_atom_entry_abstract(
    entry: ElementTree.Element, *, base_url: str
) -> tuple[str | None, str | None, list[AbstractImage]]:
    abstract, abstract_html, abstract_images = choose_best_abstract(
        [
            element_inner_xml(first_child(entry, f"{ATOM}content")),
            element_inner_xml(first_child(entry, f"{ATOM}summary")),
            child_text(entry, f"{DC}description"),
            child_text(entry, f"{ATOM}content"),
            child_text(entry, f"{ATOM}summary"),
        ],
        base_url=base_url,
    )
    return normalize_extracted_abstract(abstract, abstract_html, abstract_images)


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
        abstract, abstract_html, abstract_images = extract_rss_entry_abstract(
            item,
            base_url=link,
        )
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
                journal=rss_item_journal(item, feed_title),
                authors=authors,
                abstract=abstract,
                abstract_html=abstract_html,
                abstract_images=abstract_images,
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
        abstract, abstract_html, abstract_images = extract_atom_entry_abstract(
            entry,
            base_url=link,
        )
        doi = entry_doi(child_text(entry, f"{ATOM}id"), link)
        papers.append(
            Paper(
                source_url=source_url,
                feed_title=feed_title,
                title=title,
                url=link,
                doi=doi,
                journal=atom_entry_journal(entry, feed_title),
                authors=authors,
                abstract=abstract,
                abstract_html=abstract_html,
                abstract_images=abstract_images,
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
