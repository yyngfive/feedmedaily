import {Button, Chip} from "@heroui/react";

import {relevanceLabel, relevanceTone} from "../../app/constants";
import {authorsLine, doiHref, feedbackLabel, paperDate, sentence} from "../../app/utils";
import {tagLabel} from "../../reportData";
import type {ClassificationProfile, Paper} from "../../types";

export function DetailPanel({
  isUnread,
  onMarkRead,
  onMarkWrong,
  onSave,
  paper,
  profile,
}: {
  isUnread: boolean;
  onMarkRead: () => void;
  onMarkWrong: () => void;
  onSave: () => void;
  paper: Paper | null;
  profile: ClassificationProfile | null;
}) {
  if (!paper) {
    return (
      <aside className="sticky top-4 rounded-lg border border-[var(--line)] bg-white p-5 text-sm text-[var(--muted)]">
        No paper selected.
      </aside>
    );
  }
  const tone = relevanceTone[paper.classification.relevance];
  const feedbackText = feedbackLabel(paper);
  const zoteroSaved = paper.zotero_status?.saved ?? false;

  return (
    <aside className="sticky top-4 space-y-5 rounded-lg border border-[var(--line)] bg-white p-5">
      <div className="space-y-3">
        <div className="flex flex-wrap gap-2">
          <Chip color={tone.chip} size="sm" variant="soft">
            {relevanceLabel[paper.classification.relevance]}
          </Chip>
          <Chip size="sm" variant="secondary">
            {Math.round(paper.classification.confidence * 100)}% confidence
          </Chip>
          {feedbackText ? (
            <Chip color="danger" size="sm" variant="soft">
              {feedbackText}
            </Chip>
          ) : null}
        </div>
        <h2 className="text-xl font-semibold leading-7 text-[var(--ink)]">{paper.title}</h2>
        <p className="text-sm leading-6 text-[var(--muted)]">
          {paper.journal || "Unknown journal"} · {paperDate(paper)} · {authorsLine(paper)}
        </p>
      </div>

      <section className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
          Summary
        </h3>
        <p className="text-sm leading-6 text-[var(--body)]">
          {sentence(paper.abstract ?? paper.classification.reason)}
        </p>
      </section>

      <section className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
          LLM rationale
        </h3>
        <p className="text-sm leading-6 text-[var(--body)]">{paper.classification.reason}</p>
      </section>

      {paper.feedback_status?.note ? (
        <section className="space-y-2">
          <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
            Feedback note
          </h3>
          <p className="text-sm leading-6 text-[var(--body)]">{paper.feedback_status.note}</p>
        </section>
      ) : null}

      <section className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
          Keywords
        </h3>
        <div className="flex flex-wrap gap-2">
          {paper.classification.topic_tags.length ? (
            paper.classification.topic_tags.map((tag) => (
              <Chip key={tag} size="sm" variant="secondary">
                {tagLabel(tag, profile)}
              </Chip>
            ))
          ) : (
            <span className="text-sm text-[var(--muted)]">No keywords</span>
          )}
        </div>
      </section>

      <section className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
          Abstract
        </h3>
        <p className="max-h-64 overflow-auto pr-1 text-sm leading-6 text-[var(--body)]">
          {paper.abstract || "No abstract was available in the feed metadata."}
        </p>
      </section>

      {paper.zotero_status?.last_error ? (
        <div className="rounded-md border border-rose-300 bg-rose-50 p-3 text-sm text-rose-800">
          {paper.zotero_status.last_error}
        </div>
      ) : null}

      <div className="flex flex-wrap gap-2">
        <Button size="sm" onPress={() => window.open(doiHref(paper), "_blank")}>
          DOI link
        </Button>
        <Button
          size="sm"
          isDisabled={!isUnread}
          variant={isUnread ? "secondary" : "outline"}
          onPress={onMarkRead}
        >
          {isUnread ? "Mark as read" : "Read"}
        </Button>
        <Button size="sm" variant={zoteroSaved ? "secondary" : "tertiary"} onPress={onSave}>
          {zoteroSaved ? "Saved" : "Save to Zotero"}
        </Button>
        <Button
          size="sm"
          variant={feedbackText ? "danger-soft" : "ghost"}
          onPress={onMarkWrong}
        >
          Mark wrong
        </Button>
      </div>
    </aside>
  );
}
