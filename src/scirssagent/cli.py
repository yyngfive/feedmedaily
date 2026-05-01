from __future__ import annotations

from pathlib import Path

import typer

from scirssagent.config import load_settings
from scirssagent.pipeline import regenerate_latest_report, run_once

app = typer.Typer(help="SciRSSAgent literature monitor.")
report_app = typer.Typer(help="Report commands.")
app.add_typer(report_app, name="report")

ONCE_OPTION = typer.Option(False, "--once", help="Run one fetch/classify/report cycle.")
ROOT_OPTION = typer.Option(None, "--root", help="Project root. Defaults to cwd.")
PRINT_TASK_OPTION = typer.Option(
    True,
    "--print-only/--create",
    help="Print or create task command.",
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
