import React from "react";

import { reportDataUrl, tagLabel, loadEmbeddedReport } from "./reportData";
import type { Paper, Report, Relevance } from "./types";
import { EMPTY_REPORT } from "./types";
import { Section } from "./components/Section";

function groupedPapers(papers: Paper[]): Record<Relevance, Paper[]> {
  return {
    direct: papers.filter((paper) => paper.classification.relevance === "direct"),
    indirect: papers.filter((paper) => paper.classification.relevance === "indirect"),
    unrelated: papers.filter((paper) => paper.classification.relevance === "unrelated"),
  };
}

export function App() {
  const [report, setReport] = React.useState<Report>(() => loadEmbeddedReport() ?? EMPTY_REPORT);
  const [loadError, setLoadError] = React.useState<string | null>(null);
  const [query, setQuery] = React.useState("");
  const [tag, setTag] = React.useState("all");
  const [journal, setJournal] = React.useState("all");
  const deferredQuery = React.useDeferredValue(query);

  React.useEffect(() => {
    const embedded = loadEmbeddedReport();
    if (embedded) {
      return;
    }
    fetch(reportDataUrl())
      .then((response) => {
        if (!response.ok) {
          throw new Error(`Could not load report data (${response.status})`);
        }
        return response.text();
      })
      .then((source) => {
        const nextWindow = window as Window & { __SCIRSS_REPORT__?: Report };
        const executor = new Function(source);
        executor();
        if (!nextWindow.__SCIRSS_REPORT__) {
          throw new Error("Report script did not define window.__SCIRSS_REPORT__");
        }
        React.startTransition(() => setReport(nextWindow.__SCIRSS_REPORT__ as Report));
      })
      .catch((error: Error) => setLoadError(error.message));
  }, []);

  const tags = Array.from(
    new Set(report.papers.flatMap((paper) => paper.classification.topic_tags))
  ).sort();
  const journals = Array.from(
    new Set(report.papers.map((paper) => paper.journal).filter(Boolean) as string[])
  ).sort();

  const filtered = report.papers.filter((paper) => {
    const haystack = [
      paper.title,
      paper.classification.translated_title_zh ?? "",
      paper.abstract ?? "",
      paper.journal ?? "",
    ]
      .join(" ")
      .toLowerCase();
    const matchesQuery = !deferredQuery || haystack.includes(deferredQuery.toLowerCase());
    const matchesTag = tag === "all" || paper.classification.topic_tags.includes(tag);
    const matchesJournal = journal === "all" || paper.journal === journal;
    return matchesQuery && matchesTag && matchesJournal;
  });

  const grouped = groupedPapers(filtered);

  return (
    <main className="min-h-screen bg-[var(--paper)] text-[var(--ink)]">
      <header className="border-b border-[var(--line-strong)] bg-[var(--paper-accent)]">
        <div className="mx-auto max-w-6xl px-4 py-8">
          <div className="border-y border-[var(--line-strong)] py-6">
            <p className="text-xs uppercase tracking-[0.24em] text-[var(--muted)]">
              Literature monitor
            </p>
            <div className="mt-4 flex flex-wrap items-end justify-between gap-6">
              <div>
                <h1 className="font-serif text-5xl leading-none text-[var(--ink)]">SciRSSAgent</h1>
                <p className="mt-3 max-w-2xl text-sm leading-7 text-[var(--body)]">
                  A daily reading list shaped around chemical biology, nucleic acid chemistry,
                  directed evolution, and proximity-labeling methods.
                </p>
              </div>
              <div className="grid grid-cols-4 gap-3 text-center text-sm">
                {(["total", "direct", "indirect", "unrelated"] as const).map((key) => (
                  <div key={key} className="min-w-20 border border-[var(--line)] bg-white px-3 py-3">
                    <div className="font-serif text-2xl text-[var(--ink)]">{report.totals[key] ?? 0}</div>
                    <div className="mt-1 text-[11px] uppercase tracking-[0.16em] text-[var(--muted)]">
                      {key}
                    </div>
                  </div>
                ))}
              </div>
            </div>
            <p className="mt-4 text-xs uppercase tracking-[0.16em] text-[var(--muted)]">
              Report date {report.report_date} · Generated{" "}
              {new Date(report.generated_at).toLocaleString()}
            </p>
          </div>
        </div>
      </header>

      <div className="mx-auto max-w-6xl space-y-8 px-4 py-8">
        {loadError ? (
          <div className="border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900">
            {loadError}. Rebuild the web app and rerun the backend to refresh the embedded report
            data.
          </div>
        ) : null}

        {report.errors.length ? (
          <div className="border border-rose-300 bg-rose-50 p-4 text-sm text-rose-900">
            {report.errors.map((error) => (
              <div key={error}>{error}</div>
            ))}
          </div>
        ) : null}

        <div className="grid gap-3 border border-[var(--line)] bg-white p-4 md:grid-cols-[1fr_220px_220px]">
          <input
            className="min-h-11 border border-[var(--line)] bg-[var(--paper)] px-3 text-sm text-[var(--ink)] outline-none focus:border-[var(--accent)]"
            placeholder="Search title, Chinese title, abstract, journal"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          <select
            className="min-h-11 border border-[var(--line)] bg-[var(--paper)] px-3 text-sm text-[var(--ink)] outline-none focus:border-[var(--accent)]"
            value={tag}
            onChange={(event) => setTag(event.target.value)}
          >
            <option value="all">All tags</option>
            {tags.map((item) => (
              <option key={item} value={item}>
                {tagLabel(item)}
              </option>
            ))}
          </select>
          <select
            className="min-h-11 border border-[var(--line)] bg-[var(--paper)] px-3 text-sm text-[var(--ink)] outline-none focus:border-[var(--accent)]"
            value={journal}
            onChange={(event) => setJournal(event.target.value)}
          >
            <option value="all">All journals</option>
            {journals.map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </select>
        </div>

        {filtered.length === 0 ? (
          <div className="border border-[var(--line)] bg-white p-10 text-center text-sm text-[var(--muted)]">
            No papers match the current filters.
          </div>
        ) : (
          <>
            <Section title="Directly related" papers={grouped.direct} />
            <Section title="Indirectly related" papers={grouped.indirect} />
            <Section title="Unrelated" papers={grouped.unrelated} />
          </>
        )}
      </div>
    </main>
  );
}

