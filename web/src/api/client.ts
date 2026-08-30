import type {
  AppControlResponse,
  AppControlTarget,
  AppMeta,
  AppUpdate,
  ClassifierModelsUpdate,
  ClassificationProfile,
  CurrentProfileResponse,
  FeedSubscription,
  FeedbackRecord,
  JobInfo,
  LLMUsageRecord,
  PaperReadStatus,
  ProfileProposal,
  Report,
  Relevance,
  SchedulerSettings,
  SettingsConfigResponse,
  SettingsConfigUpdate,
  ZoteroCollectionsResponse,
  ZoteroStatus,
} from "../shared/types";

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

function localServiceUnavailableMessage(operation: string): string {
  return `Could not ${operation} because the local FeedMeDaily service is unavailable.`;
}

function normalizeFetchError(error: unknown, operation: string): Error {
  if (error instanceof Error) {
    const normalized = error.message.trim().toLowerCase();
    if (
      normalized === "failed to fetch" ||
      normalized.includes("networkerror") ||
      normalized.includes("load failed")
    ) {
      return new Error(localServiceUnavailableMessage(operation));
    }
    return error;
  }
  return new Error(localServiceUnavailableMessage(operation));
}

async function localRequest(
  input: RequestInfo | URL,
  init: RequestInit | undefined,
  operation: string,
): Promise<Response> {
  try {
    return await fetch(input, init);
  } catch (error) {
    throw normalizeFetchError(error, operation);
  }
}

async function localJSONRequest<T>(
  input: RequestInfo | URL,
  init: RequestInit | undefined,
  operation: string,
  fallbackStatusMessage: string,
): Promise<T> {
  const response = await localRequest(input, init, operation);
  if (!response.ok) {
    const detail = await responseErrorMessage(response);
    throw new Error(detail || `${fallbackStatusMessage} (${response.status})`);
  }
  return (await response.json()) as T;
}

async function localVoidRequest(
  input: RequestInfo | URL,
  init: RequestInit | undefined,
  operation: string,
  fallbackStatusMessage: string,
): Promise<void> {
  const response = await localRequest(input, init, operation);
  if (!response.ok) {
    const detail = await responseErrorMessage(response);
    throw new Error(detail || `${fallbackStatusMessage} (${response.status})`);
  }
}

export async function fetchLatestReport(): Promise<Report> {
  return localJSONRequest(
    "/api/report/latest",
    undefined,
    "load the paper list from the local library",
    "Could not load the paper list from the local library",
  );
}

export async function fetchAppMeta(): Promise<AppMeta> {
  return localJSONRequest(
    "/api/app/meta",
    undefined,
    "load local app status",
    "Could not load local app status",
  );
}

export async function fetchAppUpdate(force = false): Promise<AppUpdate> {
  const path = force ? "/api/app/update?force=1" : "/api/app/update";
  return localJSONRequest(
    path,
    undefined,
    "check local update status",
    "Could not check local update status",
  );
}

export async function openAppTarget(target: AppControlTarget): Promise<AppControlResponse> {
  return localJSONRequest(
    "/api/app/open",
    {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({target}),
    },
    "open the selected local target",
    "Could not open the selected local target",
  );
}

export async function exitApp(): Promise<AppControlResponse> {
  return localJSONRequest(
    "/api/app/exit",
    {method: "POST"},
    "exit the local FeedMeDaily service",
    "Could not exit the local FeedMeDaily service",
  );
}

export async function fetchFeedSubscriptions(): Promise<FeedSubscription[]> {
  return localJSONRequest(
    "/api/settings/feeds",
    undefined,
    "load RSS feed settings",
    "Could not load RSS feed settings",
  );
}

export async function fetchSettingsConfig(): Promise<SettingsConfigResponse> {
  return localJSONRequest(
    "/api/settings/config",
    undefined,
    "load local settings",
    "Could not load local settings",
  );
}

export async function fetchSchedulerSettings(): Promise<SchedulerSettings> {
  return localJSONRequest(
    "/api/settings/scheduler",
    undefined,
    "load local scheduler settings",
    "Could not load local scheduler settings",
  );
}

export async function saveSchedulerSettings(dailyTime: string): Promise<SchedulerSettings> {
  return localJSONRequest(
    "/api/settings/scheduler",
    {
      method: "PUT",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({daily_time: dailyTime}),
    },
    "save local scheduler settings",
    "Could not save local scheduler settings",
  );
}

export async function deleteSchedulerSettings(): Promise<SchedulerSettings> {
  return localJSONRequest(
    "/api/settings/scheduler",
    {method: "DELETE"},
    "disable local scheduler settings",
    "Could not disable local scheduler settings",
  );
}

export async function saveSettingsConfig(
  fields: Record<string, SettingsConfigUpdate>,
  classifierModels?: ClassifierModelsUpdate,
): Promise<SettingsConfigResponse> {
  const body: {fields: Record<string, SettingsConfigUpdate>; classifier_models?: ClassifierModelsUpdate} = {fields};
  if (classifierModels) body.classifier_models = classifierModels;
  return localJSONRequest(
    "/api/settings/config",
    {
      method: "PUT",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(body),
    },
    "save local settings",
    "Could not save local settings",
  );
}

export async function testClassifierModel(modelId: string, apiKey?: string): Promise<JobInfo> {
  const payload = await localJSONRequest<{job: JobInfo}>(
    "/api/settings/classifier-models/test",
    {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({model_id: modelId, ...(apiKey?.trim() ? {api_key: apiKey.trim()} : {})}),
    },
    "test the classifier model connection",
    "Could not test the classifier model connection",
  );
  return payload.job;
}

export async function saveFeedSubscriptions(
  feeds: FeedSubscription[],
): Promise<FeedSubscription[]> {
  return localJSONRequest(
    "/api/settings/feeds",
    {
      method: "PUT",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({feeds}),
    },
    "save RSS feed settings",
    "Could not save RSS feed settings",
  );
}

export async function fetchCurrentProfile(): Promise<CurrentProfileResponse> {
  return localJSONRequest(
    "/api/profile/current",
    undefined,
    "load the local profile",
    "Could not load the local profile",
  );
}

export async function saveCurrentProfile(profile: ClassificationProfile): Promise<CurrentProfileResponse> {
  return localJSONRequest(
    "/api/profile/current",
    {
      method: "PUT",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(profile),
    },
    "save the local profile",
    "Could not save the local profile",
  );
}

export async function bootstrapProfile(input: {
  interest_description: string;
  name?: string;
}): Promise<JobInfo> {
  const payload = await localJSONRequest<{job: JobInfo}>(
    "/api/profile/bootstrap",
    {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(input),
    },
    "start profile generation",
    "Could not start profile generation",
  );
  return payload.job;
}

export async function fetchFeedback(): Promise<FeedbackRecord[]> {
  return localJSONRequest(
    "/api/feedback",
    undefined,
    "load feedback records",
    "Could not load feedback records",
  );
}

export async function createFeedback(input: {
  paper_id: number;
  corrected_relevance: Relevance;
  note?: string;
}): Promise<FeedbackRecord> {
  return localJSONRequest(
    "/api/feedback",
    {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(input),
    },
    "save feedback",
    "Could not save feedback",
  );
}

export async function deleteFeedback(feedbackId: number): Promise<void> {
  return localVoidRequest(
    `/api/feedback/${feedbackId}`,
    {method: "DELETE"},
    "delete feedback",
    "Could not delete feedback",
  );
}

export async function markPaperRead(paperId: number, read = true): Promise<PaperReadStatus> {
  return localJSONRequest(
    `/api/papers/${paperId}/read`,
    {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({read}),
    },
    "update read status",
    "Could not update read status",
  );
}

export async function fetchProfileProposals(): Promise<ProfileProposal[]> {
  return localJSONRequest(
    "/api/profile/proposals",
    undefined,
    "load profile proposals",
    "Could not load profile proposals",
  );
}

export async function launchProfileProposalGeneration(): Promise<JobInfo> {
  const payload = await localJSONRequest<{job: JobInfo}>(
    "/api/profile/proposals/generate",
    {method: "POST"},
    "start profile proposal generation",
    "Could not start profile proposal generation",
  );
  return payload.job;
}

export async function applyProfileProposal(
  id: number,
  selection?: {accepted_change_ids: string[]; rejected_change_ids: string[]},
): Promise<{proposal: ProfileProposal; job?: JobInfo}> {
  const payload = await localJSONRequest<{proposal: ProfileProposal; job?: JobInfo}>(
    `/api/profile/proposals/${id}/apply`,
    {
      method: "POST",
      headers: selection ? {"Content-Type": "application/json"} : undefined,
      body: selection ? JSON.stringify(selection) : undefined,
    },
    "apply the profile proposal",
    "Could not apply the profile proposal",
  );
  return payload;
}

export async function rejectProfileProposal(id: number): Promise<ProfileProposal> {
  return localJSONRequest(
    `/api/profile/proposals/${id}/reject`,
    {method: "POST"},
    "reject the profile proposal",
    "Could not reject the profile proposal",
  );
}

export async function fetchZoteroCollections(): Promise<ZoteroCollectionsResponse> {
  return localJSONRequest(
    "/api/zotero/collections",
    undefined,
    "load Zotero collections",
    "Could not load Zotero collections",
  );
}

export async function saveToZotero(
  paperId: number,
  collectionKey?: string | null,
): Promise<ZoteroStatus> {
  return localJSONRequest(
    `/api/zotero/save/${paperId}`,
    {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({collection_key: collectionKey ?? null}),
    },
    "save to Zotero",
    "Could not save to Zotero",
  );
}

export async function launchAdminJob(
  path: "/api/admin/run",
  input?: {feed_urls?: string[]},
): Promise<JobInfo> {
  const payload = await localJSONRequest<{job: JobInfo}>(
    path,
    input?.feed_urls?.length
      ? {
          method: "POST",
          headers: {"Content-Type": "application/json"},
          body: JSON.stringify(input),
        }
      : {method: "POST"},
    "start the sync job",
    "Could not start the sync job",
  );
  return payload.job;
}

export type ReclassifyScope = "today" | "feedback" | "all" | "count" | "unclassified";

export type ReclassifyOptions = {
  paper_count: number;
  classified_paper_count: number;
  unclassified_paper_count: number;
  today_paper_count: number;
  today_classified_count: number;
  today_unclassified_count: number;
  count_paper_count: number;
  count_classified_count: number;
  count_unclassified_count: number;
};

export async function fetchReclassifyOptions(limit?: number): Promise<ReclassifyOptions> {
  const query = limit === undefined ? "" : `?limit=${limit}`;
  return localJSONRequest(
    `/api/admin/reclassify${query}`,
    undefined,
    "load reclassification options",
    "Could not load reclassification options",
  );
}

export async function launchReclassifyJob(input: {
  scope: ReclassifyScope;
  limit: number;
}): Promise<JobInfo> {
  const payload = await localJSONRequest<{job: JobInfo}>(
    "/api/admin/reclassify",
    {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(input),
    },
    "start the reclassification job",
    "Could not start the reclassification job",
  );
  return payload.job;
}

export async function fetchJob(id: string): Promise<JobInfo> {
  return localJSONRequest(
    `/api/admin/jobs/${id}`,
    undefined,
    "load job status",
    "Could not load job status",
  );
}

export async function cancelAdminJob(id: string): Promise<JobInfo> {
  const payload = await localJSONRequest<{job: JobInfo}>(
    `/api/admin/jobs/${encodeURIComponent(id)}/cancel`,
    {method: "POST"},
    "stop the job",
    "Could not stop the job",
  );
  return payload.job;
}

export async function fetchJobs(): Promise<JobInfo[]> {
  return localJSONRequest(
    "/api/admin/jobs",
    undefined,
    "load job status",
    "Could not load job status",
  );
}

export async function fetchLLMUsage(since: string): Promise<LLMUsageRecord[]> {
  return localJSONRequest(
    `/api/admin/llm-usage?since=${encodeURIComponent(since)}`,
    undefined,
    "load LLM usage",
    "Could not load LLM usage",
  );
}

export async function startFeedVerification(input: {
  job_id: string;
  feed_url: string;
}): Promise<{ok: boolean; verification_id: string}> {
  return localJSONRequest(
    "/api/feeds/verification/start",
    {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(input),
    },
    "start feed verification",
    "Could not start feed verification",
  );
}

export async function openFeedVerificationInBrowser(input: {
  job_id: string;
  feed_url: string;
}): Promise<{ok: boolean; verification_id: string}> {
  return localJSONRequest(
    "/api/feeds/verification/browser",
    {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(input),
    },
    "open the protected feed in the system browser",
    "Could not open the protected feed in the system browser",
  );
}

export async function submitFeedVerificationXML(input: {
  job_id: string;
  feed_url: string;
  feed_xml: string;
}): Promise<{ok: boolean; verification_id: string}> {
  return localJSONRequest(
    "/api/feeds/verification/manual-submit",
    {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(input),
    },
    "submit protected feed XML",
    "Could not submit protected feed XML",
  );
}

export async function completeFeedVerification(input: {
  job_id: string;
  feed_url: string;
}): Promise<{ok: boolean; verification_id: string}> {
  return localJSONRequest(
    "/api/feeds/verification/complete",
    {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(input),
    },
    "complete feed verification",
    "Could not complete feed verification",
  );
}
