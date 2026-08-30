import React from "react";

import {
  applyProfileProposal,
  bootstrapProfile,
  cancelAdminJob,
  deleteSchedulerSettings,
  exitApp,
  launchAdminJob,
  launchProfileProposalGeneration,
  launchReclassifyJob,
  openAppTarget,
  openFeedVerificationInBrowser,
  rejectProfileProposal,
  saveCurrentProfile,
  saveFeedSubscriptions,
  saveSchedulerSettings,
  saveSettingsConfig,
  startFeedVerification,
  submitFeedVerificationXML,
  testClassifierModel,
} from "../api/client";
import type {ClassifierModelsUpdate, ClassificationProfile, FeedSubscription, JobInfo, SettingsConfigUpdate} from "../shared/types";
import type {ReclassifyScope} from "../api/client";
import type {AppData} from "./useAppData";
import type {AppState} from "./useAppState";

// 管理操作 hook 负责设置、Profile、后台任务和验证入口。
export function useAdminActions(state: AppState, data: AppData) {
  const {
    errorText, feeds, hydrateEditableFeeds, pushErrorMessage, pushMessage, setAdminOpen,
    setAppControlBusy, setBusy, setFeeds, setFeedsLoaded, setFeedsSaving, setJobs,
    setProfile, setProfileSaving, setScheduler, setSchedulerSaving, setSettingsConfig, setClassifierModels,
    setSettingsConfigSaving, setVerificationSubmitError, setVerificationSubmitting,
  } = state;
  const {
    refreshAdminData, refreshFeedback, refreshProfileGate, refreshProposals, refreshReviewCore,
  } = data;

  const registerJob = React.useCallback((job: JobInfo, openAdmin = true) => {
    setJobs((current) => [job, ...current.filter((item) => item.id !== job.id)]);
    if (openAdmin) setAdminOpen(true);
  }, [setAdminOpen, setJobs]);
  const handleSaveFeeds = async (nextFeeds?: FeedSubscription[]) => {
    const cleaned = (nextFeeds ?? feeds).map((item) => ({journal: item.journal.trim(), url: item.url.trim()})).filter((item) => item.journal || item.url);
    if (cleaned.some((item) => !item.journal || !item.url)) {
      pushMessage("feeds.validation.failed");
      return false;
    }
    try {
      setFeedsSaving(true);
      setFeeds(hydrateEditableFeeds(await saveFeedSubscriptions(cleaned)));
      setFeedsLoaded(true);
      pushMessage("feeds.save.succeeded");
      return true;
    } catch (error) {
      pushErrorMessage("feeds.save.failed", error, "Could not save RSS feed settings.");
      return false;
    } finally { setFeedsSaving(false); }
  };

  const handleSaveConfig = React.useCallback(async (fields: Record<string, SettingsConfigUpdate>, classifierModels?: ClassifierModelsUpdate) => {
    try {
      setSettingsConfigSaving(true);
      const saved = await saveSettingsConfig(fields, classifierModels);
      setSettingsConfig(saved.fields);
      setClassifierModels(saved.classifier_models);
      const currentProfile = await refreshProfileGate();
      await Promise.all([refreshAdminData(), currentProfile ? refreshReviewCore(currentProfile) : Promise.resolve()]);
      pushMessage("settings.config.save.succeeded");
    } catch (error) {
      pushErrorMessage("settings.config.save.failed", error, "Could not save local settings.");
    } finally { setSettingsConfigSaving(false); }
  }, [pushErrorMessage, pushMessage, refreshAdminData, refreshProfileGate, refreshReviewCore, setClassifierModels, setSettingsConfig, setSettingsConfigSaving]);
  const handleSaveScheduler = React.useCallback(async (dailyTime: string) => {
    try {
      setSchedulerSaving(true);
      const saved = await saveSchedulerSettings(dailyTime);
      setScheduler(saved);
      pushMessage("scheduler.save.succeeded", saved.automatic_supported === false ? {text: "Daily sync time saved locally. Automatic runs are unavailable on this platform; use cron instead.", tone: "warning"} : undefined);
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not save local scheduler settings.");
    } finally { setSchedulerSaving(false); }
  }, [pushErrorMessage, pushMessage, setScheduler, setSchedulerSaving]);
  const handleSaveProfile = React.useCallback(async (nextProfile: ClassificationProfile) => {
    try {
      setProfileSaving(true);
      const saved = await saveCurrentProfile(nextProfile);
      setProfile(saved.profile);
      pushMessage("profile.current.save.succeeded");
      try {
        registerJob(await launchReclassifyJob({scope: "feedback", limit: 0}));
        pushMessage("job.reclassify.started");
      } catch (error) {
        pushErrorMessage("app.service.unavailable", error, "Could not start the feedback reclassification job.");
      }
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not save the local profile.");
      throw error;
    } finally { setProfileSaving(false); }
  }, [pushErrorMessage, pushMessage, registerJob, setProfile, setProfileSaving]);
  const handleDeleteScheduler = React.useCallback(async () => {
    try {
      setSchedulerSaving(true);
      setScheduler(await deleteSchedulerSettings());
      pushMessage("scheduler.delete.succeeded");
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not disable local scheduler settings.");
    } finally { setSchedulerSaving(false); }
  }, [pushErrorMessage, pushMessage, setScheduler, setSchedulerSaving]);
  const handleOpenAppTarget = React.useCallback(async (target: "data_dir" | "logs_dir" | "install_dir") => {
    try {
      setAppControlBusy(true);
      await openAppTarget(target);
      pushMessage("app.control.open.succeeded");
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not open the selected local target.");
    } finally { setAppControlBusy(false); }
  }, [pushErrorMessage, pushMessage, setAppControlBusy]);
  const handleExitApp = React.useCallback(async () => {
    try {
      setAppControlBusy(true);
      await exitApp();
      pushMessage("app.control.exit.succeeded");
      window.setTimeout(() => { window.location.href = "about:blank"; }, 350);
    } catch (error) {
      setAppControlBusy(false);
      pushErrorMessage("app.service.unavailable", error, "Could not exit the local FeedMeDaily service.");
    }
  }, [pushErrorMessage, pushMessage, setAppControlBusy]);

  const handleOnboardingSaveAndBootstrap = React.useCallback(async (fields: Record<string, SettingsConfigUpdate>, classifierModels: ClassifierModelsUpdate, interestDescription: string) => {
    try {
      setSettingsConfigSaving(true);
      const saved = await saveSettingsConfig(fields, classifierModels);
      setSettingsConfig(saved.fields);
      setClassifierModels(saved.classifier_models);
      const currentProfile = await refreshProfileGate();
      await Promise.all([refreshAdminData(), currentProfile ? refreshReviewCore(currentProfile) : Promise.resolve()]);
    } catch (error) {
      return {ok: false, tone: "danger" as const, message: errorText(error, "Could not save local settings.")};
    } finally { setSettingsConfigSaving(false); }
    try {
      setBusy(true);
      registerJob(await bootstrapProfile({interest_description: interestDescription}), false);
      return {ok: true, tone: "info" as const, message: "Local settings saved. Initial profile generation started."};
    } catch (error) {
      return {ok: false, tone: "warning" as const, message: `Local settings were saved, but the initial profile generation did not start: ${errorText(error, "Unknown error.")}`};
    } finally { setBusy(false); }
  }, [errorText, refreshAdminData, refreshProfileGate, refreshReviewCore, registerJob, setBusy, setClassifierModels, setSettingsConfig, setSettingsConfigSaving]);
  const handleOnboardingSaveSettings = React.useCallback(async (fields: Record<string, SettingsConfigUpdate>, classifierModels: ClassifierModelsUpdate) => {
    try {
      setSettingsConfigSaving(true);
      const saved = await saveSettingsConfig(fields, classifierModels);
      setSettingsConfig(saved.fields);
      setClassifierModels(saved.classifier_models);
      const currentProfile = await refreshProfileGate();
      await Promise.all([refreshAdminData(), currentProfile ? refreshReviewCore(currentProfile) : Promise.resolve()]);
      return {ok: true, tone: "success" as const, message: "Local settings saved."};
    } catch (error) {
      return {ok: false, tone: "danger" as const, message: errorText(error, "Could not save local settings.")};
    } finally { setSettingsConfigSaving(false); }
  }, [errorText, refreshAdminData, refreshProfileGate, refreshReviewCore, setClassifierModels, setSettingsConfig, setSettingsConfigSaving]);

  const handleTestClassifierModel = React.useCallback(async (modelId: string, apiKey?: string) => {
    try {
      const job = await testClassifierModel(modelId, apiKey);
      // Connection tests are inline on both onboarding and Settings → Model;
      // registering the job must not unexpectedly open the settings drawer.
      registerJob(job, false);
      pushMessage("classifier.model.test.started", {text: "Connection test queued. It uses a small amount of provider quota.", tone: "info"});
      return job;
    } catch (error) {
      pushErrorMessage("classifier.model.test.failed", error, "Could not start the classifier model connection test.");
      throw error;
    }
  }, [pushErrorMessage, pushMessage, registerJob]);

  const handleGenerateProposal = async () => {
    try { registerJob(await launchProfileProposalGeneration()); pushMessage("profile.proposal.started"); }
    catch (error) { pushErrorMessage("app.service.unavailable", error, "Could not start profile proposal generation."); }
  };
  const handleApplyProposal = async (id: number, selection?: {accepted_change_ids: string[]; rejected_change_ids: string[]}) => {
    try {
      setBusy(true);
      const {proposal, job} = await applyProfileProposal(id, selection);
      if (job) { registerJob(job); pushMessage("job.reclassify.started"); }
      const currentProfile = await refreshProfileGate();
      await Promise.all([refreshFeedback(), refreshProposals(), currentProfile ? refreshReviewCore(currentProfile) : Promise.resolve()]);
      pushMessage("profile.proposal.applied");
    } catch (error) {
      pushErrorMessage("app.service.unavailable", error, "Could not apply the profile proposal.");
    } finally { setBusy(false); }
  };
  const handleRejectProposal = async (id: number) => {
    try { await rejectProfileProposal(id); await refreshProposals(); pushMessage("profile.proposal.rejected"); }
    catch (error) { pushErrorMessage("app.service.unavailable", error, "Could not reject the profile proposal."); }
  };
  const handleOnboardingAcceptDraft = React.useCallback(async (id: number, draftProfile: ClassificationProfile) => {
    try { setBusy(true); await applyProfileProposal(id); }
    catch (error) { return {ok: false, tone: "danger" as const, message: errorText(error, "Could not apply the profile proposal.")}; }
    try {
      const saved = await saveCurrentProfile(draftProfile);
      setProfile(saved.profile);
      await Promise.all([refreshFeedback(), refreshProposals(), refreshReviewCore(saved.profile)]);
      pushMessage("profile.proposal.applied", {text: "Initial profile applied and saved.", tone: "success"});
      return {ok: true};
    } catch (error) {
      const currentProfile = await refreshProfileGate();
      await Promise.all([refreshFeedback(), refreshProposals(), currentProfile ? refreshReviewCore(currentProfile) : Promise.resolve()]);
      pushMessage("profile.proposal.applied", {text: `Profile proposal applied, but the edited draft was not fully saved: ${errorText(error, "Unknown error.")}`, tone: "warning"});
      return {ok: true};
    } finally { setBusy(false); }
  }, [errorText, pushMessage, refreshFeedback, refreshProfileGate, refreshProposals, refreshReviewCore, setBusy, setProfile]);
  const handleOnboardingRejectProposal = React.useCallback(async (id: number) => {
    try { await rejectProfileProposal(id); await refreshProposals(); return {ok: true, tone: "success" as const, message: "Rejected the pending proposal."}; }
    catch (error) { return {ok: false, tone: "danger" as const, message: errorText(error, "Could not reject the profile proposal.")}; }
  }, [errorText, refreshProposals]);
  const handleRunAdminJob = async (path: "/api/admin/run", feedURLs?: string[]) => {
    try { registerJob(await launchAdminJob(path, feedURLs?.length ? {feed_urls: feedURLs} : undefined)); pushMessage("job.started"); }
    catch (error) { pushErrorMessage("app.service.unavailable", error, "Could not start the sync job."); }
  };
  const handleStopJob = React.useCallback(async (jobID: string, jobType: "sync" | "reclassify") => {
    try {
      const job = await cancelAdminJob(jobID);
      registerJob(job, false);
      pushMessage(jobType === "reclassify" ? "reclassify.cancel.requested" : "sync.cancel.requested");
    } catch (error) {
      pushErrorMessage("job.cancel.failed", error, "Could not stop the job.");
    }
  }, [pushErrorMessage, pushMessage, registerJob]);
  const handleStartVerification = React.useCallback(async (job: JobInfo) => {
    if (!job.verification_feed_url) return pushMessage("app.service.unavailable", {text: "Verification feed URL is missing.", tone: "danger"});
    setVerificationSubmitError(null);
    try { await startFeedVerification({job_id: job.id, feed_url: job.verification_feed_url}); pushMessage("job.verification.started", {text: "Opened the feed verification window.", tone: "info"}); }
    catch (error) { pushErrorMessage("app.service.unavailable", error, "Could not start feed verification."); }
  }, [pushErrorMessage, pushMessage, setVerificationSubmitError]);
  const handleOpenVerificationInBrowser = React.useCallback(async (job: JobInfo) => {
    if (!job.verification_feed_url) return pushMessage("app.service.unavailable", {text: "Verification feed URL is missing.", tone: "danger"});
    setVerificationSubmitError(null);
    try { await openFeedVerificationInBrowser({job_id: job.id, feed_url: job.verification_feed_url}); pushMessage("job.verification.browser.started", {text: "Opened the protected feed in your browser. Finish the check there, then paste the final RSS/XML here.", tone: "info"}); }
    catch (error) { pushErrorMessage("app.service.unavailable", error, "Could not open the protected feed in your browser."); }
  }, [pushErrorMessage, pushMessage, setVerificationSubmitError]);
  const handleSubmitVerificationXML = React.useCallback(async (job: JobInfo, xml: string) => {
    if (!job.verification_feed_url) return setVerificationSubmitError("Verification feed URL is missing.");
    try {
      setVerificationSubmitting(true); setVerificationSubmitError(null);
      await submitFeedVerificationXML({job_id: job.id, feed_url: job.verification_feed_url, feed_xml: xml});
      pushMessage("job.verification.manual.accepted", {text: "Accepted the pasted RSS/XML. The sync is resuming now.", tone: "info"});
    } catch (error) { setVerificationSubmitError(errorText(error, "Could not submit protected feed XML.")); }
    finally { setVerificationSubmitting(false); }
  }, [errorText, pushMessage, setVerificationSubmitError, setVerificationSubmitting]);
  const handleReclassify = async (scope: ReclassifyScope, limit = 0) => {
    try { registerJob(await launchReclassifyJob({scope, limit})); pushMessage("job.reclassify.started"); }
    catch (error) { pushErrorMessage("app.service.unavailable", error, "Could not start the reclassification job."); }
  };

  return {
    registerJob, handleSaveFeeds,
    handleSaveConfig, handleSaveScheduler, handleSaveProfile, handleDeleteScheduler, handleTestClassifierModel,
    handleOpenAppTarget, handleExitApp, handleOnboardingSaveAndBootstrap,
    handleOnboardingSaveSettings, handleGenerateProposal, handleApplyProposal,
    handleRejectProposal, handleOnboardingAcceptDraft, handleOnboardingRejectProposal,
    handleRunAdminJob, handleStopJob, handleStartVerification, handleOpenVerificationInBrowser,
    handleSubmitVerificationXML, handleReclassify,
  };
}
