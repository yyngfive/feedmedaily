import {Button, Card} from "@heroui/react";
import React from "react";

import {statusMessage} from "../../app/utils";
import {StatusBanner} from "../common/StatusBanner";
import {ProfileProposalPreview} from "../profile/ProfileProposalPreview";
import type {JobInfo, ProfileProposal} from "../../types";

export function Onboarding({
  busy,
  jobs,
  onApplyProposal,
  onBootstrap,
  proposals,
}: {
  busy: boolean;
  jobs: JobInfo[];
  onApplyProposal: (proposalId: number) => Promise<void>;
  onBootstrap: (interestDescription: string, name: string) => Promise<void>;
  proposals: ProfileProposal[];
}) {
  const [name, setName] = React.useState("Default profile");
  const [interestDescription, setInterestDescription] = React.useState("");
  const pendingProposal = proposals.find((item) => item.state === "pending") ?? proposals[0] ?? null;
  const latestBootstrapJob = jobs.find((item) => item.job_type === "profile-bootstrap") ?? null;
  const bootstrapRunning =
    latestBootstrapJob?.status === "queued" || latestBootstrapJob?.status === "running";

  return (
    <main className="mx-auto max-w-7xl px-4 py-6">
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_520px]">
        <Card className="border border-[var(--line)] bg-white">
          <Card.Header className="space-y-2">
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--muted)]">
              SciRSSAgent
            </p>
            <h1 className="text-2xl font-semibold text-[var(--ink)]">
              Create your classification profile
            </h1>
            <p className="text-sm leading-6 text-[var(--muted)]">
              Describe your research interests in natural language. Model B will turn it into a
              structured classification profile for approval.
            </p>
          </Card.Header>
          <Card.Content className="space-y-4">
            <label className="block text-sm font-medium text-[var(--ink)]">
              Profile name
              <input
                className="mt-2 w-full rounded-md border border-[var(--line)] px-3 py-2 text-sm"
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </label>
            <label className="block text-sm font-medium text-[var(--ink)]">
              Research interests
              <textarea
                className="mt-2 min-h-52 w-full rounded-md border border-[var(--line)] px-3 py-2 text-sm"
                placeholder="Example: I care about nucleic acid chemistry, engineered polymerases, XNA enzymes, and method-focused directed evolution papers..."
                value={interestDescription}
                onChange={(event) => setInterestDescription(event.target.value)}
              />
            </label>
            {latestBootstrapJob ? (
              <StatusBanner
                tone={
                  latestBootstrapJob.status === "failed"
                    ? "danger"
                    : latestBootstrapJob.status === "completed"
                      ? "success"
                      : "info"
                }
              >
                <p className="font-medium">
                  {bootstrapRunning ? "Generating profile..." : "Latest profile generation job"}
                </p>
                <p className="mt-1 leading-6">{statusMessage(latestBootstrapJob)}</p>
              </StatusBanner>
            ) : null}
          </Card.Content>
          <Card.Footer>
            <Button
              isDisabled={busy || !interestDescription.trim()}
              onPress={() => void onBootstrap(interestDescription.trim(), name.trim() || "Default profile")}
            >
              {bootstrapRunning ? "Generating..." : "Generate initial profile"}
            </Button>
          </Card.Footer>
        </Card>

        <Card className="border border-[var(--line)] bg-white">
          <Card.Header>
            <h2 className="text-lg font-semibold text-[var(--ink)]">Latest proposal</h2>
          </Card.Header>
          <Card.Content className="max-h-[78vh] space-y-4 overflow-y-auto pr-1">
            {!pendingProposal ? (
              <p className="text-sm text-[var(--muted)]">No proposal yet.</p>
            ) : (
              <ProfileProposalPreview proposal={pendingProposal} />
            )}
          </Card.Content>
          {pendingProposal ? (
            <Card.Footer className="flex gap-2">
              <Button
                isDisabled={busy || bootstrapRunning || pendingProposal.state !== "pending"}
                onPress={() => void onApplyProposal(pendingProposal.id)}
              >
                Apply profile
              </Button>
            </Card.Footer>
          ) : null}
        </Card>
      </div>
    </main>
  );
}
