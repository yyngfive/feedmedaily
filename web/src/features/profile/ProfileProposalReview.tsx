import {Button, Card} from "@heroui/react";
import {Diff, Hunk, parseDiff, type FileData} from "react-diff-view";
import React from "react";

import type {
  ProfileProposal,
  ProposalChange,
  ProposalChangeOperation,
  ProposalChangeSection,
  ProposalChangeStatus,
  TopicDefinition,
} from "../../shared/types";

const sectionLabels: Record<ProposalChangeSection, string> = {
  scope: "Scope",
  direct_rule: "Direct rules",
  indirect_rule: "Indirect rules",
  unrelated_rule: "Unrelated rules",
  topic: "Topics",
};

const operationLabels: Record<ProposalChangeOperation, string> = {
  add: "Add",
  remove: "Remove",
  rewrite: "Rewrite",
  merge: "Merge",
};

type ChangeSelection = {
  accepted_change_ids: string[];
  rejected_change_ids: string[];
};

type SectionSummary = {
  added: number;
  modified: number;
  removed: number;
  section: ProposalChangeSection;
};

type CompactnessSummary = {
  added: number;
  merged: number;
  net: number;
  removed: number;
};

function topicLine(item: TopicDefinition) {
  return `${item.label} [${item.id}]`;
}

function changeLines(change: ProposalChange, side: "before" | "after") {
  if (change.section === "topic") {
    const items = side === "before" ? change.topic_before : change.topic_after;
    return items.map(topicLine);
  }
  return side === "before" ? change.text_before : change.text_after;
}

function changeUnits(section: ProposalChangeSection) {
  switch (section) {
    case "scope":
      return "scope";
    case "topic":
      return "topics";
    default:
      return "rules";
  }
}

function formatCount(value: number) {
  return value > 0 ? `+${value}` : String(value);
}

function humanDelta(value: number) {
  if (value > 0) {
    return `Net ${formatCount(value)} rules`;
  }
  if (value < 0) {
    return `Net ${value} rules`;
  }
  return "Net 0 rules";
}

function changeShapeLabel(change: ProposalChange) {
  const beforeCount = changeLines(change, "before").length;
  const afterCount = changeLines(change, "after").length;
  const unit = changeUnits(change.section);
  if (change.section === "scope") {
    return "1 -> 1 scope";
  }
  if (change.operation === "add") {
    return `0 -> ${afterCount} ${unit}`;
  }
  if (change.operation === "remove") {
    return `${beforeCount} -> 0 ${unit}`;
  }
  return `${beforeCount} -> ${afterCount} ${unit}`;
}

function buildDiffFile(change: ProposalChange): FileData | null {
  const beforeLines = changeLines(change, "before");
  const afterLines = changeLines(change, "after");
  const oldStart = beforeLines.length === 0 ? 0 : 1;
  const newStart = afterLines.length === 0 ? 0 : 1;
  const fileName = `${change.section}/${change.id}.txt`;
  const diffText = [
    `diff --git a/${fileName} b/${fileName}`,
    `--- a/${fileName}`,
    `+++ b/${fileName}`,
    `@@ -${oldStart},${beforeLines.length} +${newStart},${afterLines.length} @@`,
    ...beforeLines.map((line) => `-${line}`),
    ...afterLines.map((line) => `+${line}`),
    "",
  ].join("\n");
  return parseDiff(diffText, {nearbySequences: "zip"})[0] ?? null;
}

function createSectionSummary(section: ProposalChangeSection): SectionSummary {
  return {section, added: 0, removed: 0, modified: 0};
}

function collectSectionSummaries(changes: ProposalChange[]) {
  const orderedSections: ProposalChangeSection[] = [
    "scope",
    "direct_rule",
    "indirect_rule",
    "unrelated_rule",
    "topic",
  ];
  const bySection = new Map<ProposalChangeSection, SectionSummary>(
    orderedSections.map((section) => [section, createSectionSummary(section)]),
  );

  changes.forEach((change) => {
    const summary = bySection.get(change.section);
    if (!summary) {
      return;
    }
    const beforeCount = changeLines(change, "before").length;
    const afterCount = changeLines(change, "after").length;
    switch (change.operation) {
      case "add":
        summary.added += afterCount;
        break;
      case "remove":
        summary.removed += beforeCount;
        break;
      case "rewrite":
      case "merge":
        summary.added += afterCount;
        summary.removed += beforeCount;
        summary.modified += 1;
        break;
    }
    if (change.section === "scope") {
      summary.added = 0;
      summary.removed = 0;
      summary.modified = 1;
    }
  });

  return orderedSections.map((section) => bySection.get(section)!);
}

function collectCompactness(changes: ProposalChange[]): CompactnessSummary {
  return changes.reduce<CompactnessSummary>(
    (summary, change) => {
      if (change.section === "scope" || change.section === "topic") {
        return summary;
      }
      const beforeCount = change.text_before.length;
      const afterCount = change.text_after.length;
      switch (change.operation) {
        case "add":
          summary.added += afterCount;
          break;
        case "remove":
          summary.removed += beforeCount;
          break;
        case "rewrite":
          summary.added += afterCount;
          summary.removed += beforeCount;
          break;
        case "merge":
          summary.added += afterCount;
          summary.removed += beforeCount;
          summary.merged += 1;
          break;
      }
      summary.net = summary.added - summary.removed;
      return summary;
    },
    {added: 0, removed: 0, merged: 0, net: 0},
  );
}

function hasAnyPhrase(text: string, phrases: string[]) {
  return phrases.some((phrase) => text.includes(phrase));
}

function countPhraseHits(text: string, phrases: string[]) {
  return phrases.reduce((count, phrase) => (text.includes(phrase) ? count + 1 : count), 0);
}

function normalizedChangeText(lines: string[]) {
  return lines.join(" ").toLowerCase();
}

function reasonSignals() {
  return [
    "core innovation",
    "main innovation",
    "central to the innovation",
    "central to innovation",
    "rather than",
    "unless",
    "used only as",
    "recognition element",
    "innovation is in",
    "not the innovation",
    "innovation center",
  ];
}

function hasOverSpecificAddWarning(change: ProposalChange) {
  if (change.operation !== "add" || change.section === "scope" || change.section === "topic") {
    return false;
  }
  const normalized = normalizedChangeText(change.text_after);
  if (!normalized) {
    return false;
  }

  if (hasAnyPhrase(normalized, reasonSignals())) {
    return false;
  }

  const objectSignals = [
    "e.g.",
    "such as",
    "including",
    "for example",
    "mof",
    "cof",
    "hof",
    "nanoparticle",
    "nanowire",
    "hydrogel",
    "electrode",
    "platform",
    "sensor",
    "biosensor",
    "aptamer",
    "peptide",
    "protein",
    "device",
    "matrix",
    "assembly",
    "probe",
  ];
  const objectHitCount = countPhraseHits(normalized, objectSignals);
  const commaCount = normalized.split(",").length - 1;
  const parentheticalExamples = (normalized.includes("(") && normalized.includes(")")) || normalized.includes("/");
  return objectHitCount >= 3 || (objectHitCount >= 2 && (commaCount >= 2 || parentheticalExamples));
}

function significantTokens(text: string) {
  return Array.from(
    new Set(
      text
        .toLowerCase()
        .split(/[^a-z0-9]+/)
        .filter((token) => token.length >= 5),
    ),
  );
}

function overlapScore(left: string[], right: string[]) {
  const leftText = normalizedChangeText(left);
  const rightText = normalizedChangeText(right);
  const leftTokens = significantTokens(leftText);
  const rightTokens = new Set(significantTokens(rightText));
  if (leftTokens.length === 0 || rightTokens.size === 0) {
    return 0;
  }
  const shared = leftTokens.filter((token) => rightTokens.has(token)).length;
  return shared / Math.max(leftTokens.length, rightTokens.size);
}

function hasDuplicateBoundaryWarning(change: ProposalChange, changes: ProposalChange[]) {
  if (change.operation !== "add" || change.section === "scope" || change.section === "topic") {
    return false;
  }
  return changes.some((other) => {
    if (other.id === change.id || other.section !== change.section) {
      return false;
    }
    if (other.operation !== "merge" && other.operation !== "rewrite") {
      return false;
    }
    return overlapScore(change.text_after, other.text_after) >= 0.45;
  });
}

function hasLostCoverageWarning(change: ProposalChange) {
  if ((change.operation !== "merge" && change.operation !== "rewrite") || change.section === "scope" || change.section === "topic") {
    return false;
  }
  const beforeText = normalizedChangeText(change.text_before);
  const afterText = normalizedChangeText(change.text_after);
  if (!beforeText || !afterText) {
    return false;
  }
  const salientTerms = [
    "nanostructure",
    "nano-assembly",
    "nanoassembly",
    "nanotube",
    "nanotubes",
    "nanowire",
    "nanowires",
    "nanoparticle",
    "nanoparticles",
    "origami",
    "tile",
    "tiles",
    "platform",
    "device",
    "hydrogel",
    "photophysics",
    "junction",
    "assembly",
  ];
  const beforeHits = salientTerms.filter((term) => beforeText.includes(term));
  if (beforeHits.length === 0) {
    return false;
  }
  const afterPreserved = beforeHits.some((term) => afterText.includes(term));
  if (afterPreserved) {
    return false;
  }
  return !hasAnyPhrase(afterText, reasonSignals());
}

function SelectionButton({
  active,
  children,
  onPress,
}: {
  active: boolean;
  children: React.ReactNode;
  onPress: () => void;
}) {
  return (
    <Button size="sm" variant={active ? "secondary" : "outline"} onPress={onPress}>
      {children}
    </Button>
  );
}

function MetricCard({
  label,
  tone = "default",
  value,
}: {
  label: string;
  tone?: "default" | "danger" | "success" | "warning";
  value: string;
}) {
  const toneClass =
    tone === "danger"
      ? "border-rose-300/70 bg-rose-50 text-rose-900 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-100"
      : tone === "success"
        ? "border-emerald-300/70 bg-emerald-50 text-emerald-900 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-100"
        : tone === "warning"
          ? "border-amber-300/70 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-100"
          : "border-(--line) bg-(--paper) text-(--ink)";

  return (
    <div className={`rounded-lg border px-3 py-3 ${toneClass}`}>
      <p className="text-xs font-semibold uppercase tracking-[0.14em] opacity-75">{label}</p>
      <p className="mt-2 text-lg font-semibold">{value}</p>
    </div>
  );
}

function DiffCard({
  change,
  changes,
  maintenance,
  currentStatus,
  diffFile,
  onSelectStatus,
}: {
  change: ProposalChange;
  changes: ProposalChange[];
  maintenance: boolean;
  currentStatus: ProposalChangeStatus;
  diffFile: FileData | null;
  onSelectStatus: (status: ProposalChangeStatus) => void;
}) {
  const showSpecificityWarning = hasOverSpecificAddWarning(change);
  const showDuplicateBoundaryWarning = hasDuplicateBoundaryWarning(change, changes);
  const showLostCoverageWarning = hasLostCoverageWarning(change);
  return (
    <div className="rounded-lg border border-(--line) bg-(--paper) p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="rounded-full border border-(--line) px-2 py-1 text-xs font-semibold uppercase tracking-[0.14em] text-muted">
              {sectionLabels[change.section]}
            </span>
            <span className="rounded-full border border-(--line) px-2 py-1 text-xs font-semibold uppercase tracking-[0.14em] text-muted">
              {operationLabels[change.operation]}
            </span>
            <span className="text-xs font-medium text-muted">{changeShapeLabel(change)}</span>
          </div>
          <p className="text-sm font-semibold text-(--ink)">{change.summary}</p>
          <p className="text-sm leading-6 text-(--body)">{change.rationale}</p>
          <p className="text-xs text-muted">
            {maintenance
              ? "Profile maintenance"
              : `Feedback ${change.source_feedback_ids.length} · Papers ${change.source_paper_ids.length}`}
          </p>
          {showSpecificityWarning ? (
            <div className="rounded-lg border border-amber-300/60 bg-amber-50 px-3 py-3 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950/35 dark:text-amber-100">
              This added rule may be too specific and should probably be merged into a higher-level
              reason-based rule.
            </div>
          ) : null}
          {showDuplicateBoundaryWarning ? (
            <div className="rounded-lg border border-amber-300/60 bg-amber-50 px-3 py-3 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950/35 dark:text-amber-100">
              This added rule may duplicate the boundary already covered by another merge or rewrite in
              this proposal.
            </div>
          ) : null}
          {showLostCoverageWarning ? (
            <div className="rounded-lg border border-amber-300/60 bg-amber-50 px-3 py-3 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950/35 dark:text-amber-100">
              This merge or rewrite may have dropped important existing boundary coverage from the rules
              it replaced.
            </div>
          ) : null}
        </div>
        <div className="flex flex-wrap gap-2">
          <SelectionButton active={currentStatus === "accepted"} onPress={() => onSelectStatus("accepted")}>
            Accept
          </SelectionButton>
          <SelectionButton active={currentStatus === "rejected"} onPress={() => onSelectStatus("rejected")}>
            Reject
          </SelectionButton>
          <SelectionButton
            active={currentStatus === "proposed" || currentStatus === "ignored"}
            onPress={() => onSelectStatus("proposed")}
          >
            Keep undecided
          </SelectionButton>
        </div>
      </div>

      <div className="proposal-diff-shell mt-4 overflow-hidden rounded-lg border border-(--line)">
        {diffFile ? (
          <Diff className="proposal-diff" diffType={diffFile.type} hunks={diffFile.hunks} viewType="unified">
            {(hunks) => hunks.map((hunk) => <Hunk key={hunk.content} hunk={hunk} />)}
          </Diff>
        ) : (
          <div className="px-4 py-4 text-sm text-muted">Could not render this diff preview.</div>
        )}
      </div>
    </div>
  );
}

export function ProfileProposalReview({
  onApplySelection,
  onRejectProposal,
  proposal,
}: {
  onApplySelection: (proposalId: number, selection: ChangeSelection) => Promise<void> | void;
  onRejectProposal: (proposalId: number) => Promise<void> | void;
  proposal: ProfileProposal;
}) {
  const [selection, setSelection] = React.useState<Record<string, ProposalChangeStatus>>({});
  const [busy, setBusy] = React.useState(false);

  React.useEffect(() => {
    const nextSelection: Record<string, ProposalChangeStatus> = {};
    proposal.changes.forEach((change) => {
      nextSelection[change.id] = change.status;
    });
    setSelection(nextSelection);
  }, [proposal]);

  const groupedChanges = React.useMemo(() => {
    const orderedSections: ProposalChangeSection[] = [
      "scope",
      "direct_rule",
      "indirect_rule",
      "unrelated_rule",
      "topic",
    ];
    return orderedSections
      .map((section) => ({
        section,
        items: proposal.changes.filter((change) => change.section === section),
      }))
      .filter((group) => group.items.length > 0);
  }, [proposal.changes]);

  const acceptedChangeIDs = React.useMemo(
    () =>
      proposal.changes
        .filter((change) => selection[change.id] === "accepted")
        .map((change) => change.id),
    [proposal.changes, selection],
  );
  const rejectedChangeIDs = React.useMemo(
    () =>
      proposal.changes
        .filter((change) => selection[change.id] === "rejected")
        .map((change) => change.id),
    [proposal.changes, selection],
  );
  const sectionSummaries = React.useMemo(
    () => collectSectionSummaries(proposal.changes),
    [proposal.changes],
  );
  const compactness = React.useMemo(
    () => collectCompactness(proposal.changes),
    [proposal.changes],
  );
  const maintenanceProposal = proposal.source_feedback_ids.length === 0;
  const diffFiles = React.useMemo(() => {
    const next = new Map<string, FileData | null>();
    proposal.changes.forEach((change) => {
      next.set(change.id, buildDiffFile(change));
    });
    return next;
  }, [proposal.changes]);
  const isAddOnlyProposal =
    proposal.changes.length > 0 && proposal.changes.every((change) => change.operation === "add");

  const setChangeStatus = (changeID: string, status: ProposalChangeStatus) => {
    setSelection((current) => ({...current, [changeID]: status}));
  };

  const applySelection = async () => {
    try {
      setBusy(true);
      await onApplySelection(proposal.id, {
        accepted_change_ids: acceptedChangeIDs,
        rejected_change_ids: rejectedChangeIDs,
      });
    } finally {
      setBusy(false);
    }
  };

  const rejectProposal = async () => {
    try {
      setBusy(true);
      await onRejectProposal(proposal.id);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-4">
      <Card className="border border-(--line) bg-(--paper-accent)">
        <Card.Header className="flex flex-wrap items-start justify-between gap-4">
          <div className="space-y-2">
            <p className="text-sm font-semibold text-(--ink)">
              {maintenanceProposal ? "Pending maintenance profile proposal" : "Pending compact profile proposal"}
            </p>
            <div className="space-y-1">
              <h2 className="text-2xl font-semibold text-(--ink)">
                {proposal.proposed_profile.meta.name} · v{proposal.proposed_profile.meta.version}
              </h2>
              <p className="text-sm text-muted">
                {proposal.created_at.slice(0, 10)} · {proposal.summary}
              </p>
              {maintenanceProposal ? (
                <p className="text-sm text-muted">
                  No new feedback was used for this run. This proposal is focused on profile cleanup and rule compaction.
                </p>
              ) : null}
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              isDisabled={busy || acceptedChangeIDs.length === 0}
              size="sm"
              onPress={() => void applySelection()}
            >
              Apply accepted changes
            </Button>
            <Button isDisabled={busy} size="sm" variant="ghost" onPress={() => void rejectProposal()}>
              Reject proposal
            </Button>
          </div>
        </Card.Header>
        <Card.Content className="space-y-4">
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5">
            <MetricCard label="Changes" value={String(proposal.changes.length)} />
            <MetricCard label="Rules added" tone="success" value={formatCount(compactness.added)} />
            <MetricCard label="Rules removed" tone="danger" value={formatCount(-compactness.removed)} />
            <MetricCard label="Rules merged" tone="warning" value={String(compactness.merged)} />
            <MetricCard
              label="Net rule delta"
              tone={compactness.net > 0 ? "warning" : compactness.net < 0 ? "success" : "default"}
              value={humanDelta(compactness.net)}
            />
          </div>

          <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-5">
            {sectionSummaries.map((summary) => (
              <div key={summary.section} className="rounded-lg border border-(--line) bg-(--paper) px-3 py-3">
                <p className="text-xs font-semibold uppercase tracking-[0.14em] text-muted">
                  {sectionLabels[summary.section]}
                </p>
                <p className="mt-2 text-sm text-(--body)">
                  +{summary.added} / -{summary.removed} / ~{summary.modified}
                </p>
              </div>
            ))}
          </div>

          <div className="flex flex-wrap gap-3 text-sm text-muted">
            <span>Accepted {acceptedChangeIDs.length}</span>
            <span>Rejected {rejectedChangeIDs.length}</span>
            <span>Undecided {proposal.changes.length - acceptedChangeIDs.length - rejectedChangeIDs.length}</span>
          </div>

          {isAddOnlyProposal ? (
            <div className="rounded-lg border border-amber-300/60 bg-amber-50 px-4 py-4 text-sm text-amber-950">
              This proposal only adds new rules. It does not show a real merge, rewrite, or removal, so it may be
              expanding the profile instead of simplifying it.
            </div>
          ) : null}
        </Card.Content>
      </Card>

      <Card className="border border-(--line) bg-(--paper-accent)">
        <Card.Header className="flex flex-col items-start gap-1">
          <p className="text-sm font-semibold text-(--ink)">Change queue</p>
          <p className="text-sm text-muted">
            Git-style diff view with change-level accept/reject controls.
          </p>
        </Card.Header>
        <Card.Content className="space-y-5">
          {groupedChanges.length === 0 ? (
            <div className="rounded-lg border border-amber-300/60 bg-amber-50 px-4 py-4 text-sm text-amber-950">
              This proposal was generated with the compact review flow, but it does not contain any actionable changes.
              Reject it and regenerate the proposal instead of applying it.
            </div>
          ) : (
            groupedChanges.map((group) => (
              <section key={group.section} className="space-y-3">
                <h3 className="text-sm font-semibold uppercase tracking-[0.12em] text-muted">
                  {sectionLabels[group.section]}
                </h3>
                <div className="space-y-3">
                  {group.items.map((change) => (
                    <DiffCard
                      key={change.id}
                      change={change}
                      changes={proposal.changes}
                      maintenance={maintenanceProposal}
                      currentStatus={selection[change.id] ?? "proposed"}
                      diffFile={diffFiles.get(change.id) ?? null}
                      onSelectStatus={(status) => setChangeStatus(change.id, status)}
                    />
                  ))}
                </div>
              </section>
            ))
          )}
        </Card.Content>
      </Card>
    </div>
  );
}
