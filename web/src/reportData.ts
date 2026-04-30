import type {
  ClassificationProfile,
  CurrentProfileResponse,
  FeedbackRecord,
  JobInfo,
  ProfileProposal,
  Report,
  Relevance,
  ZoteroStatus,
} from "./types";

declare global {
  interface Window {
    __SCIRSS_REPORT__?: Report;
  }
}

export function loadEmbeddedReport(): Report | null {
  return window.__SCIRSS_REPORT__ ?? null;
}

export async function fetchLatestReport(): Promise<Report> {
  const response = await fetch("/api/report/latest");
  if (!response.ok) {
    throw new Error(`Could not load report data (${response.status})`);
  }
  return (await response.json()) as Report;
}

export async function fetchCurrentProfile(): Promise<CurrentProfileResponse> {
  const response = await fetch("/api/profile/current");
  if (!response.ok) {
    throw new Error(`Could not load current profile (${response.status})`);
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
    throw new Error(await response.text());
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
    throw new Error(await response.text());
  }
  return (await response.json()) as FeedbackRecord;
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
    throw new Error(await response.text());
  }
  const payload = (await response.json()) as {job: JobInfo};
  return payload.job;
}

export async function applyProfileProposal(id: number): Promise<ProfileProposal> {
  const response = await fetch(`/api/profile/proposals/${id}/apply`, {method: "POST"});
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return (await response.json()) as ProfileProposal;
}

export async function rejectProfileProposal(id: number): Promise<ProfileProposal> {
  const response = await fetch(`/api/profile/proposals/${id}/reject`, {method: "POST"});
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return (await response.json()) as ProfileProposal;
}

export async function saveToZotero(paperId: number): Promise<ZoteroStatus> {
  const response = await fetch(`/api/zotero/save/${paperId}`, {method: "POST"});
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return (await response.json()) as ZoteroStatus;
}

export async function launchAdminJob(
  path: "/api/admin/run" | "/api/admin/report/latest",
): Promise<JobInfo> {
  const response = await fetch(path, {method: "POST"});
  if (!response.ok) {
    throw new Error(await response.text());
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
    throw new Error(await response.text());
  }
  const payload = (await response.json()) as {job: JobInfo};
  return payload.job;
}

export async function fetchJob(id: string): Promise<JobInfo> {
  const response = await fetch(`/api/admin/jobs/${id}`);
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return (await response.json()) as JobInfo;
}

export function tagLabel(tag: string, profile: ClassificationProfile | null): string {
  const match = profile?.topic_taxonomy.find((item) => item.id === tag);
  return match?.label ?? tag.replaceAll("_", " ");
}
