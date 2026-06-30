import type {Relevance} from "../types";

export type RelevanceFilter = "all" | Relevance;
export type DateFilter = "all" | "today" | "7d" | "30d" | "180d";
export type ReadFilter = "unread" | "read" | "all";
export type FeedbackFilter = "all" | "marked" | "unmarked";
export type SortOption =
  | "date-desc"
  | "date-asc"
  | "journal-asc"
  | "confidence-desc"
  | "confidence-asc";

export const relevanceTabs: Array<{id: RelevanceFilter; label: string}> = [
  {id: "direct", label: "Direct"},
  {id: "indirect", label: "Indirect"},
  {id: "unrelated", label: "Unrelated"},
  {id: "all", label: "All"},
];

export const relevanceOrder: Relevance[] = ["direct", "indirect", "unrelated"];

export const relevanceLabel: Record<Relevance, string> = {
  direct: "Direct",
  indirect: "Indirect",
  unrelated: "Unrelated",
};

export const relevanceTone: Record<
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

export const dateFilterOptions = [
  {value: "all", label: "All dates"},
  {value: "today", label: "Today"},
  {value: "7d", label: "Last 7 days"},
  {value: "30d", label: "Last 30 days"},
  {value: "180d", label: "Last 6 months"},
] as const;

export const readFilterOptions = [
  {value: "unread", label: "Unread"},
  {value: "read", label: "Read"},
  {value: "all", label: "All"},
] as const;

export const feedbackFilterOptions = [
  {value: "all", label: "All"},
  {value: "marked", label: "Marked wrong"},
  {value: "unmarked", label: "Not marked wrong"},
] as const;

export const sortOptions = [
  {value: "date-desc", label: "Newest first"},
  {value: "date-asc", label: "Oldest first"},
  {value: "journal-asc", label: "Journal A-Z"},
  {value: "confidence-desc", label: "Confidence high-low"},
  {value: "confidence-asc", label: "Confidence low-high"},
] as const;
