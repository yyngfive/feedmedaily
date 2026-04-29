import type { Paper } from "../types";
import { PaperRow } from "./PaperRow";

export function Section({ title, papers }: { title: string; papers: Paper[] }) {
  const collapsed = title === "Unrelated";

  return (
    <section className="space-y-4">
      <div className="flex items-end justify-between border-b border-[var(--line-strong)] pb-3">
        <h2 className="font-serif text-3xl text-[var(--ink)]">{title}</h2>
        <span className="text-xs uppercase tracking-[0.18em] text-[var(--muted)]">
          {papers.length} papers
        </span>
      </div>
      {collapsed ? (
        <details className="py-2">
          <summary className="cursor-pointer text-sm font-medium text-[var(--accent)]">
            Show unrelated papers
          </summary>
          <div className="mt-3">
            {papers.map((paper) => (
              <PaperRow key={paper.id} paper={paper} />
            ))}
          </div>
        </details>
      ) : (
        <div>
          {papers.map((paper) => (
            <PaperRow key={paper.id} paper={paper} />
          ))}
        </div>
      )}
    </section>
  );
}

