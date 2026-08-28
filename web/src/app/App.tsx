import React from "react";

import {AdminPanel} from "../features/admin/AdminPanel";
import {Onboarding} from "../features/onboarding/Onboarding";
import {DetailPanel} from "../features/review/DetailPanel";
import {FeedbackModal} from "../features/review/FeedbackModal";
import {FiltersSidebar} from "../features/review/FiltersSidebar";
import {PaperListSection} from "../features/review/PaperListSection";
import {useReviewWorkspace} from "../features/review/useReviewWorkspace";
import {ZoteroSaveModal} from "../features/review/ZoteroSaveModal";
import {AppStatusBar} from "../shared/components/AppStatusBar";
import {StatusBanner} from "../shared/components/StatusBanner";
import {TopBar} from "../shared/components/TopBar";
import {useAdminActions} from "./useAdminActions";
import {useAppData} from "./useAppData";
import {useAppState} from "./useAppState";

// 应用根组件只负责加载门控、主题和功能模块组合。
export function App() {
  const state = useAppState();
  const data = useAppData(state);
  const review = useReviewWorkspace(state, data);
  const admin = useAdminActions(state, data);
  const toggleTheme = React.useCallback(() => {
    const nextResolvedTheme = state.resolvedTheme === "dark" ? "light" : "dark";
    state.setThemePreference(nextResolvedTheme === state.systemTheme ? "system" : nextResolvedTheme);
  }, [state.resolvedTheme, state.setThemePreference, state.systemTheme]);

  if (!state.profileResolved) {
    return (
      <main className="flex min-h-screen flex-col bg-[--paper] text-[--ink]">
        <TopBar message={state.message} onOpenAdmin={() => state.setAdminOpen(true)} onToggleTheme={toggleTheme} resolvedTheme={state.resolvedTheme} usingSystemTheme={state.themePreference === "system"} />
        <div className="mx-auto flex w-full max-w-5xl flex-1 items-center justify-center px-4 py-8">
          <div className="rounded-lg border border-(--line) bg-(--paper-accent) px-5 py-4 text-sm text-muted">Loading your library...</div>
        </div>
        <AppStatusBar
          appMeta={state.appMeta}
          appUpdate={state.appUpdate}
          appUpdateChecking={state.appUpdateChecking}
          busy={state.appControlBusy}
          onCheckForUpdates={() => void data.handleCheckAppUpdate()}
          onExit={() => void admin.handleExitApp()}
          onOpenData={() => void admin.handleOpenAppTarget("data_dir")}
          onOpenInstall={() => void admin.handleOpenAppTarget("install_dir")}
          onOpenLogs={() => void admin.handleOpenAppTarget("logs_dir")}
        />
      </main>
    );
  }

  if (!state.profile) {
    return (
      <>
        {state.message ? <StatusBanner className="mx-auto mt-4 max-w-5xl" tone={state.message.tone}>{state.message.text}</StatusBanner> : null}
        <Onboarding
          busy={data.onboardingBusy}
          configFields={state.settingsConfig}
          configSaving={state.settingsConfigSaving}
          classifierModels={state.classifierModels}
          jobs={state.jobs}
          proposals={state.profileProposals}
          onAcceptDraft={admin.handleOnboardingAcceptDraft}
          onRejectProposal={admin.handleOnboardingRejectProposal}
          onSaveSettings={admin.handleOnboardingSaveSettings}
          onSaveAndBootstrap={admin.handleOnboardingSaveAndBootstrap}
          onTestClassifierModel={admin.handleTestClassifierModel}
        />
      </>
    );
  }

  return (
    <main className="fixed inset-0 flex flex-col overflow-hidden bg-[--paper] text-[--ink]">
      <TopBar message={state.message} onOpenAdmin={() => state.setAdminOpen(true)} onToggleTheme={toggleTheme} resolvedTheme={state.resolvedTheme} usingSystemTheme={state.themePreference === "system"} />
      {state.adminHydrationWarning ? <StatusBanner className="mx-auto mt-3 w-full max-w-375 px-4" tone="warning">{state.adminHydrationWarning}</StatusBanner> : null}
      <AdminPanel
        activeTab={state.adminTab}
        appMeta={state.appMeta}
        appUpdate={state.appUpdate}
        appUpdateChecking={state.appUpdateChecking}
        configFields={state.settingsConfig}
        configSaving={state.settingsConfigSaving}
        classifierModels={state.classifierModels}
        open={state.adminOpen}
        profile={state.profile}
        profileSaving={state.profileSaving}
        hasFeeds={state.feeds.length > 0}
        feeds={state.feeds}
        feedsSaving={state.feedsSaving}
        feedback={state.feedbackRecords}
        jobs={state.jobs}
        proposalGenerating={Boolean(data.profileProposalJob)}
        proposals={state.profileProposals}
        onClose={() => state.setAdminOpen(false)}
        onFeedChange={admin.handleFeedChange}
        onAddFeed={admin.handleAddFeed}
        onAddFeeds={admin.handleAddFeeds}
        onCheckForUpdates={() => void data.handleCheckAppUpdate()}
        onRemoveFeed={admin.handleRemoveFeed}
        onSaveConfig={admin.handleSaveConfig}
        onTestClassifierModel={admin.handleTestClassifierModel}
        onSaveProfile={admin.handleSaveProfile}
        onSaveScheduler={admin.handleSaveScheduler}
        onSaveFeeds={() => void admin.handleSaveFeeds()}
        onTabChange={state.setAdminTab}
        onDeleteScheduler={admin.handleDeleteScheduler}
        onGenerateProposal={() => void admin.handleGenerateProposal()}
        onStartVerification={(job) => void admin.handleStartVerification(job)}
        onOpenVerificationInBrowser={(job) => void admin.handleOpenVerificationInBrowser(job)}
        onSubmitVerificationXML={admin.handleSubmitVerificationXML}
        onApplyProposal={(id, selection) => void admin.handleApplyProposal(id, selection)}
        onRejectProposal={(id) => void admin.handleRejectProposal(id)}
        onRunSync={(feedURLs) => void admin.handleRunAdminJob("/api/admin/run", feedURLs)}
        onStopSync={admin.handleStopSync}
        onReclassifyRecent={() => void admin.handleReclassify("recent")}
        onReclassifyFeedback={() => void admin.handleReclassify("feedback")}
        onReclassifyAll={() => void admin.handleReclassify("all")}
        onDeleteFeedback={(id) => void review.handleDeleteFeedback(id)}
        scheduler={state.scheduler}
        schedulerSaving={state.schedulerSaving}
        verificationSubmitting={state.verificationSubmitting}
        verificationSubmitError={state.verificationSubmitError}
      />
      <FeedbackModal paper={state.feedbackPaper} value={state.feedbackValue} note={state.feedbackNote} onValueChange={state.setFeedbackValue} onNoteChange={state.setFeedbackNote} onClose={() => state.setFeedbackPaper(null)} onSubmit={() => void review.submitFeedback()} />
      <ZoteroSaveModal paper={state.zoteroPaper} collections={review.zoteroCollections} selectedCollectionKey={review.zoteroCollectionKey} loading={review.zoteroLoading} saving={review.zoteroSaving} error={review.zoteroError} onCollectionChange={state.setZoteroCollectionKey} onClose={() => state.setZoteroPaper(null)} onSubmit={() => void review.handleSaveToZotero()} />

      <div className="mx-auto grid min-h-0 w-full max-w-375 flex-1 gap-4 overflow-hidden px-4 py-4 lg:grid-cols-[300px_minmax(0,1fr)_360px]">
        <FiltersSidebar
          dateFilter={state.dateFilter}
          feedbackFilter={state.feedbackFilter}
          journalOptions={review.journalOptions}
          selectedJournals={state.selectedJournals}
          lastUpdateLabel={review.lastUpdateLabel}
          onDateFilterChange={state.setDateFilter}
          onFeedbackFilterChange={state.setFeedbackFilter}
          onJournalClear={() => state.setSelectedJournals([])}
          onJournalToggle={review.toggleJournalFilter}
          onReadFilterChange={state.setReadFilter}
          onReset={review.resetFilters}
          onSortChange={state.setSortOption}
          profileName={state.profile.meta.name}
          profileVersion={state.profile.meta.version}
          readFilter={state.readFilter}
          shownCount={review.visibleList.length}
          sortOption={state.sortOption}
          totalCount={review.needsFeedSetup ? 0 : state.report.papers.length}
          visibleTotals={review.visibleTotals}
        />
        <PaperListSection
          hasNoFetchedPapers={review.hasNoFetchedPapers}
          loadError={state.reportLoadError}
          loading={(state.reportLoading || !state.feedsLoaded) && state.report.papers.length === 0}
          markAllReadBusy={state.bulkReadSubmitting}
          needsFeedSetup={review.needsFeedSetup}
          onMarkAllRead={() => void review.persistVisibleReadStatus()}
          onMarkSelectedRangeRead={() => void review.persistSelectedRangeReadStatus()}
          onOpenAdmin={() => state.setAdminOpen(true)}
          onResetFilters={review.resetFilters}
          onRunSync={() => void admin.handleRunAdminJob("/api/admin/run")}
          onSelectPaper={(paper) => state.setSelectedId(paper.id)}
          onStartFeedSetup={() => {
            if (state.feeds.length === 0) admin.handleAddFeed();
            state.setAdminTab("feeds");
            state.setAdminOpen(true);
          }}
          papers={review.visibleList}
          query={state.query}
          reportErrors={state.report.errors}
          relevance={state.relevance}
          selectedId={review.needsFeedSetup ? null : review.selectedPaperId}
          setQuery={state.setQuery}
          setRelevance={state.setRelevance}
          unreadSelectedRangeCount={review.selectedRangeUnreadPapers.length}
          unreadVisibleCount={review.visibleList.filter((paper) => !paper.read_at).length}
          visibleBaseCount={review.visibleBase.length}
          visibleTotals={review.visibleTotals}
        />
        <DetailPanel
          paper={review.needsFeedSetup ? null : review.selectedPaper}
          isUnread={Boolean(review.selectedPaper && !review.selectedPaper.read_at)}
          markReadBusy={review.selectedPaper?.id === state.markReadRequest?.paperId && state.markReadSubmitting}
          onMarkRead={() => review.selectedPaper && void review.persistReadStatus(review.selectedPaper.id)}
          onMarkWrong={() => review.selectedPaper && review.openFeedbackModal(review.selectedPaper)}
          onSave={() => review.selectedPaper && review.openZoteroModal(review.selectedPaper)}
        />
      </div>
      <AppStatusBar
        appMeta={state.appMeta}
        appUpdate={state.appUpdate}
        appUpdateChecking={state.appUpdateChecking}
        busy={state.appControlBusy}
        onCheckForUpdates={() => void data.handleCheckAppUpdate()}
        onExit={() => void admin.handleExitApp()}
        onOpenData={() => void admin.handleOpenAppTarget("data_dir")}
        onOpenInstall={() => void admin.handleOpenAppTarget("install_dir")}
        onOpenLogs={() => void admin.handleOpenAppTarget("logs_dir")}
      />
    </main>
  );
}
