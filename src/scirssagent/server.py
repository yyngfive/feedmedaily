from __future__ import annotations

import threading
import uuid
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Literal

import httpx
from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse, HTMLResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel, Field

from scirssagent.config import (
    Settings,
    load_settings,
    resolved_config_values,
    update_local_settings,
)
from scirssagent.feeds import read_feed_subscriptions, write_feed_subscriptions
from scirssagent.models import (
    AppControlResponse,
    AppHealthResponse,
    AppMetaResponse,
    AppOpenRequest,
    AppUpdateResponse,
    CurrentProfileResponse,
    FeedbackProposalContext,
    FeedSubscription,
    JobInfo,
    ProfileMeta,
    ProfileProposalState,
    Relevance,
    Report,
    SchedulerSettingsResponse,
    SchedulerSettingsUpdateRequest,
    SettingOption,
    SettingsConfigField,
    SettingsConfigResponse,
    SettingsConfigUpdateRequest,
    ZoteroCollectionsResponse,
    ZoteroSaveState,
)
from scirssagent.pipeline import (
    reclassify_paper_ids,
    reclassify_paper_ids_with_progress,
    regenerate_latest_report,
    run_once,
)
from scirssagent.profiles import read_profile
from scirssagent.reporting import build_report
from scirssagent.runtime import (
    APP_PUBLIC_NAME,
    SCHEDULER_TASK_NAME,
    AppOpenTarget,
    is_newer_version,
    open_external_target,
    package_version,
    process_is_running,
    read_runtime_state,
    schedule_process_exit,
)
from scirssagent.scheduler import install_scheduler_task, remove_scheduler_task, scheduler_status
from scirssagent.services import (
    generate_initial_profile_payload,
    generate_profile_proposal_payload,
    list_zotero_collections,
    save_paper_to_zotero,
    write_current_profile,
)
from scirssagent.storage import (
    all_paper_ids,
    apply_profile_proposal,
    connect,
    delete_feedback,
    feedback_by_id,
    feedback_paper_ids,
    latest_classification,
    latest_zotero_status,
    list_feedback,
    list_open_feedback,
    list_profile_proposals,
    mark_feedback_used,
    mark_paper_read,
    paper_by_id,
    paper_from_row,
    paper_ids_for_feedback_ids,
    papers_for_report,
    profile_proposal_by_id,
    recent_paper_ids,
    reject_profile_proposal,
    save_feedback,
    save_profile_proposal,
    upsert_zotero_status,
)


class FeedbackCreateRequest(BaseModel):
    paper_id: int
    corrected_relevance: Relevance
    note: str | None = None


class ReclassifyRequest(BaseModel):
    scope: Literal["recent", "feedback", "all"] = "recent"
    limit: int = Field(default=50, ge=1, le=500)


class ProfileBootstrapRequest(BaseModel):
    interest_description: str = Field(min_length=1)
    name: str | None = None


class FeedSettingsUpdateRequest(BaseModel):
    feeds: list[FeedSubscription] = Field(default_factory=list)


class PaperReadResponse(BaseModel):
    paper_id: int
    read_at: datetime


class ZoteroSaveRequest(BaseModel):
    collection_key: str | None = None


class JobLaunchResponse(BaseModel):
    job: JobInfo


@dataclass
class JobRegistry:
    lock: threading.Lock
    jobs: dict[str, JobInfo]


JOB_REGISTRY = JobRegistry(lock=threading.Lock(), jobs={})


def create_app() -> FastAPI:
    app = FastAPI(title=APP_PUBLIC_NAME)
    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_methods=["*"],
        allow_headers=["*"],
    )

    @app.get("/api/report/latest", response_model=Report)
    def api_report_latest() -> Report:
        settings = load_settings()
        return current_report(settings)

    @app.get("/api/app/meta", response_model=AppMetaResponse)
    def api_app_meta(request: Request) -> AppMetaResponse:
        settings = load_settings()
        return build_app_meta(settings, request)

    @app.get("/api/app/health", response_model=AppHealthResponse)
    def api_app_health(request: Request) -> AppHealthResponse:
        settings = load_settings()
        return AppHealthResponse(
            name=APP_PUBLIC_NAME,
            version=package_version(),
            mode=settings.mode,
            server_url=str(request.base_url).rstrip("/"),
        )

    @app.get("/api/app/update", response_model=AppUpdateResponse)
    def api_app_update() -> AppUpdateResponse:
        settings = load_settings()
        return fetch_update_status(settings)

    @app.post("/api/app/open", response_model=AppControlResponse)
    def api_app_open_target(
        payload: AppOpenRequest,
        request: Request,
    ) -> AppControlResponse:
        settings = load_settings()
        update_status = (
            fetch_update_status(settings)
            if payload.target in {
                AppOpenTarget.DOWNLOAD_URL.value,
                AppOpenTarget.RELEASE_NOTES_URL.value,
            }
            else None
        )
        target = resolve_app_open_target(
            settings=settings,
            target=payload.target,
            server_url=str(request.base_url).rstrip("/"),
            update=update_status,
        )
        open_external_target(target)
        return AppControlResponse(
            action="open",
            target=payload.target,
            detail=target,
        )

    @app.post("/api/app/exit", response_model=AppControlResponse)
    def api_app_exit() -> AppControlResponse:
        settings = load_settings()
        schedule_process_exit(settings.runtime_state_path)
        return AppControlResponse(
            action="exit",
            detail="Shutting down the local FeedMeDaily service.",
        )

    @app.get("/api/settings/feeds")
    def api_settings_feeds() -> list[dict[str, object]]:
        settings = load_settings()
        return [item.model_dump(mode="json") for item in load_feed_settings(settings)]

    @app.get("/api/settings/config", response_model=SettingsConfigResponse)
    def api_settings_config() -> SettingsConfigResponse:
        settings = build_settings_config_response()
        return SettingsConfigResponse(fields=settings)

    @app.put("/api/settings/config", response_model=SettingsConfigResponse)
    def api_settings_config_update(
        payload: SettingsConfigUpdateRequest,
    ) -> SettingsConfigResponse:
        try:
            updated = update_local_settings(
                load_settings().root,
                {
                    key: value.model_dump(mode="python")
                    for key, value in payload.fields.items()
                },
            )
        except ValueError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc
        return SettingsConfigResponse(fields=_settings_config_fields(updated))

    @app.put("/api/settings/feeds")
    def api_settings_feeds_update(payload: FeedSettingsUpdateRequest) -> list[dict[str, object]]:
        settings = load_settings()
        write_feed_subscriptions(settings.feeds_path, payload.feeds)
        return [item.model_dump(mode="json") for item in load_feed_settings(settings)]

    @app.get("/api/settings/scheduler", response_model=SchedulerSettingsResponse)
    def api_settings_scheduler() -> SchedulerSettingsResponse:
        settings = load_settings()
        return SchedulerSettingsResponse.model_validate(scheduler_status(settings))

    @app.put("/api/settings/scheduler", response_model=SchedulerSettingsResponse)
    def api_settings_scheduler_update(
        payload: SchedulerSettingsUpdateRequest,
    ) -> SchedulerSettingsResponse:
        settings = load_settings()
        return SchedulerSettingsResponse.model_validate(
            install_scheduler_task(settings, payload.daily_time)
        )

    @app.delete("/api/settings/scheduler", response_model=SchedulerSettingsResponse)
    def api_settings_scheduler_delete() -> SchedulerSettingsResponse:
        settings = load_settings()
        remove_scheduler_task()
        return SchedulerSettingsResponse.model_validate(scheduler_status(settings))

    @app.get("/api/profile/current", response_model=CurrentProfileResponse)
    def api_profile_current() -> CurrentProfileResponse:
        settings = load_settings()
        return CurrentProfileResponse(profile=read_profile(settings.profile_path))

    @app.post("/api/profile/bootstrap")
    def api_profile_bootstrap(payload: ProfileBootstrapRequest) -> JobLaunchResponse:
        settings = load_settings()
        if read_profile(settings.profile_path) is not None:
            raise HTTPException(status_code=400, detail="A classification profile already exists.")
        return JobLaunchResponse(
            job=launch_job(
                "profile-bootstrap",
                _bootstrap_profile_job,
                settings.root,
                payload.model_dump(),
                queued_message_key="profile.bootstrap.queued",
                queued_message="Queued initial profile generation.",
                running_message_key="profile.bootstrap.generating",
                running_message="Generating the initial classification profile proposal.",
            )
        )

    @app.get("/api/feedback")
    def api_feedback() -> list[dict[str, object]]:
        settings = load_settings()
        conn = connect(settings.database_path)
        try:
            return [item.model_dump(mode="json") for item in list_feedback(conn)]
        finally:
            conn.close()

    @app.post("/api/feedback")
    def api_feedback_create(payload: FeedbackCreateRequest) -> dict[str, object]:
        settings = load_settings()
        conn = connect(settings.database_path)
        try:
            row = paper_by_id(conn, payload.paper_id)
            if row is None:
                raise HTTPException(status_code=404, detail="Paper not found.")
            classification = latest_classification(conn, payload.paper_id)
            if classification is None:
                raise HTTPException(
                    status_code=400,
                    detail="Paper has no classification yet.",
                )
            feedback = save_feedback(
                conn,
                payload.paper_id,
                original_relevance=classification.relevance,
                corrected_relevance=payload.corrected_relevance,
                note=payload.note,
            )
            conn.commit()
            return feedback.model_dump(mode="json")
        finally:
            conn.close()

    @app.post("/api/papers/{paper_id}/read", response_model=PaperReadResponse)
    def api_paper_mark_read(paper_id: int) -> PaperReadResponse:
        settings = load_settings()
        conn = connect(settings.database_path)
        try:
            row = paper_by_id(conn, paper_id)
            if row is None:
                raise HTTPException(status_code=404, detail="Paper not found.")
            read_at = mark_paper_read(conn, paper_id)
            conn.commit()
            return PaperReadResponse(paper_id=paper_id, read_at=read_at)
        finally:
            conn.close()

    @app.delete("/api/feedback/{feedback_id}")
    def api_feedback_delete(feedback_id: int) -> dict[str, object]:
        settings = load_settings()
        conn = connect(settings.database_path)
        try:
            feedback = feedback_by_id(conn, feedback_id)
            if feedback is None:
                raise HTTPException(status_code=404, detail="Feedback not found.")
            delete_feedback(conn, feedback_id)
            conn.commit()
            return {"deleted": True, "feedback_id": feedback_id}
        finally:
            conn.close()

    @app.get("/api/profile/proposals")
    def api_profile_proposals() -> list[dict[str, object]]:
        settings = load_settings()
        conn = connect(settings.database_path)
        try:
            return [item.model_dump(mode="json") for item in list_profile_proposals(conn)]
        finally:
            conn.close()

    @app.get("/api/profile/proposals/{proposal_id}")
    def api_profile_proposal_detail(proposal_id: int) -> dict[str, object]:
        settings = load_settings()
        conn = connect(settings.database_path)
        try:
            proposal = profile_proposal_by_id(conn, proposal_id)
            if proposal is None:
                raise HTTPException(status_code=404, detail="Profile proposal not found.")
            return proposal.model_dump(mode="json")
        finally:
            conn.close()

    @app.post("/api/profile/proposals/generate", response_model=JobLaunchResponse)
    def api_profile_proposals_generate() -> JobLaunchResponse:
        settings = load_settings()
        return JobLaunchResponse(
            job=launch_job("profile-proposal", _generate_profile_proposal_job, settings.root)
        )

    @app.post("/api/profile/proposals/{proposal_id}/apply")
    def api_profile_proposal_apply(proposal_id: int) -> dict[str, object]:
        settings = load_settings()
        conn = connect(settings.database_path)
        try:
            proposal = profile_proposal_by_id(conn, proposal_id)
            if proposal is None:
                raise HTTPException(status_code=404, detail="Profile proposal not found.")
            if proposal.state == ProfileProposalState.APPLIED:
                return proposal.model_dump(mode="json")

            current = read_profile(settings.profile_path)
            version = 1 if current is None else current.meta.version + 1
            created_at = (
                current.meta.created_at
                if current is not None
                else proposal.proposed_profile.meta.created_at
            )
            applied_profile = proposal.proposed_profile.model_copy(
                update={
                    "meta": ProfileMeta(
                        name=proposal.proposed_profile.meta.name,
                        version=version,
                        created_at=created_at,
                        updated_at=datetime.now(UTC),
                        source_description=proposal.proposed_profile.meta.source_description,
                    )
                }
            )
            write_current_profile(settings, applied_profile)
            apply_profile_proposal(conn, proposal_id, version)
            mark_feedback_used(conn, proposal.source_feedback_ids)
            conn.commit()
            feedback_ids = list(proposal.source_feedback_ids)
        finally:
            conn.close()

        if feedback_ids:
            reclassify_paper_ids(settings, _feedback_paper_ids_for_apply(settings, feedback_ids))
        regenerate_latest_report(settings)

        conn = connect(settings.database_path)
        try:
            updated = profile_proposal_by_id(conn, proposal_id)
            if updated is None:
                raise HTTPException(
                    status_code=500,
                    detail="Profile proposal disappeared after apply.",
                )
            return updated.model_dump(mode="json")
        finally:
            conn.close()

    @app.post("/api/profile/proposals/{proposal_id}/reject")
    def api_profile_proposal_reject(proposal_id: int) -> dict[str, object]:
        settings = load_settings()
        conn = connect(settings.database_path)
        try:
            proposal = profile_proposal_by_id(conn, proposal_id)
            if proposal is None:
                raise HTTPException(status_code=404, detail="Profile proposal not found.")
            reject_profile_proposal(conn, proposal_id)
            conn.commit()
            updated = profile_proposal_by_id(conn, proposal_id)
            if updated is None:
                raise HTTPException(
                    status_code=500,
                    detail="Profile proposal disappeared after reject.",
                )
            return updated.model_dump(mode="json")
        finally:
            conn.close()

    @app.post("/api/zotero/save/{paper_id}")
    def api_zotero_save(paper_id: int, payload: ZoteroSaveRequest) -> dict[str, object]:
        settings = load_settings()
        conn = connect(settings.database_path)
        try:
            row = paper_by_id(conn, paper_id)
            if row is None:
                raise HTTPException(status_code=404, detail="Paper not found.")
            paper = paper_from_row(row)
            classification = latest_classification(conn, paper_id)
            if classification is None:
                raise HTTPException(status_code=400, detail="Paper has no classification yet.")
            current = latest_zotero_status(conn, paper_id)
            if current and current.saved:
                return current.model_dump(mode="json")
            try:
                _response_text, item_key = save_paper_to_zotero(
                    settings,
                    paper,
                    classification,
                    collection_key=payload.collection_key,
                )
                status = upsert_zotero_status(
                    conn,
                    paper_id,
                    state=ZoteroSaveState.SAVED,
                    item_key=item_key,
                )
            except Exception as exc:
                status = upsert_zotero_status(
                    conn,
                    paper_id,
                    state=ZoteroSaveState.ERROR,
                    error_message=str(exc),
                )
            conn.commit()
            return status.model_dump(mode="json")
        finally:
            conn.close()

    @app.get("/api/zotero/collections", response_model=ZoteroCollectionsResponse)
    def api_zotero_collections() -> ZoteroCollectionsResponse:
        settings = load_settings()
        try:
            return list_zotero_collections(settings)
        except ValueError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc

    @app.post("/api/admin/run", response_model=JobLaunchResponse)
    def api_admin_run() -> JobLaunchResponse:
        settings = load_settings()
        return JobLaunchResponse(job=launch_job("run", _run_pipeline_job, settings.root))

    @app.post("/api/admin/reclassify", response_model=JobLaunchResponse)
    def api_admin_reclassify(payload: ReclassifyRequest) -> JobLaunchResponse:
        settings = load_settings()
        return JobLaunchResponse(
            job=launch_job("reclassify", _reclassify_job, settings.root, payload.model_dump())
        )

    @app.post("/api/admin/report/latest", response_model=JobLaunchResponse)
    def api_admin_report_latest() -> JobLaunchResponse:
        settings = load_settings()
        return JobLaunchResponse(job=launch_job("report", _regenerate_report_job, settings.root))

    @app.get("/api/admin/jobs/{job_id}", response_model=JobInfo)
    def api_admin_job(job_id: str) -> JobInfo:
        with JOB_REGISTRY.lock:
            job = JOB_REGISTRY.jobs.get(job_id)
            if job is None:
                raise HTTPException(status_code=404, detail="Job not found.")
            return job

    @app.get("/api/admin/jobs")
    def api_admin_jobs() -> list[dict[str, object]]:
        with JOB_REGISTRY.lock:
            jobs = sorted(
                JOB_REGISTRY.jobs.values(),
                key=lambda item: (item.created_at, item.id),
                reverse=True,
            )
            return [job.model_dump(mode="json") for job in jobs]

    mount_static_app(app)
    return app


def current_report(settings: Settings) -> Report:
    conn = connect(settings.database_path)
    try:
        report_date = datetime.now(UTC).date()
        report_papers = papers_for_report(
            conn,
            report_date=None,
            limit=None,
        )
        return build_report(report_papers, report_date, [])
    finally:
        conn.close()


def load_feed_settings(settings: Settings) -> list[FeedSubscription]:
    return read_feed_subscriptions(settings.feeds_path)


def build_settings_config_response() -> list[SettingsConfigField]:
    return _settings_config_fields(resolved_config_values(load_settings().root))


def build_app_meta(settings: Settings, request: Request) -> AppMetaResponse:
    runtime_state = read_runtime_state(settings.runtime_state_path)
    return AppMetaResponse(
        name=APP_PUBLIC_NAME,
        version=package_version(),
        mode=settings.mode,
        install_dir=str(settings.app_dir),
        data_dir=str(settings.data_dir),
        logs_dir=str(settings.logs_dir),
        reports_dir=str(settings.reports_dir),
        static_dir=str(settings.web_dist_dir),
        server_url=str(request.base_url).rstrip("/"),
        scheduler_task_name=SCHEDULER_TASK_NAME,
        update_manifest_url=settings.update_manifest_url,
        process_running=process_is_running(runtime_state.pid if runtime_state else None),
    )


def fetch_update_status(settings: Settings) -> AppUpdateResponse:
    if not settings.update_manifest_url:
        return AppUpdateResponse(
            status="not_configured",
            current_version=package_version(),
            detail="Update checks are not configured for this build.",
        )
    try:
        response = httpx.get(settings.update_manifest_url, timeout=5.0, follow_redirects=True)
        response.raise_for_status()
        payload = response.json()
    except Exception as exc:
        print(f"Error fetching update manifest: {str(exc)}")
        return AppUpdateResponse(
            status="check_failed",
            current_version=package_version(),
            detail=str(exc),
        )
    latest_version = str(payload.get("version") or "").strip() or None
    download_url = str(payload.get("download_url") or "").strip() or None
    release_notes_url = str(payload.get("release_notes_url") or "").strip() or None
    has_update = bool(latest_version and is_newer_version(latest_version, package_version()))
    return AppUpdateResponse(
        status="update_available" if has_update else "up_to_date",
        current_version=package_version(),
        latest_version=latest_version,
        has_update=has_update,
        download_url=download_url,
        release_notes_url=release_notes_url,
        detail=None if latest_version else "Manifest did not include a version field.",
    )


def resolve_app_open_target(
    settings: Settings,
    target: str,
    server_url: str,
    update: AppUpdateResponse | None,
) -> str:
    mapping = {
        AppOpenTarget.DATA_DIR.value: str(settings.data_dir),
        AppOpenTarget.LOGS_DIR.value: str(settings.logs_dir),
        AppOpenTarget.REPORTS_DIR.value: str(settings.reports_dir),
        AppOpenTarget.INSTALL_DIR.value: str(settings.app_dir),
        AppOpenTarget.SERVER_URL.value: server_url,
        AppOpenTarget.DOWNLOAD_URL.value: update.download_url if update else None,
        AppOpenTarget.RELEASE_NOTES_URL.value: update.release_notes_url if update else None,
    }
    resolved = mapping.get(target)
    if not resolved:
        raise HTTPException(status_code=400, detail=f"Unsupported app open target: {target}")
    return resolved


def _settings_config_fields(values) -> list[SettingsConfigField]:
    return [
        SettingsConfigField(
            key=item.option.key,
            label=item.option.label,
            description=item.option.description,
            section=item.option.section,
            input_type=item.option.input_type,
            secret=item.option.secret,
            configured=item.configured,
            source=item.source,
            stored_in_dotenv=item.stored_in_dotenv,
            storage_label=item.storage_label,
            value=None if item.option.secret else item.value,
            default_value=item.option.default,
            options=[
                SettingOption(value=value, label=label)
                for value, label in item.option.options
            ],
        )
        for item in values
    ]


def launch_job(
    job_type: str,
    target,
    *args,
    queued_message_key: str | None = None,
    queued_message: str | None = None,
    running_message_key: str | None = None,
    running_message: str | None = None,
) -> JobInfo:
    job = JobInfo(
        id=uuid.uuid4().hex,
        job_type=job_type,
        status="queued",
        message_key=queued_message_key,
        message=queued_message,
    )
    with JOB_REGISTRY.lock:
        JOB_REGISTRY.jobs[job.id] = job

    def runner() -> None:
        update_job(
            job.id,
            status="running",
            message_key=running_message_key,
            message=running_message,
            started_at=datetime.now(UTC),
        )
        def progress(message_key: str, message: str) -> None:
            update_job(job.id, message_key=message_key, message=message)

        try:
            result = target(*args, progress=progress)
        except Exception as exc:
            update_job(
                job.id,
                status="failed",
                message_key=f"{job_type}.failed",
                message=None,
                error=str(exc),
                finished_at=datetime.now(UTC),
            )
            return
        update_job(
            job.id,
            status="completed",
            message_key=f"{job_type}.completed",
            message="Completed.",
            result=result if isinstance(result, dict) else {"value": result},
            finished_at=datetime.now(UTC),
        )

    thread = threading.Thread(target=runner, daemon=True)
    thread.start()
    return job


def update_job(job_id: str, **updates) -> None:
    with JOB_REGISTRY.lock:
        current = JOB_REGISTRY.jobs[job_id]
        JOB_REGISTRY.jobs[job_id] = current.model_copy(update=updates)


def _run_pipeline_job(root: Path, progress=None) -> dict[str, object]:
    settings = load_settings(root)
    index = run_once(settings, reclassify=False, progress=progress)
    return {"report": index}


def _regenerate_report_job(root: Path, progress=None) -> dict[str, object]:
    settings = load_settings(root)
    if progress:
        progress("pipeline.report.writing", "Publishing the latest report.")
    index = regenerate_latest_report(settings)
    return {"report": index}


def _generate_profile_proposal_job(root: Path, progress=None) -> dict[str, object]:
    settings = load_settings(root)
    current = read_profile(settings.profile_path)
    if current is None:
        raise ValueError("No classification profile exists yet.")
    if progress:
        progress("profile.proposal.collecting_feedback", "Collecting feedback for profile review.")
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
        if progress:
            progress("profile.proposal.generating", "Generating profile proposal.")
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


def _bootstrap_profile_job(
    root: Path,
    payload: dict[str, object],
    progress=None,
) -> dict[str, object]:
    settings = load_settings(root)
    if read_profile(settings.profile_path) is not None:
        raise ValueError("A classification profile already exists.")
    if progress:
        progress("profile.bootstrap.generating", "Generating the initial profile proposal.")
    proposal_payload = generate_initial_profile_payload(
        settings,
        interest_description=str(payload["interest_description"]),
        name=str(payload["name"]) if payload.get("name") else None,
    )
    conn = connect(settings.database_path)
    try:
        proposal = save_profile_proposal(
            conn,
            summary=str(proposal_payload["summary"]),
            proposed_profile=proposal_payload["proposed_profile"],
            rule_delta=proposal_payload["rule_delta"],
            source_feedback_ids=[],
            model=settings.profile_model,
        )
        conn.commit()
        return {"proposal_id": proposal.id}
    finally:
        conn.close()


def _reclassify_job(root: Path, payload: dict[str, object], progress=None) -> dict[str, object]:
    settings = load_settings(root)
    conn = connect(settings.database_path)
    try:
        if payload["scope"] == "feedback":
            paper_ids = feedback_paper_ids(conn)
        elif payload["scope"] == "all":
            paper_ids = all_paper_ids(conn)
        else:
            paper_ids = recent_paper_ids(conn, int(payload["limit"]))
    finally:
        conn.close()
    count = reclassify_paper_ids_with_progress(settings, paper_ids, progress=progress)
    if progress:
        progress("pipeline.report.writing", "Publishing the latest report.")
    report = regenerate_latest_report(settings)
    return {"reclassified": count, "report": report}


def _feedback_paper_ids_for_apply(settings: Settings, feedback_ids: list[int]) -> list[int]:
    conn = connect(settings.database_path)
    try:
        return paper_ids_for_feedback_ids(conn, feedback_ids)
    finally:
        conn.close()


def mount_static_app(app: FastAPI) -> None:
    dist_dir = load_settings().web_dist_dir
    assets_dir = dist_dir / "assets"
    if assets_dir.exists():
        app.mount("/assets", StaticFiles(directory=assets_dir), name="assets")

    @app.get("/{full_path:path}", response_model=None)
    def spa_entry(full_path: str):
        if full_path.startswith("api/"):
            raise HTTPException(status_code=404, detail="API route not found.")
        requested = (dist_dir / full_path).resolve()
        if (
            full_path
            and requested.exists()
            and requested.is_file()
            and dist_dir.resolve() in requested.parents
        ):
            return FileResponse(requested)
        index = dist_dir / "index.html"
        if index.exists():
            return FileResponse(index)
        return HTMLResponse(
            (
                f"<h1>{APP_PUBLIC_NAME}</h1>"
                "<p>Frontend build not found. "
                "Build the web assets before packaging this release.</p>"
            ),
            status_code=200,
        )


app = create_app()
