import type {
  AppControlResponse,
  AppControlTarget,
  AppMeta,
  AppUpdate,
  ClassificationProfile,
  CurrentProfileResponse,
  FeedSubscription,
  FeedbackRecord,
  JobInfo,
  PaperReadStatus,
  ProfileProposal,
  Report,
  Relevance,
  SchedulerSettings,
  SettingsConfigResponse,
  SettingsConfigUpdate,
  ZoteroCollectionsResponse,
  ZoteroStatus,
} from "./types";

declare global {
  interface Window {
    __SCIRSS_REPORT__?: Report;
  }
}

async function responseErrorMessage(response: Response): Promise<string> {
  const text = await response.text();
  try {
    const payload = JSON.parse(text) as {detail?: string};
    if (payload.detail) {
      return payload.detail;
    }
  } catch {
    // Fall back to the raw response body when the payload is not JSON.
  }
  return text || `Request failed (${response.status})`;
}

export async function fetchLatestReport(): Promise<Report> {
  const response = await fetch("/api/report/latest");
  if (!response.ok) {
    throw new Error(`Could not load report data (${response.status})`);
  }
  return (await response.json()) as Report;
}

export async function fetchAppMeta(): Promise<AppMeta> {
  const response = await fetch("/api/app/meta");
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as AppMeta;
}

export async function fetchAppUpdate(): Promise<AppUpdate> {
  const response = await fetch("/api/app/update");
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as AppUpdate;
}

export async function openAppTarget(target: AppControlTarget): Promise<AppControlResponse> {
  const response = await fetch("/api/app/open", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({target}),
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as AppControlResponse;
}

export async function exitApp(): Promise<AppControlResponse> {
  const response = await fetch("/api/app/exit", {
    method: "POST",
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as AppControlResponse;
}

export async function fetchFeedSubscriptions(): Promise<FeedSubscription[]> {
  const response = await fetch("/api/settings/feeds");
  if (!response.ok) {
    throw new Error(`Could not load feed subscriptions (${response.status})`);
  }
  return (await response.json()) as FeedSubscription[];
}

export async function fetchSettingsConfig(): Promise<SettingsConfigResponse> {
  const response = await fetch("/api/settings/config");
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as SettingsConfigResponse;
}

export async function fetchSchedulerSettings(): Promise<SchedulerSettings> {
  const response = await fetch("/api/settings/scheduler");
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as SchedulerSettings;
}

export async function saveSchedulerSettings(dailyTime: string): Promise<SchedulerSettings> {
  const response = await fetch("/api/settings/scheduler", {
    method: "PUT",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({daily_time: dailyTime}),
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as SchedulerSettings;
}

export async function deleteSchedulerSettings(): Promise<SchedulerSettings> {
  const response = await fetch("/api/settings/scheduler", {method: "DELETE"});
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as SchedulerSettings;
}

export async function saveSettingsConfig(
  fields: Record<string, SettingsConfigUpdate>,
): Promise<SettingsConfigResponse> {
  const response = await fetch("/api/settings/config", {
    method: "PUT",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({fields}),
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as SettingsConfigResponse;
}

export async function saveFeedSubscriptions(
  feeds: FeedSubscription[],
): Promise<FeedSubscription[]> {
  const response = await fetch("/api/settings/feeds", {
    method: "PUT",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({feeds}),
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as FeedSubscription[];
}

export async function fetchCurrentProfile(): Promise<CurrentProfileResponse> {
  const response = await fetch("/api/profile/current");
  if (!response.ok) {
    throw new Error(`Could not load current profile (${response.status})`);
  }
  return (await response.json()) as CurrentProfileResponse;
}

export async function saveCurrentProfile(profile: ClassificationProfile): Promise<CurrentProfileResponse> {
  const response = await fetch("/api/profile/current", {
    method: "PUT",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(profile),
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as CurrentProfileResponse;
}

export async function bootstrapProfile(input: {
  interest_description: string;
  name?: string;
}): Promise<JobInfo> {
  const response = await fetch("/api/profile/bootstrap", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  const payload = (await response.json()) as {job: JobInfo};
  return payload.job;
}

export async function fetchFeedback(): Promise<FeedbackRecord[]> {
  const response = await fetch("/api/feedback");
  if (!response.ok) {
    throw new Error(`Could not load feedback (${response.status})`);
  }
  return (await response.json()) as FeedbackRecord[];
}

export async function createFeedback(input: {
  paper_id: number;
  corrected_relevance: Relevance;
  note?: string;
}): Promise<FeedbackRecord> {
  const response = await fetch("/api/feedback", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as FeedbackRecord;
}

export async function deleteFeedback(feedbackId: number): Promise<void> {
  const response = await fetch(`/api/feedback/${feedbackId}`, {method: "DELETE"});
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
}

export async function markPaperRead(paperId: number): Promise<PaperReadStatus> {
  const response = await fetch(`/api/papers/${paperId}/read`, {method: "POST"});
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return (await response.json()) as PaperReadStatus;
}

export async function fetchProfileProposals(): Promise<ProfileProposal[]> {
  const response = await fetch("/api/profile/proposals");
  if (!response.ok) {
    throw new Error(`Could not load profile proposals (${response.status})`);
  }
  return (await response.json()) as ProfileProposal[];
}

export async function launchProfileProposalGeneration(): Promise<JobInfo> {
  const response = await fetch("/api/profile/proposals/generate", {method: "POST"});
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  const payload = (await response.json()) as {job: JobInfo};
  return payload.job;
}

export async function applyProfileProposal(id: number): Promise<ProfileProposal> {
  const response = await fetch(`/api/profile/proposals/${id}/apply`, {method: "POST"});
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as ProfileProposal;
}

export async function rejectProfileProposal(id: number): Promise<ProfileProposal> {
  const response = await fetch(`/api/profile/proposals/${id}/reject`, {method: "POST"});
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as ProfileProposal;
}

export async function fetchZoteroCollections(): Promise<ZoteroCollectionsResponse> {
  const response = await fetch("/api/zotero/collections");
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as ZoteroCollectionsResponse;
}

export async function saveToZotero(
  paperId: number,
  collectionKey?: string | null,
): Promise<ZoteroStatus> {
  const response = await fetch(`/api/zotero/save/${paperId}`, {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({collection_key: collectionKey ?? null}),
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as ZoteroStatus;
}

export async function launchAdminJob(
  path: "/api/admin/run" | "/api/admin/report/latest",
): Promise<JobInfo> {
  const response = await fetch(path, {method: "POST"});
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  const payload = (await response.json()) as {job: JobInfo};
  return payload.job;
}

export async function launchReclassifyJob(input: {
  scope: "recent" | "feedback" | "all";
  limit: number;
}): Promise<JobInfo> {
  const response = await fetch("/api/admin/reclassify", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  const payload = (await response.json()) as {job: JobInfo};
  return payload.job;
}

export async function fetchJob(id: string): Promise<JobInfo> {
  const response = await fetch(`/api/admin/jobs/${id}`);
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as JobInfo;
}

export async function fetchJobs(): Promise<JobInfo[]> {
  const response = await fetch("/api/admin/jobs");
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as JobInfo[];
}

export async function startFeedVerification(input: {
  job_id: string;
  feed_url: string;
}): Promise<{ok: boolean; verification_id: string}> {
  const response = await fetch("/api/feeds/verification/start", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as {ok: boolean; verification_id: string};
}

export async function completeFeedVerification(input: {
  job_id: string;
  feed_url: string;
}): Promise<{ok: boolean; verification_id: string}> {
  const response = await fetch("/api/feeds/verification/complete", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return (await response.json()) as {ok: boolean; verification_id: string};
}
