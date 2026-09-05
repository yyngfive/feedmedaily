import React from "react";
import { Button } from "@heroui/react";
import { Virtuoso, type VirtuosoHandle } from "react-virtuoso";

import { relevanceTabs, type RelevanceFilter } from "../../app/constants";
import { EmptyStateCard } from "../../shared/components/EmptyStateCard";
import { TextInputField } from "../../shared/components/FormFields";
import { StatusBanner } from "../../shared/components/StatusBanner";
import { PaperCard } from "./PaperCard";
import type { Paper, Relevance } from "../../shared/types";

export function PaperListSection({
  loadError,
  hasNoFetchedPapers,
  loading,
  needsFeedSetup,
  markAllReadBusy,
  onOpenAdmin,
  onMarkAllRead,
  onMarkSelectedRangeRead,
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
  unreadSelectedRangeCount,
  unreadVisibleCount,
  visibleBaseCount,
  visibleTotals,
}: {
  loadError: string | null;
  hasNoFetchedPapers: boolean;
  loading: boolean;
  needsFeedSetup: boolean;
  markAllReadBusy: boolean;
  onOpenAdmin: () => void;
  onMarkAllRead: () => void;
  onMarkSelectedRangeRead: () => void;
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
  unreadSelectedRangeCount: number;
  unreadVisibleCount: number;
  visibleBaseCount: number;
  visibleTotals: Record<Relevance, number>;
}) {
  const virtuosoRef = React.useRef<VirtuosoHandle>(null);
  const lastScrolledSelectionRef = React.useRef<string | null>(null);

  React.useEffect(() => {
    if (selectedId == null || papers.length === 0) return;
    const selectedIndex = papers.findIndex((paper) => paper.id === selectedId);
    if (selectedIndex < 0) return;
    const selectionSignature = `${selectedId}:${selectedIndex}`;
    if (lastScrolledSelectionRef.current === selectionSignature) return;
    lastScrolledSelectionRef.current = selectionSignature;
    virtuosoRef.current?.scrollToIndex({
      align: "center",
      behavior: "smooth",
      index: selectedIndex,
    });
  }, [papers, selectedId]);

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
          <TextInputField
            clearable
            label="Search"
            placeholder="Search title, abstract, author, journal"
            value={query}
            onChange={setQuery}
          />

          <div className="flex flex-wrap justify-end gap-2">
            <Button
              size="sm"
              variant="outline"
              isDisabled={unreadSelectedRangeCount === 0 || markAllReadBusy}
              onPress={onMarkSelectedRangeRead}
            >
              {markAllReadBusy
                ? "Marking..."
                : `Mark above read (${unreadSelectedRangeCount})`}
            </Button>
            <Button
              size="sm"
              variant="outline"
              isDisabled={unreadVisibleCount === 0 || markAllReadBusy}
              onPress={onMarkAllRead}
            >
              {markAllReadBusy
                ? "Marking..."
                : `Mark all shown read (${unreadVisibleCount})`}
            </Button>
          </div>
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
      ) : loading ? (
        <EmptyStateCard
          eyebrow="Loading"
          title="Loading your paper list"
          body="The review list is being prepared. You can open Admin in the meantime, but the card list will appear as soon as the latest report finishes loading."
        />
      ) : loadError ? (
        <EmptyStateCard
          eyebrow="Local data"
          title="The paper list could not be loaded"
          body={loadError}
          actions={
            <Button size="sm" variant="outline" onPress={onOpenAdmin}>
              Open admin
            </Button>
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
            ref={virtuosoRef}
            className="h-full"
            computeItemKey={(index) => papers[index].id}
            increaseViewportBy={{ bottom: 480, top: 240 }}
            initialTopMostItemIndex={0}
            style={{ height: "100%" }}
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
