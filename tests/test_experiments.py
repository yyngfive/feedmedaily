from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path
from uuid import uuid4

import pytest

from scirssagent.classifier import LlmConfig
from scirssagent.experiments import (
    GoldSampleRow,
    MeasuredBatchResponse,
    TokenUsage,
    compute_accuracy_metrics,
    load_gold_rows,
    missing_gold_label_ids,
    parse_batch_sizes,
    run_batch_size_experiment,
    write_experiment_outputs,
    write_gold_template,
)
from scirssagent.models import (
    Classification,
    ClassificationProfile,
    Paper,
    ProfileMeta,
    Relevance,
    RelevanceRules,
    TopicDefinition,
)


def test_parse_batch_sizes_deduplicates_and_validates() -> None:
    assert parse_batch_sizes("1, 5,10,5") == [1, 5, 10]

    with pytest.raises(ValueError, match="positive"):
        parse_batch_sizes("1,0")


def test_gold_template_round_trip_and_missing_labels() -> None:
    path = _ignored_test_dir("gold") / "gold.csv"
    rows = [
        GoldSampleRow(
            paper_id="1",
            paper=Paper(
                source_url="https://example.com/rss",
                title="A paper",
                url="https://example.com/a",
                journal="Example Journal",
                abstract="An abstract.",
            ),
            current_relevance="direct",
            gold_relevance=None,
        )
    ]

    write_gold_template(rows, path)
    loaded = load_gold_rows(path)

    assert loaded[0].paper.title == "A paper"
    assert missing_gold_label_ids(loaded) == ["1"]


def test_gold_rows_can_be_loaded_after_excel_style_ansi_save() -> None:
    path = _ignored_test_dir("gold-gb18030") / "gold.csv"
    text = (
        "paper_id,title,journal,published_date,authors,doi,url,abstract,"
        "current_relevance,gold_relevance\n"
        "1,中文标题,期刊,,,,https://example.com/a,摘要,direct,direct\n"
    )
    path.write_text(text, encoding="gb18030")

    loaded = load_gold_rows(path)

    assert loaded[0].paper.title == "中文标题"
    assert loaded[0].gold_relevance == Relevance.DIRECT


def test_gold_rows_accept_spreadsheet_slash_dates() -> None:
    path = _ignored_test_dir("gold-slash-date") / "gold.csv"
    text = (
        "paper_id,title,journal,published_date,authors,doi,url,abstract,"
        "current_relevance,gold_relevance\n"
        "1,Slash Date Paper,Journal,2026/4/28,,,https://example.com/a,Abstract,direct,direct\n"
    )
    path.write_text(text, encoding="utf-8-sig")

    loaded = load_gold_rows(path)

    assert loaded[0].paper.published_date is not None
    assert loaded[0].paper.published_date.isoformat() == "2026-04-28"


def test_experiment_batching_metrics_and_recommendation() -> None:
    rows = _gold_rows(80)
    config = LlmConfig(api_key="key", model="fake-model")

    report = run_batch_size_experiment(
        rows,
        batch_sizes=[1, 5, 10, 20, 30],
        config=config,
        profile=_profile(),
        batch_runner=_fake_batch_runner,
        generated_at=datetime(2026, 4, 30, tzinfo=UTC),
    )

    by_size = {result["batch_size"]: result for result in report["results"]}
    assert by_size[10]["request_count"] == 8
    assert by_size[30]["request_count"] == 3
    assert by_size[20]["total_tokens"] == 80
    assert by_size[20]["token_per_paper"] == 1
    assert by_size[20]["overall_accuracy"] == 1
    assert report["selected_batch_size"] == 30


def test_experiment_emits_progress_messages() -> None:
    rows = _gold_rows(4)
    progress_messages: list[str] = []

    run_batch_size_experiment(
        rows,
        batch_sizes=[1, 2],
        config=LlmConfig(api_key="key", model="fake-model"),
        profile=_profile(),
        batch_runner=_fake_batch_runner,
        progress=progress_messages.append,
    )

    assert any(
        "Running experiment for 4 labeled papers" in message
        for message in progress_messages
    )
    assert any("Batch size 1: starting" in message for message in progress_messages)
    assert any("Batch size 2: done" in message for message in progress_messages)


def test_gold_labels_are_required_before_experiment() -> None:
    rows = [
        GoldSampleRow(
            paper_id="missing",
            paper=Paper(
                source_url="https://example.com/rss",
                title="Unlabeled paper",
                url="https://example.com/unlabeled",
            ),
            current_relevance=None,
            gold_relevance=None,
        )
    ]

    with pytest.raises(ValueError, match="Gold labels are missing"):
        run_batch_size_experiment(
            rows,
            batch_sizes=[1],
            config=LlmConfig(api_key="key", model="fake"),
            profile=_profile(),
            batch_runner=_fake_batch_runner,
        )


def test_missing_predictions_reduce_accuracy_and_count_false_direct() -> None:
    rows = _gold_rows(3)
    predictions = {
        "0": Classification(
            relevance=Relevance.DIRECT,
            confidence=0.9,
            reason="match",
            model="fake",
        ),
        "1": Classification(
            relevance=Relevance.DIRECT,
            confidence=0.9,
            reason="false direct",
            model="fake",
        ),
    }

    metrics = compute_accuracy_metrics(rows, predictions)

    assert metrics["overall_accuracy"] == 0.3333
    assert metrics["false_direct_count"] == 1
    assert metrics["direct_precision"] == 0.5


def test_write_experiment_outputs_has_stable_summary_schema() -> None:
    rows = _gold_rows(4)
    report = run_batch_size_experiment(
        rows,
        batch_sizes=[1],
        config=LlmConfig(api_key="key", model="fake"),
        profile=_profile(),
        batch_runner=_fake_batch_runner,
        generated_at=datetime(2026, 4, 30, tzinfo=UTC),
    )

    json_path, csv_path = write_experiment_outputs(report, _ignored_test_dir("outputs"))

    assert json_path.exists()
    csv_text = csv_path.read_text(encoding="utf-8")
    assert "batch_size,total_papers,request_count" in csv_text
    assert "overall_accuracy" in csv_text
    assert "direct_precision" in csv_text


def _gold_rows(count: int) -> list[GoldSampleRow]:
    rows: list[GoldSampleRow] = []
    labels = [Relevance.DIRECT, Relevance.INDIRECT, Relevance.UNRELATED]
    for index in range(count):
        label = labels[index % len(labels)]
        rows.append(
            GoldSampleRow(
                paper_id=str(index),
                paper=Paper(
                    source_url="https://example.com/rss",
                    title=f"Paper {index}",
                    url=f"https://example.com/{index}",
                ),
                current_relevance=label.value,
                gold_relevance=label,
            )
        )
    return rows


def _ignored_test_dir(name: str) -> Path:
    path = Path(".tmp") / "test-experiments" / f"{name}-{uuid4().hex}"
    path.mkdir(parents=True, exist_ok=True)
    return path


def _fake_batch_runner(
    indexed_papers: list[tuple[str, Paper]],
    _profile: ClassificationProfile,
    _config: LlmConfig,
) -> MeasuredBatchResponse:
    labels = [Relevance.DIRECT, Relevance.INDIRECT, Relevance.UNRELATED]
    classifications = {
        item_id: Classification(
            relevance=labels[int(item_id) % len(labels)],
            confidence=0.9,
            reason="fake",
            model="fake",
            translated_title_zh=f"论文 {item_id}",
        )
        for item_id, _paper in indexed_papers
    }
    return MeasuredBatchResponse(
        classifications=classifications,
        usage=TokenUsage(prompt_tokens=10, completion_tokens=10, total_tokens=20),
        request_count=1,
        latency_seconds=0.1,
    )


def _profile() -> ClassificationProfile:
    now = datetime(2026, 4, 30, tzinfo=UTC)
    return ClassificationProfile(
        meta=ProfileMeta(
            name="Experiment profile",
            version=1,
            created_at=now,
            updated_at=now,
            source_description="Experiment fixture.",
        ),
        scope="Experiment fixture scope.",
        relevance_rules=RelevanceRules(
            direct=["Direct fixture rule."],
            indirect=["Indirect fixture rule."],
            unrelated=["Unrelated fixture rule."],
        ),
        topic_taxonomy=[
            TopicDefinition(
                id="fixture_tag",
                label="Fixture Tag",
            )
        ],
        few_shots=[],
    )
