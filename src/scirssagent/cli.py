from __future__ import annotations

from pathlib import Path

import typer

from scirssagent.config import load_settings
from scirssagent.experiments import (
    default_experiment_output_dir,
    default_gold_sample_path,
    load_gold_rows,
    missing_gold_label_ids,
    parse_batch_sizes,
    run_batch_size_experiment,
    select_gold_sample_rows,
    write_experiment_outputs,
    write_gold_template,
)
from scirssagent.pipeline import build_classifier_config, regenerate_latest_report, run_once
from scirssagent.profiles import ensure_profile
from scirssagent.repair import repair_all_abstracts
from scirssagent.storage import connect

app = typer.Typer(help="SciRSSAgent literature monitor.")
report_app = typer.Typer(help="Report commands.")
experiment_app = typer.Typer(help="Experiment commands.")
app.add_typer(report_app, name="report")
app.add_typer(experiment_app, name="experiment")

ONCE_OPTION = typer.Option(False, "--once", help="Run one fetch/classify/report cycle.")
ROOT_OPTION = typer.Option(None, "--root", help="Project root. Defaults to cwd.")
PRINT_TASK_OPTION = typer.Option(
    True,
    "--print-only/--create",
    help="Print or create task command.",
)
SAMPLE_SIZE_OPTION = typer.Option(
    80,
    "--sample-size",
    min=1,
    help="Number of papers to include in the gold-label sample.",
)
BATCH_SIZES_OPTION = typer.Option(
    "1,5,10,20,30",
    "--batch-sizes",
    help="Comma-separated LLM batch sizes to test.",
)
SEED_OPTION = typer.Option(42, "--seed", help="Deterministic sample seed.")
GOLD_FILE_OPTION = typer.Option(
    None,
    "--gold-file",
    help="Gold-label CSV. Created when it does not exist.",
)
OUTPUT_DIR_OPTION = typer.Option(
    None,
    "--output-dir",
    help="Directory for experiment JSON/CSV reports.",
)
HOST_OPTION = typer.Option(None, "--host", help="Server host. Defaults to configured value.")
PORT_OPTION = typer.Option(None, "--port", help="Server port. Defaults to configured value.")


@app.command()
def run(
    once: bool = ONCE_OPTION,
    root: Path | None = ROOT_OPTION,
    max_papers: int | None = typer.Option(
        None,
        "--max-papers",
        min=1,
        help="Limit the number of fetched papers for a test run.",
    ),
    reclassify: bool = typer.Option(
        False,
        "--reclassify",
        help="Force touched papers through classification again.",
    ),
) -> None:
    if not once:
        raise typer.BadParameter("Only --once is supported in v0.1.")
    settings = load_settings(root)
    index = run_once(settings, max_papers=max_papers, reclassify=reclassify)
    typer.echo(f"Report: {index}")


@report_app.command("latest")
def latest(root: Path | None = ROOT_OPTION) -> None:
    settings = load_settings(root)
    index = regenerate_latest_report(settings)
    typer.echo(f"Report: {index}")


@app.command("repair-abstracts")
def repair_abstracts(root: Path | None = ROOT_OPTION) -> None:
    settings = load_settings(root)
    result = repair_all_abstracts(settings)
    typer.echo(f"Database backup: {result['database_backup_path']}")
    typer.echo(f"Repair report: {result['repair_report_path']}")
    typer.echo(f"Report: {result['report_index']}")
    summary = result["summary"]
    typer.echo(
        "Summary: "
        f"total={summary['total']} "
        f"updated={summary['updated']} "
        f"fallback_from_db_html={summary['fallback_from_db_html']} "
        f"cleared_metadata_only={summary['cleared_metadata_only']} "
        f"unmatched={summary['unmatched']} "
        f"unchanged={summary['unchanged']}"
    )


@experiment_app.command("batch-size")
def experiment_batch_size(
    root: Path | None = ROOT_OPTION,
    sample_size: int = SAMPLE_SIZE_OPTION,
    batch_sizes: str = BATCH_SIZES_OPTION,
    seed: int = SEED_OPTION,
    gold_file: Path | None = GOLD_FILE_OPTION,
    output_dir: Path | None = OUTPUT_DIR_OPTION,
) -> None:
    settings = load_settings(root)
    sizes = parse_batch_sizes(batch_sizes)
    sample_path = gold_file or default_gold_sample_path(settings.root)
    output_root = output_dir or default_experiment_output_dir(settings.root)
    if not sample_path.exists():
        conn = connect(settings.database_path)
        try:
            rows = select_gold_sample_rows(conn, sample_size=sample_size, seed=seed)
        finally:
            conn.close()
        write_gold_template(rows, sample_path)
        typer.echo(f"Gold-label template written: {sample_path}")
        typer.echo(
            "Fill the gold_relevance column with direct, indirect, or unrelated, then rerun."
        )
        return

    rows = load_gold_rows(sample_path)
    missing_ids = missing_gold_label_ids(rows)
    if missing_ids:
        preview = ", ".join(missing_ids[:10])
        suffix = "..." if len(missing_ids) > 10 else ""
        raise typer.BadParameter(f"gold_relevance is missing for paper_id: {preview}{suffix}")
    if not settings.classifier_api_key:
        raise typer.BadParameter("SCIRSS_CLASSIFIER_API_KEY is required.")
    profile = ensure_profile(settings.profile_path)
    llm_config = build_classifier_config(settings)
    typer.echo(f"Gold labels: {sample_path}")
    typer.echo(f"Output dir: {output_root}")
    typer.echo(f"Model: {settings.classifier_model} | Papers: {len(rows)}")
    report = run_batch_size_experiment(rows, sizes, llm_config, profile, progress=typer.echo)
    json_path, csv_path = write_experiment_outputs(report, output_root)
    typer.echo(f"Selected batch size: {report['selected_batch_size']}")
    typer.echo(f"JSON: {json_path}")
    typer.echo(f"CSV: {csv_path}")


@app.command("init-task")
def init_task(
    print_only: bool = PRINT_TASK_OPTION,
    root: Path | None = ROOT_OPTION,
) -> None:
    settings = load_settings(root)
    command = (
        "schtasks /Create /TN SciRSSAgent /SC DAILY /ST 10:00 /F "
        f'/TR "powershell -NoProfile -ExecutionPolicy Bypass -Command '
        f"cd '{settings.root}'; uv run scirssagent run --once" + '"'
    )
    if print_only:
        typer.echo(command)
        return
    import subprocess

    subprocess.run(command, shell=True, check=True)


@app.command()
def serve(
    root: Path | None = ROOT_OPTION,
    host: str | None = HOST_OPTION,
    port: int | None = PORT_OPTION,
) -> None:
    settings = load_settings(root)
    import uvicorn

    uvicorn.run(
        "scirssagent.server:app",
        host=host or settings.server_host,
        port=port or settings.server_port,
        reload=False,
    )


if __name__ == "__main__":
    app()
