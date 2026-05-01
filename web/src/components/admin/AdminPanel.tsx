import {Button, Card, Chip} from "@heroui/react";

import {relevanceLabel} from "../../app/constants";
import {statusMessage} from "../../app/utils";
import {ProfileProposalPreview} from "../profile/ProfileProposalPreview";
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
  proposals: ProfileProposal[];
}) {
  if (!open) {
    return null;
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-slate-900/20">
      <aside className="h-full w-full max-w-[min(1100px,92vw)] overflow-auto border-l border-[var(--line)] bg-[var(--paper)] p-4 shadow-xl">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--muted)]">
              Admin
            </p>
            <h2 className="mt-2 text-2xl font-semibold text-[var(--ink)]">Control center</h2>
          </div>
          <Button size="sm" variant="ghost" onPress={onClose}>
            Close
          </Button>
        </div>

        <section className="mt-5 rounded-lg border border-[var(--line)] bg-white p-4">
          <h3 className="text-sm font-semibold text-[var(--ink)]">Actions</h3>
          {!hasFeeds ? (
            <p className="mt-2 text-sm leading-6 text-[var(--muted)]">
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
            <Button size="sm" variant="secondary" onPress={onGenerateProposal}>
              Generate profile proposal
            </Button>
          </div>
        </section>

        <section className="mt-4 rounded-lg border border-[var(--line)] bg-white p-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-sm font-semibold text-[var(--ink)]">Feed subscriptions</h3>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="outline" onPress={onAddFeed}>
                Add feed
              </Button>
              <Button size="sm" isDisabled={feedsSaving} onPress={onSaveFeeds}>
                {feedsSaving ? "Saving..." : "Save feeds"}
              </Button>
            </div>
          </div>
          <div className="mt-3 space-y-3">
            {feeds.length === 0 ? (
              <p className="text-sm text-[var(--muted)]">No RSS feeds configured yet.</p>
            ) : (
              feeds.map((item, index) => (
                <Card key={`${item.url}-${index}`} className="border border-[var(--line)]">
                  <Card.Content className="space-y-3">
                    <label className="block text-sm font-medium text-[var(--ink)]">
                      Journal name
                      <input
                        className="mt-2 w-full rounded-md border border-[var(--line)] px-3 py-2 text-sm"
                        value={item.journal}
                        onChange={(event) => onFeedChange(index, "journal", event.target.value)}
                      />
                    </label>
                    <label className="block text-sm font-medium text-[var(--ink)]">
                      RSS URL
                      <input
                        className="mt-2 w-full rounded-md border border-[var(--line)] px-3 py-2 text-sm"
                        value={item.url}
                        onChange={(event) => onFeedChange(index, "url", event.target.value)}
                      />
                    </label>
                    <div className="flex justify-end">
                      <Button size="sm" variant="ghost" onPress={() => onRemoveFeed(index)}>
                        Remove
                      </Button>
                    </div>
                  </Card.Content>
                </Card>
              ))
            )}
          </div>
        </section>

        <section className="mt-4 rounded-lg border border-[var(--line)] bg-white p-4">
          <h3 className="text-sm font-semibold text-[var(--ink)]">Current profile</h3>
          {!profile ? (
            <p className="mt-3 text-sm text-[var(--muted)]">No applied profile yet.</p>
          ) : (
            <div className="mt-3 space-y-3">
              <div className="flex flex-wrap gap-2">
                <Chip size="sm" variant="secondary">
                  {profile.meta.name}
                </Chip>
                <Chip size="sm" variant="secondary">
                  v{profile.meta.version}
                </Chip>
                <Chip size="sm" variant="secondary">
                  {profile.topic_taxonomy.length} tags
                </Chip>
              </div>
              <p className="text-sm leading-6 text-[var(--body)]">{profile.scope}</p>
            </div>
          )}
        </section>

        <section className="mt-4 rounded-lg border border-[var(--line)] bg-white p-4">
          <h3 className="text-sm font-semibold text-[var(--ink)]">Profile proposals</h3>
          <div className="mt-3 space-y-3">
            {proposals.length === 0 ? (
              <p className="text-sm text-[var(--muted)]">No profile proposals yet.</p>
            ) : (
              proposals.map((proposal) => (
                <Card key={proposal.id} className="border border-[var(--line)]">
                  <Card.Header className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <Chip size="sm" variant="secondary">
                        {proposal.state}
                      </Chip>
                      <Chip size="sm" variant="secondary">
                        {proposal.model}
                      </Chip>
                    </div>
                    <span className="text-xs text-[var(--muted)]">
                      {proposal.created_at.slice(0, 10)}
                    </span>
                  </Card.Header>
                  <Card.Content className="max-h-[78vh] overflow-y-auto pr-1">
                    <ProfileProposalPreview proposal={proposal} />
                  </Card.Content>
                  <Card.Footer className="flex flex-wrap gap-2">
                    <Button
                      isDisabled={proposal.state !== "pending"}
                      size="sm"
                      onPress={() => onApplyProposal(proposal.id)}
                    >
                      Apply
                    </Button>
                    <Button
                      isDisabled={proposal.state !== "pending"}
                      size="sm"
                      variant="ghost"
                      onPress={() => onRejectProposal(proposal.id)}
                    >
                      Reject
                    </Button>
                  </Card.Footer>
                </Card>
              ))
            )}
          </div>
        </section>

        <section className="mt-4 rounded-lg border border-[var(--line)] bg-white p-4">
          <h3 className="text-sm font-semibold text-[var(--ink)]">Feedback queue</h3>
          <div className="mt-3 space-y-3">
            {feedback.length === 0 ? (
              <p className="text-sm text-[var(--muted)]">No feedback submitted yet.</p>
            ) : (
              feedback.map((item) => (
                <Card key={item.id} className="border border-[var(--line)]">
                  <Card.Content className="space-y-2">
                    <p className="text-sm font-semibold text-[var(--ink)]">{item.paper_title}</p>
                    <p className="text-sm text-[var(--muted)]">
                      {relevanceLabel[item.original_relevance]} {" -> "}{" "}
                      {relevanceLabel[item.corrected_relevance]}
                    </p>
                    {item.note ? (
                      <p className="text-sm leading-6 text-[var(--body)]">{item.note}</p>
                    ) : null}
                    <div className="flex flex-wrap gap-2">
                      <Chip size="sm" variant="secondary">
                        {item.state}
                      </Chip>
                      {item.used_in_profile ? (
                        <Chip color="success" size="sm" variant="soft">
                          Used in profile
                        </Chip>
                      ) : null}
                      <Button
                        size="sm"
                        variant="ghost"
                        onPress={() => onDeleteFeedback(item.id)}
                      >
                        Delete
                      </Button>
                    </div>
                  </Card.Content>
                </Card>
              ))
            )}
          </div>
        </section>

        <section className="mt-4 rounded-lg border border-[var(--line)] bg-white p-4">
          <h3 className="text-sm font-semibold text-[var(--ink)]">Jobs</h3>
          <div className="mt-3 space-y-3">
            {jobs.length === 0 ? (
              <p className="text-sm text-[var(--muted)]">No jobs yet.</p>
            ) : (
              jobs.map((job) => (
                <Card key={job.id} className="border border-[var(--line)]">
                  <Card.Content className="space-y-2">
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-sm font-semibold text-[var(--ink)]">
                        {job.job_type}
                      </span>
                      <Chip size="sm" variant="secondary">
                        {job.status}
                      </Chip>
                    </div>
                    <p className="text-sm leading-6 text-[var(--body)]">{statusMessage(job)}</p>
                  </Card.Content>
                </Card>
              ))
            )}
          </div>
        </section>
      </aside>
    </div>
  );
}
