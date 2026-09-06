import {Button} from "@heroui/react";

import {
  dateFilterOptions,
  feedbackFilterOptions,
  readFilterOptions,
  relevanceOrder,
  relevanceTone,
  sortOptions,
} from "../../app/constants";
import type {TopicFilterValue} from "../../app/utils";
import type {DateFilter, FeedbackFilter, ReadFilter, SortOption} from "../../app/constants";
import {CheckboxRow} from "../../shared/components/FormFields";
import {SelectField, type SelectOption} from "../../shared/components/SelectField";
import type {Relevance, ReportTopics} from "../../shared/types";

export function FiltersSidebar({
  dateFilter,
  journalOptions,
  selectedJournals,
  feedbackFilter,
  lastUpdateLabel,
  onDateFilterChange,
  onJournalToggle,
  onJournalClear,
  onFeedbackFilterChange,
  onReadFilterChange,
  onReset,
  onSortChange,
  onTopicFilterChange,
  profileName,
  profileVersion,
  readFilter,
  shownCount,
  sortOption,
  topicFilter,
  topics,
  totalCount,
  visibleTotals,
}: {
  dateFilter: DateFilter;
  journalOptions: SelectOption[];
  selectedJournals: string[];
  feedbackFilter: FeedbackFilter;
  lastUpdateLabel: string;
  onDateFilterChange: (value: DateFilter) => void;
  onJournalToggle: (value: string) => void;
  onJournalClear: () => void;
  onFeedbackFilterChange: (value: FeedbackFilter) => void;
  onReadFilterChange: (value: ReadFilter) => void;
  onReset: () => void;
  onSortChange: (value: SortOption) => void;
  onTopicFilterChange: (value: TopicFilterValue) => void;
  profileName: string;
  profileVersion: number;
  readFilter: ReadFilter;
  shownCount: number;
  sortOption: SortOption;
  topicFilter: TopicFilterValue;
  topics: ReportTopics | null | undefined;
  totalCount: number;
  visibleTotals: Record<Relevance, number>;
}) {
  const selectedJournalSet = new Set(selectedJournals);
  // 主题过滤选项：注册表主题 + 未归类（判定过无主题/孤儿）+ 未判定（从未跑过主题）。
  const topicOptions: Array<{value: TopicFilterValue; label: string; count: number}> = [
    {value: "unassigned", label: "Unassigned", count: topics?.unassigned ?? 0},
    {value: "unprocessed", label: "Not processed", count: topics?.unprocessed ?? 0},
    ...(topics?.items ?? []).map((topic) => ({
      value: topic.id as TopicFilterValue,
      label: topic.label,
      count: topics?.counts[topic.id] ?? 0,
    })),
  ];
  const activeTopicFilter = topicFilter !== "all";

  return (
    <aside className="h-full space-y-4 overflow-auto rounded-lg border border-(--line) bg-(--paper-accent) p-4">
      <div>
        <p className="text-sm leading-6 text-muted">
          Last Update: {lastUpdateLabel}
        </p>
        <p className="mt-1 text-sm leading-6 text-muted">
          {shownCount} shown, {totalCount} total
        </p>
        <p className="mt-1 text-sm leading-6 text-muted">
          Profile: {profileName} · v{profileVersion}
        </p>
      </div>

      <div className="grid grid-cols-3 gap-2 text-center">
        {relevanceOrder.map((item) => (
          <div key={item} className="rounded-md border border-(--line) p-2">
            <div className={`text-lg font-semibold ${relevanceTone[item].text}`}>
              {visibleTotals[item] ?? 0}
            </div>
            <div className="text-xs uppercase text-muted">{item}</div>
          </div>
        ))}
      </div>

      <div className="space-y-3">
        <section className="space-y-2">
          <div className="flex items-center justify-between gap-3">
            <h3 className="text-sm font-medium text-(--ink)">Topic</h3>
            {activeTopicFilter ? (
              <Button size="sm" variant="ghost" onPress={() => onTopicFilterChange("all")}>
                All
              </Button>
            ) : null}
          </div>
          <div className="max-h-44 space-y-1 overflow-auto rounded-md border border-(--line) bg-(--paper) p-2">
            {topicOptions.length === 0 ? (
              <p className="px-1 py-2 text-sm text-muted">No topics in the profile yet.</p>
            ) : (
              topicOptions.map((option) => (
                <button
                  key={option.value}
                  aria-pressed={topicFilter === option.value}
                  className={`flex w-full items-center justify-between gap-2 rounded px-1.5 py-1 text-left text-sm ${
                    topicFilter === option.value
                      ? "bg-(--paper-accent) font-medium text-(--ink)"
                      : "text-(--body) hover:bg-(--paper-accent)"
                  }`}
                  onClick={() => onTopicFilterChange(option.value)}
                  type="button"
                >
                  <span className="min-w-0 flex-1 truncate">{option.label}</span>
                  <span className="text-xs text-muted">{option.count}</span>
                </button>
              ))
            )}
          </div>
        </section>
        <section className="space-y-2">
          <div className="flex items-center justify-between gap-3">
            <h3 className="text-sm font-medium text-(--ink)">Journal</h3>
            <Button
              size="sm"
              variant="ghost"
              isDisabled={selectedJournals.length === 0}
              onPress={onJournalClear}
            >
              All
            </Button>
          </div>
          <div className="max-h-44 space-y-1 overflow-auto rounded-md border border-(--line) bg-(--paper) p-2">
            {journalOptions.length === 0 ? (
              <p className="px-1 py-2 text-sm text-muted">No journals yet.</p>
            ) : (
              journalOptions.map((option) => (
                <CheckboxRow
                  key={option.value}
                  checked={selectedJournalSet.has(option.value)}
                  className="rounded px-1.5 py-1 text-sm text-(--body) hover:bg-(--paper-accent)"
                  onChange={() => onJournalToggle(option.value)}
                >
                  <span className="min-w-0 flex-1 warp-break-words">{option.label}</span>
                </CheckboxRow>
              ))
            )}
          </div>
        </section>
        <SelectField
          label="Date"
          options={[...dateFilterOptions]}
          value={dateFilter}
          onChange={(value) => onDateFilterChange(value as DateFilter)}
        />
        <SelectField
          label="Read status"
          options={[...readFilterOptions]}
          value={readFilter}
          onChange={(value) => onReadFilterChange(value as ReadFilter)}
        />
        <SelectField
          label="Mark wrong"
          options={[...feedbackFilterOptions]}
          value={feedbackFilter}
          onChange={(value) => onFeedbackFilterChange(value as FeedbackFilter)}
        />
        <SelectField
          label="Sort"
          options={[...sortOptions]}
          value={sortOption}
          onChange={(value) => onSortChange(value as SortOption)}
        />
      </div>

      <Button fullWidth variant="outline" onPress={onReset}>
        Reset filters
      </Button>
    </aside>
  );
}
