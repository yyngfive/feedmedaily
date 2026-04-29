import type {Key} from "@heroui/react";
import {
  Button,
  Card,
  Chip,
  Label,
  ListBox,
  SearchField,
  Select,
  Tabs,
} from "@heroui/react";
import React from "react";

import {loadEmbeddedReport, reportDataUrl, tagLabel} from "./reportData";
import type {Paper, Relevance, Report} from "./types";
import {EMPTY_REPORT} from "./types";

type RelevanceFilter = "all" | Relevance;
type DateFilter = "all" | "report" | "7d" | "30d";

const relevanceTabs: Array<{id: RelevanceFilter; label: string}> = [
  {id: "all", label: "All"},
  {id: "direct", label: "Direct"},
  {id: "indirect", label: "Indirect"},
  {id: "unrelated", label: "Unrelated"},
];

const relevanceLabel: Record<Relevance, string> = {
  direct: "Direct",
  indirect: "Indirect",
  unrelated: "Unrelated",
};

const relevanceTone: Record<
  Relevance,
  {chip: "success" | "warning" | "default"; ring: string; text: string}
> = {
  direct: {
    chip: "success",
    ring: "border-l-[var(--direct)]",
    text: "text-[var(--direct)]",
  },
  indirect: {
    chip: "warning",
    ring: "border-l-[var(--indirect)]",
    text: "text-[var(--indirect)]",
  },
  unrelated: {
    chip: "default",
    ring: "border-l-[var(--unrelated)]",
    text: "text-[var(--unrelated)]",
  },
};

function sentence(value?: string | null): string {
  if (!value) {
    return "No abstract text was available for this paper.";
  }
  const trimmed = value.replace(/\s+/g, " ").trim();
  const match = trimmed.match(/^(.+?[.!?])\s/);
  return match?.[1] ?? trimmed;
}

function paperDate(paper: Paper): string {
  return paper.published_date ?? paper.seen_date;
}

function authorsLine(paper: Paper): string {
  const authors = paper.authors ?? [];
  if (authors.length === 0) {
    return "Authors unavailable";
  }
  if (authors.length <= 3) {
    return authors.join(", ");
  }
  return `${authors.slice(0, 3).join(", ")} +${authors.length - 3}`;
}

function doiHref(paper: Paper): string {
  if (paper.doi) {
    return `https://doi.org/${paper.doi.replace(/^https?:\/\/doi.org\//i, "")}`;
  }
  return paper.url;
}

function isWithinDays(value: string, reportDate: string, days: number): boolean {
  const current = new Date(`${value}T00:00:00`);
  const report = new Date(`${reportDate}T00:00:00`);
  if (Number.isNaN(current.getTime()) || Number.isNaN(report.getTime())) {
    return false;
  }
  const diff = report.getTime() - current.getTime();
  return diff >= 0 && diff <= days * 24 * 60 * 60 * 1000;
}

function FilterSelect({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: Array<{id: string; label: string}>;
  onChange: (value: string) => void;
}) {
  return (
    <Select
      fullWidth
      placeholder={label}
      value={value}
      variant="secondary"
      onChange={(key) => onChange(String(key ?? "all"))}
    >
      <Label>{label}</Label>
      <Select.Trigger>
        <Select.Value />
        <Select.Indicator />
      </Select.Trigger>
      <Select.Popover>
        <ListBox>
          {options.map((option) => (
            <ListBox.Item key={option.id} id={option.id} textValue={option.label}>
              {option.label}
              <ListBox.ItemIndicator />
            </ListBox.Item>
          ))}
        </ListBox>
      </Select.Popover>
    </Select>
  );
}

function PaperCard({
  paper,
  isSelected,
  isUnread,
  isSaved,
  isWrong,
  onSelect,
  onSave,
  onMarkWrong,
}: {
  paper: Paper;
  isSelected: boolean;
  isUnread: boolean;
  isSaved: boolean;
  isWrong: boolean;
  onSelect: () => void;
  onSave: () => void;
  onMarkWrong: () => void;
}) {
  const tone = relevanceTone[paper.classification.relevance];

  return (
    <Card
      className={`border-l-4 ${tone.ring} ${isSelected ? "outline outline-2 outline-[var(--accent)]" : ""}`}
    >
      <button className="block w-full text-left" type="button" onClick={onSelect}>
        <Card.Header className="gap-3">
          <div className="flex flex-1 flex-wrap items-center gap-2">
            {isUnread ? (
              <span
                aria-label="Unread"
                className="size-2 rounded-full bg-[var(--unread)]"
                title="Unread"
              />
            ) : null}
            <Chip color={tone.chip} size="sm" variant="soft">
              {relevanceLabel[paper.classification.relevance]}
            </Chip>
            {paper.classification.topic_tags.slice(0, 2).map((tag) => (
              <Chip key={tag} size="sm" variant="secondary">
                {tagLabel(tag)}
              </Chip>
            ))}
            {isWrong ? (
              <Chip color="danger" size="sm" variant="soft">
                Marked wrong
              </Chip>
            ) : null}
          </div>
          <span className={`text-sm font-semibold ${tone.text}`}>
            {Math.round(paper.classification.confidence * 100)}%
          </span>
        </Card.Header>
        <Card.Content className="gap-3">
          <div>
            <Card.Title className="line-clamp-2 text-lg leading-6">{paper.title}</Card.Title>
            {paper.classification.translated_title_zh ? (
              <Card.Description className="mt-1 line-clamp-2 text-sm">
                {paper.classification.translated_title_zh}
              </Card.Description>
            ) : null}
          </div>
          <p className="text-sm text-[var(--muted)]">
            {paper.journal || "Unknown journal"} · {paperDate(paper)} · {authorsLine(paper)}
          </p>
          <p className="line-clamp-2 text-sm leading-6 text-[var(--body)]">
            {sentence(paper.abstract ?? paper.classification.reason)}
          </p>
          <p className="line-clamp-2 text-sm leading-6 text-[var(--body)]">
            <span className="font-semibold text-[var(--ink)]">Why relevant:</span>{" "}
            {paper.classification.reason}
          </p>
        </Card.Content>
      </button>
      <Card.Footer className="flex flex-wrap gap-2">
        <Button size="sm" variant="tertiary" onPress={() => window.open(doiHref(paper), "_blank")}>
          DOI
        </Button>
        <Button size="sm" variant="tertiary" onPress={onSelect}>
          Abstract
        </Button>
        <Button size="sm" variant={isSaved ? "secondary" : "tertiary"} onPress={onSave}>
          {isSaved ? "Saved" : "Save to Zotero"}
        </Button>
        <Button size="sm" variant={isWrong ? "danger-soft" : "ghost"} onPress={onMarkWrong}>
          Mark wrong
        </Button>
      </Card.Footer>
    </Card>
  );
}

function DetailPanel({
  paper,
  isSaved,
  isWrong,
  onSave,
  onMarkWrong,
}: {
  paper: Paper | null;
  isSaved: boolean;
  isWrong: boolean;
  onSave: () => void;
  onMarkWrong: () => void;
}) {
  if (!paper) {
    return (
      <aside className="sticky top-4 rounded-lg border border-[var(--line)] bg-white p-5 text-sm text-[var(--muted)]">
        No paper selected.
      </aside>
    );
  }

  const tone = relevanceTone[paper.classification.relevance];

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

      <section className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
          Keywords
        </h3>
        <div className="flex flex-wrap gap-2">
          {paper.classification.topic_tags.length ? (
            paper.classification.topic_tags.map((tag) => (
              <Chip key={tag} size="sm" variant="secondary">
                {tagLabel(tag)}
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

      <div className="flex flex-wrap gap-2">
        <Button size="sm" onPress={() => window.open(doiHref(paper), "_blank")}>
          DOI link
        </Button>
        <Button size="sm" variant={isSaved ? "secondary" : "tertiary"} onPress={onSave}>
          {isSaved ? "Saved" : "Save to Zotero"}
        </Button>
        <Button size="sm" variant={isWrong ? "danger-soft" : "ghost"} onPress={onMarkWrong}>
          Mark wrong
        </Button>
      </div>
    </aside>
  );
}

export function App() {
  const [report, setReport] = React.useState<Report>(() => loadEmbeddedReport() ?? EMPTY_REPORT);
  const [loadError, setLoadError] = React.useState<string | null>(null);
  const [query, setQuery] = React.useState("");
  const [relevance, setRelevance] = React.useState<RelevanceFilter>("all");
  const [topic, setTopic] = React.useState("all");
  const [journal, setJournal] = React.useState("all");
  const [dateFilter, setDateFilter] = React.useState<DateFilter>("all");
  const [selectedId, setSelectedId] = React.useState<number | null>(null);
  const [readIds, setReadIds] = React.useState<Set<number>>(() => new Set());
  const [savedIds, setSavedIds] = React.useState<Set<number>>(() => new Set());
  const [wrongIds, setWrongIds] = React.useState<Set<number>>(() => new Set());
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
        const nextWindow = window as Window & {__SCIRSS_REPORT__?: Report};
        const executor = new Function(source);
        executor();
        if (!nextWindow.__SCIRSS_REPORT__) {
          throw new Error("Report script did not define window.__SCIRSS_REPORT__");
        }
        React.startTransition(() => setReport(nextWindow.__SCIRSS_REPORT__ as Report));
      })
      .catch((error: Error) => setLoadError(error.message));
  }, []);

  const tags = React.useMemo(
    () =>
      Array.from(
        new Set(report.papers.flatMap((paper) => paper.classification.topic_tags)),
      ).sort(),
    [report.papers],
  );
  const journals = React.useMemo(
    () =>
      Array.from(
        new Set(report.papers.map((paper) => paper.journal).filter(Boolean) as string[]),
      ).sort(),
    [report.papers],
  );

  const filtered = React.useMemo(
    () =>
      report.papers.filter((paper) => {
        const haystack = [
          paper.title,
          paper.classification.translated_title_zh ?? "",
          paper.abstract ?? "",
          paper.journal ?? "",
          paper.authors?.join(" ") ?? "",
          paper.classification.topic_tags.join(" "),
        ]
          .join(" ")
          .toLowerCase();
        const matchesQuery = !deferredQuery || haystack.includes(deferredQuery.toLowerCase());
        const matchesRelevance = relevance === "all" || paper.classification.relevance === relevance;
        const matchesTopic = topic === "all" || paper.classification.topic_tags.includes(topic);
        const matchesJournal = journal === "all" || paper.journal === journal;
        const dateValue = paperDate(paper);
        const matchesDate =
          dateFilter === "all" ||
          (dateFilter === "report" && dateValue === report.report_date) ||
          (dateFilter === "7d" && isWithinDays(dateValue, report.report_date, 7)) ||
          (dateFilter === "30d" && isWithinDays(dateValue, report.report_date, 30));
        return matchesQuery && matchesRelevance && matchesTopic && matchesJournal && matchesDate;
      }),
    [dateFilter, deferredQuery, journal, relevance, report.papers, report.report_date, topic],
  );

  React.useEffect(() => {
    if (filtered.length === 0) {
      setSelectedId(null);
      return;
    }
    if (!selectedId || !filtered.some((paper) => paper.id === selectedId)) {
      setSelectedId(filtered[0].id);
    }
  }, [filtered, selectedId]);

  const selectedPaper = filtered.find((paper) => paper.id === selectedId) ?? null;

  const selectPaper = (paper: Paper) => {
    setSelectedId(paper.id);
    setReadIds((current) => new Set(current).add(paper.id));
  };

  const toggleSet = (setter: React.Dispatch<React.SetStateAction<Set<number>>>, id: number) => {
    setter((current) => {
      const next = new Set(current);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const selectOptions = {
    journal: [
      {id: "all", label: "All journals"},
      ...journals.map((item) => ({id: item, label: item})),
    ],
    topic: [
      {id: "all", label: "All topics"},
      ...tags.map((item) => ({id: item, label: tagLabel(item)})),
    ],
    date: [
      {id: "all", label: "All dates"},
      {id: "report", label: `Report date (${report.report_date})`},
      {id: "7d", label: "Last 7 days"},
      {id: "30d", label: "Last 30 days"},
    ],
  };

  return (
    <main className="min-h-screen bg-[var(--paper)] text-[var(--ink)]">
      <div className="mx-auto grid max-w-[1500px] gap-4 px-4 py-4 lg:grid-cols-[260px_minmax(0,1fr)_360px]">
        <aside className="space-y-4 rounded-lg border border-[var(--line)] bg-white p-4 lg:sticky lg:top-4 lg:h-[calc(100vh-2rem)] lg:overflow-auto">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--muted)]">
              SciRSSAgent
            </p>
            <h1 className="mt-2 text-2xl font-semibold">Paper review</h1>
            <p className="mt-2 text-sm leading-6 text-[var(--muted)]">
              {report.report_date} · {filtered.length}/{report.totals.total ?? report.papers.length} papers
            </p>
          </div>
          <div className="grid grid-cols-3 gap-2 text-center">
            {(["direct", "indirect", "unrelated"] as Relevance[]).map((key) => (
              <div key={key} className="rounded-md border border-[var(--line)] p-2">
                <div className={`text-lg font-semibold ${relevanceTone[key].text}`}>
                  {report.totals[key] ?? 0}
                </div>
                <div className="text-[11px] uppercase text-[var(--muted)]">{key}</div>
              </div>
            ))}
          </div>
          <div className="space-y-4">
            <FilterSelect
              label="Journal"
              options={selectOptions.journal}
              value={journal}
              onChange={setJournal}
            />
            <FilterSelect
              label="Topic"
              options={selectOptions.topic}
              value={topic}
              onChange={setTopic}
            />
            <FilterSelect
              label="Date"
              options={selectOptions.date}
              value={dateFilter}
              onChange={(value) => setDateFilter(value as DateFilter)}
            />
          </div>
          <Button
            fullWidth
            variant="tertiary"
            onPress={() => {
              setTopic("all");
              setJournal("all");
              setDateFilter("all");
              setRelevance("all");
              setQuery("");
            }}
          >
            Reset filters
          </Button>
        </aside>

        <section className="min-w-0 space-y-4">
          <div className="rounded-lg border border-[var(--line)] bg-white p-4">
            {loadError ? (
              <div className="mb-4 rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900">
                {loadError}. Rebuild the web app and rerun the backend to refresh the embedded report data.
              </div>
            ) : null}
            {report.errors.length ? (
              <div className="mb-4 rounded-md border border-rose-300 bg-rose-50 p-3 text-sm text-rose-900">
                {report.errors.map((error) => (
                  <div key={error}>{error}</div>
                ))}
              </div>
            ) : null}
            <div className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
              <SearchField
                className="w-full xl:max-w-xl"
                fullWidth
                name="paper-search"
                value={query}
                variant="secondary"
                onChange={setQuery}
              >
                <Label>Search</Label>
                <SearchField.Group>
                  <SearchField.SearchIcon />
                  <SearchField.Input placeholder="Search title, abstract, author, journal" />
                  <SearchField.ClearButton />
                </SearchField.Group>
              </SearchField>

              <Tabs
                selectedKey={relevance}
                variant="secondary"
                onSelectionChange={(key: Key) => setRelevance(String(key) as RelevanceFilter)}
              >
                <Tabs.ListContainer>
                  <Tabs.List aria-label="Relevance">
                    {relevanceTabs.map((tab, index) => (
                      <Tabs.Tab key={tab.id} id={tab.id}>
                        {index > 0 ? <Tabs.Separator /> : null}
                        {tab.label}
                        <Tabs.Indicator />
                      </Tabs.Tab>
                    ))}
                  </Tabs.List>
                </Tabs.ListContainer>
                {relevanceTabs.map((tab) => (
                  <Tabs.Panel key={`${tab.id}-panel`} className="hidden" id={tab.id} />
                ))}
              </Tabs>
            </div>
          </div>

          {filtered.length === 0 ? (
            <Card className="p-8 text-center text-sm text-[var(--muted)]">
              No papers match the current filters.
            </Card>
          ) : (
            <div className="space-y-3">
              {filtered.map((paper) => (
                <PaperCard
                  key={paper.id}
                  isSaved={savedIds.has(paper.id)}
                  isSelected={paper.id === selectedId}
                  isUnread={!readIds.has(paper.id)}
                  isWrong={wrongIds.has(paper.id)}
                  paper={paper}
                  onMarkWrong={() => toggleSet(setWrongIds, paper.id)}
                  onSave={() => toggleSet(setSavedIds, paper.id)}
                  onSelect={() => selectPaper(paper)}
                />
              ))}
            </div>
          )}
        </section>

        <DetailPanel
          isSaved={selectedPaper ? savedIds.has(selectedPaper.id) : false}
          isWrong={selectedPaper ? wrongIds.has(selectedPaper.id) : false}
          paper={selectedPaper}
          onMarkWrong={() => selectedPaper && toggleSet(setWrongIds, selectedPaper.id)}
          onSave={() => selectedPaper && toggleSet(setSavedIds, selectedPaper.id)}
        />
      </div>
    </main>
  );
}
