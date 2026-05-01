import React from "react";

import {
  applyProfileProposal,
  bootstrapProfile,
  createFeedback,
  deleteFeedback,
  fetchCurrentProfile,
  fetchFeedSubscriptions,
  fetchFeedback,
  fetchJob,
  fetchLatestReport,
  fetchProfileProposals,
  fetchZoteroCollections,
  launchAdminJob,
  launchProfileProposalGeneration,
  launchReclassifyJob,
  loadEmbeddedReport,
  markPaperRead,
  rejectProfileProposal,
  saveFeedSubscriptions,
  saveToZotero,
  tagLabel,
} from "./reportData";
import {
  matchesDateFilter,
  relevanceCounts,
} from "./app/utils";
import type {
  DateFilter,
  ReadFilter,
  RelevanceFilter,
} from "./app/constants";
import {EMPTY_REPORT} from "./types";
import {AdminPanel} from "./components/admin/AdminPanel";
import {StatusBanner} from "./components/common/StatusBanner";
import {FeedbackModal} from "./components/modals/FeedbackModal";
import {ZoteroSaveModal} from "./components/modals/ZoteroSaveModal";
import {Onboarding} from "./components/onboarding/Onboarding";
import {DetailPanel} from "./components/review/DetailPanel";
import {FiltersSidebar} from "./components/review/FiltersSidebar";
import {PaperListSection} from "./components/review/PaperListSection";
import type {
  ClassificationProfile,
  FeedSubscription,
  FeedbackRecord,
  JobInfo,
  Paper,
  ProfileProposal,
  Relevance,
  Report,
  ZoteroCollectionOption,
} from "./types";

// 应用根组件负责衔接数据加载、筛选状态、后台任务和三栏式阅读界面。
export function App() {
  const [report, setReport] = React.useState<Report>(() => loadEmbeddedReport() ?? EMPTY_REPORT);
  const [profile, setProfile] = React.useState<ClassificationProfile | null>(null);
  const [feeds, setFeeds] = React.useState<FeedSubscription[]>([]);
  const [feedsLoaded, setFeedsLoaded] = React.useState(false);
  const [loadError, setLoadError] = React.useState<string | null>(null);
  const [notice, setNotice] = React.useState<string | null>(null);
  const [query, setQuery] = React.useState("");
  const [relevance, setRelevance] = React.useState<RelevanceFilter>("all");
  const [topic, setTopic] = React.useState("all");
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
  const [zoteroPaper, setZoteroPaper] = React.useState<Paper | null>(null);
  const [zoteroCollections, setZoteroCollections] = React.useState<ZoteroCollectionOption[]>([]);
  const [zoteroCollectionKey, setZoteroCollectionKey] = React.useState("");
  const [zoteroLoading, setZoteroLoading] = React.useState(false);
  const [zoteroSaving, setZoteroSaving] = React.useState(false);
  const [zoteroError, setZoteroError] = React.useState<string | null>(null);
  const [busy, setBusy] = React.useState(false);
  const deferredQuery = React.useDeferredValue(query);

  const refreshProfile = React.useCallback(async () => {
    const next = await fetchCurrentProfile();
    setProfile(next.profile);
    return next.profile;
  }, []);

  const refreshReport = React.useCallback(async () => {
    try {
      const next = await fetchLatestReport();
      React.startTransition(() => setReport(next));
      setLoadError(null);
    } catch (error) {
      const embedded = loadEmbeddedReport();
      if (embedded) {
        React.startTransition(() => setReport(embedded));
        setLoadError("Using embedded report data because the API is unavailable.");
        return;
      }
      throw error;
    }
  }, []);

  const refreshFeedback = React.useCallback(async () => {
    setFeedbackRecords(await fetchFeedback());
  }, []);

  const refreshFeeds = React.useCallback(async () => {
    const nextFeeds = await fetchFeedSubscriptions();
    setFeeds(nextFeeds);
    setFeedsLoaded(true);
  }, []);

  const refreshProposals = React.useCallback(async () => {
    setProfileProposals(await fetchProfileProposals());
  }, []);

  const refreshAll = React.useCallback(async () => {
    try {
      const currentProfile = await refreshProfile();
      await Promise.all([refreshProposals(), refreshFeedback(), refreshFeeds()]);
      if (currentProfile) {
        await refreshReport();
      }
      setLoadError(null);
    } catch (error) {
      setLoadError((error as Error).message);
    }
  }, [refreshFeedback, refreshFeeds, refreshProfile, refreshProposals, refreshReport]);

  React.useEffect(() => {
    void refreshAll();
  }, [refreshAll]);

  React.useEffect(() => {
    if (profile && feedsLoaded && feeds.length === 0) {
      setAdminOpen(true);
    }
  }, [feeds.length, feedsLoaded, profile]);

  const runningJobs = React.useMemo(
    () => jobs.filter((job) => job.status === "queued" || job.status === "running"),
    [jobs],
  );
  const bootstrapJob = React.useMemo(
    () => jobs.find((job) => job.job_type === "profile-bootstrap") ?? null,
    [jobs],
  );
  const onboardingBusy =
    busy ||
    bootstrapJob?.status === "queued" ||
    bootstrapJob?.status === "running";

  React.useEffect(() => {
    if (runningJobs.length === 0) {
      return;
    }
    const timer = window.setInterval(() => {
      Promise.all(runningJobs.map((job) => fetchJob(job.id)))
        .then((updatedJobs) => {
          setJobs((current) => {
            const byId = new Map(current.map((job) => [job.id, job]));
            updatedJobs.forEach((job) => byId.set(job.id, job));
            return Array.from(byId.values()).sort((left, right) =>
              left.created_at < right.created_at ? 1 : -1,
            );
          });
          const failedJob = updatedJobs.find((job) => job.status === "failed" && job.error);
          if (failedJob?.error) {
            setLoadError(failedJob.error);
          }
          if (updatedJobs.some((job) => job.status === "completed")) {
            void refreshAll();
          }
        })
        .catch((error) => setLoadError((error as Error).message));
    }, 2500);
    return () => window.clearInterval(timer);
  }, [refreshAll, runningJobs]);

  const tags = React.useMemo(
    () => profile?.topic_taxonomy.map((item) => item.id).sort() ?? [],
    [profile],
  );
  const journals = React.useMemo(
    () => Array.from(new Set(report.papers.map((paper) => paper.journal).filter(Boolean) as string[])).sort(),
    [report.papers],
  );
  const topicOptions = React.useMemo(
    () => [
      {value: "all", label: "All topics"},
      ...tags.map((item) => ({value: item, label: tagLabel(item, profile)})),
    ],
    [profile, tags],
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
          paper.classification.topic_tags.join(" "),
          paper.feedback_status?.note ?? "",
        ]
          .join(" ")
          .toLowerCase();
        const matchesQuery = !deferredQuery || haystack.includes(deferredQuery.toLowerCase());
        const matchesTopic = topic === "all" || paper.classification.topic_tags.includes(topic);
        const matchesJournal = journal === "all" || paper.journal === journal;
        const dateValue = paper.published_date ?? paper.seen_date;
        const matchesRead =
          readFilter === "all" ||
          (readFilter === "read" ? Boolean(paper.read_at) : !paper.read_at);
        const matchesDate = matchesDateFilter(dateValue, report.report_date, dateFilter);
        return matchesQuery && matchesTopic && matchesJournal && matchesRead && matchesDate;
      }),
    [dateFilter, deferredQuery, journal, readFilter, report.papers, report.report_date, topic],
  );

  const filtered = React.useMemo(
    () =>
      filteredBase.filter(
        (paper) => relevance === "all" || paper.classification.relevance === relevance,
      ),
    [filteredBase, relevance],
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
      setLoadError((error as Error).message);
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
        setNotice("Saved to Zotero.");
        setZoteroPaper(null);
        setZoteroError(null);
        return;
      }
      setZoteroError(status.last_error ?? "Zotero save updated.");
    } catch (error) {
      setZoteroError((error as Error).message);
      setLoadError((error as Error).message);
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
      setNotice("Feedback saved.");
    } catch (error) {
      setLoadError((error as Error).message);
    }
  };

  const handleDeleteFeedback = async (feedbackId: number) => {
    try {
      await deleteFeedback(feedbackId);
      await Promise.all([refreshReport(), refreshFeedback(), refreshProposals()]);
      setNotice("Feedback deleted.");
    } catch (error) {
      setLoadError((error as Error).message);
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
    setFeeds((current) => [...current, {journal: "", url: ""}]);
  };

  const handleRemoveFeed = (index: number) => {
    setFeeds((current) => current.filter((_item, itemIndex) => itemIndex !== index));
  };

  const handleSaveFeeds = async () => {
    const cleaned = feeds
      .map((item) => ({journal: item.journal.trim(), url: item.url.trim()}))
      .filter((item) => item.journal || item.url);
    if (cleaned.some((item) => !item.journal || !item.url)) {
      setLoadError("Each feed needs both a journal name and an RSS URL.");
      return;
    }
    try {
      setFeedsSaving(true);
      const saved = await saveFeedSubscriptions(cleaned);
      setFeeds(saved);
      setFeedsLoaded(true);
      setNotice("Feed subscriptions saved.");
      setLoadError(null);
    } catch (error) {
      setLoadError((error as Error).message);
    } finally {
      setFeedsSaving(false);
    }
  };

  const handleBootstrap = async (interestDescription: string, name: string) => {
    try {
      setBusy(true);
      registerJob(
        await bootstrapProfile({interest_description: interestDescription, name}),
        false,
      );
      setNotice("Initial profile generation started.");
    } catch (error) {
      setLoadError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleGenerateProposal = async () => {
    try {
      registerJob(await launchProfileProposalGeneration());
      setNotice("Profile proposal job started.");
    } catch (error) {
      setLoadError((error as Error).message);
    }
  };

  const handleApplyProposal = async (id: number) => {
    try {
      setBusy(true);
      await applyProfileProposal(id);
      await refreshAll();
      setNotice("Profile proposal applied.");
    } catch (error) {
      setLoadError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleRejectProposal = async (id: number) => {
    try {
      await rejectProfileProposal(id);
      await refreshProposals();
      setNotice("Profile proposal rejected.");
    } catch (error) {
      setLoadError((error as Error).message);
    }
  };

  const handleRunAdminJob = async (path: "/api/admin/run" | "/api/admin/report/latest") => {
    try {
      registerJob(await launchAdminJob(path));
      setNotice("Job started.");
    } catch (error) {
      setLoadError((error as Error).message);
    }
  };

  const handleReclassify = async (scope: "recent" | "feedback" | "all") => {
    try {
      registerJob(await launchReclassifyJob({scope, limit: scope === "all" ? 500 : 50}));
      setNotice("Reclassification job started.");
    } catch (error) {
      setLoadError((error as Error).message);
    }
  };

  // 统一重置筛选器，供侧栏和空状态按钮复用。
  const resetFilters = React.useCallback(() => {
    setTopic("all");
    setJournal("all");
    setDateFilter("30d");
    setReadFilter("unread");
    setRelevance("all");
    setQuery("");
  }, []);

  if (!profile) {
    return (
      <>
        {loadError ? (
          <StatusBanner className="mx-auto mt-4 max-w-5xl" tone="warning">
            {loadError}
          </StatusBanner>
        ) : null}
        {notice ? (
          <StatusBanner className="mx-auto mt-4 max-w-5xl" tone="success">
            {notice}
          </StatusBanner>
        ) : null}
        <Onboarding
          busy={onboardingBusy}
          jobs={jobs}
          proposals={profileProposals}
          onBootstrap={handleBootstrap}
          onApplyProposal={handleApplyProposal}
        />
      </>
    );
  }

  return (
    <main className="min-h-screen bg-[var(--paper)] text-[var(--ink)]">
      <AdminPanel
        open={adminOpen}
        profile={profile}
        hasFeeds={feeds.length > 0}
        feeds={feeds}
        feedsSaving={feedsSaving}
        feedback={feedbackRecords}
        jobs={jobs}
        proposals={profileProposals}
        onClose={() => setAdminOpen(false)}
        onFeedChange={handleFeedChange}
        onAddFeed={handleAddFeed}
        onRemoveFeed={handleRemoveFeed}
        onSaveFeeds={() => void handleSaveFeeds()}
        onGenerateProposal={() => void handleGenerateProposal()}
        onApplyProposal={(id) => void handleApplyProposal(id)}
        onRejectProposal={(id) => void handleRejectProposal(id)}
        onRunFeedSync={() => void handleRunAdminJob("/api/admin/run")}
        onReclassifyRecent={() => void handleReclassify("recent")}
        onReclassifyFeedback={() => void handleReclassify("feedback")}
        onReclassifyAll={() => void handleReclassify("all")}
        onDeleteFeedback={(id) => void handleDeleteFeedback(id)}
        onRefreshReport={() => void handleRunAdminJob("/api/admin/report/latest")}
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

      <div className="mx-auto grid max-w-[1500px] gap-4 px-4 py-4 lg:grid-cols-[260px_minmax(0,1fr)_360px]">
        <FiltersSidebar
          dateFilter={dateFilter}
          journal={journal}
          journalOptions={journalOptions}
          onDateFilterChange={setDateFilter}
          onJournalChange={setJournal}
          onReadFilterChange={setReadFilter}
          onReset={resetFilters}
          onTopicChange={setTopic}
          profileName={profile.meta.name}
          profileVersion={profile.meta.version}
          readFilter={readFilter}
          reportDate={report.report_date}
          shownCount={visibleList.length}
          topic={topic}
          topicOptions={topicOptions}
          totalCount={needsFeedSetup ? 0 : report.papers.length}
          visibleTotals={visibleTotals}
        />

        <PaperListSection
          hasNoFetchedPapers={hasNoFetchedPapers}
          loadError={loadError}
          needsFeedSetup={needsFeedSetup}
          notice={notice}
          onOpenAdmin={() => setAdminOpen(true)}
          onResetFilters={resetFilters}
          onRunFetchAndClassify={() => void handleRunAdminJob("/api/admin/run")}
          onSelectPaper={(paper) => setSelectedId(paper.id)}
          onStartFeedSetup={() => {
            if (feeds.length === 0) {
              handleAddFeed();
            }
            setAdminOpen(true);
          }}
          papers={visibleList}
          profile={profile}
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
          profile={profile}
          isUnread={Boolean(selectedPaper && !selectedPaper.read_at)}
          onMarkRead={() => selectedPaper && void persistReadStatus(selectedPaper.id)}
          onMarkWrong={() => selectedPaper && openFeedbackModal(selectedPaper)}
          onSave={() => selectedPaper && openZoteroModal(selectedPaper)}
        />
      </div>
    </main>
  );
}
