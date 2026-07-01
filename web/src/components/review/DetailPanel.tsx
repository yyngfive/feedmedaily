import {Button} from "@heroui/react";

import {authorsLine, doiHref, feedbackLabel, paperDate} from "../../app/utils";
import type {Paper} from "../../types";

function abstractHtmlForDisplay(paper: Paper): string {
  const images = new Set((paper.abstract_images ?? []).map((image) => image.src));
  return (paper.abstract_html ?? "").replace(
    /(<img\b[^>]*\bsrc=["'])([^"']+)(["'][^>]*>)/gi,
    (match, prefix: string, src: string, suffix: string) => {
      if (!images.has(src)) {
        return match;
      }
      return `${prefix}/api/papers/${paper.id}/abstract-image?src=${encodeURIComponent(src)}${suffix}`;
    },
  );
}

export function DetailPanel({
  isUnread,
  markReadBusy,
  onMarkRead,
  onMarkWrong,
  onSave,
  paper,
}: {
  isUnread: boolean;
  markReadBusy: boolean;
  onMarkRead: () => void;
  onMarkWrong: () => void;
  onSave: () => void;
  paper: Paper | null;
}) {
  if (!paper) {
    return (
      <aside className="h-full overflow-hidden rounded-lg border border-(--line) bg-(--paper-accent) p-5 text-sm text-muted">
        No paper selected.
      </aside>
    );
  }
  const feedbackText = feedbackLabel(paper);
  const zoteroSaved = paper.zotero_status?.saved ?? false;
  const hasAbstractHtml = Boolean(paper.abstract_html);
  const abstractHtml = hasAbstractHtml ? abstractHtmlForDisplay(paper) : "";

  return (
    <aside className="h-full space-y-5 overflow-hidden rounded-lg border border-(--line) bg-(--paper-accent) p-5">
      <div className="space-y-3">
        <h2 className="text-xl font-semibold leading-7 text-(--ink)">{paper.title}</h2>
        <p className="text-base leading-6 text-muted">{paper.journal || "Unknown journal"}</p>
        <p className="text-sm leading-6 text-muted">
          {paperDate(paper)} · {authorsLine(paper)}
        </p>
      </div>

      {paper.feedback_status?.note ? (
        <section className="space-y-2">
          <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-muted">
            Feedback note
          </h3>
          <p className="text-sm leading-6 text-(--body)">{paper.feedback_status.note}</p>
        </section>
      ) : null}

      <section className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-muted">
          Abstract
        </h3>
        {hasAbstractHtml ? (
          <div
            className="max-h-64 overflow-auto pr-1 text-sm leading-6 text-(--body) [&_a]:text-(--accent) [&_a]:underline [&_p+*]:mt-3 [&_ul]:list-disc [&_ul]:pl-5 [&_ol]:list-decimal [&_ol]:pl-5"
            dangerouslySetInnerHTML={{__html: abstractHtml}}
          />
        ) : paper.abstract ? (
          <p className="max-h-64 overflow-auto pr-1 text-sm leading-6 text-(--body)">
            {paper.abstract}
          </p>
        ) : (
          <p className="text-sm text-muted">No abstract is available for this paper.</p>
        )}
      </section>

      {paper.zotero_status?.last_error ? (
        <div className="rounded-md border border-[var(--danger-line)] bg-[var(--danger-bg)] p-3 text-sm text-[var(--danger-ink)]">
          {paper.zotero_status.last_error}
        </div>
      ) : null}

      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="primary" onPress={() => window.open(doiHref(paper), "_blank")}>
          DOI link
        </Button>
        <Button
          size="sm"
          isDisabled={!isUnread || markReadBusy}
          variant={isUnread ? "secondary" : "outline"}
          onPress={onMarkRead}
        >
          {markReadBusy ? "Marking..." : isUnread ? "Mark as read" : "Read"}
        </Button>
        <Button size="sm" variant={zoteroSaved ? "tertiary" : "tertiary"} onPress={onSave}>
          {zoteroSaved ? "Saved" : "Save to Zotero"}
        </Button>
        <Button
          size="sm"
          variant={feedbackText ? "danger-soft" : "tertiary"}
          onPress={onMarkWrong}
        >
          Mark wrong
        </Button>
      </div>
    </aside>
  );
}
