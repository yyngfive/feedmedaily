from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path

import typer
import uvicorn

from scirssagent.config import load_settings
from scirssagent.pipeline import regenerate_latest_report, run_once
from scirssagent.runtime import (
    APP_PUBLIC_NAME,
    find_available_local_port,
    launch_background_process,
    open_browser,
    package_version,
    read_runtime_state,
    wait_for_healthcheck,
    write_runtime_state,
)
from scirssagent.scheduler import install_scheduler_task, remove_scheduler_task, scheduler_status
from scirssagent.server import create_app

app = typer.Typer(help=f"{APP_PUBLIC_NAME} literature monitor.")
report_app = typer.Typer(help="Report commands.")
scheduler_app = typer.Typer(help="Windows Task Scheduler commands.")
app.add_typer(report_app, name="report")
app.add_typer(scheduler_app, name="scheduler")

ONCE_OPTION = typer.Option(False, "--once", help="Run one fetch/classify/report cycle.")
ROOT_OPTION = typer.Option(
    None,
    "--root",
    help="Application root. Defaults to cwd or the packaged app directory.",
)
HOST_OPTION = typer.Option(None, "--host", help="Server host. Defaults to configured value.")
PORT_OPTION = typer.Option(None, "--port", help="Server port. Defaults to configured value.")
VERSION_OPTION = typer.Option(None, "--version", help="Display the version and exit.")

#show version
def version_callback(value: bool):
    if value:
        typer.echo(f"scirssagent {package_version()}")
        raise typer.Exit()
    
@app.callback()
def main(
    version: bool = typer.Option(
        False,
        "--version",
        "-v",
        callback=version_callback,
        is_eager=True,
        help="Show version and exit.",
    )
):
    pass

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


@app.command("open")
def open_app(root: Path | None = ROOT_OPTION) -> None:
    settings = load_settings(root)
    state = read_runtime_state(settings.runtime_state_path)
    if state and state.port:
        existing_url = f"http://{settings.server_host}:{state.port}"
        if wait_for_healthcheck(f"{existing_url}/api/app/health", timeout_seconds=1.2):
            open_browser(existing_url)
            typer.echo(existing_url)
            return

    port = find_available_local_port(settings.server_host, settings.server_port)
    command = _serve_command(settings, port)
    process = launch_background_process(command, settings.root)
    url = f"http://{settings.server_host}:{port}"
    if not wait_for_healthcheck(f"{url}/api/app/health"):
        raise typer.Exit(code=1)
    write_runtime_state(
        settings.runtime_state_path,
        pid=process.pid,
        port=port,
        started_at=datetime.now(UTC).isoformat(),
    )
    open_browser(url)
    typer.echo(url)


@scheduler_app.command("show")
def scheduler_show(root: Path | None = ROOT_OPTION) -> None:
    settings = load_settings(root)
    typer.echo(scheduler_status(settings))


@scheduler_app.command("install")
def scheduler_install(
    daily_time: str = typer.Option("10:00", "--time", help="Daily local time in HH:MM format."),
    root: Path | None = ROOT_OPTION,
) -> None:
    settings = load_settings(root)
    typer.echo(install_scheduler_task(settings, daily_time))


@scheduler_app.command("remove")
def scheduler_remove() -> None:
    remove_scheduler_task()
    typer.echo("Removed scheduler task.")


@app.command()
def serve(
    root: Path | None = ROOT_OPTION,
    host: str | None = HOST_OPTION,
    port: int | None = PORT_OPTION,
) -> None:
    settings = load_settings(root)

    uvicorn.run(
        create_app(),
        host=host or settings.server_host,
        port=port or settings.server_port,
        reload=False,
    )


def _serve_command(settings, port: int) -> list[str]:
    if settings.mode == "release":
        return [
            str(settings.launch_command_path),
            "serve",
            "--host",
            settings.server_host,
            "--port",
            str(port),
        ]
    return [
        str(settings.launch_command_path),
        "-m",
        "scirssagent.cli",
        "serve",
        "--root",
        str(settings.root),
        "--host",
        settings.server_host,
        "--port",
        str(port),
    ]


if __name__ == "__main__":
    app()
