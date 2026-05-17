from __future__ import annotations

import json
import shutil
import subprocess
from datetime import UTC, datetime
from pathlib import Path

import typer
import uvicorn

from scirssagent.config import load_settings
from scirssagent.pipeline import reclassify_paper_ids as run_reclassify_paper_ids
from scirssagent.profiles import read_profile
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
from scirssagent.services import (
    generate_initial_profile_payload,
    generate_profile_proposal_payload,
)
from scirssagent.storage import (
    all_paper_ids,
    connect,
    list_open_feedback,
    feedback_paper_ids,
    latest_classification,
    paper_by_id,
    paper_from_row,
    save_profile_proposal,
    recent_paper_ids,
)
from scirssagent.models import FeedbackProposalContext

app = typer.Typer(help=f"{APP_PUBLIC_NAME} literature monitor.")
report_app = typer.Typer(help="Report commands.")
scheduler_app = typer.Typer(help="Windows Task Scheduler commands.")
profile_app = typer.Typer(help="Profile commands.")
zotero_app = typer.Typer(help="Zotero commands.")
app.add_typer(report_app, name="report")
app.add_typer(scheduler_app, name="scheduler")
app.add_typer(profile_app, name="profile")
app.add_typer(zotero_app, name="zotero")

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
    payload = _run_once_via_go(settings, max_papers=max_papers, reclassify=reclassify)
    typer.echo(json.dumps(payload, ensure_ascii=False))


@report_app.command("latest")
def latest(root: Path | None = ROOT_OPTION) -> None:
    settings = load_settings(root)
    payload = _report_latest_via_go(settings)
    typer.echo(json.dumps(payload, ensure_ascii=False))


@profile_app.command("bootstrap")
def profile_bootstrap(
    interest_description: str = typer.Option(..., "--interest-description"),
    name: str | None = typer.Option(None, "--name"),
    root: Path | None = ROOT_OPTION,
) -> None:
    if not interest_description.strip():
        raise typer.BadParameter("--interest-description cannot be blank.")
    result = _bootstrap_profile_cli(root, interest_description, name)
    typer.echo(json.dumps(result, ensure_ascii=False))


@profile_app.command("proposal-generate")
def profile_proposal_generate(root: Path | None = ROOT_OPTION) -> None:
    result = _generate_profile_proposal_cli(root)
    typer.echo(json.dumps(result, ensure_ascii=False))


@app.command()
def reclassify(
    root: Path | None = ROOT_OPTION,
    scope: str | None = typer.Option(
        None,
        "--scope",
        help="Reclassify recent, feedback, or all papers when no explicit paper ids are supplied.",
    ),
    limit: int | None = typer.Option(
        None,
        "--limit",
        min=1,
        max=500,
        help="Recent-paper limit used together with --scope recent.",
    ),
    paper_id: list[int] | None = typer.Option(
        None,
        "--paper-id",
        help="Reclassify one or more explicit paper ids. Repeat the flag to pass multiple ids.",
    ),
) -> None:
    if paper_id:
        if scope is not None or limit is not None:
            raise typer.BadParameter("--paper-id cannot be used together with --scope or --limit.")
        settings = load_settings(root)
        count = run_reclassify_paper_ids(settings, paper_id)
        typer.echo(f"Reclassified: {count}")
        return

    selected_scope = (scope or "recent").strip().lower()
    if selected_scope not in {"recent", "feedback", "all"}:
        raise typer.BadParameter("--scope must be one of recent, feedback, or all.")
    selected_limit = limit or 50

    settings = load_settings(root)
    conn = connect(settings.database_path)
    try:
        if selected_scope == "feedback":
            paper_ids = feedback_paper_ids(conn)
        elif selected_scope == "all":
            paper_ids = all_paper_ids(conn)
        else:
            paper_ids = recent_paper_ids(conn, selected_limit)
    finally:
        conn.close()

    count = run_reclassify_paper_ids(settings, paper_ids)
    typer.echo(f"Reclassified: {count}")


@zotero_app.command("collections")
def zotero_collections(root: Path | None = ROOT_OPTION) -> None:
    settings = load_settings(root)
    payload = _zotero_collections_via_go(settings)
    typer.echo(json.dumps(payload, ensure_ascii=False))


@zotero_app.command("save")
def zotero_save(
    paper_id: int = typer.Option(..., "--paper-id", min=1),
    collection_key: str | None = typer.Option(None, "--collection-key"),
    root: Path | None = ROOT_OPTION,
) -> None:
    settings = load_settings(root)
    payload = _zotero_save_via_go(settings, paper_id=paper_id, collection_key=collection_key)
    typer.echo(json.dumps(payload, ensure_ascii=False))


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
    """Run the legacy Python reference server for regression/debug work only."""
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


def _bootstrap_profile_cli(
    root: Path | None,
    interest_description: str,
    name: str | None,
) -> dict[str, object]:
    settings = load_settings(root)
    if read_profile(settings.profile_path) is not None:
        raise ValueError("A classification profile already exists.")
    payload = generate_initial_profile_payload(
        settings,
        interest_description=interest_description,
        name=name,
    )
    conn = connect(settings.database_path)
    try:
        proposal = save_profile_proposal(
            conn,
            summary=str(payload["summary"]),
            proposed_profile=payload["proposed_profile"],
            rule_delta=payload["rule_delta"],
            source_feedback_ids=[],
            model=str(payload.get("model") or settings.profile_model),
        )
        conn.commit()
        return {"proposal_id": proposal.id}
    finally:
        conn.close()


def _generate_profile_proposal_cli(root: Path | None) -> dict[str, object]:
    settings = load_settings(root)
    current = read_profile(settings.profile_path)
    if current is None:
        raise ValueError("No classification profile exists yet.")
    conn = connect(settings.database_path)
    try:
        feedback_items = list_open_feedback(conn)
        feedback_context: list[FeedbackProposalContext] = []
        for item in feedback_items:
            row = paper_by_id(conn, item.paper_id)
            paper = paper_from_row(row) if row is not None else None
            feedback_context.append(
                FeedbackProposalContext(
                    feedback_id=item.id,
                    paper_id=item.paper_id,
                    paper_title=item.paper_title,
                    journal=paper.journal if paper is not None else None,
                    abstract=paper.abstract if paper is not None else None,
                    original_relevance=item.original_relevance,
                    corrected_relevance=item.corrected_relevance,
                    note=item.note,
                )
            )
        payload = generate_profile_proposal_payload(settings, current, feedback_context)
        proposal = save_profile_proposal(
            conn,
            summary=str(payload["summary"]),
            proposed_profile=payload["proposed_profile"],
            rule_delta=payload["rule_delta"],
            source_feedback_ids=list(payload["source_feedback_ids"]),
            model=str(payload["model"]),
        )
        conn.commit()
        return {"proposal_id": proposal.id}
    finally:
        conn.close()


def _run_once_via_go(settings, *, max_papers: int | None, reclassify: bool) -> dict[str, object]:
    command = _go_backend_command(settings, ["--run-once"])
    if max_papers is not None:
        command.extend(["--max-papers", str(max_papers)])
    if reclassify:
        command.append("--reclassify")
    return _run_go_json_command(command, settings.root)


def _report_latest_via_go(settings) -> dict[str, object]:
    command = _go_backend_command(settings, ["--report-latest"])
    return _run_go_json_command(command, settings.root)


def _zotero_collections_via_go(settings) -> dict[str, object]:
    command = _go_backend_command(settings, ["--zotero-collections"])
    return _run_go_json_command(command, settings.root)


def _zotero_save_via_go(
    settings,
    *,
    paper_id: int,
    collection_key: str | None,
) -> dict[str, object]:
    command = _go_backend_command(settings, ["--zotero-save", "--paper-id", str(paper_id)])
    if collection_key is not None:
        command.extend(["--collection-key", collection_key])
    return _run_go_json_command(command, settings.root)


def _go_backend_command(settings, extra_args: list[str]) -> list[str]:
    if settings.mode == "release":
        return [str(settings.app_dir / "feedmedailyd.exe"), *extra_args, "--root", str(settings.root)]
    go_path = shutil.which("go")
    if not go_path:
        raise RuntimeError("The Go toolchain is required to run the migrated backend command in source mode.")
    return [go_path, "run", "./cmd/feedmedailyd", *extra_args, "--root", str(settings.root)]


def _run_go_json_command(command: list[str], cwd: Path) -> dict[str, object]:
    result = subprocess.run(
        command,
        cwd=str(cwd),
        capture_output=True,
        text=True,
        encoding="utf-8",
        check=False,
    )
    if result.returncode != 0:
        message = (result.stderr or result.stdout).strip() or "Go backend command failed."
        raise RuntimeError(message)
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"Invalid Go backend JSON output: {exc}") from exc


if __name__ == "__main__":
    app()
