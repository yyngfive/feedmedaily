from __future__ import annotations

from datetime import UTC, date, datetime
from enum import StrEnum
from typing import Any

from pydantic import BaseModel, Field, HttpUrl, field_validator


class Relevance(StrEnum):
    DIRECT = "direct"
    INDIRECT = "indirect"
    UNRELATED = "unrelated"


class FeedSource(BaseModel):
    url: HttpUrl


class Paper(BaseModel):
    source_url: str
    feed_title: str | None = None
    title: str
    url: str
    doi: str | None = None
    journal: str | None = None
    authors: list[str] = Field(default_factory=list)
    abstract: str | None = None
    published_date: date | None = None
    first_seen_at: datetime = Field(default_factory=lambda: datetime.now(UTC))
    raw: dict[str, Any] = Field(default_factory=dict)

    @field_validator("title", "url")
    @classmethod
    def required_text(cls, value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("value cannot be blank")
        return value


class Classification(BaseModel):
    relevance: Relevance
    confidence: float = Field(ge=0, le=1)
    topic_tags: list[str] = Field(default_factory=list)
    reason: str
    recommended_action: str = "scan"
    model: str = "heuristic"
    translated_title_zh: str | None = None

    @field_validator("topic_tags")
    @classmethod
    def normalize_tags(cls, value: list[str]) -> list[str]:
        seen: set[str] = set()
        normalized: list[str] = []
        for tag in value:
            clean = tag.strip().lower().replace(" ", "_").replace("-", "_")
            if clean and clean not in seen:
                seen.add(clean)
                normalized.append(clean)
        return normalized

    @field_validator("translated_title_zh")
    @classmethod
    def normalize_translated_title(cls, value: str | None) -> str | None:
        if value is None:
            return None
        clean = value.strip()
        return clean or None


class ReportPaper(Paper):
    id: int
    classification: Classification
    seen_date: date


class Report(BaseModel):
    generated_at: datetime
    report_date: date
    totals: dict[str, int]
    papers: list[ReportPaper]
    errors: list[str] = Field(default_factory=list)
