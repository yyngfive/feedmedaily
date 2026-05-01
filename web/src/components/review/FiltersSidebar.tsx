import {Button} from "@heroui/react";

import {
  dateFilterOptions,
  readFilterOptions,
  relevanceOrder,
  relevanceTone,
} from "../../app/constants";
import type {DateFilter, ReadFilter} from "../../app/constants";
import {SelectField, type SelectOption} from "../common/SelectField";
import type {Relevance} from "../../types";

export function FiltersSidebar({
  dateFilter,
  journal,
  journalOptions,
  onDateFilterChange,
  onJournalChange,
  onReadFilterChange,
  onReset,
  onTopicChange,
  profileName,
  profileVersion,
  readFilter,
  reportDate,
  shownCount,
  topic,
  topicOptions,
  totalCount,
  visibleTotals,
}: {
  dateFilter: DateFilter;
  journal: string;
  journalOptions: SelectOption[];
  onDateFilterChange: (value: DateFilter) => void;
  onJournalChange: (value: string) => void;
  onReadFilterChange: (value: ReadFilter) => void;
  onReset: () => void;
  onTopicChange: (value: string) => void;
  profileName: string;
  profileVersion: number;
  readFilter: ReadFilter;
  reportDate: string;
  shownCount: number;
  topic: string;
  topicOptions: SelectOption[];
  totalCount: number;
  visibleTotals: Record<Relevance, number>;
}) {
  return (
    <aside className="h-full space-y-4 overflow-hidden rounded-lg border border-(--line) bg-(--paper-accent) p-4">
      <div>
        <p className="text-sm leading-6 text-muted">
          Last Update: {reportDate}
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
        <SelectField
          label="Journal"
          options={journalOptions}
          value={journal}
          onChange={onJournalChange}
        />
        <SelectField label="Topic" options={topicOptions} value={topic} onChange={onTopicChange} />
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
      </div>

      <Button fullWidth variant="outline" onPress={onReset}>
        Reset filters
      </Button>
    </aside>
  );
}
