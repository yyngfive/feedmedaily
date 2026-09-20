import type {Paper} from "../../shared/types";

// Compatibility fallback for older report fixtures; all alias resolution lives in Go.
export function journalDisplay(paper: Pick<Paper, "journal" | "journal_display" | "feed_title">): string {
  return paper.journal_display || paper.journal || paper.feed_title || "Unknown journal";
}
export function journalKey(paper: Pick<Paper, "journal" | "journal_display" | "journal_key" | "feed_title">): string {
  return paper.journal_key || journalDisplay(paper).trim().toLowerCase().replace(/\s+/g, " ");
}
export function toggleJournalSelection(values: string[], value: string): string[] {
  return values.includes(value) ? values.filter((item) => item !== value) : [...values, value];
}
