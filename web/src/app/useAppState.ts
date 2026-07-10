import React from "react";

import type {AdminTab} from "../features/admin/AdminPanel";
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
} from "../shared/types";
import {EMPTY_REPORT} from "../shared/types";
import type {DateFilter, FeedbackFilter, ReadFilter, RelevanceFilter, SortOption} from "./constants";
import {createUiMessage, type UiMessage} from "./messages";

declare global {
  interface Window {
    __MARK_READ_DEBUG__?: Array<Record<string, unknown>>;
  }
}

export type MarkReadRequest = {
  requestId: string;
  paperId: number;
  originSelectedId: number | null;
  plannedNextSelectedId: number | null;
  startedAt: number;
};

export type RefreshRequestFlags = {all: boolean; admin: boolean; review: boolean};

export type LocalMutation = {
  requestId: string;
  kind: "mark-read" | "bulk-mark-read" | "feedback-save" | "feedback-delete";
  entityId: number;
  startedAt: number;
};

// 集中保存跨功能状态和引用，领域行为由各自 hook 管理。
export function useAppState() {
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
  const [selectedJournals, setSelectedJournals] = React.useState<string[]>([]);
  const [dateFilter, setDateFilter] = React.useState<DateFilter>("30d");
  const [readFilter, setReadFilter] = React.useState<ReadFilter>("unread");
  const [feedbackFilter, setFeedbackFilter] = React.useState<FeedbackFilter>("all");
  const [sortOption, setSortOption] = React.useState<SortOption>("date-desc");
  const [markReadRequest, setMarkReadRequest] = React.useState<MarkReadRequest | null>(null);
  const [bulkReadSubmitting, setBulkReadSubmitting] = React.useState(false);
  const [pendingReadOverrides, setPendingReadOverrides] = React.useState<Record<number, string | null>>({});
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
  const [adminTab, setAdminTab] = React.useState<AdminTab>("dashboard");
  const [themePreference, setThemePreference] = React.useState<"system" | "light" | "dark">("system");
  const [systemTheme, setSystemTheme] = React.useState<"light" | "dark">("light");
  const knownJobStateRef = React.useRef(new Map<string, string>());
  const jobsHydratedRef = React.useRef(false);
  const bootstrapRefreshRef = React.useRef<string | null>(null);
  const activeJobMessageRef = React.useRef<string | null>(null);
  const queuedRefreshRef = React.useRef<RefreshRequestFlags>({all: false, admin: false, review: false});
  const markReadRequestRef = React.useRef<MarkReadRequest | null>(null);
  const localMutationRef = React.useRef<LocalMutation | null>(null);
  const profileRef = React.useRef<ClassificationProfile | null>(null);
  const pendingReadOverridesRef = React.useRef<Record<number, string | null>>({});
  const selectedIdRef = React.useRef<number | null>(null);
  const markReadSequenceRef = React.useRef(0);
  const feedbackMutationSequenceRef = React.useRef(0);
  const reportRefreshSequenceRef = React.useRef(0);
  const reportRefreshInflightRef = React.useRef(0);
  const deferredQuery = React.useDeferredValue(query);
  const resolvedTheme = themePreference === "system" ? systemTheme : themePreference;
  const markReadSubmitting = markReadRequest != null;
  const readMutationSubmitting = markReadSubmitting || bulkReadSubmitting;
  const markReadDebugEnabled = React.useMemo(() => {
    try {
      return new URLSearchParams(window.location.search).get("mark-read-debug") === "1";
    } catch {
      return false;
    }
  }, []);

  const hydrateEditableFeeds = React.useCallback((items: FeedSubscription[]) => items.map((item) => ({
    ...item,
    client_id: item.client_id ?? (typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`),
  })), []);
  const pushMessage = React.useCallback((name: string, override?: {text?: string; tone?: UiMessage["tone"]}) => setMessage(createUiMessage(name, override)), []);
  const errorText = React.useCallback((error: unknown, fallback: string) => error instanceof Error && error.message.trim() ? error.message : fallback, []);
  const pushErrorMessage = React.useCallback((name: string, error: unknown, fallback: string) => pushMessage(name, {text: errorText(error, fallback), tone: "danger"}), [errorText, pushMessage]);
  const logMarkReadDebug = React.useCallback((event: string, payload: Record<string, unknown> = {}) => {
    if (!markReadDebugEnabled) return;
    const entry = {at: new Date().toISOString(), event, ...payload};
    const history = window.__MARK_READ_DEBUG__ ?? [];
    history.push(entry);
    window.__MARK_READ_DEBUG__ = history.slice(-100);
    console.info("[mark-read-debug]", entry);
  }, [markReadDebugEnabled]);
  const beginLocalMutation = React.useCallback((mutation: LocalMutation) => {
    localMutationRef.current = mutation;
    logMarkReadDebug("mutation.started", mutation);
  }, [logMarkReadDebug]);
  const endLocalMutation = React.useCallback((requestId: string) => {
    if (localMutationRef.current?.requestId !== requestId) return;
    logMarkReadDebug("mutation.finished", localMutationRef.current);
    localMutationRef.current = null;
  }, [logMarkReadDebug]);

  React.useEffect(() => {
    if (!message || message.ttlMs <= 0) return;
    const timer = window.setTimeout(() => setMessage((current) => current?.createdAt === message.createdAt ? null : current), message.ttlMs);
    return () => window.clearTimeout(timer);
  }, [message]);
  React.useEffect(() => {
    const saved = window.localStorage.getItem("feedmedaily-theme");
    if (saved === "light" || saved === "dark" || saved === "system") setThemePreference(saved);
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const applySystemTheme = () => setSystemTheme(media.matches ? "dark" : "light");
    applySystemTheme();
    media.addEventListener("change", applySystemTheme);
    return () => media.removeEventListener("change", applySystemTheme);
  }, []);
  React.useEffect(() => { document.documentElement.dataset.theme = resolvedTheme; }, [resolvedTheme]);
  React.useEffect(() => { window.localStorage.setItem("feedmedaily-theme", themePreference); }, [themePreference]);
  React.useEffect(() => { markReadRequestRef.current = markReadRequest; }, [markReadRequest]);
  React.useEffect(() => { profileRef.current = profile; }, [profile]);
  React.useEffect(() => { pendingReadOverridesRef.current = pendingReadOverrides; }, [pendingReadOverrides]);
  React.useEffect(() => { selectedIdRef.current = selectedId; }, [selectedId]);

  return {
    report, setReport, profile, setProfile, appMeta, setAppMeta, appUpdate, setAppUpdate,
    appUpdateChecking, setAppUpdateChecking, feeds, setFeeds, scheduler, setScheduler,
    settingsConfig, setSettingsConfig, feedsLoaded, setFeedsLoaded, profileResolved, setProfileResolved,
    reportLoading, setReportLoading, adminDataLoading, setAdminDataLoading,
    verificationSubmitting, setVerificationSubmitting, verificationSubmitError, setVerificationSubmitError,
    reportLoadError, setReportLoadError, adminHydrationWarning, setAdminHydrationWarning,
    message, setMessage, query, setQuery, relevance, setRelevance, selectedJournals, setSelectedJournals,
    dateFilter, setDateFilter, readFilter, setReadFilter, feedbackFilter, setFeedbackFilter,
    sortOption, setSortOption, markReadRequest, setMarkReadRequest, bulkReadSubmitting, setBulkReadSubmitting,
    pendingReadOverrides, setPendingReadOverrides, selectedId, setSelectedId,
    feedbackRecords, setFeedbackRecords, profileProposals, setProfileProposals, jobs, setJobs,
    adminOpen, setAdminOpen, feedbackPaper, setFeedbackPaper, feedbackValue, setFeedbackValue,
    feedbackNote, setFeedbackNote, feedsSaving, setFeedsSaving, schedulerSaving, setSchedulerSaving,
    settingsConfigSaving, setSettingsConfigSaving, profileSaving, setProfileSaving,
    zoteroPaper, setZoteroPaper, zoteroCollections, setZoteroCollections,
    zoteroCollectionKey, setZoteroCollectionKey, zoteroLoading, setZoteroLoading,
    zoteroSaving, setZoteroSaving, zoteroError, setZoteroError, busy, setBusy,
    appControlBusy, setAppControlBusy, adminTab, setAdminTab, themePreference, setThemePreference,
    systemTheme, knownJobStateRef, jobsHydratedRef, bootstrapRefreshRef, activeJobMessageRef,
    queuedRefreshRef, markReadRequestRef, localMutationRef, profileRef, pendingReadOverridesRef,
    selectedIdRef, markReadSequenceRef, feedbackMutationSequenceRef, reportRefreshSequenceRef,
    reportRefreshInflightRef, deferredQuery, resolvedTheme, markReadSubmitting, readMutationSubmitting,
    hydrateEditableFeeds, pushMessage, errorText, pushErrorMessage, logMarkReadDebug,
    beginLocalMutation, endLocalMutation,
  };
}

export type AppState = ReturnType<typeof useAppState>;
