from __future__ import annotations

import csv
import json
import random
import sqlite3
import time
from collections import defaultdict
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from datetime import UTC, date, datetime
from pathlib import Path
from typing import Any

from openai import OpenAI
from pydantic import ValidationError

from scirssagent.classifier import (
    LlmConfig,
    batch_classification_prompt,
    title_translation_prompt,
)
from scirssagent.models import Classification, ClassificationProfile, Paper, Relevance
from scirssagent.storage import paper_from_row

GOLD_COLUMNS = [
    "paper_id",
    "title",
    "journal",
    "published_date",
    "authors",
    "doi",
    "url",
    "abstract",
    "current_relevance",
    "gold_relevance",
]
GOLD_CSV_ENCODINGS = ("utf-8-sig", "utf-8", "gb18030", "cp1252")


@dataclass(frozen=True)
class GoldSampleRow:
    paper_id: str
    paper: Paper
    current_relevance: str | None
    gold_relevance: Relevance | None


@dataclass(frozen=True)
class TokenUsage:
    prompt_tokens: int | None = 0
    completion_tokens: int | None = 0
    total_tokens: int | None = 0

    def add(self, other: TokenUsage) -> TokenUsage:
        return TokenUsage(
            prompt_tokens=_add_token_value(self.prompt_tokens, other.prompt_tokens),
            completion_tokens=_add_token_value(self.completion_tokens, other.completion_tokens),
            total_tokens=_add_token_value(self.total_tokens, other.total_tokens),
        )


@dataclass(frozen=True)
class MeasuredBatchResponse:
    classifications: dict[str, Classification]
    usage: TokenUsage
    request_count: int
    latency_seconds: float
    invalid_json_count: int = 0
    missing_item_count: int = 0
    error: str | None = None


BatchRunner = Callable[
    [list[tuple[str, Paper]], ClassificationProfile, LlmConfig],
    MeasuredBatchResponse,
]
ProgressReporter = Callable[[str], None]


def parse_batch_sizes(value: str) -> list[int]:
    sizes: list[int] = []
    for raw_part in value.split(","):
        part = raw_part.strip()
        if not part:
            continue
        size = int(part)
        if size < 1:
            raise ValueError("Batch sizes must be positive integers.")
        if size not in sizes:
            sizes.append(size)
    if not sizes:
        raise ValueError("At least one batch size is required.")
    return sizes


def default_gold_sample_path(root: Path) -> Path:
    return root / "reports" / "experiments" / "batch-size-gold-sample.csv"


def default_experiment_output_dir(root: Path) -> Path:
    return root / "reports" / "experiments"


def select_gold_sample_rows(
    conn: sqlite3.Connection,
    sample_size: int,
    seed: int,
) -> list[GoldSampleRow]:
    rows = conn.execute(
        """
        SELECT p.*, c.relevance AS current_relevance
        FROM papers p
        LEFT JOIN classifications c
          ON c.id = (
            SELECT c2.id
            FROM classifications c2
            WHERE c2.paper_id = p.id
            ORDER BY c2.classified_at DESC, c2.id DESC
            LIMIT 1
          )
        ORDER BY p.first_seen_at DESC, p.id DESC
        """
    ).fetchall()
    candidates: list[GoldSampleRow] = []
    for row in rows:
        paper = paper_from_row(row)
        candidates.append(
            GoldSampleRow(
                paper_id=str(row["id"]),
                paper=paper,
                current_relevance=row["current_relevance"],
                gold_relevance=None,
            )
        )

    rng = random.Random(seed)
    buckets: dict[tuple[str, str], list[GoldSampleRow]] = defaultdict(list)
    for candidate in candidates:
        relevance = candidate.current_relevance or "unclassified"
        journal = candidate.paper.journal or candidate.paper.feed_title or "unknown"
        buckets[(relevance, journal)].append(candidate)
    for bucket in buckets.values():
        rng.shuffle(bucket)

    relevance_order = {"direct": 0, "indirect": 1, "unrelated": 2, "unclassified": 3}
    keys = sorted(buckets, key=lambda key: (relevance_order.get(key[0], 99), key[1].lower()))
    selected: list[GoldSampleRow] = []
    while keys and len(selected) < sample_size:
        next_keys: list[tuple[str, str]] = []
        for key in keys:
            bucket = buckets[key]
            if bucket and len(selected) < sample_size:
                selected.append(bucket.pop())
            if bucket:
                next_keys.append(key)
        keys = next_keys
    return selected


def write_gold_template(rows: Sequence[GoldSampleRow], path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="", encoding="utf-8-sig") as file:
        writer = csv.DictWriter(file, fieldnames=GOLD_COLUMNS)
        writer.writeheader()
        for row in rows:
            writer.writerow(_gold_row_to_csv(row))


def load_gold_rows(path: Path) -> list[GoldSampleRow]:
    last_decode_error: UnicodeDecodeError | None = None
    for encoding in GOLD_CSV_ENCODINGS:
        try:
            with path.open("r", newline="", encoding=encoding) as file:
                reader = csv.DictReader(file)
                missing_columns = [
                    column for column in GOLD_COLUMNS if column not in (reader.fieldnames or [])
                ]
                if missing_columns:
                    joined = ", ".join(missing_columns)
                    raise ValueError(f"Gold label file is missing required columns: {joined}")
                return [_gold_row_from_csv(row) for row in reader]
        except UnicodeDecodeError as exc:
            last_decode_error = exc
    supported = ", ".join(GOLD_CSV_ENCODINGS)
    message = f"Could not decode gold label file with supported encodings: {supported}"
    raise ValueError(message) from last_decode_error


def missing_gold_label_ids(rows: Sequence[GoldSampleRow]) -> list[str]:
    return [row.paper_id for row in rows if row.gold_relevance is None]


def run_batch_size_experiment(
    rows: Sequence[GoldSampleRow],
    batch_sizes: Sequence[int],
    config: LlmConfig,
    profile: ClassificationProfile,
    batch_runner: BatchRunner | None = None,
    generated_at: datetime | None = None,
    progress: ProgressReporter | None = None,
) -> dict[str, Any]:
    missing_ids = missing_gold_label_ids(rows)
    if missing_ids:
        preview = ", ".join(missing_ids[:10])
        suffix = "..." if len(missing_ids) > 10 else ""
        raise ValueError(f"Gold labels are missing for paper_id: {preview}{suffix}")
    if not rows:
        raise ValueError("Gold label file does not contain any papers.")

    runner = batch_runner or measured_classify_and_translate_batch
    generated = generated_at or datetime.now(UTC)
    if progress:
        progress(
            f"Running experiment for {len(rows)} labeled papers with batch sizes: "
            f"{', '.join(str(size) for size in batch_sizes)}"
        )
    results = [
        run_experiment_for_batch_size(
            rows,
            batch_size,
            config,
            profile,
            runner,
            progress=progress,
        )
        for batch_size in batch_sizes
    ]
    selected_batch_size = recommend_batch_size(results)
    if progress:
        progress(f"Recommendation complete. Selected batch size: {selected_batch_size}")
    return {
        "generated_at": generated.isoformat(),
        "model": config.model,
        "token_scope": "classification+translation",
        "sample_size": len(rows),
        "batch_sizes": list(batch_sizes),
        "baseline_batch_size": 1 if any(result["batch_size"] == 1 for result in results) else None,
        "selected_batch_size": selected_batch_size,
        "results": results,
    }


def run_experiment_for_batch_size(
    rows: Sequence[GoldSampleRow],
    batch_size: int,
    config: LlmConfig,
    profile: ClassificationProfile,
    batch_runner: BatchRunner,
    progress: ProgressReporter | None = None,
) -> dict[str, Any]:
    totals = TokenUsage()
    request_count = 0
    latency_seconds = 0.0
    invalid_json_count = 0
    missing_item_count = 0
    predictions: dict[str, Classification] = {}
    errors: list[str] = []

    indexed_rows = [(row.paper_id, row.paper) for row in rows]
    total_batches = (len(indexed_rows) + batch_size - 1) // batch_size
    if progress:
        progress(
            f"Batch size {batch_size}: starting {total_batches} request(s) "
            f"for {len(indexed_rows)} papers"
        )
    for start in range(0, len(indexed_rows), batch_size):
        batch = indexed_rows[start : start + batch_size]
        batch_number = start // batch_size + 1
        if progress:
            progress(
                f"Batch size {batch_size}: request {batch_number}/{total_batches} "
                f"({len(batch)} paper(s))"
            )
        measured = batch_runner(batch, profile, config)
        totals = totals.add(measured.usage)
        request_count += measured.request_count
        latency_seconds += measured.latency_seconds
        invalid_json_count += measured.invalid_json_count
        missing_item_count += measured.missing_item_count
        predictions.update(measured.classifications)
        if measured.error:
            errors.append(measured.error)

    metrics = compute_accuracy_metrics(rows, predictions)
    total_tokens = totals.total_tokens
    token_per_paper = round(total_tokens / len(rows), 3) if total_tokens is not None else None
    if progress:
        progress(
            f"Batch size {batch_size}: done "
            f"(accuracy={metrics['overall_accuracy']}, total_tokens={total_tokens})"
        )
    return {
        "batch_size": batch_size,
        "total_papers": len(rows),
        "request_count": request_count,
        "prompt_tokens": totals.prompt_tokens,
        "completion_tokens": totals.completion_tokens,
        "total_tokens": total_tokens,
        "token_per_paper": token_per_paper,
        "latency_seconds": round(latency_seconds, 3),
        "invalid_json_count": invalid_json_count,
        "missing_item_count": missing_item_count,
        "missing_response_rate": round(missing_item_count / len(rows), 4),
        "errors": errors,
        **metrics,
    }


def measured_classify_and_translate_batch(
    indexed_papers: list[tuple[str, Paper]],
    profile: ClassificationProfile,
    config: LlmConfig,
) -> MeasuredBatchResponse:
    client = _openai_client(config)
    request_kwargs: dict[str, object] = {
        "model": config.model,
        "messages": [
            {"role": "system", "content": "You are a careful scientific literature classifier."},
            {
                "role": "user",
                "content": batch_classification_prompt(indexed_papers, profile),
            },
        ],
        "temperature": 0,
        "max_tokens": max(600, 220 * len(indexed_papers)),
        "response_format": {"type": "json_object"},
        "extra_body": {"thinking": {"type": config.thinking}},
    }

    usage = TokenUsage()
    request_count = 0
    started = time.perf_counter()
    last_content = ""
    payload: dict[str, Any] | None = None
    invalid_json_count = 0
    error: str | None = None
    for _attempt in range(2):
        response = client.chat.completions.create(**request_kwargs)
        request_count += 1
        usage = usage.add(_usage_from_response(response))
        content = response.choices[0].message.content or ""
        if not content.strip():
            last_content = content
            continue
        try:
            loaded = json.loads(content)
        except json.JSONDecodeError as exc:
            invalid_json_count += 1
            error = f"Classification response was not valid JSON: {exc}"
            break
        if isinstance(loaded, dict):
            payload = loaded
            break
        invalid_json_count += 1
        error = "Classification response JSON root was not an object."
        break

    classifications: dict[str, Classification] = {}
    if payload is None:
        if not error:
            error = f"Batch model returned empty JSON content: {last_content!r}"
        missing_count = len(indexed_papers)
        return MeasuredBatchResponse(
            classifications={},
            usage=usage,
            request_count=request_count,
            latency_seconds=time.perf_counter() - started,
            invalid_json_count=invalid_json_count,
            missing_item_count=missing_count,
            error=error,
        )

    raw_items = payload.get("items")
    if not isinstance(raw_items, list):
        return MeasuredBatchResponse(
            classifications={},
            usage=usage,
            request_count=request_count,
            latency_seconds=time.perf_counter() - started,
            invalid_json_count=invalid_json_count + 1,
            missing_item_count=len(indexed_papers),
            error="Batch JSON response missing items list.",
        )

    for item in raw_items:
        if not isinstance(item, dict) or "id" not in item:
            continue
        item_id = str(item["id"])
        try:
            classifications[item_id] = Classification.model_validate(
                {**item, "model": config.model}
            )
        except ValidationError:
            continue

    missing_translation = [
        (item_id, paper)
        for item_id, paper in indexed_papers
        if item_id in classifications and not classifications[item_id].translated_title_zh
    ]
    if missing_translation:
        translation = measured_translate_titles_batch(missing_translation, config, client=client)
        usage = usage.add(translation.usage)
        request_count += translation.request_count
        invalid_json_count += translation.invalid_json_count
        if translation.error:
            error = translation.error if error is None else f"{error}; {translation.error}"
        for item_id, translated_title in translation.translations.items():
            classifications[item_id] = classifications[item_id].model_copy(
                update={"translated_title_zh": translated_title}
            )

    expected_ids = {item_id for item_id, _paper in indexed_papers}
    missing_count = len(expected_ids - set(classifications))
    return MeasuredBatchResponse(
        classifications=classifications,
        usage=usage,
        request_count=request_count,
        latency_seconds=time.perf_counter() - started,
        invalid_json_count=invalid_json_count,
        missing_item_count=missing_count,
        error=error,
    )


@dataclass(frozen=True)
class MeasuredTranslationResponse:
    translations: dict[str, str]
    usage: TokenUsage
    request_count: int
    invalid_json_count: int = 0
    error: str | None = None


def measured_translate_titles_batch(
    indexed_papers: list[tuple[str, Paper]],
    config: LlmConfig,
    client: OpenAI | None = None,
) -> MeasuredTranslationResponse:
    llm = client or _openai_client(config)
    request_kwargs: dict[str, object] = {
        "model": config.model,
        "messages": [
            {
                "role": "system",
                "content": "You translate scientific paper titles into concise Simplified Chinese.",
            },
            {"role": "user", "content": title_translation_prompt(indexed_papers)},
        ],
        "temperature": 0,
        "max_tokens": max(300, 120 * len(indexed_papers)),
        "response_format": {"type": "json_object"},
    }
    request_kwargs["extra_body"] = {"thinking": {"type": "disabled"}}

    response = llm.chat.completions.create(**request_kwargs)
    usage = _usage_from_response(response)
    content = response.choices[0].message.content or ""
    try:
        payload = json.loads(content)
    except json.JSONDecodeError as exc:
        return MeasuredTranslationResponse(
            translations={},
            usage=usage,
            request_count=1,
            invalid_json_count=1,
            error=f"Translation response was not valid JSON: {exc}",
        )
    raw_items = payload.get("items") if isinstance(payload, dict) else None
    if not isinstance(raw_items, list):
        return MeasuredTranslationResponse(
            translations={},
            usage=usage,
            request_count=1,
            invalid_json_count=1,
            error="Translation JSON response missing items list.",
        )

    translations: dict[str, str] = {}
    for item in raw_items:
        if not isinstance(item, dict) or "id" not in item:
            continue
        translated_title = str(item.get("translated_title_zh", "")).strip()
        if translated_title:
            translations[str(item["id"])] = translated_title
    return MeasuredTranslationResponse(translations=translations, usage=usage, request_count=1)


def compute_accuracy_metrics(
    rows: Sequence[GoldSampleRow],
    predictions: dict[str, Classification],
) -> dict[str, Any]:
    correct = 0
    true_direct = 0
    predicted_direct = 0
    direct_true_positive = 0
    false_direct = 0
    false_indirect = 0
    false_unrelated = 0
    item_results: list[dict[str, Any]] = []

    for row in rows:
        gold = row.gold_relevance
        prediction = predictions.get(row.paper_id)
        predicted = prediction.relevance if prediction else None
        is_correct = gold is not None and predicted == gold
        if is_correct:
            correct += 1
        if gold == Relevance.DIRECT:
            true_direct += 1
        if predicted == Relevance.DIRECT:
            predicted_direct += 1
            if gold == Relevance.DIRECT:
                direct_true_positive += 1
            else:
                false_direct += 1
        if predicted == Relevance.INDIRECT and gold != Relevance.INDIRECT:
            false_indirect += 1
        if predicted == Relevance.UNRELATED and gold != Relevance.UNRELATED:
            false_unrelated += 1
        item_results.append(
            {
                "paper_id": row.paper_id,
                "title": row.paper.title,
                "gold_relevance": gold.value if gold else None,
                "predicted_relevance": predicted.value if predicted else None,
                "correct": is_correct,
                "reason": prediction.reason if prediction else None,
                "translated_title_zh": prediction.translated_title_zh if prediction else None,
            }
        )

    direct_precision = _safe_divide(direct_true_positive, predicted_direct)
    direct_recall = _safe_divide(direct_true_positive, true_direct)
    direct_f1 = None
    if (
        direct_precision is not None
        and direct_recall is not None
        and direct_precision + direct_recall
    ):
        direct_f1 = round(
            2 * direct_precision * direct_recall / (direct_precision + direct_recall),
            4,
        )

    return {
        "overall_accuracy": round(correct / len(rows), 4),
        "direct_precision": direct_precision,
        "direct_recall": direct_recall,
        "direct_f1": direct_f1,
        "false_direct_count": false_direct,
        "false_indirect_count": false_indirect,
        "false_unrelated_count": false_unrelated,
        "predictions": item_results,
    }


def recommend_batch_size(results: Sequence[dict[str, Any]]) -> int | None:
    baseline = next((result for result in results if result["batch_size"] == 1), None)
    if baseline is None:
        return None
    baseline_precision = baseline["direct_precision"] or 0
    baseline_false_direct = baseline["false_direct_count"]
    eligible = [
        result
        for result in results
        if result["invalid_json_count"] == 0
        and result["missing_item_count"] == 0
        and result["token_per_paper"] is not None
        and (result["direct_precision"] or 0) >= baseline_precision
        and result["false_direct_count"] <= baseline_false_direct
    ]
    if not eligible:
        return None
    return min(eligible, key=lambda result: (result["token_per_paper"], result["batch_size"]))[
        "batch_size"
    ]


def write_experiment_outputs(report: dict[str, Any], output_dir: Path) -> tuple[Path, Path]:
    output_dir.mkdir(parents=True, exist_ok=True)
    stamp = datetime.now(UTC).strftime("%Y%m%d-%H%M%S")
    json_path = output_dir / f"batch-size-results-{stamp}.json"
    csv_path = output_dir / f"batch-size-results-{stamp}.csv"
    with json_path.open("w", encoding="utf-8") as file:
        json.dump(report, file, ensure_ascii=False, indent=2)
        file.write("\n")
    with csv_path.open("w", newline="", encoding="utf-8") as file:
        fieldnames = [
            "batch_size",
            "total_papers",
            "request_count",
            "prompt_tokens",
            "completion_tokens",
            "total_tokens",
            "token_per_paper",
            "latency_seconds",
            "invalid_json_count",
            "missing_item_count",
            "missing_response_rate",
            "overall_accuracy",
            "direct_precision",
            "direct_recall",
            "direct_f1",
            "false_direct_count",
            "false_indirect_count",
            "false_unrelated_count",
        ]
        writer = csv.DictWriter(file, fieldnames=fieldnames)
        writer.writeheader()
        for result in report["results"]:
            writer.writerow({field: result.get(field) for field in fieldnames})
    return json_path, csv_path


def _gold_row_to_csv(row: GoldSampleRow) -> dict[str, str]:
    paper = row.paper
    return {
        "paper_id": row.paper_id,
        "title": paper.title,
        "journal": paper.journal or "",
        "published_date": paper.published_date.isoformat() if paper.published_date else "",
        "authors": "; ".join(paper.authors),
        "doi": paper.doi or "",
        "url": paper.url,
        "abstract": paper.abstract or "",
        "current_relevance": row.current_relevance or "",
        "gold_relevance": row.gold_relevance.value if row.gold_relevance else "",
    }


def _gold_row_from_csv(row: dict[str, str]) -> GoldSampleRow:
    gold_value = row["gold_relevance"].strip().lower()
    current_value = row["current_relevance"].strip().lower() or None
    gold = Relevance(gold_value) if gold_value else None
    authors = [author.strip() for author in row["authors"].split(";") if author.strip()]
    published_date = row["published_date"].strip() or None
    paper = Paper(
        source_url="gold-label-sample",
        feed_title=None,
        title=row["title"],
        url=row["url"],
        doi=row["doi"].strip() or None,
        journal=row["journal"].strip() or None,
        authors=authors,
        abstract=row["abstract"].strip() or None,
        published_date=_parse_optional_date(published_date),
        raw={},
    )
    return GoldSampleRow(
        paper_id=row["paper_id"],
        paper=paper,
        current_relevance=current_value,
        gold_relevance=gold,
    )


def _openai_client(config: LlmConfig) -> OpenAI:
    client_kwargs: dict[str, str] = {"api_key": config.api_key or ""}
    if config.base_url:
        client_kwargs["base_url"] = config.base_url
    return OpenAI(**client_kwargs)


def _usage_from_response(response: Any) -> TokenUsage:
    usage = getattr(response, "usage", None)
    if usage is None:
        return TokenUsage(prompt_tokens=None, completion_tokens=None, total_tokens=None)
    if isinstance(usage, dict):
        return TokenUsage(
            prompt_tokens=usage.get("prompt_tokens"),
            completion_tokens=usage.get("completion_tokens"),
            total_tokens=usage.get("total_tokens"),
        )
    return TokenUsage(
        prompt_tokens=getattr(usage, "prompt_tokens", None),
        completion_tokens=getattr(usage, "completion_tokens", None),
        total_tokens=getattr(usage, "total_tokens", None),
    )


def _add_token_value(left: int | None, right: int | None) -> int | None:
    if left is None or right is None:
        return None
    return left + right


def _safe_divide(numerator: int, denominator: int) -> float | None:
    if denominator == 0:
        return None
    return round(numerator / denominator, 4)


def _parse_optional_date(value: str | None) -> date | None:
    if not value:
        return None
    clean = value.strip()
    if not clean:
        return None

    for parser in (
        date.fromisoformat,
        lambda text: datetime.strptime(text, "%Y/%m/%d").date(),
        lambda text: datetime.strptime(text, "%Y/%m/%d %H:%M:%S").date(),
        lambda text: datetime.strptime(text, "%m/%d/%Y").date(),
        lambda text: datetime.strptime(text, "%m/%d/%Y %H:%M:%S").date(),
    ):
        try:
            return parser(clean)
        except ValueError:
            continue

    raise ValueError(f"Unsupported published_date format in gold label CSV: {clean!r}")
