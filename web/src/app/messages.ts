import type {StatusTone} from "../components/common/StatusBanner";
import type {JobInfo} from "../types";

export type UiMessage = {
  name: string;
  text: string;
  tone: StatusTone;
  createdAt: number;
  ttlMs: number;
};

const MESSAGE_TTL_MS = 4200;

const catalog = {
  "app.report.embedded_fallback": {
    text: "Using embedded report data because the API is unavailable.",
    tone: "warning",
  },
  "app.load.failed": {text: "Could not load data.", tone: "danger"},
  "paper.read.failed": {text: "Could not update read status.", tone: "danger"},
  "zotero.save.succeeded": {text: "Saved to Zotero.", tone: "success"},
  "feedback.save.succeeded": {text: "Feedback saved.", tone: "success"},
  "feedback.delete.succeeded": {text: "Feedback deleted.", tone: "success"},
  "settings.config.save.succeeded": {text: "Local settings saved.", tone: "success"},
  "scheduler.save.succeeded": {text: "Scheduled task saved.", tone: "success"},
  "scheduler.delete.succeeded": {text: "Scheduled task removed.", tone: "success"},
  "feeds.validation.failed": {
    text: "Each feed needs both a journal name and an RSS URL.",
    tone: "danger",
  },
  "feeds.save.succeeded": {text: "Feed subscriptions saved.", tone: "success"},
  "profile.bootstrap.started": {text: "Initial profile generation started.", tone: "info"},
  "profile.proposal.started": {text: "Profile proposal job started.", tone: "info"},
  "profile.proposal.applied": {text: "Profile proposal applied.", tone: "success"},
  "profile.proposal.rejected": {text: "Profile proposal rejected.", tone: "success"},
  "job.started": {text: "Job started.", tone: "info"},
  "job.reclassify.started": {text: "Reclassification job started.", tone: "info"},
  "job.completed": {text: "Completed.", tone: "success"},
  "job.failed": {text: "Job failed.", tone: "danger"},
  "pipeline.feeds.fetching": {text: "Fetching RSS feeds.", tone: "info"},
  "pipeline.metadata.enriching": {text: "Getting metadata.", tone: "info"},
  "pipeline.classifier.classifying": {text: "Classifying papers.", tone: "info"},
  "pipeline.report.writing": {text: "Publishing report.", tone: "info"},
  "profile.proposal.collecting_feedback": {
    text: "Collecting feedback for profile review.",
    tone: "info",
  },
  "profile.proposal.generating": {text: "Generating profile proposal.", tone: "info"},
  "profile.bootstrap.generating": {text: "Generating initial profile proposal.", tone: "info"},
} satisfies Record<string, {text: string; tone: StatusTone}>;

export function createUiMessage(
  name: keyof typeof catalog | string,
  override?: {text?: string; tone?: StatusTone},
): UiMessage {
  const item = catalog[name as keyof typeof catalog] ?? catalog["app.load.failed"];
  return {
    name,
    text: override?.text ?? item.text,
    tone: override?.tone ?? item.tone,
    createdAt: Date.now(),
    ttlMs: MESSAGE_TTL_MS,
  };
}

export function messageFromJob(job: JobInfo): UiMessage {
  if (job.status === "failed") {
    return createUiMessage(job.message_key ?? "job.failed", {
      text: job.error ?? job.message ?? "Job failed.",
      tone: "danger",
    });
  }
  if (job.status === "completed") {
    return createUiMessage(job.message_key ?? "job.completed", {
      text: job.message ?? "Completed.",
      tone: "success",
    });
  }
  return createUiMessage(job.message_key ?? "job.started", {
    text: job.message ?? undefined,
    tone: "info",
  });
}
