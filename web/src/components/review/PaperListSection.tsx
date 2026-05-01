import {Button} from "@heroui/react";
import {Virtuoso} from "react-virtuoso";

import {relevanceTabs, type RelevanceFilter} from "../../app/constants";
import {EmptyStateCard} from "../common/EmptyStateCard";
import {StatusBanner} from "../common/StatusBanner";
import {PaperCard} from "./PaperCard";
import type {ClassificationProfile, Paper, Relevance} from "../../types";

export function PaperListSection({
  hasNoFetchedPapers,
  loadError,
  needsFeedSetup,
  notice,
  onOpenAdmin,
  onResetFilters,
  onRunFetchAndClassify,
  onSelectPaper,
  onStartFeedSetup,
  papers,
  profile,
  query,
  reportErrors,
  relevance,
  selectedId,
  setQuery,
  setRelevance,
  visibleBaseCount,
  visibleTotals,
}: {
  hasNoFetchedPapers: boolean;
  loadError: string | null;
  needsFeedSetup: boolean;
  notice: string | null;
  onOpenAdmin: () => void;
  onResetFilters: () => void;
  onRunFetchAndClassify: () => void;
  onSelectPaper: (paper: Paper) => void;
  onStartFeedSetup: () => void;
  papers: Paper[];
  profile: ClassificationProfile | null;
  query: string;
  reportErrors: string[];
  relevance: RelevanceFilter;
  selectedId: number | null;
  setQuery: (value: string) => void;
  setRelevance: (value: RelevanceFilter) => void;
  visibleBaseCount: number;
  visibleTotals: Record<Relevance, number>;
}) {
  return (
    <section className="min-w-0 space-y-4">
      <div className="rounded-lg border border-(--line) bg-white p-4">
        {loadError ? <StatusBanner className="mb-4" tone="warning">{loadError}</StatusBanner> : null}
        {notice ? <StatusBanner className="mb-4" tone="success">{notice}</StatusBanner> : null}
        {reportErrors.length ? (
          <StatusBanner className="mb-4" tone="danger">
            {reportErrors.map((item) => (
              <div key={item}>{item}</div>
            ))}
          </StatusBanner>
        ) : null}

        <div className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
          <label className="block w-full xl:max-w-xl">
            <span className="text-sm font-medium text-(--ink)">Search</span>
            <input
              className="mt-2 w-full rounded-md border border-(--line) px-3 py-2 text-sm"
              placeholder="Search title, abstract, author, journal"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </label>

          <div className="flex flex-col gap-3 xl:items-end">
            <Button size="sm" variant="secondary" onPress={onOpenAdmin}>
              Open admin
            </Button>
            <div className="flex flex-wrap gap-2">
              {relevanceTabs.map((tab) => (
                <Button
                  key={tab.id}
                  size="sm"
                  variant={relevance === tab.id ? "secondary" : "outline"}
                  onPress={() => setRelevance(tab.id)}
                >
                  {tab.label}
                </Button>
              ))}
            </div>
          </div>
        </div>
      </div>

      {needsFeedSetup ? (
        <EmptyStateCard
          eyebrow="Feed setup"
          title="Add RSS feeds before reviewing papers"
          body="No RSS feed subscriptions are saved yet. Open Admin, add one or more journal feeds, save them, and then run fetch manually or wait for your scheduled job."
          actions={
            <>
              <Button size="sm" variant="secondary" onPress={onStartFeedSetup}>
                Add first feed
              </Button>
              <Button size="sm" variant="outline" onPress={onOpenAdmin}>
                Open admin
              </Button>
            </>
          }
        />
      ) : hasNoFetchedPapers ? (
        <EmptyStateCard
          eyebrow="Waiting for fetch"
          title="Feeds are ready, but no papers have been fetched yet"
          body="Save is complete. You can run a manual fetch right now, or let your scheduled task populate the library automatically."
          actions={
            <>
              <Button size="sm" variant="secondary" onPress={onRunFetchAndClassify}>
                Run fetch + classify
              </Button>
              <Button size="sm" variant="outline" onPress={onOpenAdmin}>
                Open admin
              </Button>
            </>
          }
        />
      ) : papers.length === 0 ? (
        <EmptyStateCard
          eyebrow="No results"
          title="No papers match the current filters"
          body="Try broadening the journal, topic, date, or read-status filters to bring more papers back into view."
          actions={
            <Button size="sm" variant="outline" onPress={onResetFilters}>
              Reset filters
            </Button>
          }
        />
      ) : (
        <div className="overflow-hidden px-1 py-1">
          <Virtuoso
            className="min-h-105"
            computeItemKey={(index) => papers[index].id}
            increaseViewportBy={{bottom: 480, top: 240}}
            initialTopMostItemIndex={0}
            style={{height: "calc(100vh - 13rem)"}}
            totalCount={papers.length}
            itemContent={(index) => {
              const paper = papers[index];
              return (
                <div className="px-1 py-1.5">
                  <PaperCard
                    paper={paper}
                    profile={profile}
                    isSelected={paper.id === selectedId}
                    isUnread={!paper.read_at}
                    onSelect={() => onSelectPaper(paper)}
                  />
                </div>
              );
            }}
          />
        </div>
      )}
    </section>
  );
}
