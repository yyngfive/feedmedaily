import {relevanceLabel} from "./constants";
import type {DateFilter} from "./constants";
import type {JobInfo, Paper, Relevance} from "../shared/types";

export function sentence(value?: string | null): string {
  if (!value) {
    return "No abstract text was available for this paper.";
  }
  const trimmed = value.replace(/\s+/g, " ").trim();
  const match = trimmed.match(/^(.+?[.!?])\s/);
  return match?.[1] ?? trimmed;
}

export function paperDate(paper: Paper): string {
  return paper.published_date ?? paper.seen_date;
}

export function authorsLine(paper: Paper): string {
  const authors = paper.authors ?? [];
  if (authors.length === 0) {
    return "Authors unavailable";
  }
  if (authors.length <= 3) {
    return authors.join(", ");
  }
  return `${authors.slice(0, 3).join(", ")} +${authors.length - 3}`;
}

// 详情面板的 Link 按钮：优先 DOI，缺失时回退出版社 URL，两者皆无返回空串。
export function paperLinkHref(paper: Paper): string {
  if (paper.doi) {
    return `https://doi.org/${paper.doi.replace(/^https?:\/\/doi.org\//i, "")}`;
  }
  return paper.url ?? "";
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

export function matchesDateFilter(
  value: string,
  reportDate: string,
  filter: DateFilter,
): boolean {
  if (filter === "all") {
    return true;
  }
  if (filter === "today") {
    return value === reportDate;
  }
  if (filter === "7d") {
    return isWithinDays(value, reportDate, 7);
  }
  if (filter === "30d") {
    return isWithinDays(value, reportDate, 30);
  }
  return isWithinDays(value, reportDate, 183);
}

export function relevanceCounts(papers: Paper[]): Record<Relevance, number> {
  return papers.reduce<Record<Relevance, number>>(
    (counts, paper) => {
      counts[paper.classification.relevance] += 1;
      return counts;
    },
    {direct: 0, indirect: 0, unrelated: 0},
  );
}

export function feedbackLabel(paper: Paper): string | null {
  if (!paper.feedback_status?.has_feedback || !paper.feedback_status.corrected_relevance) {
    return null;
  }
  return `Feedback -> ${relevanceLabel[paper.feedback_status.corrected_relevance]}`;
}

export function statusMessage(job: JobInfo): string {
  if (job.error) {
    return job.error;
  }
  if (job.message) {
    return job.message;
  }
  return job.status;
}
