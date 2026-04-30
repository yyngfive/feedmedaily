export type Relevance = "direct" | "indirect" | "unrelated";
export type FeedbackState = "open" | "used";
export type ProfileProposalState = "pending" | "applied" | "rejected";
export type ZoteroSaveState = "pending" | "saved" | "error";
export type FeedSubscription = {
  journal: string;
  url: string;
};

export type PaperReadStatus = {
  paper_id: number;
  read_at: string;
};

export type Classification = {
  relevance: Relevance;
  confidence: number;
  topic_tags: string[];
  reason: string;
  recommended_action: string;
  model: string;
  translated_title_zh?: string | null;
};

export type FeedbackStatus = {
  has_feedback: boolean;
  corrected_relevance?: Relevance | null;
  note?: string | null;
  latest_feedback_at?: string | null;
  state?: FeedbackState | null;
  used_in_profile?: boolean;
};

export type ZoteroStatus = {
  state?: ZoteroSaveState | null;
  saved: boolean;
  item_key?: string | null;
  last_error?: string | null;
  attempted_at?: string | null;
  saved_at?: string | null;
};

export type Paper = {
  id: number;
  title: string;
  url: string;
  doi?: string | null;
  journal?: string | null;
  authors?: string[];
  abstract?: string | null;
  published_date?: string | null;
  seen_date: string;
  read_at?: string | null;
  classification: Classification;
  feedback_status?: FeedbackStatus | null;
  zotero_status?: ZoteroStatus | null;
};

export type Report = {
  generated_at: string;
  report_date: string;
  totals: Record<string, number>;
  papers: Paper[];
  errors: string[];
};

export type FeedbackRecord = {
  id: number;
  paper_id: number;
  paper_title: string;
  original_relevance: Relevance;
  corrected_relevance: Relevance;
  note?: string | null;
  state: FeedbackState;
  used_in_profile: boolean;
  created_at: string;
};

export type ProfileMeta = {
  name: string;
  version: number;
  created_at: string;
  updated_at: string;
  source_description: string;
};

export type TopicDefinition = {
  id: string;
  label: string;
  description: string;
  examples: string[];
};

export type ProfileFewShot = {
  title: string;
  relevance: Relevance;
  tags: string[];
  rationale: string;
};

export type RelevanceRules = {
  direct: string[];
  indirect: string[];
  unrelated: string[];
};

export type ClassificationProfile = {
  meta: ProfileMeta;
  scope: string;
  relevance_rules: RelevanceRules;
  topic_taxonomy: TopicDefinition[];
  few_shots: ProfileFewShot[];
  classification_notes: string[];
};

export type CurrentProfileResponse = {
  profile: ClassificationProfile | null;
};

export type ProfileProposal = {
  id: number;
  summary: string;
  proposed_profile: ClassificationProfile;
  source_feedback_ids: number[];
  model: string;
  state: ProfileProposalState;
  created_at: string;
  applied_at?: string | null;
  rejected_at?: string | null;
  applied_version?: number | null;
};

export type JobInfo = {
  id: string;
  job_type: string;
  status: string;
  message?: string | null;
  error?: string | null;
  result?: Record<string, unknown> | null;
  created_at: string;
  started_at?: string | null;
  finished_at?: string | null;
};

export const EMPTY_REPORT: Report = {
  generated_at: new Date().toISOString(),
  report_date: new Date().toISOString().slice(0, 10),
  totals: {total: 0, direct: 0, indirect: 0, unrelated: 0},
  papers: [],
  errors: [],
};
