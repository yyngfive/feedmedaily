import {Button, Card, Spinner} from "@heroui/react";

import {relevanceLabel} from "../../app/constants";
import type {ClassificationProfile, FeedbackRecord, ProfileProposal} from "../../shared/types";
import {ProfileProposalReview} from "../profile/ProfileProposalReview";
import {ProfileRulesDocument} from "../profile/ProfileRulesDocument";

export function ProfileTab({
  feedback,
  onApplyProposal,
  onDeleteFeedback,
  onGenerateProposal,
  onRejectProposal,
  onSaveProfile,
  profile,
  profileSaving,
  proposalGenerating,
  proposals,
}: {
  feedback: FeedbackRecord[];
  onApplyProposal: (id: number, selection?: {accepted_change_ids: string[]; rejected_change_ids: string[]}) => Promise<void> | void;
  onDeleteFeedback: (id: number) => void;
  onGenerateProposal: () => void;
  onRejectProposal: (id: number) => void;
  onSaveProfile: (profile: ClassificationProfile) => Promise<void> | void;
  profile: ClassificationProfile | null;
  profileSaving: boolean;
  proposalGenerating: boolean;
  proposals: ProfileProposal[];
}) {
  const pendingProposal = proposals.find((item) => item.state === "pending") ?? null;
  const openFeedback = feedback.filter((item) => item.state === "open");

  return (
    <div className="mt-5 space-y-5">
      <div className="space-y-4">
        <div className="flex justify-end">
          <Button isDisabled={proposalGenerating} size="sm" variant="secondary" onPress={onGenerateProposal}>
            <span className="inline-flex items-center gap-2">
              {proposalGenerating ? <Spinner color="current" size="sm" /> : null}
              Generate profile proposal
            </span>
          </Button>
        </div>
        {pendingProposal ? (
          profile ? (
            <ProfileProposalReview proposal={pendingProposal} onApplySelection={onApplyProposal} onRejectProposal={onRejectProposal} />
          ) : (
            <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
              <Card.Content className="p-4 text-sm text-muted">No current profile available for proposal review.</Card.Content>
            </Card>
          )
        ) : profile ? (
          <ProfileRulesDocument editable onSave={onSaveProfile} profile={profile} saving={profileSaving} />
        ) : (
          <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
            <Card.Content className="p-4 text-sm text-muted">No profile available yet.</Card.Content>
          </Card>
        )}

        <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
          <Card.Header className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-sm font-semibold text-(--ink)">Feedback queue</h3>
            <span className="text-sm text-muted">{openFeedback.length} open</span>
          </Card.Header>
          <Card.Content>
            <div className="overflow-hidden rounded-md border border-(--line)">
              {openFeedback.length === 0 ? (
                <p className="px-3 py-4 text-sm text-muted">No feedback submitted yet.</p>
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
                        <td className="px-3 py-2 text-muted">{relevanceLabel[item.original_relevance]}</td>
                        <td className="px-3 py-2 text-muted">{relevanceLabel[item.corrected_relevance]}</td>
                        <td className="px-3 py-2 leading-6 text-(--body)">{item.note?.trim() ? item.note : "-"}</td>
                        <td className="px-3 py-2 text-muted">{new Date(item.created_at).toLocaleDateString()}</td>
                        <td className="px-3 py-2"><Button size="sm" variant="ghost" onPress={() => onDeleteFeedback(item.id)}>Delete</Button></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </Card.Content>
        </Card>
      </div>
    </div>
  );
}
