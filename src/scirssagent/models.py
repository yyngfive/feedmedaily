from __future__ import annotations

from datetime import UTC, date, datetime
from enum import StrEnum
from typing import Any

from pydantic import BaseModel, ConfigDict, Field, HttpUrl, field_validator


class Relevance(StrEnum):
    DIRECT = "direct"
    INDIRECT = "indirect"
    UNRELATED = "unrelated"


class FeedbackState(StrEnum):
    OPEN = "open"
    USED = "used"


class ProfileProposalState(StrEnum):
    PENDING = "pending"
    APPLIED = "applied"
    REJECTED = "rejected"


class ZoteroSaveState(StrEnum):
    PENDING = "pending"
    SAVED = "saved"
    ERROR = "error"


class AbstractSource(StrEnum):
    OPENALEX = "openalex"
    CROSSREF = "crossref"
    RSS = "rss"
    NONE = "none"


def abstract_source_priority(source: AbstractSource) -> int:
    priorities = {
        AbstractSource.NONE: 0,
        AbstractSource.RSS: 1,
        AbstractSource.CROSSREF: 2,
        AbstractSource.OPENALEX: 3,
    }
    return priorities[source]


class FeedSubscription(BaseModel):
    journal: str
    url: HttpUrl

    @field_validator("journal")
    @classmethod
    def required_journal(cls, value: str) -> str:
        clean = value.strip()
        if not clean:
            raise ValueError("journal cannot be blank")
        return clean


class SettingOption(BaseModel):
    value: str
    label: str


class SettingsConfigField(BaseModel):
    key: str
    label: str
    description: str
    section: str
    input_type: str
    secret: bool = False
    configured: bool = False
    source: str
    stored_in_dotenv: bool = False
    storage_label: str | None = None
    value: str | None = None
    default_value: str | None = None
    options: list[SettingOption] = Field(default_factory=list)


class SettingsConfigResponse(BaseModel):
    fields: list[SettingsConfigField] = Field(default_factory=list)


class SettingsConfigFieldUpdate(BaseModel):
    value: str | None = None
    clear: bool = False


class SettingsConfigUpdateRequest(BaseModel):
    fields: dict[str, SettingsConfigFieldUpdate] = Field(default_factory=dict)


class AppMetaResponse(BaseModel):
    name: str
    version: str
    mode: str
    install_dir: str
    data_dir: str
    logs_dir: str
    reports_dir: str
    static_dir: str
    server_url: str | None = None
    scheduler_task_name: str
    update_manifest_url: str | None = None


class AppHealthResponse(BaseModel):
    status: str = "ok"
    name: str
    version: str
    mode: str
    server_url: str | None = None


class AppUpdateResponse(BaseModel):
    status: str
    current_version: str
    latest_version: str | None = None
    has_update: bool = False
    download_url: str | None = None
    release_notes_url: str | None = None
    detail: str | None = None
    checked_at: datetime = Field(default_factory=lambda: datetime.now(UTC))


class SchedulerSettingsResponse(BaseModel):
    installed: bool = False
    task_name: str
    mode: str
    scheduled_time: str | None = None
    state: str | None = None
    next_run_time: str | None = None
    last_run_time: str | None = None
    last_result: int | None = None
    command: str | None = None


class SchedulerSettingsUpdateRequest(BaseModel):
    daily_time: str

    @field_validator("daily_time")
    @classmethod
    def validate_daily_time(cls, value: str) -> str:
        clean = value.strip()
        parts = clean.split(":")
        if len(parts) != 2 or not all(part.isdigit() for part in parts):
            raise ValueError("daily_time must be in HH:MM format")
        hour, minute = (int(parts[0]), int(parts[1]))
        if hour < 0 or hour > 23 or minute < 0 or minute > 59:
            raise ValueError("daily_time must be a valid 24-hour time")
        return f"{hour:02d}:{minute:02d}"


class AbstractImage(BaseModel):
    src: str
    alt: str | None = None

    @field_validator("src")
    @classmethod
    def required_src(cls, value: str) -> str:
        clean = value.strip()
        if not clean:
            raise ValueError("src cannot be blank")
        return clean

    @field_validator("alt")
    @classmethod
    def normalize_alt(cls, value: str | None) -> str | None:
        if value is None:
            return None
        clean = " ".join(value.split()).strip()
        return clean or None


class Paper(BaseModel):
    source_url: str
    feed_title: str | None = None
    title: str
    url: str
    doi: str | None = None
    journal: str | None = None
    authors: list[str] = Field(default_factory=list)
    abstract: str | None = None
    abstract_html: str | None = None
    abstract_images: list[AbstractImage] = Field(default_factory=list)
    abstract_source: AbstractSource = AbstractSource.NONE
    published_date: date | None = None
    first_seen_at: datetime = Field(default_factory=lambda: datetime.now(UTC))
    read_at: datetime | None = None
    raw: dict[str, Any] = Field(default_factory=dict)

    @field_validator("title", "url")
    @classmethod
    def required_text(cls, value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("value cannot be blank")
        return value

    @field_validator("abstract_html")
    @classmethod
    def normalize_abstract_html(cls, value: str | None) -> str | None:
        if value is None:
            return None
        clean = value.strip()
        return clean or None

    def model_post_init(self, __context: Any) -> None:
        if (
            self.abstract_source == AbstractSource.NONE
            and (self.abstract or self.abstract_html or self.abstract_images)
        ):
            self.abstract_source = AbstractSource.RSS


class Classification(BaseModel):
    relevance: Relevance
    confidence: float = Field(ge=0, le=1)
    topic_tags: list[str] = Field(default_factory=list)
    reason: str
    recommended_action: str = "scan"
    model: str
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


class FeedbackStatus(BaseModel):
    has_feedback: bool = False
    corrected_relevance: Relevance | None = None
    note: str | None = None
    latest_feedback_at: datetime | None = None
    state: FeedbackState | None = None
    used_in_profile: bool = False


class ZoteroStatus(BaseModel):
    state: ZoteroSaveState | None = None
    saved: bool = False
    item_key: str | None = None
    last_error: str | None = None
    attempted_at: datetime | None = None
    saved_at: datetime | None = None


class ZoteroCollectionOption(BaseModel):
    key: str
    name: str
    path_label: str
    parent_key: str | None = None
    is_default: bool = False


class ZoteroCollectionsResponse(BaseModel):
    collections: list[ZoteroCollectionOption] = Field(default_factory=list)
    default_collection_key: str | None = None


class ReportPaper(Paper):
    id: int
    classification: Classification
    seen_date: date
    feedback_status: FeedbackStatus | None = None
    zotero_status: ZoteroStatus | None = None


class Report(BaseModel):
    generated_at: datetime
    report_date: date
    totals: dict[str, int]
    papers: list[ReportPaper]
    errors: list[str] = Field(default_factory=list)


class FeedbackRecord(BaseModel):
    id: int
    paper_id: int
    paper_title: str
    original_relevance: Relevance
    corrected_relevance: Relevance
    note: str | None = None
    state: FeedbackState = FeedbackState.OPEN
    used_in_profile: bool = False
    created_at: datetime


class ProfileMeta(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    version: int = 1
    created_at: datetime
    updated_at: datetime
    source_description: str


class TopicDefinition(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    label: str

    @field_validator("id")
    @classmethod
    def normalize_id(cls, value: str) -> str:
        clean = value.strip().lower().replace(" ", "_").replace("-", "_")
        if not clean:
            raise ValueError("topic id cannot be blank")
        return clean

    @field_validator("label")
    @classmethod
    def normalize_label(cls, value: str) -> str:
        clean = " ".join(value.split()).strip()
        if not clean:
            raise ValueError("topic label cannot be blank")
        return clean


class ProfileFewShot(BaseModel):
    model_config = ConfigDict(extra="forbid")

    title: str
    relevance: Relevance
    tags: list[str] = Field(default_factory=list)
    rationale: str

    @field_validator("tags")
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


class RelevanceRules(BaseModel):
    model_config = ConfigDict(extra="forbid")

    direct: list[str] = Field(default_factory=list)
    indirect: list[str] = Field(default_factory=list)
    unrelated: list[str] = Field(default_factory=list)


class ProfileProposalDelta(BaseModel):
    model_config = ConfigDict(extra="forbid")

    summary: str
    direct_rule_additions: list[str] = Field(default_factory=list)
    indirect_rule_additions: list[str] = Field(default_factory=list)
    unrelated_rule_additions: list[str] = Field(default_factory=list)
    scope_rewrite: str | None = None
    tag_additions: list[TopicDefinition] = Field(default_factory=list)
    tag_removals: list[str] = Field(default_factory=list)

    @field_validator("summary")
    @classmethod
    def normalize_summary(cls, value: str) -> str:
        clean = " ".join(value.split()).strip()
        if not clean:
            raise ValueError("summary cannot be blank")
        return clean

    @field_validator(
        "direct_rule_additions",
        "indirect_rule_additions",
        "unrelated_rule_additions",
        mode="before",
    )
    @classmethod
    def normalize_rule_lists(cls, value: list[str] | None) -> list[str]:
        if not value:
            return []
        normalized: list[str] = []
        seen: set[str] = set()
        for item in value:
            clean = " ".join(str(item).split()).strip()
            if clean and clean not in seen:
                seen.add(clean)
                normalized.append(clean)
        return normalized

    @field_validator("scope_rewrite")
    @classmethod
    def normalize_scope_rewrite(cls, value: str | None) -> str | None:
        if value is None:
            return None
        clean = " ".join(value.split()).strip()
        return clean or None

    @field_validator("tag_removals", mode="before")
    @classmethod
    def normalize_tag_removals(cls, value: list[str] | None) -> list[str]:
        if not value:
            return []
        normalized: list[str] = []
        seen: set[str] = set()
        for item in value:
            clean = str(item).strip().lower().replace(" ", "_").replace("-", "_")
            if clean and clean not in seen:
                seen.add(clean)
                normalized.append(clean)
        return normalized


class ClassificationProfile(BaseModel):
    model_config = ConfigDict(extra="forbid")

    meta: ProfileMeta
    scope: str
    relevance_rules: RelevanceRules
    topic_taxonomy: list[TopicDefinition] = Field(default_factory=list)
    few_shots: list[ProfileFewShot] = Field(default_factory=list)

    @field_validator("scope")
    @classmethod
    def non_empty_scope(cls, value: str) -> str:
        clean = value.strip()
        if not clean:
            raise ValueError("scope cannot be blank")
        return clean

    @field_validator("few_shots")
    @classmethod
    def capped_few_shots(cls, value: list[ProfileFewShot]) -> list[ProfileFewShot]:
        if len(value) > 2:
            raise ValueError("few_shots cannot contain more than 2 items")
        return value


class CurrentProfileResponse(BaseModel):
    profile: ClassificationProfile | None = None


class ProfileProposal(BaseModel):
    id: int
    summary: str
    proposed_profile: ClassificationProfile
    rule_delta: ProfileProposalDelta
    source_feedback_ids: list[int] = Field(default_factory=list)
    model: str
    state: ProfileProposalState = ProfileProposalState.PENDING
    created_at: datetime
    applied_at: datetime | None = None
    rejected_at: datetime | None = None
    applied_version: int | None = None


class FeedbackProposalContext(BaseModel):
    feedback_id: int
    paper_id: int
    paper_title: str
    journal: str | None = None
    abstract: str | None = None
    original_relevance: Relevance
    corrected_relevance: Relevance
    note: str | None = None


class JobInfo(BaseModel):
    id: str
    job_type: str
    status: str
    message_key: str | None = None
    message: str | None = None
    error: str | None = None
    result: dict[str, Any] | None = None
    created_at: datetime = Field(default_factory=lambda: datetime.now(UTC))
    started_at: datetime | None = None
    finished_at: datetime | None = None
