export type Relevance = "direct" | "indirect" | "unrelated";
export type FeedbackState = "open" | "used";
export type ProfileProposalState = "pending" | "applied" | "rejected";
export type ZoteroSaveState = "pending" | "saved" | "error";
export type FeedSubscription = {
  client_id?: string;
  journal: string;
  url: string;
};

export type SettingsConfigSource =
  | "dotenv"
  | "environment"
  | "default"
  | "unset"
  | "settings"
  | "secret_store";

export type SettingOption = {
  value: string;
  label: string;
};

export type SettingsConfigField = {
  key: string;
  label: string;
  description: string;
  section: string;
  input_type: "text" | "password" | "url" | "number" | "select";
  secret: boolean;
  configured: boolean;
  source: SettingsConfigSource;
  stored_in_dotenv: boolean;
  storage_label?: string | null;
  value?: string | null;
  default_value?: string | null;
  options: SettingOption[];
};

export type SettingsConfigResponse = {
  fields: SettingsConfigField[];
};

export type AppMeta = {
  name: string;
  version: string;
  mode: string;
  install_dir: string;
  config_dir?: string;
  data_dir: string;
  logs_dir: string;
  static_dir: string;
  tray_settings_path?: string | null;
  server_url?: string | null;
  scheduler_task_name: string;
  process_running: boolean;
};

export type AppHealth = {
  status: string;
  name: string;
  version: string;
  mode: string;
  server_url?: string | null;
};

export type AppUpdate = {
  status: string;
  current_version: string;
  latest_version?: string | null;
  has_update: boolean;
  download_url?: string | null;
  release_notes_url?: string | null;
  detail?: string | null;
  checked_at: string;
};

export type AppControlTarget =
  | "data_dir"
  | "logs_dir"
  | "install_dir"
  | "server_url"
  | "download_url"
  | "release_notes_url";

export type AppControlResponse = {
  ok: boolean;
  action: string;
  target?: string | null;
  detail?: string | null;
};

export type SchedulerSettings = {
  installed: boolean;
  task_name: string;
  mode: string;
  scheduler_backend?: string | null;
  platform?: string | null;
  automatic_supported?: boolean;
  advisory?: string | null;
  scheduled_time?: string | null;
  settings_path?: string | null;
  state?: string | null;
  next_run_time?: string | null;
  last_run_time?: string | null;
  last_result?: number | null;
  command?: string | null;
};

export type AbstractImage = {
  src: string;
  alt?: string | null;
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

export type ZoteroCollectionOption = {
  key: string;
  name: string;
  path_label: string;
  parent_key?: string | null;
  is_default: boolean;
};

export type ZoteroCollectionsResponse = {
  collections: ZoteroCollectionOption[];
  default_collection_key?: string | null;
};

export type Paper = {
  id: number;
  title: string;
  url: string;
  doi?: string | null;
  journal?: string | null;
  feed_title?: string | null;
  authors?: string[];
  abstract?: string | null;
  abstract_html?: string | null;
  abstract_images: AbstractImage[];
  published_date?: string | null;
  seen_date: string;
  read_at?: string | null;
  classification: Classification;
  feedback_status?: FeedbackStatus | null;
  zotero_status?: ZoteroStatus | null;
};

export type Report = {
  generated_at: string;
  last_updated_at?: string | null;
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
};

export type ProfileProposalDelta = {
  summary: string;
  direct_rule_additions: string[];
  indirect_rule_additions: string[];
  unrelated_rule_additions: string[];
  scope_rewrite?: string | null;
  tag_additions: TopicDefinition[];
  tag_removals: string[];
};

export type ProposalChangeSection =
  | "scope"
  | "direct_rule"
  | "indirect_rule"
  | "unrelated_rule"
  | "topic";

export type ProposalChangeOperation = "add" | "remove" | "rewrite" | "merge";
export type ProposalChangeStatus = "proposed" | "accepted" | "rejected" | "ignored";

export type ProposalChange = {
  id: string;
  section: ProposalChangeSection;
  operation: ProposalChangeOperation;
  summary: string;
  text_before: string[];
  text_after: string[];
  topic_before: TopicDefinition[];
  topic_after: TopicDefinition[];
  rationale: string;
  source_feedback_ids: number[];
  source_paper_ids: number[];
  status: ProposalChangeStatus;
};

export type CurrentProfileResponse = {
  profile: ClassificationProfile | null;
};

export type ProfileProposal = {
  id: number;
  summary: string;
  base_profile_version: number;
  proposed_profile: ClassificationProfile;
  applied_profile?: ClassificationProfile | null;
  changes: ProposalChange[];
  rule_delta: ProfileProposalDelta;
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
  message_key?: string | null;
  message?: string | null;
  error?: string | null;
  progress_stage?: string | null;
  progress_current?: number | null;
  progress_total?: number | null;
  progress_percent?: number | null;
  progress_label?: string | null;
  progress_mode?: "item" | "step" | null;
  verification_required?: boolean;
  verification_target?: string | null;
  verification_feed_url?: string | null;
  verification_journal?: string | null;
  verification_host?: string | null;
  verification_method?: "native_webview2" | "browser_manual" | null;
  verification_session_state?: "verified" | "needs_reverify" | null;
  log_path?: string | null;
  warning_count?: number;
  result?: Record<string, unknown> | null;
  created_at: string;
  started_at?: string | null;
  finished_at?: string | null;
};

export type SettingsConfigUpdate = {
  value?: string | null;
  clear?: boolean;
};

export const EMPTY_REPORT: Report = {
  generated_at: new Date().toISOString(),
  last_updated_at: null,
  report_date: new Date().toISOString().slice(0, 10),
  totals: {total: 0, direct: 0, indirect: 0, unrelated: 0},
  papers: [],
  errors: [],
};
