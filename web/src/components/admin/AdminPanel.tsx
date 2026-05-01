import {Button, Input, Spinner} from "@heroui/react";

import {relevanceLabel} from "../../app/constants";
import {ProfileProposalPreview} from "../profile/ProfileProposalPreview";
import {ProfileRulesDocument} from "../profile/ProfileRulesDocument";
import type {
  ClassificationProfile,
  FeedSubscription,
  FeedbackRecord,
  JobInfo,
  ProfileProposal,
} from "../../types";

export function AdminPanel({
  feedback,
  feeds,
  feedsSaving,
  hasFeeds,
  jobs,
  onAddFeed,
  onApplyProposal,
  onClose,
  onDeleteFeedback,
  onFeedChange,
  onGenerateProposal,
  onReclassifyAll,
  onReclassifyFeedback,
  onReclassifyRecent,
  onRefreshReport,
  onRejectProposal,
  onRemoveFeed,
  onRunFeedSync,
  onSaveFeeds,
  open,
  profile,
  proposalGenerating,
  proposals,
}: {
  feedback: FeedbackRecord[];
  feeds: FeedSubscription[];
  feedsSaving: boolean;
  hasFeeds: boolean;
  jobs: JobInfo[];
  onAddFeed: () => void;
  onApplyProposal: (id: number) => void;
  onClose: () => void;
  onDeleteFeedback: (id: number) => void;
  onFeedChange: (index: number, field: "journal" | "url", value: string) => void;
  onGenerateProposal: () => void;
  onReclassifyAll: () => void;
  onReclassifyFeedback: () => void;
  onReclassifyRecent: () => void;
  onRefreshReport: () => void;
  onRejectProposal: (id: number) => void;
  onRemoveFeed: (index: number) => void;
  onRunFeedSync: () => void;
  onSaveFeeds: () => void;
  open: boolean;
  profile: ClassificationProfile | null;
  proposalGenerating: boolean;
  proposals: ProfileProposal[];
}) {
  if (!open) {
    return null;
  }
  const pendingProposal = proposals.find((item) => item.state === "pending") ?? null;
  const openFeedback = feedback.filter((item) => item.state === "open");

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-slate-900/20">
      <aside className="h-full w-full max-w-[min(1100px,92vw)] overflow-auto border-l border-(--line) bg-(--paper) p-4 shadow-xl">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2 className="mt-2 text-2xl font-semibold text-(--ink)">Settings</h2>
          </div>
          <Button size="sm" variant="ghost" onPress={onClose}>
            Close
          </Button>
        </div>

        <section className="mt-5 rounded-lg border border-(--line) bg-(--paper-accent) p-4">
          <h3 className="text-sm font-semibold text-(--ink)">Actions</h3>
          {!hasFeeds ? (
            <p className="mt-2 text-sm leading-6 text-muted">
              Add and save at least one RSS feed before running a manual fetch job.
            </p>
          ) : null}
          <div className="mt-3 flex flex-wrap gap-2">
            <Button isDisabled={!hasFeeds} size="sm" onPress={onRunFeedSync}>
              Run fetch + classify
            </Button>
            <Button size="sm" variant="outline" onPress={onRefreshReport}>
              Refresh report
            </Button>
            <Button size="sm" variant="outline" onPress={onReclassifyRecent}>
              Reclassify recent 50
            </Button>
            <Button size="sm" variant="outline" onPress={onReclassifyFeedback}>
              Reclassify feedback papers
            </Button>
            <Button size="sm" variant="outline" onPress={onReclassifyAll}>
              Reclassify all
            </Button>
            <Button
              isDisabled={proposalGenerating}
              size="sm"
              variant="secondary"
              onPress={onGenerateProposal}
            >
              <span className="inline-flex items-center gap-2">
                {proposalGenerating ? <Spinner color="current" size="sm" /> : null}
                Generate profile proposal
              </span>
            </Button>
          </div>
        </section>

        <section className="mt-4 rounded-lg border border-(--line) bg-(--paper-accent) p-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-sm font-semibold text-(--ink)">Feed subscriptions</h3>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="outline" onPress={onAddFeed}>
                Add row
              </Button>
              <Button size="sm" isDisabled={feedsSaving} onPress={onSaveFeeds}>
                {feedsSaving ? "Saving..." : "Save feeds"}
              </Button>
            </div>
          </div>
          <div className="mt-3 max-h-[52vh] overflow-auto rounded-lg border border-(--line)">
            <table className="w-full border-collapse text-sm">
              <thead className="sticky top-0 bg-(--paper)">
                <tr className="text-left text-(--ink)">
                  <th className="px-3 py-2 font-semibold">Name</th>
                  <th className="px-3 py-2 font-semibold">URL</th>
                  <th className="w-20 px-3 py-2 font-semibold">Action</th>
                </tr>
              </thead>
              <tbody>
                {feeds.length === 0 ? (
                  <tr>
                    <td className="px-3 py-4 text-muted" colSpan={3}>
                      No RSS feeds configured yet.
                    </td>
                  </tr>
                ) : (
                  feeds.map((item, index) => (
                    <tr key={`${item.url}-${index}`} className="border-t border-(--line)">
                      <td className="px-3 py-2 align-top">
                        <Input
                          aria-label={`Feed name ${index + 1}`}
                          className="w-full"
                          value={item.journal}
                          onChange={(event) =>
                            onFeedChange(index, "journal", event.target.value)
                          }
                        />
                      </td>
                      <td className="px-3 py-2 align-top">
                        <Input
                          aria-label={`Feed URL ${index + 1}`}
                          className="w-full"
                          value={item.url}
                          onChange={(event) => onFeedChange(index, "url", event.target.value)}
                        />
                      </td>
                      <td className="px-3 py-2 align-top">
                        <Button size="sm" variant="ghost" onPress={() => onRemoveFeed(index)}>
                          Delete
                        </Button>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </section>

        <section className="mt-4 rounded-lg border border-(--line) bg-(--paper-accent) p-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-sm font-semibold text-(--ink)">Classification profile</h3>
            <Button
              isDisabled={proposalGenerating}
              size="sm"
              variant="secondary"
              onPress={onGenerateProposal}
            >
              <span className="inline-flex items-center gap-2">
                {proposalGenerating ? <Spinner color="current" size="sm" /> : null}
                Generate proposal
              </span>
            </Button>
          </div>
          <div className="mt-3 space-y-3">
            {pendingProposal ? (
              <>
                <ProfileProposalPreview proposal={pendingProposal} />
                <div className="flex flex-wrap gap-2">
                  <Button
                    isDisabled={pendingProposal.state !== "pending"}
                    size="sm"
                    onPress={() => onApplyProposal(pendingProposal.id)}
                  >
                    Apply
                  </Button>
                  <Button
                    isDisabled={pendingProposal.state !== "pending"}
                    size="sm"
                    variant="ghost"
                    onPress={() => onRejectProposal(pendingProposal.id)}
                  >
                    Reject
                  </Button>
                </div>
              </>
            ) : profile ? (
              <ProfileRulesDocument profile={profile} />
            ) : (
              <p className="text-sm text-muted">No profile available yet.</p>
            )}
          </div>
        </section>

        <section className="mt-4 rounded-lg border border-(--line) bg-(--paper-accent) p-4">
          <h3 className="text-sm font-semibold text-(--ink)">Feedback queue</h3>
          <div className="mt-3 overflow-hidden rounded-lg border border-(--line)">
            {openFeedback.length === 0 ? (
              <p className="text-sm text-muted">No feedback submitted yet.</p>
            ) : (
              <table className="w-full border-collapse text-sm">
                <thead className="bg-(--paper)">
                  <tr className="text-left text-(--ink)">
                    <th className="px-3 py-2 font-semibold">Paper</th>
                    <th className="px-3 py-2 font-semibold">From</th>
                    <th className="px-3 py-2 font-semibold">To</th>
                    <th className="px-3 py-2 font-semibold">Note</th>
                    <th className="px-3 py-2 font-semibold">Created</th>
                    <th className="w-20 px-3 py-2 font-semibold">Action</th>
                  </tr>
                </thead>
                <tbody>
                  {openFeedback.map((item) => (
                    <tr key={item.id} className="border-t border-(--line) align-top">
                      <td className="px-3 py-2 text-(--ink)">{item.paper_title}</td>
                      <td className="px-3 py-2 text-muted">
                        {relevanceLabel[item.original_relevance]}
                      </td>
                      <td className="px-3 py-2 text-muted">
                        {relevanceLabel[item.corrected_relevance]}
                      </td>
                      <td className="px-3 py-2 leading-6 text-(--body)">
                        {item.note?.trim() ? item.note : "—"}
                      </td>
                      <td className="px-3 py-2 text-muted">
                        {new Date(item.created_at).toLocaleDateString()}
                      </td>
                      <td className="px-3 py-2">
                        <Button size="sm" variant="ghost" onPress={() => onDeleteFeedback(item.id)}>
                          Delete
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </section>
      </aside>
    </div>
  );
}
