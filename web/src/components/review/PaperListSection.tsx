import {Button} from "@heroui/react";
import {Virtuoso} from "react-virtuoso";

import {relevanceTabs, type RelevanceFilter} from "../../app/constants";
import {EmptyStateCard} from "../common/EmptyStateCard";
import {StatusBanner} from "../common/StatusBanner";
import {PaperCard} from "./PaperCard";
import type {Paper, Relevance} from "../../types";

export function PaperListSection({
  hasNoFetchedPapers,
  needsFeedSetup,
  onOpenAdmin,
  onResetFilters,
  onRunSync,
  onSelectPaper,
  onStartFeedSetup,
  papers,
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
  needsFeedSetup: boolean;
  onOpenAdmin: () => void;
  onResetFilters: () => void;
  onRunSync: () => void;
  onSelectPaper: (paper: Paper) => void;
  onStartFeedSetup: () => void;
  papers: Paper[];
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
    <section className="flex h-full min-w-0 min-h-0 flex-col gap-4">
      <div className="flex-none rounded-lg border border-(--line) bg-(--paper-accent) p-4">
        {reportErrors.length ? (
          <StatusBanner className="mb-4" tone="danger">
            {reportErrors.map((item) => (
              <div key={item}>{item}</div>
            ))}
          </StatusBanner>
        ) : null}

        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-2">
            {relevanceTabs.map((tab) => (
              <Button
                key={tab.id}
                size="sm"
                variant={
                  relevance === tab.id
                    ? "secondary"
                    : tab.id === "all"
                      ? "ghost"
                      : "outline"
                }
                onPress={() => setRelevance(tab.id)}
              >
                {tab.label}
                {tab.id !== "all" ? ` (${visibleTotals[tab.id] ?? 0})` : ` (${visibleBaseCount})`}
              </Button>
            ))}
          </div>

          <label className="block w-full">
            <span className="text-sm font-medium text-(--ink)">Search</span>
            <input
              className="mt-2 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink) placeholder:text-muted"
              placeholder="Search title, abstract, author, journal"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </label>
        </div>
      </div>

      {needsFeedSetup ? (
        <EmptyStateCard
          eyebrow="Feed setup"
          title="Add RSS feeds before reviewing papers"
          body="No RSS feed subscriptions are saved yet. Open Admin, add one or more journal feeds, save them, and then run a sync manually or wait for your scheduled job."
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
          body="Save is complete. You can run a sync right now, or let your scheduled task populate the library automatically."
          actions={
            <>
              <Button size="sm" variant="secondary" onPress={onRunSync}>
                Sync now
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
          body="Try broadening the journal, date, or read-status filters to bring more papers back into view."
          actions={
            <Button size="sm" variant="outline" onPress={onResetFilters}>
              Reset filters
            </Button>
          }
        />
      ) : (
        <div className="min-h-0 flex-1 overflow-hidden px-1 py-1">
          <Virtuoso
            className="h-full"
            computeItemKey={(index) => papers[index].id}
            increaseViewportBy={{bottom: 480, top: 240}}
            initialTopMostItemIndex={0}
            style={{height: "100%"}}
            totalCount={papers.length}
            itemContent={(index) => {
              const paper = papers[index];
              return (
                <div className="px-1 py-1.5">
                  <PaperCard
                    paper={paper}
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
