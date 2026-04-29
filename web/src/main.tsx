import React from "react";
import ReactDOM from "react-dom/client";
import "./styles.css";

type Relevance = "direct" | "indirect" | "unrelated";

type Classification = {
  relevance: Relevance;
  confidence: number;
  topic_tags: string[];
  reason: string;
  recommended_action: string;
  model: string;
};

type Paper = {
  id: number;
  title: string;
  url: string;
  doi?: string | null;
  journal?: string | null;
  abstract?: string | null;
  published_date?: string | null;
  seen_date: string;
  classification: Classification;
};

type Report = {
  generated_at: string;
  report_date: string;
  totals: Record<string, number>;
  papers: Paper[];
  errors: string[];
};

const EMPTY_REPORT: Report = {
  generated_at: new Date().toISOString(),
  report_date: new Date().toISOString().slice(0, 10),
  totals: { total: 0, direct: 0, indirect: 0, unrelated: 0 },
  papers: [],
  errors: []
};

function reportDataUrl() {
  return new URL("../data/latest.json", window.location.href).toString();
}

function tagLabel(tag: string) {
  return tag.replaceAll("_", " ");
}

function PaperCard({ paper }: { paper: Paper }) {
  const relevance = paper.classification.relevance;
  const accent =
    relevance === "direct"
      ? "border-l-emerald-500"
      : relevance === "indirect"
        ? "border-l-amber-500"
        : "border-l-slate-300";
  return (
    <article className={`border-l-4 ${accent} bg-white p-4 shadow-sm ring-1 ring-slate-200`}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <a className="block text-base font-semibold leading-6 text-slate-950 hover:text-sky-700" href={paper.url}>
            {paper.title}
          </a>
          <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-slate-500">
            <span>{paper.journal || "Unknown journal"}</span>
            <span>{paper.published_date || paper.seen_date}</span>
            {paper.doi ? <span>DOI: {paper.doi}</span> : null}
            <span>model: {paper.classification.model}</span>
          </div>
        </div>
        <span className="rounded-sm bg-slate-100 px-2 py-1 text-xs font-medium text-slate-700">
          {Math.round(paper.classification.confidence * 100)}%
        </span>
      </div>
      <div className="mt-3 flex flex-wrap gap-2">
        {paper.classification.topic_tags.map((tag) => (
          <span
            key={tag}
            className={
              tag === "proximity_labeling"
                ? "rounded-sm bg-fuchsia-100 px-2 py-1 text-xs font-medium text-fuchsia-800"
                : "rounded-sm bg-sky-100 px-2 py-1 text-xs font-medium text-sky-800"
            }
          >
            {tagLabel(tag)}
          </span>
        ))}
      </div>
      <p className="mt-3 text-sm leading-6 text-slate-700">{paper.classification.reason}</p>
      {paper.abstract ? (
        <details className="mt-3">
          <summary className="cursor-pointer text-sm font-medium text-slate-700">Abstract</summary>
          <p className="mt-2 text-sm leading-6 text-slate-600">{paper.abstract}</p>
        </details>
      ) : null}
    </article>
  );
}

function Section({ title, papers }: { title: string; papers: Paper[] }) {
  const collapsed = title === "Unrelated";
  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-slate-950">{title}</h2>
        <span className="text-sm text-slate-500">{papers.length}</span>
      </div>
      {collapsed ? (
        <details className="bg-white p-4 ring-1 ring-slate-200">
          <summary className="cursor-pointer text-sm font-medium text-slate-700">Show unrelated papers</summary>
          <div className="mt-4 space-y-3">{papers.map((paper) => <PaperCard key={paper.id} paper={paper} />)}</div>
        </details>
      ) : (
        <div className="space-y-3">{papers.map((paper) => <PaperCard key={paper.id} paper={paper} />)}</div>
      )}
    </section>
  );
}

function App() {
  const [report, setReport] = React.useState<Report>(EMPTY_REPORT);
  const [loadError, setLoadError] = React.useState<string | null>(null);
  const [query, setQuery] = React.useState("");
  const [tag, setTag] = React.useState("all");
  const [journal, setJournal] = React.useState("all");

  React.useEffect(() => {
    fetch(reportDataUrl())
      .then((response) => {
        if (!response.ok) throw new Error(`Could not load report JSON (${response.status})`);
        return response.json();
      })
      .then(setReport)
      .catch((error: Error) => setLoadError(error.message));
  }, []);

  const tags = Array.from(new Set(report.papers.flatMap((paper) => paper.classification.topic_tags))).sort();
  const journals = Array.from(new Set(report.papers.map((paper) => paper.journal).filter(Boolean) as string[])).sort();
  const filtered = report.papers.filter((paper) => {
    const haystack = `${paper.title} ${paper.abstract ?? ""} ${paper.journal ?? ""}`.toLowerCase();
    const matchesQuery = !query || haystack.includes(query.toLowerCase());
    const matchesTag = tag === "all" || paper.classification.topic_tags.includes(tag);
    const matchesJournal = journal === "all" || paper.journal === journal;
    return matchesQuery && matchesTag && matchesJournal;
  });
  const grouped: Record<Relevance, Paper[]> = {
    direct: filtered.filter((paper) => paper.classification.relevance === "direct"),
    indirect: filtered.filter((paper) => paper.classification.relevance === "indirect"),
    unrelated: filtered.filter((paper) => paper.classification.relevance === "unrelated")
  };

  return (
    <main className="min-h-screen bg-slate-50 text-slate-900">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto max-w-6xl px-4 py-5">
          <div className="flex flex-wrap items-end justify-between gap-4">
            <div>
              <h1 className="text-2xl font-semibold tracking-normal text-slate-950">SciRSSAgent</h1>
              <p className="mt-1 text-sm text-slate-600">
                Report date {report.report_date}. Generated {new Date(report.generated_at).toLocaleString()}.
              </p>
            </div>
            <div className="grid grid-cols-4 gap-2 text-center text-sm">
              {(["total", "direct", "indirect", "unrelated"] as const).map((key) => (
                <div key={key} className="bg-slate-100 px-3 py-2">
                  <div className="font-semibold text-slate-950">{report.totals[key] ?? 0}</div>
                  <div className="text-xs capitalize text-slate-500">{key}</div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </header>
      <div className="mx-auto max-w-6xl space-y-6 px-4 py-6">
        {loadError ? (
          <div className="bg-amber-50 p-4 text-sm text-amber-900 ring-1 ring-amber-200">
            {loadError}. Run the backend first to create reports/data/latest.json.
          </div>
        ) : null}
        {report.errors.length ? (
          <div className="bg-red-50 p-4 text-sm text-red-900 ring-1 ring-red-200">
            {report.errors.map((error) => (
              <div key={error}>{error}</div>
            ))}
          </div>
        ) : null}
        <div className="grid gap-3 bg-white p-4 ring-1 ring-slate-200 md:grid-cols-[1fr_220px_220px]">
          <input
            className="min-h-10 border border-slate-300 px-3 text-sm outline-none focus:border-sky-500"
            placeholder="Search title, abstract, journal"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          <select
            className="min-h-10 border border-slate-300 px-3 text-sm outline-none focus:border-sky-500"
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
            className="min-h-10 border border-slate-300 px-3 text-sm outline-none focus:border-sky-500"
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
          <div className="bg-white p-8 text-center text-sm text-slate-500 ring-1 ring-slate-200">No papers match.</div>
        ) : (
          <>
            <Section title="Direct" papers={grouped.direct} />
            <Section title="Indirect" papers={grouped.indirect} />
            <Section title="Unrelated" papers={grouped.unrelated} />
          </>
        )}
      </div>
    </main>
  );
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);

