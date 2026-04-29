export type Relevance = "direct" | "indirect" | "unrelated";

export type Classification = {
  relevance: Relevance;
  confidence: number;
  topic_tags: string[];
  reason: string;
  recommended_action: string;
  model: string;
  translated_title_zh?: string | null;
};

export type Paper = {
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

export type Report = {
  generated_at: string;
  report_date: string;
  totals: Record<string, number>;
  papers: Paper[];
  errors: string[];
};

export const EMPTY_REPORT: Report = {
  generated_at: new Date().toISOString(),
  report_date: new Date().toISOString().slice(0, 10),
  totals: { total: 0, direct: 0, indirect: 0, unrelated: 0 },
  papers: [],
  errors: [],
};

