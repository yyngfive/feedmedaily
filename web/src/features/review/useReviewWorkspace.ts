import React from "react";
import {flushSync} from "react-dom";

import {createFeedback, deleteFeedback, fetchZoteroCollections, markPaperRead, saveToZotero} from "../../api/client";
import {matchesDateFilter, relevanceCounts} from "../../app/utils";
import type {AppData} from "../../app/useAppData";
import type {AppState, MarkReadRequest} from "../../app/useAppState";
import type {FeedbackRecord, Paper} from "../../shared/types";

// 论文审阅 hook 管理筛选、选择和用户直接触发的论文变更。
export function useReviewWorkspace(state: AppState, data: AppData) {
  const {
    beginLocalMutation, bulkReadSubmitting, dateFilter, deferredQuery, endLocalMutation, errorText,
    feedbackFilter, feedbackMutationSequenceRef, feedbackNote, feedbackPaper, feedbackRecords,
    feedbackValue, feeds, feedsLoaded, logMarkReadDebug, markReadRequest, markReadSequenceRef,
    pendingReadOverrides, profile, pushErrorMessage, pushMessage, query, readFilter,
    readMutationSubmitting, relevance, report, reportLoadError, reportLoading, selectedId,
    selectedJournals, setBulkReadSubmitting, setDateFilter, setFeedbackFilter, setFeedbackNote,
    setFeedbackPaper, setFeedbackRecords, setFeedbackValue, setMarkReadRequest,
    setPendingReadOverrides, setQuery, setReadFilter, setRelevance, setReport, setSelectedId,
    setSelectedJournals, setSortOption, setZoteroCollectionKey, setZoteroCollections,
    setZoteroError, setZoteroLoading, setZoteroPaper, setZoteroSaving, sortOption,
    zoteroCollectionKey, zoteroCollections, zoteroError, zoteroLoading, zoteroPaper, zoteroSaving,
  } = state;
  const {refreshFeedback, refreshProposals, scheduleDeferredReviewRefresh} = data;

  const effectivePapers = React.useMemo(() => report.papers.map((paper) =>
    Object.prototype.hasOwnProperty.call(pendingReadOverrides, paper.id)
      ? {...paper, read_at: pendingReadOverrides[paper.id]}
      : paper,
  ), [pendingReadOverrides, report.papers]);
  const journals = React.useMemo(() => Array.from(new Set(effectivePapers.map((paper) => paper.journal).filter(Boolean) as string[])).sort(), [effectivePapers]);
  const journalOptions = React.useMemo(() => journals.map((item) => ({value: item, label: item})), [journals]);
  const selectedJournalSet = React.useMemo(() => new Set(selectedJournals), [selectedJournals]);
  const filteredBase = React.useMemo(() => effectivePapers.filter((paper) => {
    const haystack = [paper.title, paper.classification.translated_title_zh ?? "", paper.abstract ?? "", paper.journal ?? "", paper.authors?.join(" ") ?? "", paper.feedback_status?.note ?? ""].join(" ").toLowerCase();
    const hasFeedback = Boolean(paper.feedback_status?.has_feedback);
    return (!deferredQuery || haystack.includes(deferredQuery.toLowerCase())) &&
      (selectedJournalSet.size === 0 || Boolean(paper.journal && selectedJournalSet.has(paper.journal))) &&
      (readFilter === "all" || (readFilter === "read" ? Boolean(paper.read_at) : !paper.read_at)) &&
      (feedbackFilter === "all" || (feedbackFilter === "marked" ? hasFeedback : !hasFeedback)) &&
      matchesDateFilter(paper.published_date ?? paper.seen_date, report.report_date, dateFilter);
  }), [dateFilter, deferredQuery, effectivePapers, feedbackFilter, readFilter, report.report_date, selectedJournalSet]);
  const filtered = React.useMemo(() => filteredBase.filter((paper) => relevance === "all" || paper.classification.relevance === relevance), [filteredBase, relevance]);
  const sortedFiltered = React.useMemo(() => {
    const byDate = (paper: Paper) => paper.published_date ?? paper.seen_date;
    return [...filtered].sort((left, right) => {
      if (sortOption === "journal-asc") return (left.journal ?? "").localeCompare(right.journal ?? "") || byDate(right).localeCompare(byDate(left));
      if (sortOption === "confidence-desc" || sortOption === "confidence-asc") {
        const difference = left.classification.confidence - right.classification.confidence;
        return difference ? (sortOption === "confidence-desc" ? -difference : difference) : byDate(right).localeCompare(byDate(left));
      }
      const difference = byDate(left).localeCompare(byDate(right));
      return sortOption === "date-desc" ? -difference : difference;
    });
  }, [filtered, sortOption]);
  const lastUpdateLabel = React.useMemo(() => report.last_updated_at?.slice(0, 10) ?? "Never", [report.last_updated_at]);
  const needsFeedSetup = Boolean(profile && feedsLoaded && feeds.length === 0);
  const hasNoFetchedPapers = feedsLoaded && !reportLoading && !reportLoadError && !needsFeedSetup && feeds.length > 0 && report.papers.length === 0;
  const visibleBase = React.useMemo(() => needsFeedSetup ? [] : filteredBase, [filteredBase, needsFeedSetup]);
  const visibleList = React.useMemo(() => needsFeedSetup ? [] : sortedFiltered, [needsFeedSetup, sortedFiltered]);
  const visibleTotals = React.useMemo(() => relevanceCounts(visibleBase), [visibleBase]);

  React.useEffect(() => {
    if (readMutationSubmitting) return;
    if (visibleList.length === 0) return setSelectedId(null);
    if (!selectedId || !visibleList.some((paper) => paper.id === selectedId)) setSelectedId(visibleList[0].id);
  }, [readMutationSubmitting, selectedId, setSelectedId, visibleList]);

  React.useEffect(() => {
    if (!zoteroPaper) return;
    let cancelled = false;
    setZoteroLoading(true);
    setZoteroError(null);
    setZoteroCollections([]);
    setZoteroCollectionKey("");
    void fetchZoteroCollections()
      .then((payload) => {
        if (!cancelled) {
          setZoteroCollections(payload.collections);
          setZoteroCollectionKey(payload.default_collection_key ?? "");
        }
      })
      .catch((error) => { if (!cancelled) setZoteroError((error as Error).message); })
      .finally(() => { if (!cancelled) setZoteroLoading(false); });
    return () => { cancelled = true; };
  }, [setZoteroCollectionKey, setZoteroCollections, setZoteroError, setZoteroLoading, zoteroPaper]);

  const selectedPaperId = React.useMemo(() => {
    if (!markReadRequest) return selectedId;
    if (selectedId != null && selectedId !== markReadRequest.originSelectedId && selectedId !== markReadRequest.paperId) return selectedId;
    return markReadRequest.paperId;
  }, [markReadRequest, selectedId]);
  const selectedPaper = React.useMemo(() => {
    if (needsFeedSetup || selectedPaperId == null) return null;
    return visibleList.find((paper) => paper.id === selectedPaperId) ?? effectivePapers.find((paper) => paper.id === selectedPaperId) ?? null;
  }, [effectivePapers, needsFeedSetup, selectedPaperId, visibleList]);
  const selectedRangeUnreadPapers = React.useMemo(() => {
    if (selectedPaperId == null) return [];
    const selectedIndex = visibleList.findIndex((paper) => paper.id === selectedPaperId);
    return selectedIndex < 0 ? [] : visibleList.slice(0, selectedIndex + 1).filter((paper) => !paper.read_at);
  }, [selectedPaperId, visibleList]);

  React.useEffect(() => logMarkReadDebug("selection.changed", {selectedId, displayedSelectedId: selectedPaperId, selectedPaperId: selectedPaper?.id ?? null, requestId: markReadRequest?.requestId ?? null}), [logMarkReadDebug, markReadRequest?.requestId, selectedId, selectedPaper?.id, selectedPaperId]);
  React.useEffect(() => logMarkReadDebug("visible-list.changed", {readFilter, count: visibleList.length, firstPaperIds: visibleList.slice(0, 8).map((paper) => paper.id), overrideIds: Object.keys(pendingReadOverrides).map(Number)}), [logMarkReadDebug, pendingReadOverrides, readFilter, visibleList]);

  const updatePaper = (paperId: number, updater: (paper: Paper) => Paper) => setReport((current) => ({...current, papers: current.papers.map((paper) => paper.id === paperId ? updater(paper) : paper)}));
  const applyFeedbackRecordToPaper = React.useCallback((record: FeedbackRecord) => updatePaper(record.paper_id, (paper) => ({...paper, feedback_status: {has_feedback: true, corrected_relevance: record.corrected_relevance, note: record.note ?? null, latest_feedback_at: record.created_at, state: record.state, used_in_profile: record.used_in_profile}})), []);
  const clearPaperFeedbackStatus = React.useCallback((paperId: number) => updatePaper(paperId, (paper) => ({...paper, feedback_status: null})), []);

  const persistReadStatus = async (paperId: number) => {
    const paper = effectivePapers.find((item) => item.id === paperId);
    if (!paper || readMutationSubmitting) return;
    const nextRead = !paper.read_at;
    const requestId = `mark-read-${++markReadSequenceRef.current}`;
    const visibleIds = visibleList.map((item) => item.id);
    const currentIndex = visibleIds.indexOf(paperId);
    let plannedNextSelectedId: number | null = paperId;
    if (nextRead && readFilter === "unread" && currentIndex >= 0) plannedNextSelectedId = currentIndex + 1 < visibleIds.length ? visibleIds[currentIndex + 1] : currentIndex > 0 ? visibleIds[currentIndex - 1] : null;
    const request: MarkReadRequest = {requestId, paperId, originSelectedId: selectedId, plannedNextSelectedId, startedAt: performance.now()};
    beginLocalMutation({requestId, kind: "mark-read", entityId: paperId, startedAt: request.startedAt});
    logMarkReadDebug("mark-read.clicked", {requestId, paperId, readFilter, nextRead, selectedId, plannedNextSelectedId, visibleIds: visibleIds.slice(0, 12)});
    setMarkReadRequest(request);
    let succeeded = false;
    try {
      logMarkReadDebug("mark-read.request.started", {requestId, paperId, nextRead});
      const status = await markPaperRead(paperId, nextRead);
      logMarkReadDebug("mark-read.request.succeeded", {requestId, paperId, durationMs: Math.round(performance.now() - request.startedAt), readAt: status.read_at});
      succeeded = true;
      flushSync(() => {
        setPendingReadOverrides((current) => ({...current, [paperId]: status.read_at}));
        setSelectedId((current) => current != null && current !== request.originSelectedId && current !== request.paperId ? current : request.plannedNextSelectedId);
        setMarkReadRequest((current) => current?.requestId === requestId ? null : current);
      });
    } catch (error) {
      logMarkReadDebug("mark-read.request.failed", {requestId, paperId, durationMs: Math.round(performance.now() - request.startedAt), message: errorText(error, "Could not update read status.")});
      flushSync(() => setMarkReadRequest((current) => current?.requestId === requestId ? null : current));
      pushMessage("paper.read.failed", {text: (error as Error).message, tone: "danger"});
    } finally {
      endLocalMutation(requestId);
      if (succeeded) scheduleDeferredReviewRefresh(`mark-read:${requestId}:reconcile`);
    }
  };

  const persistPaperBatchRead = async (unreadPapers: Paper[], requestPrefix: string, successLabel: string, clearSelectedWhenMarked: boolean) => {
    if (unreadPapers.length === 0 || readMutationSubmitting) return;
    const requestId = `${requestPrefix}-${++markReadSequenceRef.current}`;
    const startedAt = performance.now();
    beginLocalMutation({requestId, kind: "bulk-mark-read", entityId: 0, startedAt});
    setBulkReadSubmitting(true);
    logMarkReadDebug("bulk-mark-read.started", {requestId, count: unreadPapers.length, paperIds: unreadPapers.slice(0, 25).map((paper) => paper.id)});
    try {
      const settledResults = await Promise.allSettled(unreadPapers.map(async (paper) => ({paperId: paper.id, status: await markPaperRead(paper.id)})));
      const results = settledResults.flatMap((result) => result.status === "fulfilled" ? [result.value] : []);
      const failedCount = settledResults.length - results.length;
      flushSync(() => {
        setPendingReadOverrides((current) => {
          const next = {...current};
          results.forEach((result) => { next[result.paperId] = result.status.read_at; });
          return next;
        });
        setSelectedId((current) => clearSelectedWhenMarked && current != null && results.some((result) => result.paperId === current) ? null : current);
      });
      pushMessage(failedCount ? "paper.bulk_read.failed" : "paper.bulk_read.succeeded", {
        text: failedCount ? `Marked ${results.length} ${successLabel}${results.length === 1 ? "" : "s"} as read; ${failedCount} failed.` : `Marked ${results.length} ${successLabel}${results.length === 1 ? "" : "s"} as read.`,
        tone: failedCount ? (results.length ? "warning" : "danger") : "success",
      });
      scheduleDeferredReviewRefresh(`${requestPrefix}:${requestId}:reconcile`);
      logMarkReadDebug("bulk-mark-read.succeeded", {requestId, durationMs: Math.round(performance.now() - startedAt), count: results.length, failedCount});
    } catch (error) {
      pushMessage("paper.bulk_read.failed", {text: errorText(error, "Could not mark visible papers as read."), tone: "danger"});
      logMarkReadDebug("bulk-mark-read.failed", {requestId, durationMs: Math.round(performance.now() - startedAt), message: errorText(error, "Could not mark visible papers as read.")});
    } finally {
      setBulkReadSubmitting(false);
      endLocalMutation(requestId);
    }
  };

  const persistVisibleReadStatus = () => persistPaperBatchRead(visibleList.filter((paper) => !paper.read_at), "bulk-mark-read", "visible paper", true);
  const persistSelectedRangeReadStatus = () => persistPaperBatchRead(selectedRangeUnreadPapers, "mark-above-read", "selected-range paper", false);
  const openZoteroModal = (paper: Paper) => { setZoteroPaper(paper); setZoteroError(null); };
  const handleSaveToZotero = async () => {
    if (!zoteroPaper) return;
    try {
      setZoteroSaving(true);
      const status = await saveToZotero(zoteroPaper.id, zoteroCollectionKey || null);
      updatePaper(zoteroPaper.id, (paper) => ({...paper, zotero_status: status}));
      if (status.saved) { pushMessage("zotero.save.succeeded"); setZoteroPaper(null); setZoteroError(null); return; }
      setZoteroError(status.last_error ?? "Zotero save updated.");
    } catch (error) {
      setZoteroError((error as Error).message);
      pushMessage("app.service.unavailable", {text: errorText(error, "Could not save to Zotero."), tone: "danger"});
    } finally { setZoteroSaving(false); }
  };
  const openFeedbackModal = (paper: Paper) => { setFeedbackPaper(paper); setFeedbackValue(paper.feedback_status?.corrected_relevance ?? paper.classification.relevance); setFeedbackNote(paper.feedback_status?.note ?? ""); };
  const submitFeedback = async () => {
    if (!feedbackPaper) return;
    const requestId = `feedback-save-${++feedbackMutationSequenceRef.current}`;
    const startedAt = performance.now();
    beginLocalMutation({requestId, kind: "feedback-save", entityId: feedbackPaper.id, startedAt});
    logMarkReadDebug("feedback.save.started", {requestId, paperId: feedbackPaper.id, correctedRelevance: feedbackValue});
    try {
      const record = await createFeedback({paper_id: feedbackPaper.id, corrected_relevance: feedbackValue, note: feedbackNote.trim() || undefined});
      logMarkReadDebug("feedback.save.succeeded", {requestId, paperId: record.paper_id, feedbackId: record.id, durationMs: Math.round(performance.now() - startedAt)});
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
      logMarkReadDebug("feedback.save.failed", {requestId, paperId: feedbackPaper.id, durationMs: Math.round(performance.now() - startedAt), message: errorText(error, "Could not save feedback.")});
      pushErrorMessage("feedback.save.failed", error, "Could not save feedback.");
    } finally { endLocalMutation(requestId); }
  };
  const handleDeleteFeedback = async (feedbackId: number) => {
    const existingRecord = feedbackRecords.find((item) => item.id === feedbackId) ?? null;
    const requestId = `feedback-delete-${++feedbackMutationSequenceRef.current}`;
    const startedAt = performance.now();
    beginLocalMutation({requestId, kind: "feedback-delete", entityId: feedbackId, startedAt});
    logMarkReadDebug("feedback.delete.started", {requestId, feedbackId, paperId: existingRecord?.paper_id ?? null});
    try {
      await deleteFeedback(feedbackId);
      logMarkReadDebug("feedback.delete.succeeded", {requestId, feedbackId, paperId: existingRecord?.paper_id ?? null, durationMs: Math.round(performance.now() - startedAt)});
      flushSync(() => {
        setFeedbackRecords((current) => current.filter((item) => item.id !== feedbackId));
        if (existingRecord) clearPaperFeedbackStatus(existingRecord.paper_id);
      });
      void refreshFeedback();
      void refreshProposals();
      scheduleDeferredReviewRefresh("feedback.delete");
      pushMessage("feedback.delete.succeeded");
    } catch (error) {
      logMarkReadDebug("feedback.delete.failed", {requestId, feedbackId, paperId: existingRecord?.paper_id ?? null, durationMs: Math.round(performance.now() - startedAt), message: errorText(error, "Could not delete feedback.")});
      pushErrorMessage("feedback.delete.failed", error, "Could not delete feedback.");
    } finally { endLocalMutation(requestId); }
  };
  const toggleJournalFilter = React.useCallback((value: string) => setSelectedJournals((current) => current.includes(value) ? current.filter((item) => item !== value) : [...current, value].sort()), [setSelectedJournals]);
  const resetFilters = React.useCallback(() => {
    setSelectedJournals([]); setDateFilter("30d"); setReadFilter("unread"); setFeedbackFilter("all"); setSortOption("date-desc"); setRelevance("all"); setQuery("");
  }, [setDateFilter, setFeedbackFilter, setQuery, setReadFilter, setRelevance, setSelectedJournals, setSortOption]);

  return {
    effectivePapers, journalOptions, lastUpdateLabel, needsFeedSetup, hasNoFetchedPapers,
    visibleBase, visibleList, visibleTotals, selectedPaper, selectedPaperId, selectedRangeUnreadPapers,
    persistReadStatus, persistVisibleReadStatus, persistSelectedRangeReadStatus,
    openZoteroModal, handleSaveToZotero, openFeedbackModal, submitFeedback, handleDeleteFeedback,
    toggleJournalFilter, resetFilters, query,
    zoteroCollections, zoteroCollectionKey, zoteroLoading, zoteroSaving, zoteroError,
  };
}

export type ReviewWorkspace = ReturnType<typeof useReviewWorkspace>;
