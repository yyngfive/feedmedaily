import {Button, Chip, Spinner} from "@heroui/react";
import React from "react";

import {fetchCleanupReviews, fetchCleanupStatus, type CleanupReviewDecision} from "../../api/client";
import {ModalShell} from "../../shared/components/ModalShell";
import type {CleanupReview, CleanupStatus, JobInfo} from "../../shared/types";
import {AdminDisclosure} from "./AdminDisclosure";

type ConfirmedReviewAction = {
  decision: CleanupReviewDecision;
  description: string;
  label: string;
  review: CleanupReview;
  title: string;
};

function formatDate(value?: string | null) {
  if (!value || Number.isNaN(Date.parse(value))) return "Unknown date";
  return new Date(value).toLocaleDateString();
}

function reviewTypeLabel(review: CleanupReview) {
  if (review.match_type === "doi_conflict") return "DOI conflict";
  if (review.match_type === "doi_uncertain") return "DOI needs confirmation";
  return "Possible duplicate";
}

function confirmationPaper(action: ConfirmedReviewAction): CleanupReview["candidate"] {
  if ((action.decision === "delete_match" || action.decision === "clear_match_doi") && action.review.matched) {
    return action.review.matched;
  }
  return action.review.candidate;
}

function doiHref(value: string) {
  const normalized = value
    .trim()
    .replace(/^https?:\/\/(?:dx\.)?doi\.org\//i, "")
    .replace(/^doi:\s*/i, "");
  return `https://doi.org/${encodeURIComponent(normalized).replaceAll("%2F", "/")}`;
}

function PaperMeta({paper}: {paper: CleanupReview["candidate"]}) {
  const doi = paper.doi?.trim();
  return (
    <span>
      {paper.journal?.trim() || "Unknown journal"} · {paper.published_date?.trim() || "Unknown date"} · {doi ? (
        <a
          className="text-(--accent) underline decoration-(--accent)/50 underline-offset-2 hover:decoration-(--accent)"
          href={doiHref(doi)}
          rel="noreferrer"
          target="_blank"
        >
          DOI: {doi}
        </a>
      ) : "No DOI"}
    </span>
  );
}

export function UnclassifiedCleanupPanel({
  activeJob,
  onCleanup,
  onCleanupReview,
  refreshKey,
}: {
  activeJob: JobInfo | null;
  onCleanup: () => Promise<void> | void;
  onCleanupReview: (reviewID: number, decision: CleanupReviewDecision) => Promise<void> | void;
  refreshKey: string;
}) {
  const [status, setStatus] = React.useState<CleanupStatus | null>(null);
  const [reviews, setReviews] = React.useState<CleanupReview[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [starting, setStarting] = React.useState(false);
  const [processingReviewID, setProcessingReviewID] = React.useState<number | null>(null);
  const [confirmCleanup, setConfirmCleanup] = React.useState(false);
  const [confirmedAction, setConfirmedAction] = React.useState<ConfirmedReviewAction | null>(null);
  const loadSequence = React.useRef(0);
  const disabled = Boolean(activeJob) || starting || processingReviewID !== null;

  const loadCleanup = React.useCallback(async () => {
    const sequence = ++loadSequence.current;
    setLoading(true);
    try {
      const [nextStatus, nextReviews] = await Promise.all([
        fetchCleanupStatus(),
        fetchCleanupReviews(),
      ]);
      if (sequence !== loadSequence.current) return;
      setStatus(nextStatus);
      setReviews(nextReviews);
      setError(null);
    } catch (loadError) {
      if (sequence === loadSequence.current) {
        setError(loadError instanceof Error ? loadError.message : "Could not load database cleanup status.");
      }
    } finally {
      if (sequence === loadSequence.current) setLoading(false);
    }
  }, [loadSequence]);

  React.useEffect(() => {
    void loadCleanup();
  }, [loadCleanup, refreshKey]);

  const runCleanup = async () => {
    if (disabled) return;
    setConfirmCleanup(false);
    setStarting(true);
    try {
      await onCleanup();
    } finally {
      setStarting(false);
    }
  };

  const applyReview = async (review: CleanupReview, decision: CleanupReviewDecision) => {
    if (disabled) return;
    setProcessingReviewID(review.id);
    try {
      await onCleanupReview(review.id, decision);
      await loadCleanup();
    } finally {
      setProcessingReviewID(null);
    }
  };

  const confirmReviewAction = async () => {
    if (!confirmedAction) return;
    const action = confirmedAction;
    setConfirmedAction(null);
    await applyReview(action.review, action.decision);
  };

  const pendingCount = status?.pending_review_count ?? 0;

  return (
    <>
      <section className="border-b border-(--line) pb-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold text-(--ink)">Database cleanup</h3>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-muted">
              Scan all unclassified papers. Exact URL or DOI duplicates with matching titles are removed, DOI conflicts are sent to a DOI-only review, clear DOI mismatches are repaired while keeping the article, and uncertain cases wait for your decision. No remaining papers are classified while this review list is unresolved.
            </p>
          </div>
          {activeJob ? <Chip color="warning" size="sm" variant="soft">{activeJob.job_type === "cleanup-review" ? "Review applying" : "Scanning"}</Chip> : null}
        </div>

        {activeJob?.progress_percent != null ? (
          <p className="mt-3 text-sm text-muted">
            {activeJob.progress_label || "Processing cleanup"} · {activeJob.progress_percent}%
          </p>
        ) : null}
        {error ? <p className="mt-3 text-sm text-rose-700">{error}</p> : null}
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <Button isDisabled={disabled || loading} size="sm" onPress={() => setConfirmCleanup(true)}>
            <span className="inline-flex items-center gap-2">
              {starting ? <Spinner color="current" size="sm" /> : null}
              {activeJob ? "Cleanup running" : starting ? "Starting cleanup…" : "Run cleanup"}
            </span>
          </Button>
          <span className="text-sm text-muted">
            {status ? `${status.unclassified_paper_count.toLocaleString()} unclassified papers` : "Unclassified papers: —"}
          </span>
        </div>
      </section>

      <AdminDisclosure
        meta={<Chip color={pendingCount > 0 ? "warning" : "default"} size="sm" variant="soft">{pendingCount}</Chip>}
        title="Needs manual review"
      >
        {loading ? (
          <p className="text-sm text-muted">Loading cleanup reviews…</p>
        ) : reviews.length === 0 ? (
          <p className="text-sm text-muted">No pending cleanup reviews.</p>
        ) : (
          <div className="space-y-3">
            {reviews.map((review) => (
              <CleanupReviewCard
                key={review.id}
                disabled={disabled}
                processing={processingReviewID === review.id}
                review={review}
                onConfirm={(action) => setConfirmedAction(action)}
                onDecision={(decision) => void applyReview(review, decision)}
              />
            ))}
          </div>
        )}
      </AdminDisclosure>

      {confirmCleanup ? (
        <ModalShell
          eyebrow="Database cleanup"
          footer={
            <>
              <Button size="sm" variant="ghost" onPress={() => setConfirmCleanup(false)}>Cancel</Button>
              <Button isDisabled={starting} size="sm" variant="danger" onPress={() => void runCleanup()}>Run cleanup</Button>
            </>
          }
          onClose={() => setConfirmCleanup(false)}
          title="Scan all unclassified papers?"
        >
          <p className="text-sm leading-6 text-(--body)">
            The job will inspect every paper without a classification. Only exact URL or DOI duplicates with matching titles can be deleted automatically. Articles with a clear DOI mismatch are kept and repaired; title-only matches, DOI conflicts, and uncertain DOI checks stay in the review queue. If reviews are created or remain unresolved, the job stops before bulk classification. Run cleanup again after reviewing them.
          </p>
          <div className="rounded-md border border-amber-300/70 bg-amber-50 px-3 py-3 text-sm leading-6 text-amber-950">
            A SQLite backup is created before any database mutation. The cleanup can take time because DOI checks and any later classification run as a cancellable background job.
          </div>
        </ModalShell>
      ) : null}

      {confirmedAction ? (
        <ModalShell
          eyebrow="Database cleanup"
          footer={
            <>
              <Button size="sm" variant="ghost" onPress={() => setConfirmedAction(null)}>Cancel</Button>
              <Button size="sm" variant="danger" onPress={() => void confirmReviewAction()}>{confirmedAction.label}</Button>
            </>
          }
          onClose={() => setConfirmedAction(null)}
          title={confirmedAction.title}
        >
          <p className="text-sm leading-6 text-(--body)">{confirmedAction.description}</p>
          <div className="rounded-md border border-amber-300/70 bg-amber-50 px-3 py-3 text-sm leading-6 text-amber-950">
            <p className="font-medium">{confirmationPaper(confirmedAction).title}</p>
            <p className="mt-1"><PaperMeta paper={confirmationPaper(confirmedAction)} /></p>
          </div>
        </ModalShell>
      ) : null}
    </>
  );
}

function CleanupReviewCard({
  disabled,
  onConfirm,
  onDecision,
  processing,
  review,
}: {
  disabled: boolean;
  onConfirm: (action: ConfirmedReviewAction) => void;
  onDecision: (decision: CleanupReviewDecision) => void;
  processing: boolean;
  review: CleanupReview;
}) {
  const candidate = review.candidate;
  const matched = review.matched;
  const isTitleDuplicate = review.match_type === "title_duplicate" && Boolean(matched);
  const isDOIConflict = review.match_type === "doi_conflict" && Boolean(matched);
  const isUncertainDOI = review.match_type === "doi_uncertain";
  const clearCandidateDOI = Boolean(candidate.doi?.trim());
  const clearMatchedDOI = Boolean(matched?.doi?.trim());
  const actionDisabled = disabled || processing;
  return (
    <article className="min-w-0 rounded-md border border-(--line) bg-(--paper) p-3 text-sm [overflow-wrap:anywhere]">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted">{reviewTypeLabel(review)}</p>
        </div>
        {processing ? <Spinner color="current" size="sm" /> : null}
      </div>
      <div className="mt-3">
        {isTitleDuplicate || isDOIConflict ? (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <div className="min-w-0 rounded-md border border-(--line) p-3">
              <p className="text-xs font-semibold uppercase tracking-[0.12em] text-muted">Item A</p>
              <h4 className="mt-1 font-semibold text-(--ink)">{candidate.title}</h4>
              <p className="mt-1 text-xs leading-5 text-muted"><PaperMeta paper={candidate} /> · first seen {formatDate(candidate.first_seen_at)}</p>
            </div>
            {matched ? (
              <div className="min-w-0 rounded-md border border-(--line) p-3">
                <p className="text-xs font-semibold uppercase tracking-[0.12em] text-muted">Item B</p>
                <p className="mt-1 font-medium text-(--ink)">{matched.title}</p>
                <p className="mt-1 text-xs leading-5 text-muted"><PaperMeta paper={matched} /> · first seen {formatDate(matched.first_seen_at)}</p>
              </div>
            ) : null}
          </div>
        ) : (
          <>
            <p className="text-xs font-semibold uppercase tracking-[0.12em] text-muted">Current article</p>
            <h4 className="mt-1 font-semibold text-(--ink)">{candidate.title}</h4>
            <p className="mt-1 text-xs leading-5 text-muted"><PaperMeta paper={candidate} /> · first seen {formatDate(candidate.first_seen_at)}</p>
          </>
        )}
      </div>
      <p className="mt-3 leading-6 text-(--body)">{review.reason}</p>
      <div className="mt-3 flex flex-wrap gap-2">
        {isTitleDuplicate ? (
          <Button
            isDisabled={actionDisabled}
            size="sm"
            variant="danger"
            onPress={() => onConfirm({
              decision: "delete",
              description: "Delete Item A and keep Item B. Any feedback or Zotero link is moved to the retained article.",
              label: "Delete Item A",
              review,
              title: "Delete Item A?",
            })}
          >
            Delete Item A
          </Button>
        ) : null}
        {isTitleDuplicate ? (
          <Button
            isDisabled={actionDisabled}
            size="sm"
            variant="danger"
            onPress={() => onConfirm({
              decision: "delete_match",
              description: "Delete Item B and keep Item A. Any feedback or Zotero link is moved to the retained article, which remains unclassified for the next cleanup classification batch.",
              label: "Delete Item B",
              review,
              title: "Delete Item B?",
            })}
          >
            Delete Item B
          </Button>
        ) : null}
        {isTitleDuplicate ? (
          <Button isDisabled={actionDisabled} size="sm" onPress={() => onDecision("keep")}>Keep current</Button>
        ) : null}
        {isDOIConflict ? (
          <>
            {clearCandidateDOI ? (
              <Button
                isDisabled={actionDisabled}
                size="sm"
                variant="danger"
                onPress={() => onConfirm({
                  decision: "clear_doi",
                  description: "Clear the DOI from Item A and keep both articles. Item A remains unclassified for the next cleanup classification batch.",
                  label: "Clear Item A DOI",
                  review,
                  title: "Clear Item A DOI?",
                })}
              >
                Clear Item A DOI
              </Button>
            ) : null}
            {clearMatchedDOI ? (
              <Button
                isDisabled={actionDisabled}
                size="sm"
                variant="danger"
                onPress={() => onConfirm({
                  decision: "clear_match_doi",
                  description: "Clear the DOI from Item B and keep both articles. Item A remains unclassified for the next cleanup classification batch.",
                  label: "Clear Item B DOI",
                  review,
                  title: "Clear Item B DOI?",
                })}
              >
                Clear Item B DOI
              </Button>
            ) : null}
            <Button isDisabled={actionDisabled} size="sm" onPress={() => onDecision("keep")}>Keep both unchanged</Button>
          </>
        ) : null}
        {isUncertainDOI ? (
          <>
            <Button isDisabled={actionDisabled} size="sm" onPress={() => onDecision("keep")}>Keep current</Button>
            {clearCandidateDOI ? (
              <Button
                isDisabled={actionDisabled}
                size="sm"
                variant="danger"
                onPress={() => onConfirm({
                  decision: "clear_doi",
                  description: "Keep this article, clear the unverified DOI, and leave it unclassified for the next cleanup classification batch.",
                  label: "Clear DOI",
                  review,
                  title: "Clear this DOI?",
                })}
              >
                Clear DOI
              </Button>
            ) : null}
          </>
        ) : null}
      </div>
    </article>
  );
}
