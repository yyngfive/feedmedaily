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
const JOB_FINAL_TTL_MS = 9000;
const STICKY_TTL_MS = 0;

const catalog = {
  "app.load.failed": {text: "Could not load data.", tone: "danger"},
  "paper.read.failed": {text: "Could not update read status.", tone: "danger"},
  "zotero.save.succeeded": {text: "Saved to Zotero.", tone: "success"},
  "feedback.save.succeeded": {text: "Feedback saved.", tone: "success"},
  "feedback.delete.succeeded": {text: "Feedback deleted.", tone: "success"},
  "settings.config.save.succeeded": {text: "Local settings saved.", tone: "success"},
  "scheduler.save.succeeded": {text: "Daily sync settings saved.", tone: "success"},
  "scheduler.delete.succeeded": {text: "Daily sync disabled.", tone: "success"},
  "app.control.open.succeeded": {text: "Opened local target.", tone: "success"},
  "app.control.exit.succeeded": {text: "FeedMeDaily is shutting down.", tone: "info"},
  "feeds.validation.failed": {
    text: "Each feed needs both a journal name and an RSS URL.",
    tone: "danger",
  },
  "feeds.save.succeeded": {text: "Feed subscriptions saved.", tone: "success"},
  "profile.proposal.started": {text: "Profile proposal job started.", tone: "info"},
  "profile.proposal.applied": {text: "Profile proposal applied.", tone: "success"},
  "profile.proposal.rejected": {text: "Profile proposal rejected.", tone: "success"},
  "profile.current.save.succeeded": {text: "Classification profile saved.", tone: "success"},
  "job.started": {text: "Job started.", tone: "info"},
  "job.reclassify.started": {text: "Reclassification job started.", tone: "info"},
  "job.verification.started": {text: "Opened the feed verification window.", tone: "info"},
  "job.verification.completed": {text: "Trying the captured feed XML again.", tone: "info"},
  "job.completed": {text: "Completed.", tone: "success"},
  "job.failed": {text: "Job failed.", tone: "danger"},
  "pipeline.feeds.fetching": {text: "Fetching RSS feeds.", tone: "info"},
  "pipeline.metadata.enriching": {text: "Getting metadata.", tone: "info"},
  "pipeline.classifier.classifying": {text: "Classifying papers.", tone: "info"},
  "pipeline.report.refreshing": {text: "Refreshing report from SQLite.", tone: "info"},
  "pipeline.feeds.verification_required": {
    text: "A protected feed needs Cloudflare verification before the sync can continue.",
    tone: "warning",
  },
  "profile.proposal.collecting_feedback": {
    text: "Collecting feedback and current profile context.",
    tone: "info",
  },
  "profile.proposal.generating": {text: "Generating profile proposal.", tone: "info"},
  "profile.bootstrap.generating": {text: "Generating initial profile proposal.", tone: "info"},
  "profile.bootstrap.preparing": {text: "Preparing initial profile request.", tone: "info"},
  "profile.bootstrap.validating": {text: "Validating generated profile.", tone: "info"},
  "profile.bootstrap.saving": {text: "Saving initial profile proposal.", tone: "info"},
  "profile.proposal.validating": {text: "Validating generated profile proposal.", tone: "info"},
  "profile.proposal.saving": {text: "Saving profile proposal.", tone: "info"},
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
  const progressText = formatProgressMessage(job);
  if (job.status === "failed") {
    const message = createUiMessage(job.message_key ?? "job.failed", {
      text: job.error ?? job.message ?? "Job failed.",
      tone: "danger",
    });
    return {...message, ttlMs: JOB_FINAL_TTL_MS};
  }
  if (job.status === "completed") {
    const warningCount = job.warning_count ?? 0;
    const message = createUiMessage(job.message_key ?? "job.completed", {
      text: job.message ?? "Completed.",
      tone: warningCount > 0 ? "warning" : "success",
    });
    return {...message, ttlMs: JOB_FINAL_TTL_MS};
  }
  if (job.status === "waiting_for_user") {
    const message = createUiMessage(job.message_key ?? "pipeline.feeds.verification_required", {
      text: job.message ?? undefined,
      tone: "warning",
    });
    return {...message, ttlMs: STICKY_TTL_MS};
  }
  const message = createUiMessage(job.message_key ?? "job.started", {
    text: progressText ?? job.message ?? undefined,
    tone: "info",
  });
  return {...message, ttlMs: STICKY_TTL_MS};
}

function formatProgressMessage(job: JobInfo): string | null {
  const stage = job.progress_stage?.trim();
  if (!stage) {
    return null;
  }
  const current = job.progress_current;
  const total = job.progress_total;
  const percent = job.progress_percent;
  const label = job.progress_label?.trim();

  if (stage === "fetch" && current != null && total != null) {
    if (current <= 0) {
      return `Fetching feeds ${current}/${total}.`;
    }
    return `Fetching feed ${current}/${total}: ${label || "Current feed"}.`;
  }
  if (stage === "metadata" && current != null && total != null && percent != null) {
    return `Getting metadata ${current}/${total} (${percent}%).`;
  }
  if (stage === "classification" && current != null && total != null && percent != null) {
    return `Classifying papers ${current}/${total} (${percent}%).`;
  }
  if ((stage === "profile-bootstrap" || stage === "profile-proposal") && job.message) {
    return job.message;
  }
  if (stage === "report" && job.message) {
    return job.message;
  }
  return null;
}
