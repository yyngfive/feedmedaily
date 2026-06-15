import React from "react";
import {flushSync} from "react-dom";

import {
  applyProfileProposal,
  bootstrapProfile,
  createFeedback,
  deleteFeedback,
  deleteSchedulerSettings,
  exitApp,
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
  fetchZoteroCollections,
  launchAdminJob,
  launchProfileProposalGeneration,
  launchReclassifyJob,
  markPaperRead,
  openFeedVerificationInBrowser,
  openAppTarget,
  rejectProfileProposal,
  saveFeedSubscriptions,
  saveCurrentProfile,
  saveSchedulerSettings,
  saveSettingsConfig,
  saveToZotero,
  startFeedVerification,
  submitFeedVerificationXML,
} from "./reportData";
import {
  matchesDateFilter,
  relevanceCounts,
} from "./app/utils";
import {
  createUiMessage,
  messageFromJob,
  type UiMessage,
} from "./app/messages";
import type {
  DateFilter,
  ReadFilter,
  RelevanceFilter,
} from "./app/constants";
import {EMPTY_REPORT} from "./types";
import {AdminPanel, type AdminTab} from "./components/admin/AdminPanel";
import {AppStatusBar} from "./components/common/AppStatusBar";
import {StatusBanner} from "./components/common/StatusBanner";
import {TopBar} from "./components/common/TopBar";
import {FeedbackModal} from "./components/modals/FeedbackModal";
import {ZoteroSaveModal} from "./components/modals/ZoteroSaveModal";
import {Onboarding} from "./components/onboarding/Onboarding";
import {DetailPanel} from "./components/review/DetailPanel";
import {FiltersSidebar} from "./components/review/FiltersSidebar";
import {PaperListSection} from "./components/review/PaperListSection";
import type {
  AppMeta,
  AppUpdate,
  ClassificationProfile,
  FeedSubscription,
  FeedbackRecord,
  JobInfo,
  Paper,
  ProfileProposal,
  Relevance,
  Report,
  SchedulerSettings,
  SettingsConfigField,
  ZoteroCollectionOption,
} from "./types";

declare global {
  interface Window {
    __MARK_READ_DEBUG__?: Array<Record<string, unknown>>;
  }
}

type MarkReadRequest = {
  requestId: string;
  paperId: number;
  originSelectedId: number | null;
  plannedNextSelectedId: number | null;
  startedAt: number;
};

type RefreshRequestFlags = {
  all: boolean;
  admin: boolean;
  review: boolean;
};

type LocalMutation = {
  requestId: string;
  kind: "mark-read" | "feedback-save" | "feedback-delete";
  entityId: number;
  startedAt: number;
};

// 应用根组件负责衔接数据加载、筛选状态、后台任务和三栏式阅读界面。
export function App() {
  const [report, setReport] = React.useState<Report>(EMPTY_REPORT);
  const [profile, setProfile] = React.useState<ClassificationProfile | null>(null);
  const [appMeta, setAppMeta] = React.useState<AppMeta | null>(null);
  const [appUpdate, setAppUpdate] = React.useState<AppUpdate | null>(null);
  const [appUpdateChecking, setAppUpdateChecking] = React.useState(false);
  const [feeds, setFeeds] = React.useState<FeedSubscription[]>([]);
  const [scheduler, setScheduler] = React.useState<SchedulerSettings | null>(null);
  const [settingsConfig, setSettingsConfig] = React.useState<SettingsConfigField[]>([]);
  const [feedsLoaded, setFeedsLoaded] = React.useState(false);
  const [profileResolved, setProfileResolved] = React.useState(false);
  const [reportLoading, setReportLoading] = React.useState(false);
  const [adminDataLoading, setAdminDataLoading] = React.useState(false);
  const [verificationSubmitting, setVerificationSubmitting] = React.useState(false);
  const [verificationSubmitError, setVerificationSubmitError] = React.useState<string | null>(null);
  const [reportLoadError, setReportLoadError] = React.useState<string | null>(null);
  const [adminHydrationWarning, setAdminHydrationWarning] = React.useState<string | null>(null);
  const [message, setMessage] = React.useState<UiMessage | null>(null);
  const [query, setQuery] = React.useState("");
  const [relevance, setRelevance] = React.useState<RelevanceFilter>("direct");
  const [journal, setJournal] = React.useState("all");
  const [dateFilter, setDateFilter] = React.useState<DateFilter>("30d");
  const [readFilter, setReadFilter] = React.useState<ReadFilter>("unread");
  const [markReadRequest, setMarkReadRequest] = React.useState<MarkReadRequest | null>(null);
  const [pendingReadOverrides, setPendingReadOverrides] = React.useState<Record<number, string>>({});
  const [selectedId, setSelectedId] = React.useState<number | null>(null);
  const [feedbackRecords, setFeedbackRecords] = React.useState<FeedbackRecord[]>([]);
  const [profileProposals, setProfileProposals] = React.useState<ProfileProposal[]>([]);
  const [jobs, setJobs] = React.useState<JobInfo[]>([]);
  const [adminOpen, setAdminOpen] = React.useState(false);
  const [feedbackPaper, setFeedbackPaper] = React.useState<Paper | null>(null);
  const [feedbackValue, setFeedbackValue] = React.useState<Relevance>("indirect");
  const [feedbackNote, setFeedbackNote] = React.useState("");
  const [feedsSaving, setFeedsSaving] = React.useState(false);
  const [schedulerSaving, setSchedulerSaving] = React.useState(false);
  const [settingsConfigSaving, setSettingsConfigSaving] = React.useState(false);
  const [profileSaving, setProfileSaving] = React.useState(false);
  const [zoteroPaper, setZoteroPaper] = React.useState<Paper | null>(null);
  const [zoteroCollections, setZoteroCollections] = React.useState<ZoteroCollectionOption[]>([]);
  const [zoteroCollectionKey, setZoteroCollectionKey] = React.useState("");
  const [zoteroLoading, setZoteroLoading] = React.useState(false);
  const [zoteroSaving, setZoteroSaving] = React.useState(false);
  const [zoteroError, setZoteroError] = React.useState<string | null>(null);
  const [busy, setBusy] = React.useState(false);
  const [appControlBusy, setAppControlBusy] = React.useState(false);
  const [adminTab, setAdminTab] = React.useState<AdminTab>("config");
  const [themePreference, setThemePreference] = React.useState<"system" | "light" | "dark">(
    "system",
  );
  const [systemTheme, setSystemTheme] = React.useState<"light" | "dark">("light");
  const knownJobStateRef = React.useRef(new Map<string, string>());
  const jobsHydratedRef = React.useRef(false);
  const bootstrapRefreshRef = React.useRef<string | null>(null);
  const activeJobMessageRef = React.useRef<string | null>(null);
  const queuedRefreshRef = React.useRef<RefreshRequestFlags>({all: false, admin: false, review: false});
  const markReadRequestRef = React.useRef<MarkReadRequest | null>(null);
  const localMutationRef = React.useRef<LocalMutation | null>(null);
  const profileRef = React.useRef<ClassificationProfile | null>(null);
  const pendingReadOverridesRef = React.useRef<Record<number, string>>({});
  const selectedIdRef = React.useRef<number | null>(null);
  const markReadSequenceRef = React.useRef(0);
  const feedbackMutationSequenceRef = React.useRef(0);
  const reportRefreshSequenceRef = React.useRef(0);
  const reportRefreshInflightRef = React.useRef(0);
  const deferredQuery = React.useDeferredValue(query);
  const resolvedTheme = themePreference === "system" ? systemTheme : themePreference;
  const markReadDebugEnabled = React.useMemo(() => {
    try {
      return new URLSearchParams(window.location.search).get("mark-read-debug") === "1";
    } catch {
      return false;
    }
  }, []);
  const markReadSubmitting = markReadRequest != null;

  const hydrateEditableFeeds = React.useCallback((items: FeedSubscription[]) =>
    items.map((item) => ({
      ...item,
      client_id:
        item.client_id ??
        (typeof crypto !== "undefined" && "randomUUID" in crypto
          ? crypto.randomUUID()
          : `${Date.now()}-${Math.random()}`),
    })), []);

  const pushMessage = React.useCallback((
    name: string,
    override?: {text?: string; tone?: UiMessage["tone"]},
  ) => {
    setMessage(createUiMessage(name, override));
  }, []);

  React.useEffect(() => {
    if (!message || message.ttlMs <= 0) {
      return;
    }
    const timer = window.setTimeout(() => {
      setMessage((current) => (current?.createdAt === message.createdAt ? null : current));
    }, message.ttlMs);
    return () => window.clearTimeout(timer);
  }, [message]);

  // 管理主题来源：默认跟随系统，用户手动切换后写入本地覆盖。
  React.useEffect(() => {
    const saved = window.localStorage.getItem("feedmedaily-theme");
    if (saved === "light" || saved === "dark" || saved === "system") {
      setThemePreference(saved);
    }
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const applySystemTheme = () => setSystemTheme(media.matches ? "dark" : "light");
    applySystemTheme();
    media.addEventListener("change", applySystemTheme);
    return () => media.removeEventListener("change", applySystemTheme);
  }, []);

  React.useEffect(() => {
    document.documentElement.dataset.theme = resolvedTheme;
  }, [resolvedTheme]);

  React.useEffect(() => {
    window.localStorage.setItem("feedmedaily-theme", themePreference);
  }, [themePreference]);

  React.useEffect(() => {
    markReadRequestRef.current = markReadRequest;
  }, [markReadRequest]);

  React.useEffect(() => {
    profileRef.current = profile;
  }, [profile]);

  React.useEffect(() => {
    pendingReadOverridesRef.current = pendingReadOverrides;
  }, [pendingReadOverrides]);

  React.useEffect(() => {
    selectedIdRef.current = selectedId;
  }, [selectedId]);

  const logMarkReadDebug = React.useCallback((
    event: string,
    payload: Record<string, unknown> = {},
  ) => {
    if (!markReadDebugEnabled) {
      return;
    }
    const entry = {
      at: new Date().toISOString(),
      event,
      ...payload,
    };
    const history = window.__MARK_READ_DEBUG__ ?? [];
    history.push(entry);
    window.__MARK_READ_DEBUG__ = history.slice(-100);
    console.info("[mark-read-debug]", entry);
  }, [markReadDebugEnabled]);

  const scheduleRefreshFlags = React.useCallback((
    flags: Partial<RefreshRequestFlags>,
    source: string,
  ) => {
    const nextFlags: RefreshRequestFlags = {
      all: Boolean(flags.all),
      admin: Boolean(flags.admin),
      review: Boolean(flags.review),
    };
    if (localMutationRef.current) {
      queuedRefreshRef.current = {
        all: queuedRefreshRef.current.all || nextFlags.all,
        admin: queuedRefreshRef.current.admin || nextFlags.admin,
        review: queuedRefreshRef.current.review || nextFlags.review,
      };
      logMarkReadDebug("refresh.queued", {
        source,
        activeMutation: localMutationRef.current,
        queued: queuedRefreshRef.current,
      });
      return false;
    }
    return true;
  }, [logMarkReadDebug]);

  const beginLocalMutation = React.useCallback((
    mutation: LocalMutation,
  ) => {
    localMutationRef.current = mutation;
    logMarkReadDebug("mutation.started", mutation);
  }, [logMarkReadDebug]);

  const endLocalMutation = React.useCallback((requestId: string) => {
    if (localMutationRef.current?.requestId !== requestId) {
      return;
    }
    logMarkReadDebug("mutation.finished", localMutationRef.current);
    localMutationRef.current = null;
  }, [logMarkReadDebug]);

  const errorText = React.useCallback((error: unknown, fallback: string) => {
    if (error instanceof Error && error.message.trim()) {
      return error.message;
    }
    return fallback;
  }, []);

  const pushErrorMessage = React.useCallback((
    name: string,
    error: unknown,
    fallback: string,
  ) => {
    pushMessage(name, {text: errorText(error, fallback), tone: "danger"});
  }, [errorText, pushMessage]);

  const formatAdminHydrationWarning = React.useCallback((areas: string[]) => {
    if (areas.length === 0) {
      return null;
    }
    if (areas.length === 1) {
      return `The paper list is ready, but ${areas[0]} did not finish loading yet.`;
    }
    if (areas.length === 2) {
      return `The paper list is ready, but ${areas[0]} and ${areas[1]} did not finish loading yet.`;
    }
    const prefix = areas.slice(0, -1).join(", ");
    return `The paper list is ready, but ${prefix}, and ${areas[areas.length - 1]} did not finish loading yet.`;
  }, []);

  const refreshProfileGate = React.useCallback(async () => {
    const next = await fetchCurrentProfile();
    setProfile(next.profile);
    setProfileResolved(true);
    profileRef.current = next.profile;
    return next.profile;
  }, []);

  const refreshReport = React.useCallback(async (source = "report") => {
    const refreshId = `report-${++reportRefreshSequenceRef.current}`;
    const startedAt = performance.now();
    const currentSelectedId = selectedIdRef.current;
    const currentPendingReadOverrides = pendingReadOverridesRef.current;
    reportRefreshInflightRef.current += 1;
    setReportLoading(true);
    logMarkReadDebug("report.refresh.started", {
      refreshId,
      source,
      selectedId: currentSelectedId,
      displayedSelectedId: markReadRequestRef.current?.plannedNextSelectedId ?? currentSelectedId,
      pendingOverrideIds: Object.keys(currentPendingReadOverrides).map((value) => Number(value)),
    });
    try {
      const next = await fetchLatestReport();
      const nextPapersById = new Map(next.papers.map((paper) => [paper.id, paper]));
      const pendingPaperEntries = Object.keys(currentPendingReadOverrides).map((value) => Number(value));
      const pendingReadStatus = pendingPaperEntries.map((paperId) => {
        const paper = nextPapersById.get(paperId) ?? null;
        return {
          paperId,
          stillUnread: paper ? !paper.read_at : null,
        };
      });
      logMarkReadDebug("report.refresh.succeeded", {
        refreshId,
        source,
        durationMs: Math.round(performance.now() - startedAt),
        paperCount: next.papers.length,
        pendingReadStatus,
      });
      const confirmedOverrides: Array<{paperId: number; refreshedReadAt: string}> = [];
      flushSync(() => {
        setReportLoadError(null);
        setReport(next);
        setPendingReadOverrides((current) => {
          const pendingPaperIds = Object.keys(current);
          if (pendingPaperIds.length === 0) {
            return current;
          }
          let changed = false;
          const nextOverrides = {...current};
          for (const pendingPaperId of pendingPaperIds) {
            const paperId = Number(pendingPaperId);
            const refreshedPaper = nextPapersById.get(paperId) ?? null;
            if (!refreshedPaper?.read_at) {
              continue;
            }
            if (!(paperId in nextOverrides)) {
              continue;
            }
            delete nextOverrides[paperId];
            confirmedOverrides.push({
              paperId,
              refreshedReadAt: refreshedPaper.read_at,
            });
            changed = true;
          }
          return changed ? nextOverrides : current;
        });
      });
      confirmedOverrides.forEach(({paperId, refreshedReadAt}) => {
        logMarkReadDebug("mark-read.override.confirmed", {
          paperId,
          refreshId,
          source,
          refreshedReadAt,
        });
      });
      return next;
    } catch (error) {
      setReportLoadError(errorText(error, "Could not load the paper list from the local library."));
      logMarkReadDebug("report.refresh.failed", {
        refreshId,
        source,
        durationMs: Math.round(performance.now() - startedAt),
        message: errorText(error, "Could not load the paper list from the local library."),
      });
      throw error;
    } finally {
      reportRefreshInflightRef.current = Math.max(0, reportRefreshInflightRef.current - 1);
      if (reportRefreshInflightRef.current === 0) {
        setReportLoading(false);
      }
    }
  }, [errorText, logMarkReadDebug]);

  const refreshFeedback = React.useCallback(async () => {
    setFeedbackRecords(await fetchFeedback());
  }, []);

  const refreshFeeds = React.useCallback(async () => {
    const nextFeeds = await fetchFeedSubscriptions();
    setFeeds(hydrateEditableFeeds(nextFeeds));
    setFeedsLoaded(true);
  }, [hydrateEditableFeeds]);

  const refreshAppMeta = React.useCallback(async () => {
    setAppMeta(await fetchAppMeta());
  }, []);

  const refreshAppUpdate = React.useCallback(async () => {
    setAppUpdate(await fetchAppUpdate());
  }, []);

  const handleCheckAppUpdate = React.useCallback(async () => {
    try {
      setAppUpdateChecking(true);
      setAppUpdate(await fetchAppUpdate(true));
    } catch (error) {
      pushErrorMessage("app.update.check.failed", error, "Could not check local update status.");
    } finally {
      setAppUpdateChecking(false);
    }
  }, [pushErrorMessage]);

  const refreshScheduler = React.useCallback(async () => {
    setScheduler(await fetchSchedulerSettings());
  }, []);

  const refreshConfig = React.useCallback(async () => {
    const nextConfig = await fetchSettingsConfig();
    setSettingsConfig(nextConfig.fields);
  }, []);

  const refreshProposals = React.useCallback(async () => {
    setProfileProposals(await fetchProfileProposals());
  }, []);

  const refreshReviewCore = React.useCallback(async (
    currentProfile?: ClassificationProfile | null,
    source = "review-core",
  ) => {
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
    for (const [index, result] of results.entries()) {
      if (result.status === "rejected") {
        const task = tasks[index];
        pushErrorMessage(task.name, result.reason, task.fallback);
      }
    }
  }, [pushErrorMessage, refreshFeeds, refreshReport]);

  const refreshAdminData = React.useCallback(async () => {
    setAdminDataLoading(true);
    try {
      const tasks = [
        {label: "profile proposals", run: refreshProposals},
        {label: "feedback records", run: refreshFeedback},
        {label: "local settings", run: refreshConfig},
        {label: "app status", run: refreshAppMeta},
        {label: "update status", run: refreshAppUpdate},
        {label: "scheduler status", run: refreshScheduler},
      ] as const;
      const results = await Promise.allSettled(tasks.map((task) => task.run()));
      const failedAreas = results.flatMap((result, index) =>
        result.status === "rejected" ? [tasks[index].label] : [],
      );
      setAdminHydrationWarning(formatAdminHydrationWarning(failedAreas));
      if (failedAreas.length === 0) {
        setAdminHydrationWarning(null);
      }
    } finally {
      setAdminDataLoading(false);
    }
  }, [
    formatAdminHydrationWarning,
    refreshAppMeta,
    refreshAppUpdate,
    refreshConfig,
    refreshFeedback,
    refreshProposals,
    refreshScheduler,
  ]);

  const refreshAll = React.useCallback(async (source = "refresh-all") => {
    try {
      const currentProfile = await refreshProfileGate();
      await Promise.all([
        refreshAdminData(),
        currentProfile ? refreshReviewCore(currentProfile, `${source}:review`) : Promise.resolve(),
      ]);
    } catch (error) {
      pushErrorMessage("profile.current.load.failed", error, "Could not load the local profile.");
    }
  }, [pushErrorMessage, refreshAdminData, refreshProfileGate, refreshReviewCore]);

  const runScheduledRefresh = React.useCallback((
    flags: Partial<RefreshRequestFlags>,
    source: string,
  ) => {
    logMarkReadDebug("refresh.requested", {
      source,
      flags,
      pendingPaperId: markReadRequestRef.current?.paperId ?? null,
    });
    if (!scheduleRefreshFlags(flags, source)) {
      return;
    }
    if (flags.all) {
      void refreshAll(source);
      return;
    }
    if (flags.review) {
      void refreshReviewCore(undefined, source);
    }
    if (flags.admin) {
      void refreshAdminData();
    }
  }, [logMarkReadDebug, refreshAdminData, refreshAll, refreshReviewCore, scheduleRefreshFlags]);

  const scheduleDeferredReviewRefresh = React.useCallback((source: string, delayMs = 150) => {
    window.setTimeout(() => {
      runScheduledRefresh({review: true}, source);
    }, delayMs);
  }, [runScheduledRefresh]);

  React.useEffect(() => {
    if (markReadSubmitting) {
      return;
    }
    const queued = queuedRefreshRef.current;
    if (!queued.all && !queued.admin && !queued.review) {
      return;
    }
    queuedRefreshRef.current = {all: false, admin: false, review: false};
    logMarkReadDebug("refresh.flushed", {queued});
    if (queued.all) {
      void refreshAll("queued-refresh:all");
      return;
    }
    if (queued.review) {
      void refreshReviewCore(undefined, "queued-refresh:review");
    }
    if (queued.admin) {
      void refreshAdminData();
    }
  }, [logMarkReadDebug, markReadSubmitting, refreshAdminData, refreshAll, refreshReviewCore]);

  React.useEffect(() => {
    let cancelled = false;

    const loadInitialView = async () => {
      try {
        const currentProfile = await refreshProfileGate();
        if (cancelled) {
          return;
        }
        if (currentProfile) {
          void refreshReviewCore(currentProfile);
        }
        void refreshAdminData();
      } catch (error) {
        if (!cancelled) {
          pushErrorMessage("profile.current.load.failed", error, "Could not load the local profile.");
          setProfileResolved(true);
        }
      }
    };

    void loadInitialView();
    return () => {
      cancelled = true;
    };
  }, [pushErrorMessage, refreshAdminData, refreshProfileGate, refreshReviewCore]);

  React.useEffect(() => {
    if (profile && feedsLoaded && feeds.length === 0) {
      setAdminTab("feeds");
      setAdminOpen(true);
    }
  }, [feeds.length, feedsLoaded, profile]);

  const bootstrapJob = React.useMemo(
    () => jobs.find((job) => job.job_type === "profile-bootstrap") ?? null,
    [jobs],
  );
  const profileProposalJob = React.useMemo(
    () =>
      jobs.find(
        (job) =>
          job.job_type === "profile-proposal" &&
          (job.status === "queued" || job.status === "running"),
      ) ?? null,
    [jobs],
  );
  const onboardingBusy =
    busy ||
    bootstrapJob?.status === "queued" ||
    bootstrapJob?.status === "running";

  React.useEffect(() => {
    if (!bootstrapJob) {
      bootstrapRefreshRef.current = null;
      return;
    }
    const completionKey = `${bootstrapJob.id}:${bootstrapJob.status}:${bootstrapJob.finished_at ?? ""}`;
    if (bootstrapJob.status !== "completed" || bootstrapRefreshRef.current === completionKey) {
      return;
    }
    bootstrapRefreshRef.current = completionKey;
    runScheduledRefresh({all: true}, "bootstrap.complete");
  }, [bootstrapJob, runScheduledRefresh]);

  React.useEffect(() => {
    let cancelled = false;

    const pollJobs = async () => {
      try {
        const serverJobs = await fetchJobs();
        if (cancelled) {
          return;
        }

        let shouldRefresh = false;
        let announcement: JobInfo | null = null;
        let activeAnnouncement: JobInfo | null = null;
        const nextJobState = new Map<string, string>();
        for (const job of serverJobs) {
          const signature = `${job.status}:${job.finished_at ?? job.started_at ?? ""}:${job.message_key ?? ""}:${job.message ?? ""}:${job.error ?? ""}:${job.progress_stage ?? ""}:${job.progress_current ?? ""}:${job.progress_total ?? ""}:${job.progress_percent ?? ""}:${job.progress_label ?? ""}:${job.progress_mode ?? ""}`;
          const previousSignature = knownJobStateRef.current.get(job.id);
          nextJobState.set(job.id, signature);
          if (
            activeAnnouncement == null &&
            (job.status === "queued" || job.status === "running")
          ) {
            activeAnnouncement = job;
          }
          if (!jobsHydratedRef.current || previousSignature === signature) {
            continue;
          }
          if (!profile && job.job_type === "profile-bootstrap") {
            continue;
          }
          if (job.status === "failed") {
            announcement = job;
            break;
          }
          if (job.status === "completed") {
            announcement = job;
            continue;
          }
          if (announcement == null) {
            announcement = job;
          }
        }
        setJobs((current) => {
          const previousStatusById = new Map(current.map((job) => [job.id, job.status]));
          const byId = new Map(current.map((job) => [job.id, job]));
          serverJobs.forEach((job) => {
            const previousStatus = previousStatusById.get(job.id);
            if (
              (
                (previousStatus && previousStatus !== job.status) ||
                (!previousStatus &&
                  (job.status === "completed" || job.status === "failed") &&
                  Boolean(job.finished_at))
              ) &&
              (job.status === "completed" || job.status === "failed")
            ) {
              shouldRefresh = true;
            }
            byId.set(job.id, job);
          });
          return Array.from(byId.values()).sort((left, right) =>
            left.created_at < right.created_at ? 1 : -1,
          );
        });
        knownJobStateRef.current = nextJobState;
        if (jobsHydratedRef.current && announcement) {
          activeJobMessageRef.current = null;
          setMessage(messageFromJob(announcement));
        } else if (!jobsHydratedRef.current && activeAnnouncement) {
          const activeSignature = `${activeAnnouncement.id}:${activeAnnouncement.status}:${activeAnnouncement.message_key ?? ""}:${activeAnnouncement.message ?? ""}:${activeAnnouncement.progress_stage ?? ""}:${activeAnnouncement.progress_current ?? ""}:${activeAnnouncement.progress_total ?? ""}:${activeAnnouncement.progress_percent ?? ""}:${activeAnnouncement.progress_label ?? ""}:${activeAnnouncement.progress_mode ?? ""}`;
          if (activeJobMessageRef.current !== activeSignature) {
            activeJobMessageRef.current = activeSignature;
            setMessage(messageFromJob(activeAnnouncement));
          }
        } else if (!activeAnnouncement && activeJobMessageRef.current !== null) {
          activeJobMessageRef.current = null;
          setMessage((current) => (current?.ttlMs === 0 ? null : current));
        }
        jobsHydratedRef.current = true;

        if (shouldRefresh) {
          if (profile) {
            runScheduledRefresh({review: true, admin: true}, "jobs.refresh");
          } else {
            runScheduledRefresh({all: true}, "jobs.refresh");
          }
        }
      } catch (error) {
        pushErrorMessage("app.service.unavailable", error, "Could not load job status.");
      }
    };

    void pollJobs();
    const timer = window.setInterval(() => {
      void pollJobs();
    }, 2500);

    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [profile, pushErrorMessage, runScheduledRefresh]);

  const effectivePapers = React.useMemo(
    () =>
      report.papers.map((paper) =>
        pendingReadOverrides[paper.id]
          ? {...paper, read_at: pendingReadOverrides[paper.id]}
          : paper,
      ),
    [pendingReadOverrides, report.papers],
  );

  const journals = React.useMemo(
    () => Array.from(new Set(effectivePapers.map((paper) => paper.journal).filter(Boolean) as string[])).sort(),
    [effectivePapers],
  );
  const journalOptions = React.useMemo(
    () => [{value: "all", label: "All journals"}, ...journals.map((item) => ({value: item, label: item}))],
    [journals],
  );

  const filteredBase = React.useMemo(
    () =>
      effectivePapers.filter((paper) => {
        const haystack = [
          paper.title,
          paper.classification.translated_title_zh ?? "",
          paper.abstract ?? "",
          paper.journal ?? "",
          paper.authors?.join(" ") ?? "",
          paper.feedback_status?.note ?? "",
        ]
          .join(" ")
          .toLowerCase();
        const matchesQuery = !deferredQuery || haystack.includes(deferredQuery.toLowerCase());
        const matchesJournal = journal === "all" || paper.journal === journal;
        const dateValue = paper.published_date ?? paper.seen_date;
        const matchesRead =
          readFilter === "all" ||
          (readFilter === "read" ? Boolean(paper.read_at) : !paper.read_at);
        const matchesDate = matchesDateFilter(dateValue, report.report_date, dateFilter);
        return matchesQuery && matchesJournal && matchesRead && matchesDate;
      }),
    [dateFilter, deferredQuery, effectivePapers, journal, readFilter, report.report_date],
  );

  const filtered = React.useMemo(
    () =>
      filteredBase.filter(
        (paper) => relevance === "all" || paper.classification.relevance === relevance,
      ),
    [filteredBase, relevance],
  );
  const lastUpdateLabel = React.useMemo(
    () => report.last_updated_at?.slice(0, 10) ?? "Never",
    [report.last_updated_at],
  );

  const needsFeedSetup = Boolean(profile && feedsLoaded && feeds.length === 0);
  const hasNoFetchedPapers =
    feedsLoaded &&
    !reportLoading &&
    !reportLoadError &&
    !needsFeedSetup &&
    feeds.length > 0 &&
    report.papers.length === 0;
  const visibleBase = React.useMemo(
    () => (needsFeedSetup ? [] : filteredBase),
    [filteredBase, needsFeedSetup],
  );
  const visibleList = React.useMemo(
    () => (needsFeedSetup ? [] : filtered),
    [filtered, needsFeedSetup],
  );
  const visibleTotals = React.useMemo(() => relevanceCounts(visibleBase), [visibleBase]);

  React.useEffect(() => {
    if (markReadSubmitting) {
      return;
    }
    if (visibleList.length === 0) {
      setSelectedId(null);
      return;
    }
    if (!selectedId || !visibleList.some((paper) => paper.id === selectedId)) {
      setSelectedId(visibleList[0].id);
    }
  }, [markReadSubmitting, selectedId, visibleList]);

  React.useEffect(() => {
    if (!zoteroPaper) {
      return;
    }
    let cancelled = false;
    setZoteroLoading(true);
    setZoteroError(null);
    setZoteroCollections([]);
    setZoteroCollectionKey("");

    void fetchZoteroCollections()
      .then((payload) => {
        if (cancelled) {
          return;
        }
        setZoteroCollections(payload.collections);
        setZoteroCollectionKey(payload.default_collection_key ?? "");
      })
      .catch((error) => {
        if (cancelled) {
          return;
        }
        setZoteroError((error as Error).message);
      })
      .finally(() => {
        if (!cancelled) {
          setZoteroLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [zoteroPaper]);

  const selectedPaperId = React.useMemo(() => {
    if (!markReadRequest) {
      return selectedId;
    }
    if (
      selectedId != null &&
      selectedId !== markReadRequest.originSelectedId &&
      selectedId !== markReadRequest.paperId
    ) {
      return selectedId;
    }
    return markReadRequest.paperId;
  }, [markReadRequest, selectedId]);

  const selectedPaper = React.useMemo(() => {
    if (needsFeedSetup || selectedPaperId == null) {
      return null;
    }
    const visiblePaper = visibleList.find((paper) => paper.id === selectedPaperId);
    if (visiblePaper) {
      return visiblePaper;
    }
    return effectivePapers.find((paper) => paper.id === selectedPaperId) ?? null;
  }, [effectivePapers, needsFeedSetup, selectedPaperId, visibleList]);

  React.useEffect(() => {
    logMarkReadDebug("selection.changed", {
      selectedId,
      displayedSelectedId: selectedPaperId,
      selectedPaperId: selectedPaper?.id ?? null,
      requestId: markReadRequest?.requestId ?? null,
    });
  }, [logMarkReadDebug, markReadRequest?.requestId, selectedId, selectedPaper?.id, selectedPaperId]);

  React.useEffect(() => {
    logMarkReadDebug("visible-list.changed", {
      readFilter,
      count: visibleList.length,
      firstPaperIds: visibleList.slice(0, 8).map((paper) => paper.id),
      overrideIds: Object.keys(pendingReadOverrides).map((value) => Number(value)),
    });
  }, [logMarkReadDebug, pendingReadOverrides, readFilter, visibleList]);

  const updatePaper = (paperId: number, updater: (paper: Paper) => Paper) => {
    setReport((current) => ({
      ...current,
      papers: current.papers.map((paper) => (paper.id === paperId ? updater(paper) : paper)),
    }));
  };

  const applyFeedbackRecordToPaper = React.useCallback((record: FeedbackRecord) => {
    updatePaper(record.paper_id, (paper) => ({
      ...paper,
      feedback_status: {
        has_feedback: true,
        corrected_relevance: record.corrected_relevance,
        note: record.note ?? null,
        latest_feedback_at: record.created_at,
        state: record.state,
        used_in_profile: record.used_in_profile,
      },
    }));
  }, []);

  const clearPaperFeedbackStatus = React.useCallback((paperId: number) => {
    updatePaper(paperId, (paper) => ({
      ...paper,
      feedback_status: null,
    }));
  }, []);

  const persistReadStatus = async (paperId: number) => {
    const paper = effectivePapers.find((item) => item.id === paperId);
    if (!paper || paper.read_at || markReadRequest != null) {
      return;
    }
    const requestId = `mark-read-${++markReadSequenceRef.current}`;
    const visibleIds = visibleList.map((item) => item.id);
    const currentIndex = visibleIds.indexOf(paperId);
    let plannedNextSelectedId: number | null = paperId;
    if (readFilter === "unread" && currentIndex >= 0) {
      if (currentIndex + 1 < visibleIds.length) {
        plannedNextSelectedId = visibleIds[currentIndex + 1];
      } else if (currentIndex - 1 >= 0) {
        plannedNextSelectedId = visibleIds[currentIndex - 1];
      } else {
        plannedNextSelectedId = null;
      }
    }
    const request: MarkReadRequest = {
      requestId,
      paperId,
      originSelectedId: selectedId,
      plannedNextSelectedId,
      startedAt: performance.now(),
    };
    beginLocalMutation({
      requestId,
      kind: "mark-read",
      entityId: paperId,
      startedAt: request.startedAt,
    });
    logMarkReadDebug("mark-read.clicked", {
      requestId,
      paperId,
      readFilter,
      selectedId,
      plannedNextSelectedId,
      visibleIds: visibleIds.slice(0, 12),
    });
    setMarkReadRequest(request);
    let succeeded = false;
    try {
      logMarkReadDebug("mark-read.request.started", {
        requestId,
        paperId,
      });
      const status = await markPaperRead(paperId);
      logMarkReadDebug("mark-read.request.succeeded", {
        requestId,
        paperId,
        durationMs: Math.round(performance.now() - request.startedAt),
        readAt: status.read_at,
      });
      succeeded = true;
      flushSync(() => {
        setPendingReadOverrides((current) => ({
          ...current,
          [paperId]: status.read_at,
        }));
        setSelectedId((current) => {
          if (
            current != null &&
            current !== request.originSelectedId &&
            current !== request.paperId
          ) {
            return current;
          }
          return request.plannedNextSelectedId;
        });
        setMarkReadRequest((current) => (current?.requestId === requestId ? null : current));
      });
    } catch (error) {
      logMarkReadDebug("mark-read.request.failed", {
        requestId,
        paperId,
        durationMs: Math.round(performance.now() - request.startedAt),
        message: errorText(error, "Could not update read status."),
      });
      flushSync(() => {
        setMarkReadRequest((current) => (current?.requestId === requestId ? null : current));
      });
      pushMessage("paper.read.failed", {text: (error as Error).message, tone: "danger"});
    } finally {
      endLocalMutation(requestId);
      if (succeeded) {
        scheduleDeferredReviewRefresh(`mark-read:${requestId}:reconcile`);
      }
    }
  };

  const openZoteroModal = (paper: Paper) => {
    setZoteroPaper(paper);
    setZoteroError(null);
  };

  const handleSaveToZotero = async () => {
    if (!zoteroPaper) {
      return;
    }
    try {
      setZoteroSaving(true);
      const status = await saveToZotero(zoteroPaper.id, zoteroCollectionKey || null);
      updatePaper(zoteroPaper.id, (current) => ({...current, zotero_status: status}));
      if (status.saved) {
        pushMessage("zotero.save.succeeded");
        setZoteroPaper(null);
        setZoteroError(null);
        return;
      }
      setZoteroError(status.last_error ?? "Zotero save updated.");
    } catch (error) {
      setZoteroError((error as Error).message);
      pushMessage("app.service.unavailable", {text: errorText(error, "Could not save to Zotero."), tone: "danger"});
    } finally {
      setZoteroSaving(false);
    }
  };

  const openFeedbackModal = (paper: Paper) => {
    setFeedbackPaper(paper);
    setFeedbackValue(paper.feedback_status?.corrected_relevance ?? paper.classification.relevance);
    setFeedbackNote(paper.feedback_status?.note ?? "");
  };

  const submitFeedback = async () => {
    if (!feedbackPaper) {
      return;
    }
    const requestId = `feedback-save-${++feedbackMutationSequenceRef.current}`;
    const startedAt = performance.now();
    beginLocalMutation({
      requestId,
      kind: "feedback-save",
      entityId: feedbackPaper.id,
      startedAt,
    });
    logMarkReadDebug("feedback.save.started", {
      requestId,
      paperId: feedbackPaper.id,
      correctedRelevance: feedbackValue,
    });
    let succeeded = false;
    try {
      const record = await createFeedback({
        paper_id: feedbackPaper.id,
        corrected_relevance: feedbackValue,
        note: feedbackNote.trim() || undefined,
      });
      logMarkReadDebug("feedback.save.succeeded", {
        requestId,
        paperId: record.paper_id,
        feedbackId: record.id,
        durationMs: Math.round(performance.now() - startedAt),
      });
      succeeded = true;
      flushSync(() => {
        setFeedbackPaper(null);
        setFeedbackNote("");
        applyFeedbackRecordToPaper(record);
        setFeedbackRecords((current) => [record, ...current.filter((item) => item.id !== record.id)]);
      });
      void refreshFeedback();
      scheduleDeferredReviewRefresh("feedback.save");
      pushMessage("feedback.save.succeeded");
    } catch (error) {
      logMarkReadDebug("feedback.save.failed", {
        requestId,
        paperId: feedbackPaper.id,
        durationMs: Math.round(performance.now() - startedAt),
        message: errorText(error, "Could not save feedback."),
      });
      pushErrorMessage("feedback.save.failed", error, "Could not save feedback.");
    } finally {
      endLocalMutation(requestId);
    }
  };

  const handleDeleteFeedback = async (feedbackId: number) => {
    const existingRecord = feedbackRecords.find((item) => item.id === feedbackId) ?? null;
    const requestId = `feedback-delete-${++feedbackMutationSequenceRef.current}`;
    const startedAt = performance.now();
    beginLocalMutation({
      requestId,
      kind: "feedback-delete",
      entityId: feedbackId,
      startedAt,
    });
    logMarkReadDebug("feedback.delete.started", {
      requestId,
      feedbackId,
      paperId: existingRecord?.paper_id ?? null,
    });
    try {
      await deleteFeedback(feedbackId);
      logMarkReadDebug("feedback.delete.succeeded", {
        requestId,
        feedbackId,
        paperId: existingRecord?.paper_id ?? null,
        durationMs: Math.round(performance.now() - startedAt),
      });
      flushSync(() => {
        setFeedbackRecords((current) => current.filter((item) => item.id !== feedbackId));
        if (existingRecord) {
          clearPaperFeedbackStatus(existingRecord.paper_id);
        }
      });
      void refreshFeedback();
      void refreshProposals();
      scheduleDeferredReviewRefresh("feedback.delete");
      pushMessage("feedback.delete.succeeded");
    } catch (error) {
      logMarkReadDebug("feedback.delete.failed", {
        requestId,
        feedbackId,
        paperId: existingRecord?.paper_id ?? null,
        durationMs: Math.round(performance.now() - startedAt),
        message: errorText(error, "Could not delete feedback."),
      });
      pushErrorMessage("feedback.delete.failed", error, "Could not delete feedback.");
    } finally {
      endLocalMutation(requestId);
    }
  };

  const registerJob = (job: JobInfo, openAdmin = true) => {
    setJobs((current) => [job, ...current.filter((item) => item.id !== job.id)]);
    if (openAdmin) {
      setAdminOpen(true);
    }
  };

  const handleFeedChange = (index: number, field: "journal" | "url", value: string) => {
    setFeeds((current) =>
      current.map((item, itemIndex) =>
        itemIndex === index ? {...item, [field]: value} : item,
      ),
    );
  };

  const handleAddFeed = () => {
    setFeeds((current) => [
      ...current,
      {
        client_id:
          typeof crypto !== "undefined" && "randomUUID" in crypto
            ? crypto.randomUUID()
            : `${Date.now()}-${Math.random()}`,
        journal: "",
        url: "",
      },
    ]);
  };

  const handleRemoveFeed = (index: number) => {
    setFeeds((current) => current.filter((_item, itemIndex) => itemIndex !== index));
  };

  const handleSaveFeeds = async () => {
    const cleaned = feeds
      .map((item) => ({journal: item.journal.trim(), url: item.url.trim()}))
      .filter((item) => item.journal || item.url);
    if (cleaned.some((item) => !item.journal || !item.url)) {
      pushMessage("feeds.validation.failed");
      return;
    }
    try {
      setFeedsSaving(true);
      const saved = await saveFeedSubscriptions(cleaned);
      setFeeds(hydrateEditableFeeds(saved));
      setFeedsLoaded(true);
      pushMessage("feeds.save.succeeded");
    } catch (error) {
      pushErrorMessage("feeds.save.failed", error, "Could not save RSS feed settings.");
    } finally {
      setFeedsSaving(false);
    }
  };

  const handleSaveConfig = React.useCallback(async (
    fields: Record<string, {value?: string | null; clear?: boolean}>,
  ) => {
    try {
      setSettingsConfigSaving(true);
      const saved = await saveSettingsConfig(fields);
      setSettingsConfig(saved.fields);
      const currentProfile = await refreshProfileGate();
      await Promise.all([
        refreshAdminData(),
        currentProfile ? refreshReviewCore(currentProfile) : Promise.resolve(),
      ]);
      pushMessage("settings.config.save.succeeded");
    } catch (error) {
      pushErrorMessage("settings.config.save.failed", error, "Could not save local settings.");
    } finally {
      setSettingsConfigSaving(false);
    }
  }, [pushErrorMessage, pushMessage, refreshAdminData, refreshProfileGate, refreshReviewCore]);

  const handleSaveScheduler = React.useCallback(async (dailyTime: string) => {
    try {
      setSchedulerSaving(true);
      const saved = await saveSchedulerSettings(dailyTime);
      setScheduler(saved);
      if (saved.automatic_supported === false) {
        pushMessage("scheduler.save.succeeded", {
          text: "Daily sync time saved locally. Automatic runs are unavailable on this platform; use cron instead.",
          tone: "warning",
        });
      } else {
        pushMessage("scheduler.save.succeeded");
      }
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not save local scheduler settings.");
    } finally {
      setSchedulerSaving(false);
    }
  }, [pushErrorMessage, pushMessage]);

  const handleSaveProfile = React.useCallback(async (nextProfile: ClassificationProfile) => {
    try {
      setProfileSaving(true);
      const saved = await saveCurrentProfile(nextProfile);
      setProfile(saved.profile);
      pushMessage("profile.current.save.succeeded");
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not save the local profile.");
      throw error;
    } finally {
      setProfileSaving(false);
    }
  }, [pushErrorMessage, pushMessage]);

  const handleDeleteScheduler = React.useCallback(async () => {
    try {
      setSchedulerSaving(true);
      setScheduler(await deleteSchedulerSettings());
      pushMessage("scheduler.delete.succeeded");
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not disable local scheduler settings.");
    } finally {
      setSchedulerSaving(false);
    }
  }, [pushErrorMessage, pushMessage]);

  const handleOpenAppTarget = React.useCallback(
    async (target: "data_dir" | "logs_dir" | "install_dir") => {
      try {
        setAppControlBusy(true);
        await openAppTarget(target);
        pushMessage("app.control.open.succeeded");
      } catch (error) {
        pushErrorMessage("app.service.unavailable", error, "Could not open the selected local target.");
      } finally {
        setAppControlBusy(false);
      }
    },
    [pushErrorMessage, pushMessage],
  );

  const handleExitApp = React.useCallback(async () => {
    try {
      setAppControlBusy(true);
      await exitApp();
      pushMessage("app.control.exit.succeeded");
      window.setTimeout(() => {
        window.location.href = "about:blank";
      }, 350);
    } catch (error) {
      setAppControlBusy(false);
      pushErrorMessage("app.service.unavailable", error, "Could not exit the local FeedMeDaily service.");
    }
  }, [pushErrorMessage, pushMessage]);

  const handleOnboardingSaveAndBootstrap = React.useCallback(async (
    fields: Record<string, {value?: string | null; clear?: boolean}>,
    interestDescription: string,
  ) => {
    try {
      setSettingsConfigSaving(true);
      const saved = await saveSettingsConfig(fields);
      setSettingsConfig(saved.fields);
      const currentProfile = await refreshProfileGate();
      await Promise.all([
        refreshAdminData(),
        currentProfile ? refreshReviewCore(currentProfile) : Promise.resolve(),
      ]);
    } catch (error) {
      return {
        ok: false,
        tone: "danger" as const,
        message: errorText(error, "Could not save local settings."),
      };
    } finally {
      setSettingsConfigSaving(false);
    }

    try {
      setBusy(true);
      registerJob(await bootstrapProfile({interest_description: interestDescription}), false);
      return {
        ok: true,
        tone: "info" as const,
        message: "Local settings saved. Initial profile generation started.",
      };
    } catch (error) {
      return {
        ok: false,
        tone: "warning" as const,
        message: `Local settings were saved, but the initial profile generation did not start: ${errorText(error, "Unknown error.")}`,
      };
    } finally {
      setBusy(false);
    }
  }, [errorText, refreshAdminData, refreshProfileGate, refreshReviewCore]);

  const handleGenerateProposal = async () => {
    try {
      registerJob(await launchProfileProposalGeneration());
      pushMessage("profile.proposal.started");
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not start profile proposal generation.");
    }
  };

  const handleApplyProposal = async (
    id: number,
    selection?: {accepted_change_ids: string[]; rejected_change_ids: string[]},
  ) => {
    try {
      setBusy(true);
      await applyProfileProposal(id, selection);
      const currentProfile = await refreshProfileGate();
      await Promise.all([
        refreshFeedback(),
        refreshProposals(),
        currentProfile ? refreshReviewCore(currentProfile) : Promise.resolve(),
      ]);
      pushMessage("profile.proposal.applied");
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not apply the profile proposal.");
    } finally {
      setBusy(false);
    }
  };

  const handleRejectProposal = async (id: number) => {
    try {
      await rejectProfileProposal(id);
      await refreshProposals();
      pushMessage("profile.proposal.rejected");
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not reject the profile proposal.");
    }
  };

  const handleOnboardingAcceptDraft = React.useCallback(async (
    id: number,
    draftProfile: ClassificationProfile,
  ) => {
    try {
      setBusy(true);
      await applyProfileProposal(id);
    } catch (error) {
      return {
        ok: false,
        tone: "danger" as const,
        message: errorText(error, "Could not apply the profile proposal."),
      };
    }

    try {
      const saved = await saveCurrentProfile(draftProfile);
      setProfile(saved.profile);
      await Promise.all([
        refreshFeedback(),
        refreshProposals(),
        refreshReviewCore(saved.profile),
      ]);
      pushMessage("profile.proposal.applied", {
        text: "Initial profile applied and saved.",
        tone: "success",
      });
      return {ok: true};
    } catch (error) {
      const currentProfile = await refreshProfileGate();
      await Promise.all([
        refreshFeedback(),
        refreshProposals(),
        currentProfile ? refreshReviewCore(currentProfile) : Promise.resolve(),
      ]);
      pushMessage("profile.proposal.applied", {
        text: `Profile proposal applied, but the edited draft was not fully saved: ${errorText(error, "Unknown error.")}`,
        tone: "warning",
      });
      return {ok: true};
    } finally {
      setBusy(false);
    }
  }, [errorText, pushMessage, refreshFeedback, refreshProfileGate, refreshProposals, refreshReviewCore]);

  const handleOnboardingRejectProposal = React.useCallback(async (id: number) => {
    try {
      await rejectProfileProposal(id);
      await refreshProposals();
      return {
        ok: true,
        tone: "success" as const,
        message: "Rejected the pending proposal.",
      };
    } catch (error) {
      return {
        ok: false,
        tone: "danger" as const,
        message: errorText(error, "Could not reject the profile proposal."),
      };
    }
  }, [errorText, refreshProposals]);

  const handleRunAdminJob = async (path: "/api/admin/run") => {
    try {
      registerJob(await launchAdminJob(path));
      pushMessage("job.started");
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not start the sync job.");
    }
  };

  const handleStartVerification = React.useCallback(async (job: JobInfo) => {
    if (!job.verification_feed_url) {
      pushMessage("app.service.unavailable", {text: "Verification feed URL is missing.", tone: "danger"});
      return;
    }
    setVerificationSubmitError(null);
    try {
      await startFeedVerification({job_id: job.id, feed_url: job.verification_feed_url});
      pushMessage("job.verification.started", {
        text: "Opened the feed verification window.",
        tone: "info",
      });
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not start feed verification.");
    }
  }, [pushErrorMessage, pushMessage]);

  const handleOpenVerificationInBrowser = React.useCallback(async (job: JobInfo) => {
    if (!job.verification_feed_url) {
      pushMessage("app.service.unavailable", {text: "Verification feed URL is missing.", tone: "danger"});
      return;
    }
    setVerificationSubmitError(null);
    try {
      await openFeedVerificationInBrowser({job_id: job.id, feed_url: job.verification_feed_url});
      pushMessage("job.verification.browser.started", {
        text: "Opened the protected feed in your browser. Finish the check there, then paste the final RSS/XML here.",
        tone: "info",
      });
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not open the protected feed in your browser.");
    }
  }, [pushErrorMessage, pushMessage]);

  const handleSubmitVerificationXML = React.useCallback(async (job: JobInfo, xml: string) => {
    if (!job.verification_feed_url) {
      setVerificationSubmitError("Verification feed URL is missing.");
      return;
    }
    try {
      setVerificationSubmitting(true);
      setVerificationSubmitError(null);
      await submitFeedVerificationXML({
        job_id: job.id,
        feed_url: job.verification_feed_url,
        feed_xml: xml,
      });
      pushMessage("job.verification.manual.accepted", {
        text: "Accepted the pasted RSS/XML. The sync is resuming now.",
        tone: "info",
      });
    } catch (error) {
      setVerificationSubmitError(errorText(error, "Could not submit protected feed XML."));
    } finally {
      setVerificationSubmitting(false);
    }
  }, [errorText, pushMessage]);

  const handleReclassify = async (scope: "recent" | "feedback" | "all") => {
    try {
      registerJob(await launchReclassifyJob({scope, limit: scope === "all" ? 500 : 50}));
      pushMessage("job.reclassify.started");
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not start the reclassification job.");
    }
  };

  const toggleTheme = React.useCallback(() => {
    const currentSystemTheme = systemTheme;
    const nextResolvedTheme = resolvedTheme === "dark" ? "light" : "dark";
    setThemePreference(nextResolvedTheme === currentSystemTheme ? "system" : nextResolvedTheme);
  }, [resolvedTheme, systemTheme]);

  // 统一重置筛选器，供侧栏和空状态按钮复用。
  const resetFilters = React.useCallback(() => {
    setJournal("all");
    setDateFilter("30d");
    setReadFilter("unread");
    setRelevance("all");
    setQuery("");
  }, []);

  if (!profileResolved) {
    return (
      <main className="flex min-h-screen flex-col bg-[--paper] text-[--ink]">
        <TopBar
          message={message}
          onOpenAdmin={() => setAdminOpen(true)}
          onToggleTheme={toggleTheme}
          resolvedTheme={resolvedTheme}
          usingSystemTheme={themePreference === "system"}
        />
        <div className="mx-auto flex w-full max-w-5xl flex-1 items-center justify-center px-4 py-8">
          <div className="rounded-lg border border-(--line) bg-(--paper-accent) px-5 py-4 text-sm text-muted">
            Loading your library...
          </div>
        </div>
        <AppStatusBar
          appMeta={appMeta}
          appUpdate={appUpdate}
          appUpdateChecking={appUpdateChecking}
          busy={appControlBusy}
          onCheckForUpdates={() => void handleCheckAppUpdate()}
          onExit={() => void handleExitApp()}
          onOpenData={() => void handleOpenAppTarget("data_dir")}
          onOpenInstall={() => void handleOpenAppTarget("install_dir")}
          onOpenLogs={() => void handleOpenAppTarget("logs_dir")}
        />
      </main>
    );
  }

  if (!profile) {
    return (
      <>
        {message ? (
          <StatusBanner className="mx-auto mt-4 max-w-5xl" tone={message.tone}>
            {message.text}
          </StatusBanner>
        ) : null}
        <Onboarding
          busy={onboardingBusy}
          configFields={settingsConfig}
          configSaving={settingsConfigSaving}
          jobs={jobs}
          proposals={profileProposals}
          onAcceptDraft={handleOnboardingAcceptDraft}
          onRejectProposal={handleOnboardingRejectProposal}
          onSaveAndBootstrap={handleOnboardingSaveAndBootstrap}
        />
      </>
    );
  }

  return (
    <main className="flex h-screen flex-col overflow-hidden bg-[--paper] text-[--ink]">
      <TopBar
        message={message}
        onOpenAdmin={() => setAdminOpen(true)}
        onToggleTheme={toggleTheme}
        resolvedTheme={resolvedTheme}
        usingSystemTheme={themePreference === "system"}
      />
      {adminHydrationWarning ? (
        <StatusBanner className="mx-auto mt-3 w-full max-w-375 px-4" tone="warning">
          {adminHydrationWarning}
        </StatusBanner>
      ) : null}
      <AdminPanel
        activeTab={adminTab}
        appMeta={appMeta}
        appUpdate={appUpdate}
        appUpdateChecking={appUpdateChecking}
        configFields={settingsConfig}
        configSaving={settingsConfigSaving}
        open={adminOpen}
        profile={profile}
        profileSaving={profileSaving}
        hasFeeds={feeds.length > 0}
        feeds={feeds}
        feedsSaving={feedsSaving}
        feedback={feedbackRecords}
        jobs={jobs}
        proposalGenerating={Boolean(profileProposalJob)}
        proposals={profileProposals}
        onClose={() => setAdminOpen(false)}
        onFeedChange={handleFeedChange}
        onAddFeed={handleAddFeed}
        onCheckForUpdates={() => void handleCheckAppUpdate()}
        onRemoveFeed={handleRemoveFeed}
        onSaveConfig={handleSaveConfig}
        onSaveProfile={handleSaveProfile}
        onSaveScheduler={handleSaveScheduler}
        onSaveFeeds={() => void handleSaveFeeds()}
        onTabChange={setAdminTab}
        onDeleteScheduler={handleDeleteScheduler}
        onGenerateProposal={() => void handleGenerateProposal()}
        onStartVerification={(job) => void handleStartVerification(job)}
        onOpenVerificationInBrowser={(job) => void handleOpenVerificationInBrowser(job)}
        onSubmitVerificationXML={(job, xml) => handleSubmitVerificationXML(job, xml)}
        onApplyProposal={(id, selection) => void handleApplyProposal(id, selection)}
        onRejectProposal={(id) => void handleRejectProposal(id)}
        onRunSync={() => void handleRunAdminJob("/api/admin/run")}
        onReclassifyRecent={() => void handleReclassify("recent")}
        onReclassifyFeedback={() => void handleReclassify("feedback")}
        onReclassifyAll={() => void handleReclassify("all")}
        onDeleteFeedback={(id) => void handleDeleteFeedback(id)}
        scheduler={scheduler}
        schedulerSaving={schedulerSaving}
        verificationSubmitting={verificationSubmitting}
        verificationSubmitError={verificationSubmitError}
      />
      <FeedbackModal
        paper={feedbackPaper}
        value={feedbackValue}
        note={feedbackNote}
        onValueChange={setFeedbackValue}
        onNoteChange={setFeedbackNote}
        onClose={() => setFeedbackPaper(null)}
        onSubmit={() => void submitFeedback()}
      />
      <ZoteroSaveModal
        paper={zoteroPaper}
        collections={zoteroCollections}
        selectedCollectionKey={zoteroCollectionKey}
        loading={zoteroLoading}
        saving={zoteroSaving}
        error={zoteroError}
        onCollectionChange={setZoteroCollectionKey}
        onClose={() => setZoteroPaper(null)}
        onSubmit={() => void handleSaveToZotero()}
      />

      <div className="mx-auto grid min-h-0 w-full max-w-375 flex-1 gap-4 overflow-hidden px-4 py-4 lg:grid-cols-[300px_minmax(0,1fr)_360px]">
        <FiltersSidebar
          dateFilter={dateFilter}
          journal={journal}
          journalOptions={journalOptions}
          lastUpdateLabel={lastUpdateLabel}
          onDateFilterChange={setDateFilter}
          onJournalChange={setJournal}
          onReadFilterChange={setReadFilter}
          onReset={resetFilters}
          profileName={profile.meta.name}
          profileVersion={profile.meta.version}
          readFilter={readFilter}
          shownCount={visibleList.length}
          totalCount={needsFeedSetup ? 0 : report.papers.length}
          visibleTotals={visibleTotals}
        />

        <PaperListSection
          hasNoFetchedPapers={hasNoFetchedPapers}
          loadError={reportLoadError}
          loading={(reportLoading || !feedsLoaded) && report.papers.length === 0}
          needsFeedSetup={needsFeedSetup}
          onOpenAdmin={() => setAdminOpen(true)}
          onResetFilters={resetFilters}
          onRunSync={() => void handleRunAdminJob("/api/admin/run")}
          onSelectPaper={(paper) => setSelectedId(paper.id)}
          onStartFeedSetup={() => {
            if (feeds.length === 0) {
              handleAddFeed();
            }
            setAdminTab("feeds");
            setAdminOpen(true);
          }}
          papers={visibleList}
          query={query}
          reportErrors={report.errors}
          relevance={relevance}
          selectedId={needsFeedSetup ? null : selectedPaperId}
          setQuery={setQuery}
          setRelevance={setRelevance}
          visibleBaseCount={visibleBase.length}
          visibleTotals={visibleTotals}
        />

        <DetailPanel
          paper={needsFeedSetup ? null : selectedPaper}
          isUnread={Boolean(selectedPaper && !selectedPaper.read_at)}
          markReadBusy={
            selectedPaper?.id === markReadRequest?.paperId &&
            markReadSubmitting
          }
          onMarkRead={() => selectedPaper && void persistReadStatus(selectedPaper.id)}
          onMarkWrong={() => selectedPaper && openFeedbackModal(selectedPaper)}
          onSave={() => selectedPaper && openZoteroModal(selectedPaper)}
        />
      </div>
      <AppStatusBar
        appMeta={appMeta}
        appUpdate={appUpdate}
        appUpdateChecking={appUpdateChecking}
        busy={appControlBusy}
        onCheckForUpdates={() => void handleCheckAppUpdate()}
        onExit={() => void handleExitApp()}
        onOpenData={() => void handleOpenAppTarget("data_dir")}
        onOpenInstall={() => void handleOpenAppTarget("install_dir")}
        onOpenLogs={() => void handleOpenAppTarget("logs_dir")}
      />
    </main>
  );
}
