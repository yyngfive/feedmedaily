import React from "react";

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
  openAppTarget,
  rejectProfileProposal,
  saveFeedSubscriptions,
  saveCurrentProfile,
  saveSchedulerSettings,
  saveSettingsConfig,
  saveToZotero,
  startFeedVerification,
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

// 应用根组件负责衔接数据加载、筛选状态、后台任务和三栏式阅读界面。
export function App() {
  const [report, setReport] = React.useState<Report>(EMPTY_REPORT);
  const [profile, setProfile] = React.useState<ClassificationProfile | null>(null);
  const [appMeta, setAppMeta] = React.useState<AppMeta | null>(null);
  const [appUpdate, setAppUpdate] = React.useState<AppUpdate | null>(null);
  const [feeds, setFeeds] = React.useState<FeedSubscription[]>([]);
  const [scheduler, setScheduler] = React.useState<SchedulerSettings | null>(null);
  const [settingsConfig, setSettingsConfig] = React.useState<SettingsConfigField[]>([]);
  const [feedsLoaded, setFeedsLoaded] = React.useState(false);
  const [message, setMessage] = React.useState<UiMessage | null>(null);
  const [query, setQuery] = React.useState("");
  const [relevance, setRelevance] = React.useState<RelevanceFilter>("direct");
  const [journal, setJournal] = React.useState("all");
  const [dateFilter, setDateFilter] = React.useState<DateFilter>("30d");
  const [readFilter, setReadFilter] = React.useState<ReadFilter>("unread");
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
  const deferredQuery = React.useDeferredValue(query);
  const resolvedTheme = themePreference === "system" ? systemTheme : themePreference;

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

  const refreshProfile = React.useCallback(async () => {
    const next = await fetchCurrentProfile();
    setProfile(next.profile);
    return next.profile;
  }, []);

  const refreshReport = React.useCallback(async () => {
    const next = await fetchLatestReport();
    React.startTransition(() => setReport(next));
  }, [pushMessage]);

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

  const refreshAll = React.useCallback(async () => {
    try {
      const currentProfile = await refreshProfile();
      await Promise.all([
        refreshProposals(),
        refreshFeedback(),
        refreshFeeds(),
        refreshConfig(),
        refreshAppMeta(),
        refreshAppUpdate(),
        refreshScheduler(),
      ]);
      if (currentProfile) {
        await refreshReport();
      }
    } catch (error) {
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
    }
  }, [
    pushMessage,
    refreshAppMeta,
    refreshAppUpdate,
    refreshConfig,
    refreshFeedback,
    refreshFeeds,
    refreshProfile,
    refreshProposals,
    refreshReport,
    refreshScheduler,
  ]);

  React.useEffect(() => {
    void refreshAll();
  }, [refreshAll]);

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
    void refreshAll();
  }, [bootstrapJob, refreshAll]);

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
          const signature = `${job.status}:${job.finished_at ?? job.started_at ?? ""}:${job.message_key ?? ""}:${job.message ?? ""}:${job.error ?? ""}`;
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
          const activeSignature = `${activeAnnouncement.id}:${activeAnnouncement.status}:${activeAnnouncement.message_key ?? ""}:${activeAnnouncement.message ?? ""}`;
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
          void refreshAll();
        }
      } catch (error) {
        pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
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
  }, [profile, pushMessage, refreshAll]);

  const journals = React.useMemo(
    () => Array.from(new Set(report.papers.map((paper) => paper.journal).filter(Boolean) as string[])).sort(),
    [report.papers],
  );
  const journalOptions = React.useMemo(
    () => [{value: "all", label: "All journals"}, ...journals.map((item) => ({value: item, label: item}))],
    [journals],
  );

  const filteredBase = React.useMemo(
    () =>
      report.papers.filter((paper) => {
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
    [dateFilter, deferredQuery, journal, readFilter, report.papers, report.report_date],
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
  const hasNoFetchedPapers = !needsFeedSetup && feeds.length > 0 && report.papers.length === 0;
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
    if (visibleList.length === 0) {
      setSelectedId(null);
      return;
    }
    if (!selectedId || !visibleList.some((paper) => paper.id === selectedId)) {
      setSelectedId(visibleList[0].id);
    }
  }, [selectedId, visibleList]);

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

  const selectedPaper = visibleList.find((paper) => paper.id === selectedId) ?? null;

  const updatePaper = (paperId: number, updater: (paper: Paper) => Paper) => {
    setReport((current) => ({
      ...current,
      papers: current.papers.map((paper) => (paper.id === paperId ? updater(paper) : paper)),
    }));
  };

  const persistReadStatus = async (paperId: number) => {
    const paper = report.papers.find((item) => item.id === paperId);
    if (!paper || paper.read_at) {
      return;
    }
    const optimisticReadAt = new Date().toISOString();
    updatePaper(paperId, (current) => ({...current, read_at: optimisticReadAt}));
    try {
      const status = await markPaperRead(paperId);
      updatePaper(paperId, (current) => ({...current, read_at: status.read_at}));
    } catch (error) {
      updatePaper(paperId, (current) => ({...current, read_at: null}));
      pushMessage("paper.read.failed", {text: (error as Error).message, tone: "danger"});
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
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
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
    try {
      await createFeedback({
        paper_id: feedbackPaper.id,
        corrected_relevance: feedbackValue,
        note: feedbackNote.trim() || undefined,
      });
      setFeedbackPaper(null);
      setFeedbackNote("");
      await Promise.all([refreshReport(), refreshFeedback()]);
      pushMessage("feedback.save.succeeded");
    } catch (error) {
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
    }
  };

  const handleDeleteFeedback = async (feedbackId: number) => {
    try {
      await deleteFeedback(feedbackId);
      await Promise.all([refreshReport(), refreshFeedback(), refreshProposals()]);
      pushMessage("feedback.delete.succeeded");
    } catch (error) {
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
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
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
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
      await refreshAll();
      pushMessage("settings.config.save.succeeded");
    } catch (error) {
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
    } finally {
      setSettingsConfigSaving(false);
    }
  }, [pushMessage, refreshAll]);

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
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
    } finally {
      setSchedulerSaving(false);
    }
  }, [pushMessage]);

  const handleSaveProfile = React.useCallback(async (nextProfile: ClassificationProfile) => {
    try {
      setProfileSaving(true);
      const saved = await saveCurrentProfile(nextProfile);
      setProfile(saved.profile);
      pushMessage("profile.current.save.succeeded");
    } catch (error) {
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
      throw error;
    } finally {
      setProfileSaving(false);
    }
  }, [pushMessage]);

  const handleDeleteScheduler = React.useCallback(async () => {
    try {
      setSchedulerSaving(true);
      setScheduler(await deleteSchedulerSettings());
      pushMessage("scheduler.delete.succeeded");
    } catch (error) {
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
    } finally {
      setSchedulerSaving(false);
    }
  }, [pushMessage]);

  const handleOpenAppTarget = React.useCallback(
    async (target: "data_dir" | "logs_dir" | "install_dir") => {
      try {
        setAppControlBusy(true);
        await openAppTarget(target);
        pushMessage("app.control.open.succeeded");
      } catch (error) {
        pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
      } finally {
        setAppControlBusy(false);
      }
    },
    [pushMessage],
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
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
    }
  }, [pushMessage]);

  const handleBootstrap = async (interestDescription: string, name: string) => {
    try {
      setBusy(true);
      registerJob(
        await bootstrapProfile({interest_description: interestDescription, name}),
        false,
      );
    } catch (error) {
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
    } finally {
      setBusy(false);
    }
  };

  const handleGenerateProposal = async () => {
    try {
      registerJob(await launchProfileProposalGeneration());
      pushMessage("profile.proposal.started");
    } catch (error) {
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
    }
  };

  const handleApplyProposal = async (
    id: number,
    selection?: {accepted_change_ids: string[]; rejected_change_ids: string[]},
  ) => {
    try {
      setBusy(true);
      await applyProfileProposal(id, selection);
      await refreshAll();
      pushMessage("profile.proposal.applied");
    } catch (error) {
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
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
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
    }
  };

  const handleRunAdminJob = async (path: "/api/admin/run") => {
    try {
      registerJob(await launchAdminJob(path));
      pushMessage("job.started");
    } catch (error) {
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
    }
  };

  const handleStartVerification = React.useCallback(async (job: JobInfo) => {
    if (!job.verification_feed_url) {
      pushMessage("app.load.failed", {text: "Verification feed URL is missing.", tone: "danger"});
      return;
    }
    try {
      await startFeedVerification({job_id: job.id, feed_url: job.verification_feed_url});
      pushMessage("job.verification.started", {
        text: "Opened the feed verification window.",
        tone: "info",
      });
    } catch (error) {
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
    }
  }, [pushMessage]);

  const handleReclassify = async (scope: "recent" | "feedback" | "all") => {
    try {
      registerJob(await launchReclassifyJob({scope, limit: scope === "all" ? 500 : 50}));
      pushMessage("job.reclassify.started");
    } catch (error) {
      pushMessage("app.load.failed", {text: (error as Error).message, tone: "danger"});
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
          onBootstrap={handleBootstrap}
          onApplyProposal={handleApplyProposal}
          onSaveConfig={handleSaveConfig}
        />
        <AppStatusBar
          appMeta={appMeta}
          appUpdate={appUpdate}
          busy={appControlBusy}
          onExit={() => void handleExitApp()}
          onOpenData={() => void handleOpenAppTarget("data_dir")}
          onOpenInstall={() => void handleOpenAppTarget("install_dir")}
          onOpenLogs={() => void handleOpenAppTarget("logs_dir")}
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
      <AdminPanel
        activeTab={adminTab}
        appMeta={appMeta}
        appUpdate={appUpdate}
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
        onRemoveFeed={handleRemoveFeed}
        onSaveConfig={handleSaveConfig}
        onSaveProfile={handleSaveProfile}
        onSaveScheduler={handleSaveScheduler}
        onSaveFeeds={() => void handleSaveFeeds()}
        onTabChange={setAdminTab}
          onDeleteScheduler={handleDeleteScheduler}
          onGenerateProposal={() => void handleGenerateProposal()}
          onStartVerification={(job) => void handleStartVerification(job)}
        onApplyProposal={(id, selection) => void handleApplyProposal(id, selection)}
        onRejectProposal={(id) => void handleRejectProposal(id)}
        onRunSync={() => void handleRunAdminJob("/api/admin/run")}
        onReclassifyRecent={() => void handleReclassify("recent")}
        onReclassifyFeedback={() => void handleReclassify("feedback")}
        onReclassifyAll={() => void handleReclassify("all")}
        onDeleteFeedback={(id) => void handleDeleteFeedback(id)}
        scheduler={scheduler}
        schedulerSaving={schedulerSaving}
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
          selectedId={selectedId}
          setQuery={setQuery}
          setRelevance={setRelevance}
          visibleBaseCount={visibleBase.length}
          visibleTotals={visibleTotals}
        />

        <DetailPanel
          paper={needsFeedSetup ? null : selectedPaper}
          isUnread={Boolean(selectedPaper && !selectedPaper.read_at)}
          onMarkRead={() => selectedPaper && void persistReadStatus(selectedPaper.id)}
          onMarkWrong={() => selectedPaper && openFeedbackModal(selectedPaper)}
          onSave={() => selectedPaper && openZoteroModal(selectedPaper)}
        />
      </div>
      <AppStatusBar
        appMeta={appMeta}
        appUpdate={appUpdate}
        busy={appControlBusy}
        onExit={() => void handleExitApp()}
        onOpenData={() => void handleOpenAppTarget("data_dir")}
        onOpenInstall={() => void handleOpenAppTarget("install_dir")}
        onOpenLogs={() => void handleOpenAppTarget("logs_dir")}
      />
    </main>
  );
}
