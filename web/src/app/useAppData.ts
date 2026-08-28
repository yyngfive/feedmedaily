import React from "react";
import {flushSync} from "react-dom";

import {
  fetchAppMeta,
  fetchAppUpdate,
  fetchCurrentProfile,
  fetchFeedSubscriptions,
  fetchFeedback,
  fetchJobs,
  fetchLatestReport,
  fetchProfileProposals,
  fetchSchedulerSettings,
  fetchSettingsConfig,
} from "../api/client";
import {EMPTY_REPORT, type ClassificationProfile, type JobInfo} from "../shared/types";
import {messageFromJob} from "./messages";
import type {AppState, RefreshRequestFlags} from "./useAppState";

// 数据编排保持首屏读取与管理数据解耦，并协调后台 job 完成后的刷新。
export function useAppData(state: AppState) {
  const {
    activeJobMessageRef, adminOpen, bootstrapRefreshRef, busy, errorText, feeds, feedsLoaded,
    hydrateEditableFeeds, jobs, jobsHydratedRef, knownJobStateRef, localMutationRef,
    logMarkReadDebug, markReadRequestRef, pendingReadOverridesRef, profile, profileRef,
    pushErrorMessage, queuedRefreshRef, readMutationSubmitting, reportRefreshInflightRef,
    reportRefreshSequenceRef, schedulerSaving, selectedIdRef, setAdminDataLoading,
    setAdminHydrationWarning, setAdminOpen, setAdminTab, setAppMeta, setAppUpdate,
    setAppUpdateChecking, setFeedbackRecords, setFeeds, setFeedsLoaded, setJobs, setMessage,
    setPendingReadOverrides, setProfile, setProfileProposals, setProfileResolved, setReport,
    setReportLoadError, setReportLoading, setScheduler, setSettingsConfig, setClassifierModels,
  } = state;

  const formatAdminHydrationWarning = React.useCallback((areas: string[]) => {
    if (areas.length === 0) return null;
    if (areas.length === 1) return `The paper list is ready, but ${areas[0]} did not finish loading yet.`;
    if (areas.length === 2) return `The paper list is ready, but ${areas[0]} and ${areas[1]} did not finish loading yet.`;
    return `The paper list is ready, but ${areas.slice(0, -1).join(", ")}, and ${areas[areas.length - 1]} did not finish loading yet.`;
  }, []);

  const scheduleRefreshFlags = React.useCallback((flags: Partial<RefreshRequestFlags>, source: string) => {
    const nextFlags: RefreshRequestFlags = {all: Boolean(flags.all), admin: Boolean(flags.admin), review: Boolean(flags.review)};
    if (!localMutationRef.current) return true;
    queuedRefreshRef.current = {
      all: queuedRefreshRef.current.all || nextFlags.all,
      admin: queuedRefreshRef.current.admin || nextFlags.admin,
      review: queuedRefreshRef.current.review || nextFlags.review,
    };
    logMarkReadDebug("refresh.queued", {source, activeMutation: localMutationRef.current, queued: queuedRefreshRef.current});
    return false;
  }, [localMutationRef, logMarkReadDebug, queuedRefreshRef]);

  const refreshProfileGate = React.useCallback(async () => {
    const next = await fetchCurrentProfile();
    setProfile(next.profile);
    setProfileResolved(true);
    profileRef.current = next.profile;
    return next.profile;
  }, [profileRef, setProfile, setProfileResolved]);

  const refreshReport = React.useCallback(async (source = "report") => {
    const refreshId = `report-${++reportRefreshSequenceRef.current}`;
    const startedAt = performance.now();
    const currentSelectedId = selectedIdRef.current;
    const currentPendingReadOverrides = pendingReadOverridesRef.current;
    reportRefreshInflightRef.current += 1;
    setReportLoading(true);
    logMarkReadDebug("report.refresh.started", {
      refreshId, source, selectedId: currentSelectedId,
      displayedSelectedId: markReadRequestRef.current?.plannedNextSelectedId ?? currentSelectedId,
      pendingOverrideIds: Object.keys(currentPendingReadOverrides).map(Number),
    });
    try {
      const next = await fetchLatestReport();
      const nextPapersById = new Map(next.papers.map((paper) => [paper.id, paper]));
      logMarkReadDebug("report.refresh.succeeded", {
        refreshId, source, durationMs: Math.round(performance.now() - startedAt), paperCount: next.papers.length,
        pendingReadStatus: Object.keys(currentPendingReadOverrides).map(Number).map((paperId) => ({paperId, stillUnread: nextPapersById.get(paperId) ? !nextPapersById.get(paperId)?.read_at : null})),
      });
      const confirmedOverrides: Array<{paperId: number; refreshedReadAt: string | null}> = [];
      flushSync(() => {
        setReportLoadError(null);
        setReport(next);
        setPendingReadOverrides((current) => {
          const nextOverrides = {...current};
          let changed = false;
          for (const pendingPaperId of Object.keys(current)) {
            const paperId = Number(pendingPaperId);
            const refreshedPaper = nextPapersById.get(paperId);
            if (!refreshedPaper || !(paperId in nextOverrides) || (nextOverrides[paperId] ?? null) !== (refreshedPaper.read_at ?? null)) continue;
            delete nextOverrides[paperId];
            confirmedOverrides.push({paperId, refreshedReadAt: refreshedPaper.read_at ?? null});
            changed = true;
          }
          return changed ? nextOverrides : current;
        });
      });
      confirmedOverrides.forEach(({paperId, refreshedReadAt}) => logMarkReadDebug("mark-read.override.confirmed", {paperId, refreshId, source, refreshedReadAt}));
      return next;
    } catch (error) {
      setReportLoadError(errorText(error, "Could not load the paper list from the local library."));
      logMarkReadDebug("report.refresh.failed", {refreshId, source, durationMs: Math.round(performance.now() - startedAt), message: errorText(error, "Could not load the paper list from the local library.")});
      throw error;
    } finally {
      reportRefreshInflightRef.current = Math.max(0, reportRefreshInflightRef.current - 1);
      if (reportRefreshInflightRef.current === 0) setReportLoading(false);
    }
  }, [errorText, logMarkReadDebug, markReadRequestRef, pendingReadOverridesRef, reportRefreshInflightRef, reportRefreshSequenceRef, selectedIdRef, setPendingReadOverrides, setReport, setReportLoadError, setReportLoading]);

  const refreshFeedback = React.useCallback(async () => setFeedbackRecords(await fetchFeedback()), [setFeedbackRecords]);
  const refreshFeeds = React.useCallback(async () => {
    setFeeds(hydrateEditableFeeds(await fetchFeedSubscriptions()));
    setFeedsLoaded(true);
  }, [hydrateEditableFeeds, setFeeds, setFeedsLoaded]);
  const refreshAppMeta = React.useCallback(async () => setAppMeta(await fetchAppMeta()), [setAppMeta]);
  const refreshAppUpdate = React.useCallback(async () => setAppUpdate(await fetchAppUpdate()), [setAppUpdate]);
  const handleCheckAppUpdate = React.useCallback(async () => {
    try {
      setAppUpdateChecking(true);
      setAppUpdate(await fetchAppUpdate(true));
    } catch (error) {
      pushErrorMessage("app.update.check.failed", error, "Could not check local update status.");
    } finally {
      setAppUpdateChecking(false);
    }
  }, [pushErrorMessage, setAppUpdate, setAppUpdateChecking]);
  const refreshScheduler = React.useCallback(async () => setScheduler(await fetchSchedulerSettings()), [setScheduler]);
  const refreshConfig = React.useCallback(async () => {
    const next = await fetchSettingsConfig();
    setSettingsConfig(next.fields);
    setClassifierModels(next.classifier_models);
  }, [setClassifierModels, setSettingsConfig]);
  const refreshProposals = React.useCallback(async () => setProfileProposals(await fetchProfileProposals()), [setProfileProposals]);

  React.useEffect(() => {
    if (!adminOpen) return;
    let cancelled = false;
    const pollScheduler = async () => {
      if (schedulerSaving) return;
      try {
        const next = await fetchSchedulerSettings();
        if (!cancelled) setScheduler(next);
      } catch {
        // Admin 初始化已有错误提示；后台轮询保持安静，避免刷屏。
      }
    };
    void pollScheduler();
    const timer = window.setInterval(() => void pollScheduler(), 2500);
    return () => { cancelled = true; window.clearInterval(timer); };
  }, [adminOpen, schedulerSaving, setScheduler]);

  const refreshReviewCore = React.useCallback(async (currentProfile?: ClassificationProfile | null, source = "review-core") => {
    const profileValue = currentProfile ?? profileRef.current;
    if (!profileValue) {
      setReport(EMPTY_REPORT);
      setReportLoadError(null);
      setReportLoading(false);
      return;
    }
    const tasks = [
      {name: "report.load.failed", run: () => refreshReport(`${source}:report`), fallback: "Could not load the paper list from the local library."},
      {name: "feeds.load.failed", run: refreshFeeds, fallback: "Could not load RSS feed settings."},
    ] as const;
    const results = await Promise.allSettled(tasks.map((task) => task.run()));
    results.forEach((result, index) => {
      if (result.status === "rejected") pushErrorMessage(tasks[index].name, result.reason, tasks[index].fallback);
    });
  }, [profileRef, pushErrorMessage, refreshFeeds, refreshReport, setReport, setReportLoadError, setReportLoading]);

  const refreshAdminData = React.useCallback(async () => {
    setAdminDataLoading(true);
    try {
      const tasks = [
        {label: "profile proposals", run: refreshProposals}, {label: "feedback records", run: refreshFeedback},
        {label: "local settings", run: refreshConfig}, {label: "app status", run: refreshAppMeta},
        {label: "update status", run: refreshAppUpdate}, {label: "scheduler status", run: refreshScheduler},
      ] as const;
      const results = await Promise.allSettled(tasks.map((task) => task.run()));
      setAdminHydrationWarning(formatAdminHydrationWarning(results.flatMap((result, index) => result.status === "rejected" ? [tasks[index].label] : [])));
    } finally {
      setAdminDataLoading(false);
    }
  }, [formatAdminHydrationWarning, refreshAppMeta, refreshAppUpdate, refreshConfig, refreshFeedback, refreshProposals, refreshScheduler, setAdminDataLoading, setAdminHydrationWarning]);

  const refreshAll = React.useCallback(async (source = "refresh-all") => {
    try {
      const currentProfile = await refreshProfileGate();
      await Promise.all([refreshAdminData(), currentProfile ? refreshReviewCore(currentProfile, `${source}:review`) : Promise.resolve()]);
    } catch (error) {
      pushErrorMessage("profile.current.load.failed", error, "Could not load the local profile.");
    }
  }, [pushErrorMessage, refreshAdminData, refreshProfileGate, refreshReviewCore]);

  const runScheduledRefresh = React.useCallback((flags: Partial<RefreshRequestFlags>, source: string) => {
    logMarkReadDebug("refresh.requested", {source, flags, pendingPaperId: markReadRequestRef.current?.paperId ?? null});
    if (!scheduleRefreshFlags(flags, source)) return;
    if (flags.all) return void refreshAll(source);
    if (flags.review) void refreshReviewCore(undefined, source);
    if (flags.admin) void refreshAdminData();
  }, [logMarkReadDebug, markReadRequestRef, refreshAdminData, refreshAll, refreshReviewCore, scheduleRefreshFlags]);
  const scheduleDeferredReviewRefresh = React.useCallback((source: string, delayMs = 150) => {
    window.setTimeout(() => runScheduledRefresh({review: true}, source), delayMs);
  }, [runScheduledRefresh]);

  React.useEffect(() => {
    if (readMutationSubmitting) return;
    const queued = queuedRefreshRef.current;
    if (!queued.all && !queued.admin && !queued.review) return;
    queuedRefreshRef.current = {all: false, admin: false, review: false};
    logMarkReadDebug("refresh.flushed", {queued});
    if (queued.all) return void refreshAll("queued-refresh:all");
    if (queued.review) void refreshReviewCore(undefined, "queued-refresh:review");
    if (queued.admin) void refreshAdminData();
  }, [logMarkReadDebug, queuedRefreshRef, readMutationSubmitting, refreshAdminData, refreshAll, refreshReviewCore]);

  React.useEffect(() => {
    let cancelled = false;
    const loadInitialView = async () => {
      try {
        const currentProfile = await refreshProfileGate();
        if (cancelled) return;
        if (currentProfile) void refreshReviewCore(currentProfile);
        void refreshAdminData();
      } catch (error) {
        if (!cancelled) {
          pushErrorMessage("profile.current.load.failed", error, "Could not load the local profile.");
          setProfileResolved(true);
        }
      }
    };
    void loadInitialView();
    return () => { cancelled = true; };
  }, [pushErrorMessage, refreshAdminData, refreshProfileGate, refreshReviewCore, setProfileResolved]);

  React.useEffect(() => {
    if (profile && feedsLoaded && feeds.length === 0) {
      setAdminTab("feeds");
      setAdminOpen(true);
    }
  }, [feeds.length, feedsLoaded, profile, setAdminOpen, setAdminTab]);

  const bootstrapJob = React.useMemo(() => jobs.find((job) => job.job_type === "profile-bootstrap") ?? null, [jobs]);
  const profileProposalJob = React.useMemo(() => jobs.find((job) => job.job_type === "profile-proposal" && (job.status === "queued" || job.status === "running")) ?? null, [jobs]);
  const onboardingBusy = busy || bootstrapJob?.status === "queued" || bootstrapJob?.status === "running";

  React.useEffect(() => {
    if (!bootstrapJob) { bootstrapRefreshRef.current = null; return; }
    const completionKey = `${bootstrapJob.id}:${bootstrapJob.status}:${bootstrapJob.finished_at ?? ""}`;
    if (bootstrapJob.status !== "completed" || bootstrapRefreshRef.current === completionKey) return;
    bootstrapRefreshRef.current = completionKey;
    runScheduledRefresh({all: true}, "bootstrap.complete");
  }, [bootstrapJob, bootstrapRefreshRef, runScheduledRefresh]);

  React.useEffect(() => {
    let cancelled = false;
    const pollJobs = async () => {
      try {
        const serverJobs = await fetchJobs();
        if (cancelled) return;
        let shouldRefresh = false;
        let announcement: JobInfo | null = null;
        let activeAnnouncement: JobInfo | null = null;
        const nextJobState = new Map<string, string>();
        for (const job of serverJobs) {
          const signature = `${job.status}:${job.finished_at ?? job.started_at ?? ""}:${job.message_key ?? ""}:${job.message ?? ""}:${job.error ?? ""}:${job.cancel_requested ?? false}:${job.progress_stage ?? ""}:${job.progress_current ?? ""}:${job.progress_total ?? ""}:${job.progress_percent ?? ""}:${job.progress_label ?? ""}:${job.progress_mode ?? ""}`;
          const previousSignature = knownJobStateRef.current.get(job.id);
          nextJobState.set(job.id, signature);
          if (!activeAnnouncement && (job.status === "queued" || job.status === "running")) activeAnnouncement = job;
          if (!jobsHydratedRef.current || previousSignature === signature || (!profile && job.job_type === "profile-bootstrap")) continue;
          if (job.status === "failed" || job.status === "cancelled") { announcement = job; break; }
          if (job.status === "completed" || !announcement) announcement = job;
        }
        setJobs((current) => {
          const previousStatusById = new Map(current.map((job) => [job.id, job.status]));
          const byId = new Map(current.map((job) => [job.id, job]));
          serverJobs.forEach((job) => {
            const previousStatus = previousStatusById.get(job.id);
            if (job.job_type !== "model-test" && (((previousStatus && previousStatus !== job.status) || (!previousStatus && (job.status === "completed" || job.status === "failed" || job.status === "cancelled") && Boolean(job.finished_at))) && (job.status === "completed" || job.status === "failed" || job.status === "cancelled"))) shouldRefresh = true;
            byId.set(job.id, job);
          });
          return Array.from(byId.values()).sort((left, right) => left.created_at < right.created_at ? 1 : -1);
        });
        knownJobStateRef.current = nextJobState;
        if (jobsHydratedRef.current && announcement) {
          activeJobMessageRef.current = null;
          setMessage(messageFromJob(announcement));
        } else if (!jobsHydratedRef.current && activeAnnouncement) {
          const activeSignature = `${activeAnnouncement.id}:${activeAnnouncement.status}:${activeAnnouncement.message_key ?? ""}:${activeAnnouncement.message ?? ""}:${activeAnnouncement.cancel_requested ?? false}:${activeAnnouncement.progress_stage ?? ""}:${activeAnnouncement.progress_current ?? ""}:${activeAnnouncement.progress_total ?? ""}:${activeAnnouncement.progress_percent ?? ""}:${activeAnnouncement.progress_label ?? ""}:${activeAnnouncement.progress_mode ?? ""}`;
          if (activeJobMessageRef.current !== activeSignature) {
            activeJobMessageRef.current = activeSignature;
            setMessage(messageFromJob(activeAnnouncement));
          }
        } else if (!activeAnnouncement && activeJobMessageRef.current !== null) {
          activeJobMessageRef.current = null;
          setMessage((current) => current?.ttlMs === 0 ? null : current);
        }
        jobsHydratedRef.current = true;
        if (shouldRefresh) runScheduledRefresh(profile ? {review: true, admin: true} : {all: true}, "jobs.refresh");
      } catch (error) {
        pushErrorMessage("app.service.unavailable", error, "Could not load job status.");
      }
    };
    void pollJobs();
    const timer = window.setInterval(() => void pollJobs(), 2500);
    return () => { cancelled = true; window.clearInterval(timer); };
  }, [activeJobMessageRef, jobsHydratedRef, knownJobStateRef, profile, pushErrorMessage, runScheduledRefresh, setJobs, setMessage]);

  return {
    refreshProfileGate, refreshReport, refreshFeedback, refreshFeeds, refreshConfig, refreshProposals,
    refreshReviewCore, refreshAdminData, refreshAll, runScheduledRefresh, scheduleDeferredReviewRefresh,
    handleCheckAppUpdate, bootstrapJob, profileProposalJob, onboardingBusy,
  };
}

export type AppData = ReturnType<typeof useAppData>;
