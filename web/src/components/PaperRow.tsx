import { tagLabel } from "../reportData";
import type { Paper } from "../types";

export function PaperRow({ paper }: { paper: Paper }) {
  const relevance = paper.classification.relevance;
  const sectionTone =
    relevance === "direct"
      ? "text-emerald-800"
      : relevance === "indirect"
        ? "text-amber-800"
        : "text-slate-500";

  return (
    <article className="border-b border-[var(--line)] py-6">
      <div className="mb-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] uppercase tracking-[0.16em] text-[var(--muted)]">
        <span className={`font-semibold ${sectionTone}`}>{relevance}</span>
        <span>{paper.journal || "Unknown journal"}</span>
        <span>{paper.published_date || paper.seen_date}</span>
        {paper.doi ? <span>{paper.doi}</span> : null}
      </div>
      <div className="grid gap-3 lg:grid-cols-[1fr_220px] lg:items-start">
        <div className="min-w-0">
          <a
            className="block font-serif text-[1.34rem] leading-8 text-[var(--ink)] transition-colors hover:text-[var(--accent)]"
            href={paper.url}
          >
            {paper.title}
          </a>
          {paper.classification.translated_title_zh ? (
            <p className="mt-2 font-serif text-base leading-7 text-[var(--subtle-ink)]">
              {paper.classification.translated_title_zh}
            </p>
          ) : null}
          <p className="mt-3 max-w-4xl text-sm leading-7 text-[var(--body)]">
            {paper.classification.reason}
          </p>
          <div className="mt-3 flex flex-wrap gap-2">
            {paper.classification.topic_tags.map((tag) => (
              <span
                key={tag}
                className={
                  tag === "proximity_labeling"
                    ? "border border-fuchsia-300 bg-fuchsia-50 px-2 py-1 text-[11px] uppercase tracking-[0.12em] text-fuchsia-900"
                    : "border border-[var(--line)] bg-white px-2 py-1 text-[11px] uppercase tracking-[0.12em] text-[var(--muted)]"
                }
              >
                {tagLabel(tag)}
              </span>
            ))}
          </div>
          {paper.abstract ? (
            <details className="mt-4">
              <summary className="cursor-pointer text-sm font-medium text-[var(--accent)]">
                Abstract
              </summary>
              <p className="mt-3 max-w-4xl text-sm leading-7 text-[var(--body)]">{paper.abstract}</p>
            </details>
          ) : null}
        </div>
        <div className="border-l border-[var(--line)] pl-4 text-sm text-[var(--muted)]">
          <div className="text-xs uppercase tracking-[0.16em]">Confidence</div>
          <div className="mt-1 font-serif text-3xl text-[var(--ink)]">
            {Math.round(paper.classification.confidence * 100)}
          </div>
          <div className="mt-4 text-xs uppercase tracking-[0.16em]">Model</div>
          <div className="mt-1 break-words text-sm text-[var(--body)]">
            {paper.classification.model}
          </div>
        </div>
      </div>
    </article>
  );
}

