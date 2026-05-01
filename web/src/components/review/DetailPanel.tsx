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
      <aside className="sticky top-4 rounded-lg border border-(--line) bg-white p-5 text-sm text-muted">
        No paper selected.
      </aside>
    );
  }
  const tone = relevanceTone[paper.classification.relevance];
  const feedbackText = feedbackLabel(paper);
  const zoteroSaved = paper.zotero_status?.saved ?? false;
  const hasAbstractHtml = Boolean(paper.abstract_html);
  const abstractImages = paper.abstract_images ?? [];
  const hasAbstractImages = abstractImages.length > 0;

  return (
    <aside className="sticky top-4 space-y-5 rounded-lg border border-(--line) bg-white p-5">
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
            <span className="text-sm text-muted">No keywords</span>
          )}
        </div>
      </section>

      <section className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-muted">
          Abstract
        </h3>
        {hasAbstractHtml ? (
          <div
            className="max-h-64 overflow-auto pr-1 text-sm leading-6 text-(--body) [&_a]:text-(--accent) [&_a]:underline [&_p+*]:mt-3 [&_ul]:list-disc [&_ul]:pl-5 [&_ol]:list-decimal [&_ol]:pl-5"
            dangerouslySetInnerHTML={{__html: paper.abstract_html ?? ""}}
          />
        ) : paper.abstract ? (
          <p className="max-h-64 overflow-auto pr-1 text-sm leading-6 text-(--body)">
            {paper.abstract}
          </p>
        ) : (
          <p className="text-sm text-muted">No abstract was available in the feed metadata.</p>
        )}
        {hasAbstractImages ? (
          <details className="rounded-md border border-(--line) p-3">
            <summary className="cursor-pointer text-sm font-medium text-(--ink)">
              Images in abstract ({abstractImages.length})
            </summary>
            <div className="mt-3 space-y-3">
              {abstractImages.map((image, index) => (
                <figure key={`${image.src}-${index}`} className="space-y-2">
                  <img
                    alt={image.alt ?? `Abstract image ${index + 1}`}
                    className="max-h-64 rounded-md border border-(--line) object-contain"
                    src={image.src}
                  />
                  {image.alt ? (
                    <figcaption className="text-xs leading-5 text-muted">{image.alt}</figcaption>
                  ) : null}
                </figure>
              ))}
            </div>
          </details>
        ) : null}
      </section>

      {paper.zotero_status?.last_error ? (
        <div className="rounded-md border border-rose-300 bg-rose-50 p-3 text-sm text-rose-800">
          {paper.zotero_status.last_error}
        </div>
      ) : null}

      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="primary" onPress={() => window.open(doiHref(paper), "_blank")}>
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
