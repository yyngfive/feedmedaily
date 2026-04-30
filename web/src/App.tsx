import {Button, Card, Chip} from "@heroui/react";
import React from "react";
import {Virtuoso} from "react-virtuoso";

import {
  applyProfileProposal,
  bootstrapProfile,
  createFeedback,
  deleteFeedback,
  fetchFeedSubscriptions,
  fetchCurrentProfile,
  fetchFeedback,
  fetchJob,
  fetchLatestReport,
  fetchProfileProposals,
  fetchZoteroCollections,
  launchAdminJob,
  launchProfileProposalGeneration,
  launchReclassifyJob,
  loadEmbeddedReport,
  markPaperRead,
  rejectProfileProposal,
  saveFeedSubscriptions,
  saveToZotero,
  tagLabel,
} from "./reportData";
import type {
  ClassificationProfile,
  FeedSubscription,
  FeedbackRecord,
  JobInfo,
  Paper,
  ProfileProposal,
  Relevance,
  Report,
  ZoteroCollectionOption,
} from "./types";
import {EMPTY_REPORT} from "./types";

type RelevanceFilter = "all" | Relevance;
type DateFilter = "all" | "today" | "7d" | "30d" | "180d";
type ReadFilter = "unread" | "read" | "all";

const relevanceTabs: Array<{id: RelevanceFilter; label: string}> = [
  {id: "all", label: "All"},
  {id: "direct", label: "Direct"},
  {id: "indirect", label: "Indirect"},
  {id: "unrelated", label: "Unrelated"},
];

const relevanceLabel: Record<Relevance, string> = {
  direct: "Direct",
  indirect: "Indirect",
  unrelated: "Unrelated",
};

const relevanceTone: Record<
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

// 提取一段适合卡片和详情展示的简短摘要首句。
function sentence(value?: string | null): string {
  if (!value) {
    return "No abstract text was available for this paper.";
  }
  const trimmed = value.replace(/\s+/g, " ").trim();
  const match = trimmed.match(/^(.+?[.!?])\s/);
  return match?.[1] ?? trimmed;
}

// 统一返回文献用于筛选和展示的主日期，优先使用发表日期。
function paperDate(paper: Paper): string {
  return paper.published_date ?? paper.seen_date;
}

// 将作者列表压缩成适合窄卡片展示的一行文本。
function authorsLine(paper: Paper): string {
  const authors = paper.authors ?? [];
  if (authors.length === 0) {
    return "Authors unavailable";
  }
  if (authors.length <= 3) {
    return authors.join(", ");
  }
  return `${authors.slice(0, 3).join(", ")} +${authors.length - 3}`;
}

// 将 DOI 或原始 URL 规范成可直接打开的外链地址。
function doiHref(paper: Paper): string {
  if (paper.doi) {
    return `https://doi.org/${paper.doi.replace(/^https?:\/\/doi.org\//i, "")}`;
  }
  return paper.url;
}

// 判断某个日期是否落在相对报告日期的最近 N 天窗口内。
function isWithinDays(value: string, reportDate: string, days: number): boolean {
  const current = new Date(`${value}T00:00:00`);
  const report = new Date(`${reportDate}T00:00:00`);
  if (Number.isNaN(current.getTime()) || Number.isNaN(report.getTime())) {
    return false;
  }
  const diff = report.getTime() - current.getTime();
  return diff >= 0 && diff <= days * 24 * 60 * 60 * 1000;
}

// 统一处理日期筛选下拉框的时间范围判断。
function matchesDateFilter(value: string, reportDate: string, filter: DateFilter): boolean {
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

// 统计当前筛选结果中三类相关性的数量，用于左侧卡片和顶部 tab。
function relevanceCounts(papers: Paper[]): Record<Relevance, number> {
  return papers.reduce<Record<Relevance, number>>(
    (counts, paper) => {
      counts[paper.classification.relevance] += 1;
      return counts;
    },
    {direct: 0, indirect: 0, unrelated: 0},
  );
}

// 将用户反馈状态压缩成卡片上可读的一行提示。
function feedbackLabel(paper: Paper): string | null {
  if (!paper.feedback_status?.has_feedback || !paper.feedback_status.corrected_relevance) {
    return null;
  }
  return `Feedback -> ${relevanceLabel[paper.feedback_status.corrected_relevance]}`;
}

// 为后台任务提供一个稳定的人类可读状态文案。
function statusMessage(job: JobInfo): string {
  if (job.error) {
    return job.error;
  }
  if (job.message) {
    return job.message;
  }
  return job.status;
}

// 统一原生 select 的样式，避免页面上多个筛选器分散定义。
function nativeSelectClassName(): string {
  return "w-full rounded-md border border-[var(--line)] bg-white px-3 py-2 text-sm";
}

// 复用的空状态卡片，用来承接首次配置和无结果等场景。
function EmptyStateCard({
  eyebrow,
  title,
  body,
  actions,
}: {
  eyebrow: string;
  title: string;
  body: string;
  actions?: React.ReactNode;
}) {
  return (
    <Card className="border border-[var(--line)] bg-white p-8 text-center">
      <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--muted)]">
        {eyebrow}
      </p>
      <h2 className="mt-3 text-xl font-semibold text-[var(--ink)]">{title}</h2>
      <p className="mx-auto mt-3 max-w-2xl text-sm leading-7 text-[var(--muted)]">{body}</p>
      {actions ? <div className="mt-5 flex flex-wrap justify-center gap-2">{actions}</div> : null}
    </Card>
  );
}

// 首次建档时展示 profile 生成入口和最新提案预览。
function Onboarding({
  proposals,
  jobs,
  onBootstrap,
  onApplyProposal,
  busy,
}: {
  proposals: ProfileProposal[];
  jobs: JobInfo[];
  onBootstrap: (interestDescription: string, name: string) => Promise<void>;
  onApplyProposal: (proposalId: number) => Promise<void>;
  busy: boolean;
}) {
  const [name, setName] = React.useState("Default profile");
  const [interestDescription, setInterestDescription] = React.useState("");
  const pendingProposal = proposals.find((item) => item.state === "pending") ?? proposals[0] ?? null;
  const latestBootstrapJob =
    jobs.find((item) => item.job_type === "profile-bootstrap") ?? null;
  const bootstrapRunning =
    latestBootstrapJob?.status === "queued" || latestBootstrapJob?.status === "running";

  return (
    <main className="mx-auto max-w-7xl px-4 py-6">
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_520px]">
        <Card className="border border-[var(--line)] bg-white">
          <Card.Header className="space-y-2">
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--muted)]">
              SciRSSAgent
            </p>
            <h1 className="text-2xl font-semibold text-[var(--ink)]">
              Create your classification profile
            </h1>
            <p className="text-sm leading-6 text-[var(--muted)]">
              Describe your research interests in natural language. Model B will turn it into a
              structured classification profile for approval.
            </p>
          </Card.Header>
          <Card.Content className="space-y-4">
            <label className="block text-sm font-medium text-[var(--ink)]">
              Profile name
              <input
                className="mt-2 w-full rounded-md border border-[var(--line)] px-3 py-2 text-sm"
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </label>
            <label className="block text-sm font-medium text-[var(--ink)]">
              Research interests
              <textarea
                className="mt-2 min-h-52 w-full rounded-md border border-[var(--line)] px-3 py-2 text-sm"
                placeholder="Example: I care about nucleic acid chemistry, engineered polymerases, XNA enzymes, and method-focused directed evolution papers..."
                value={interestDescription}
                onChange={(event) => setInterestDescription(event.target.value)}
              />
            </label>
            {latestBootstrapJob ? (
              <div
                className={`rounded-md border p-3 text-sm ${
                  latestBootstrapJob.status === "failed"
                    ? "border-rose-300 bg-rose-50 text-rose-900"
                    : latestBootstrapJob.status === "completed"
                      ? "border-emerald-300 bg-emerald-50 text-emerald-900"
                      : "border-sky-300 bg-sky-50 text-sky-900"
                }`}
              >
                <p className="font-medium">
                  {bootstrapRunning ? "Generating profile..." : "Latest profile generation job"}
                </p>
                <p className="mt-1 leading-6">{statusMessage(latestBootstrapJob)}</p>
              </div>
            ) : null}
          </Card.Content>
          <Card.Footer>
            <Button
              isDisabled={busy || !interestDescription.trim()}
              onPress={() => void onBootstrap(interestDescription.trim(), name.trim() || "Default profile")}
            >
              {bootstrapRunning ? "Generating..." : "Generate initial profile"}
            </Button>
          </Card.Footer>
        </Card>

        <Card className="border border-[var(--line)] bg-white">
          <Card.Header>
            <h2 className="text-lg font-semibold text-[var(--ink)]">Latest proposal</h2>
          </Card.Header>
          <Card.Content className="max-h-[78vh] space-y-4 overflow-y-auto pr-1">
            {!pendingProposal ? (
              <p className="text-sm text-[var(--muted)]">No proposal yet.</p>
            ) : (
              <ProfileProposalPreview proposal={pendingProposal} />
            )}
          </Card.Content>
          {pendingProposal ? (
            <Card.Footer className="flex gap-2">
              <Button
                isDisabled={busy || bootstrapRunning || pendingProposal.state !== "pending"}
                onPress={() => void onApplyProposal(pendingProposal.id)}
              >
                Apply profile
              </Button>
            </Card.Footer>
          ) : null}
        </Card>
      </div>
    </main>
  );
}

// 中间列表里的文献卡片，承载快速浏览和快捷操作。
function PaperCard({
  paper,
  profile,
  isSelected,
  isUnread,
  onSelect,
}: {
  paper: Paper;
  profile: ClassificationProfile | null;
  isSelected: boolean;
  isUnread: boolean;
  onSelect: () => void;
}) {
  const tone = relevanceTone[paper.classification.relevance];
  const feedbackText = feedbackLabel(paper);
  const handleSelectKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onSelect();
    }
  };

  return (
    <Card
      className={`border-l-4 ${tone.ring} ${isSelected ? "outline outline-2 outline-[var(--accent)]" : ""}`}
    >
      <div
        className="block w-full cursor-pointer text-left"
        role="button"
        tabIndex={0}
        onClick={onSelect}
        onKeyDown={handleSelectKeyDown}
      >
        <Card.Header className="gap-3">
          <div className="flex flex-1 flex-wrap items-center gap-2">
            {isUnread ? (
              <span
                aria-label="Unread"
                className="size-2 rounded-full bg-[var(--unread)]"
                title="Unread"
              />
            ) : null}
            <Chip color={tone.chip} size="sm" variant="soft">
              {relevanceLabel[paper.classification.relevance]}
            </Chip>
            {paper.classification.topic_tags.slice(0, 2).map((tag) => (
              <Chip key={tag} size="sm" variant="secondary">
                {tagLabel(tag, profile)}
              </Chip>
            ))}
            {feedbackText ? (
              <Chip color="danger" size="sm" variant="soft">
                {feedbackText}
              </Chip>
            ) : null}
          </div>
          <span className={`text-sm font-semibold ${tone.text}`}>
            {Math.round(paper.classification.confidence * 100)}%
          </span>
        </Card.Header>
        <Card.Content className="gap-3">
          <div>
            <Card.Title className="line-clamp-2 text-lg leading-6">{paper.title}</Card.Title>
            {paper.classification.translated_title_zh ? (
              <Card.Description className="mt-1 line-clamp-2 text-sm">
                {paper.classification.translated_title_zh}
              </Card.Description>
            ) : null}
          </div>
          <p className="text-sm text-[var(--muted)]">
            {paper.journal || "Unknown journal"} · {paperDate(paper)} · {authorsLine(paper)}
          </p>
          <p className="line-clamp-2 text-sm leading-6 text-[var(--body)]">
            {sentence(paper.abstract ?? paper.classification.reason)}
          </p>
          <p className="line-clamp-2 text-sm leading-6 text-[var(--body)]">
            <span className="font-semibold text-[var(--ink)]">Why relevant:</span>{" "}
            {paper.classification.reason}
          </p>
        </Card.Content>
      </div>
    </Card>
  );
}

// 右侧详情面板负责展示当前选中文献的完整摘要和操作。
function DetailPanel({
  paper,
  profile,
  isUnread,
  onMarkRead,
  onSave,
  onMarkWrong,
}: {
  paper: Paper | null;
  profile: ClassificationProfile | null;
  isUnread: boolean;
  onMarkRead: () => void;
  onSave: () => void;
  onMarkWrong: () => void;
}) {
  if (!paper) {
    return (
      <aside className="sticky top-4 rounded-lg border border-[var(--line)] bg-white p-5 text-sm text-[var(--muted)]">
        No paper selected.
      </aside>
    );
  }
  const tone = relevanceTone[paper.classification.relevance];
  const feedbackText = feedbackLabel(paper);
  const zoteroSaved = paper.zotero_status?.saved ?? false;

  return (
    <aside className="sticky top-4 space-y-5 rounded-lg border border-[var(--line)] bg-white p-5">
      <div className="space-y-3">
        <div className="flex flex-wrap gap-2">
          <Chip color={tone.chip} size="sm" variant="soft">
            {relevanceLabel[paper.classification.relevance]}
          </Chip>
          <Chip size="sm" variant="secondary">
            {Math.round(paper.classification.confidence * 100)}% confidence
          </Chip>
          {feedbackText ? (
            <Chip color="danger" size="sm" variant="soft">
              {feedbackText}
            </Chip>
          ) : null}
        </div>
        <h2 className="text-xl font-semibold leading-7 text-[var(--ink)]">{paper.title}</h2>
        <p className="text-sm leading-6 text-[var(--muted)]">
          {paper.journal || "Unknown journal"} · {paperDate(paper)} · {authorsLine(paper)}
        </p>
      </div>

      <section className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
          Summary
        </h3>
        <p className="text-sm leading-6 text-[var(--body)]">
          {sentence(paper.abstract ?? paper.classification.reason)}
        </p>
      </section>

      <section className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
          LLM rationale
        </h3>
        <p className="text-sm leading-6 text-[var(--body)]">{paper.classification.reason}</p>
      </section>

      {paper.feedback_status?.note ? (
        <section className="space-y-2">
          <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
            Feedback note
          </h3>
          <p className="text-sm leading-6 text-[var(--body)]">{paper.feedback_status.note}</p>
        </section>
      ) : null}

      <section className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
          Keywords
        </h3>
        <div className="flex flex-wrap gap-2">
          {paper.classification.topic_tags.length ? (
            paper.classification.topic_tags.map((tag) => (
              <Chip key={tag} size="sm" variant="secondary">
                {tagLabel(tag, profile)}
              </Chip>
            ))
          ) : (
            <span className="text-sm text-[var(--muted)]">No keywords</span>
          )}
        </div>
      </section>

      <section className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
          Abstract
        </h3>
        <p className="max-h-64 overflow-auto pr-1 text-sm leading-6 text-[var(--body)]">
          {paper.abstract || "No abstract was available in the feed metadata."}
        </p>
      </section>

      {paper.zotero_status?.last_error ? (
        <div className="rounded-md border border-rose-300 bg-rose-50 p-3 text-sm text-rose-800">
          {paper.zotero_status.last_error}
        </div>
      ) : null}

      <div className="flex flex-wrap gap-2">
        <Button size="sm" onPress={() => window.open(doiHref(paper), "_blank")}>
          DOI link
        </Button>
        <Button
          size="sm"
          isDisabled={!isUnread}
          variant={isUnread ? "secondary" : "outline"}
          onPress={onMarkRead}
        >
          {isUnread ? "Mark as read" : "Read"}
        </Button>
        <Button size="sm" variant={zoteroSaved ? "secondary" : "tertiary"} onPress={onSave}>
          {zoteroSaved ? "Saved" : "Save to Zotero"}
        </Button>
        <Button
          size="sm"
          variant={feedbackText ? "danger-soft" : "ghost"}
          onPress={onMarkWrong}
        >
          Mark wrong
        </Button>
      </div>
    </aside>
  );
}

// 错误反馈弹窗负责提交人工纠正标签与说明。
function FeedbackModal({
  paper,
  value,
  note,
  onValueChange,
  onNoteChange,
  onClose,
  onSubmit,
}: {
  paper: Paper | null;
  value: Relevance;
  note: string;
  onValueChange: (value: Relevance) => void;
  onNoteChange: (value: string) => void;
  onClose: () => void;
  onSubmit: () => void;
}) {
  if (!paper) {
    return null;
  }
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/35 p-4">
      <div className="w-full max-w-xl rounded-lg border border-[var(--line)] bg-white p-5 shadow-xl">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--muted)]">
              Mark wrong
            </p>
            <h2 className="mt-2 text-lg font-semibold text-[var(--ink)]">{paper.title}</h2>
          </div>
          <Button size="sm" variant="ghost" onPress={onClose}>
            Close
          </Button>
        </div>

        <div className="mt-4 space-y-4">
          <label className="block text-sm font-medium text-[var(--ink)]">
            Correct label
            <select
              className={`${nativeSelectClassName()} mt-2`}
              value={value}
              onChange={(event) => onValueChange(event.target.value as Relevance)}
            >
              <option value="direct">Direct</option>
              <option value="indirect">Indirect</option>
              <option value="unrelated">Unrelated</option>
            </select>
          </label>

          <label className="block text-sm font-medium text-[var(--ink)]">
            Note
            <textarea
              className="mt-2 min-h-28 w-full rounded-md border border-[var(--line)] px-3 py-2 text-sm"
              placeholder="Why should this be classified differently?"
              value={note}
              onChange={(event) => onNoteChange(event.target.value)}
            />
          </label>
        </div>

        <div className="mt-5 flex justify-end gap-2">
          <Button size="sm" variant="ghost" onPress={onClose}>
            Cancel
          </Button>
          <Button size="sm" onPress={onSubmit}>
            Save feedback
          </Button>
        </div>
      </div>
    </div>
  );
}

// Zotero 保存弹窗负责选择目标 collection 并发起保存。
function ZoteroSaveModal({
  paper,
  collections,
  selectedCollectionKey,
  loading,
  saving,
  error,
  onCollectionChange,
  onClose,
  onSubmit,
}: {
  paper: Paper | null;
  collections: ZoteroCollectionOption[];
  selectedCollectionKey: string;
  loading: boolean;
  saving: boolean;
  error: string | null;
  onCollectionChange: (value: string) => void;
  onClose: () => void;
  onSubmit: () => void;
}) {
  if (!paper) {
    return null;
  }
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/35 p-4">
      <div className="w-full max-w-xl rounded-lg border border-[var(--line)] bg-white p-5 shadow-xl">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--muted)]">
              Zotero
            </p>
            <h2 className="mt-2 text-lg font-semibold text-[var(--ink)]">{paper.title}</h2>
          </div>
          <Button size="sm" variant="ghost" onPress={onClose}>
            Close
          </Button>
        </div>

        <div className="mt-4 space-y-4">
          <label className="block text-sm font-medium text-[var(--ink)]">
            Save into collection
            <select
              className={`${nativeSelectClassName()} mt-2`}
              disabled={loading || saving}
              value={selectedCollectionKey}
              onChange={(event) => onCollectionChange(event.target.value)}
            >
              <option value="">No collection (save to library only)</option>
              {collections.map((item) => (
                <option key={item.key} value={item.key}>
                  {item.path_label}
                </option>
              ))}
            </select>
          </label>

          {loading ? (
            <div className="rounded-md border border-sky-300 bg-sky-50 p-3 text-sm text-sky-900">
              Loading Zotero collections...
            </div>
          ) : null}
          {error ? (
            <div className="rounded-md border border-rose-300 bg-rose-50 p-3 text-sm text-rose-900">
              {error}
            </div>
          ) : null}
          <div className="rounded-md border border-[var(--line)] bg-slate-50 p-3 text-sm text-[var(--muted)]">
            If you prefer the browser connector workflow, open the original article first and then
            use the Zotero browser extension manually.
          </div>
        </div>

        <div className="mt-5 flex justify-end gap-2">
          <Button size="sm" variant="ghost" onPress={onClose}>
            Cancel
          </Button>
          <Button isDisabled={loading || saving} size="sm" onPress={onSubmit}>
            {saving ? "Saving..." : "Save to Zotero"}
          </Button>
        </div>
      </div>
    </div>
  );
}

// 在 profile 预览里复用的小规则列表组件。
function RuleList({title, items}: {title: string; items: string[]}) {
  return (
    <div className="space-y-2">
      <h4 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
        {title}
      </h4>
      {items.length === 0 ? (
        <p className="text-sm text-[var(--muted)]">No rules.</p>
      ) : (
        <ul className="list-disc space-y-1 pl-5 text-sm leading-6 text-[var(--body)]">
          {items.map((item) => (
            <li key={`${title}-${item}`}>{item}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

// 将完整 profile proposal 格式化成适合人工审核的长面板。
function ProfileProposalPreview({proposal}: {proposal: ProfileProposal}) {
  const profile = proposal.proposed_profile;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Chip size="sm" variant="secondary">
          {proposal.state}
        </Chip>
        <Chip size="sm" variant="secondary">
          {proposal.model}
        </Chip>
        <Chip size="sm" variant="secondary">
          v{profile.meta.version}
        </Chip>
      </div>
      <p className="text-sm leading-6 text-[var(--body)]">{proposal.summary}</p>
      <div className="grid gap-4 xl:grid-cols-3">
        <div className="rounded-md border border-[var(--line)] p-3">
          <RuleList title="Direct" items={profile.relevance_rules.direct} />
        </div>
        <div className="rounded-md border border-[var(--line)] p-3">
          <RuleList title="Indirect" items={profile.relevance_rules.indirect} />
        </div>
        <div className="rounded-md border border-[var(--line)] p-3">
          <RuleList title="Unrelated" items={profile.relevance_rules.unrelated} />
        </div>
      </div>
      <div className="space-y-2">
        <h4 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
          Topic taxonomy
        </h4>
        <div className="max-h-[320px] overflow-y-auto rounded-md border border-[var(--line)]">
          <table className="min-w-full table-fixed text-left text-sm">
            <thead className="sticky top-0 bg-slate-50 text-[var(--muted)]">
              <tr>
                <th className="w-[26%] px-3 py-2 font-medium">Tag</th>
                <th className="w-[22%] px-3 py-2 font-medium">Label</th>
                <th className="w-[34%] px-3 py-2 font-medium">Description</th>
                <th className="w-[18%] px-3 py-2 font-medium">Examples</th>
              </tr>
            </thead>
            <tbody>
              {profile.topic_taxonomy.map((topic) => (
                <tr key={topic.id} className="border-t border-[var(--line)] align-top">
                  <td className="px-3 py-3 font-mono text-xs break-all">{topic.id}</td>
                  <td className="px-3 py-3 font-medium text-[var(--ink)]">{topic.label}</td>
                  <td className="px-3 py-3 leading-6 text-[var(--body)]">{topic.description}</td>
                  <td className="px-3 py-3">
                    <div className="flex flex-wrap gap-1.5">
                      {topic.examples.length === 0 ? (
                        <span className="text-xs text-[var(--muted)]">None</span>
                      ) : (
                        topic.examples.slice(0, 3).map((example) => (
                          <Chip key={`${topic.id}-${example}`} size="sm" variant="secondary">
                            {example}
                          </Chip>
                        ))
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
        <div className="space-y-2 rounded-md border border-[var(--line)] p-3">
          <h4 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
            Few-shot examples
          </h4>
          <div className="max-h-[260px] space-y-3 overflow-y-auto pr-1">
            {profile.few_shots.length === 0 ? (
              <p className="text-sm text-[var(--muted)]">No few-shot examples.</p>
            ) : (
              profile.few_shots.map((item) => (
                <div
                  key={`${item.title}-${item.relevance}`}
                  className="rounded-md border border-[var(--line)] p-3"
                >
                  <div className="flex flex-wrap items-center gap-2">
                    <Chip size="sm" variant="secondary">
                      {relevanceLabel[item.relevance]}
                    </Chip>
                    {item.tags.map((tag) => (
                      <Chip key={`${item.title}-${tag}`} size="sm" variant="secondary">
                        {tag}
                      </Chip>
                    ))}
                  </div>
                  <p className="mt-2 text-sm font-medium leading-6 text-[var(--ink)]">
                    {item.title}
                  </p>
                  <p className="mt-1 text-sm leading-6 text-[var(--body)]">{item.rationale}</p>
                </div>
              ))
            )}
          </div>
        </div>
        <div className="space-y-2 rounded-md border border-[var(--line)] p-3">
          <h4 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
            Notes
          </h4>
          <div className="max-h-[260px] overflow-y-auto pr-1">
            {profile.classification_notes.length === 0 ? (
              <p className="text-sm text-[var(--muted)]">No extra notes.</p>
            ) : (
              <ul className="list-disc space-y-2 pl-5 text-sm leading-6 text-[var(--body)]">
                {profile.classification_notes.map((note) => (
                  <li key={note}>{note}</li>
                ))}
              </ul>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// 管理面板统一承载 feed 设置、后台任务、提案和反馈管理。
function AdminPanel({
  open,
  profile,
  hasFeeds,
  feeds,
  feedsSaving,
  feedback,
  jobs,
  proposals,
  onClose,
  onFeedChange,
  onAddFeed,
  onRemoveFeed,
  onSaveFeeds,
  onGenerateProposal,
  onApplyProposal,
  onRejectProposal,
  onRunFeedSync,
  onReclassifyRecent,
  onReclassifyFeedback,
  onReclassifyAll,
  onDeleteFeedback,
  onRefreshReport,
}: {
  open: boolean;
  profile: ClassificationProfile | null;
  hasFeeds: boolean;
  feeds: FeedSubscription[];
  feedsSaving: boolean;
  feedback: FeedbackRecord[];
  jobs: JobInfo[];
  proposals: ProfileProposal[];
  onClose: () => void;
  onFeedChange: (index: number, field: "journal" | "url", value: string) => void;
  onAddFeed: () => void;
  onRemoveFeed: (index: number) => void;
  onSaveFeeds: () => void;
  onGenerateProposal: () => void;
  onApplyProposal: (id: number) => void;
  onRejectProposal: (id: number) => void;
  onRunFeedSync: () => void;
  onReclassifyRecent: () => void;
  onReclassifyFeedback: () => void;
  onReclassifyAll: () => void;
  onDeleteFeedback: (id: number) => void;
  onRefreshReport: () => void;
}) {
  if (!open) {
    return null;
  }
  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-slate-900/20">
      <aside className="h-full w-full max-w-[min(1100px,92vw)] overflow-auto border-l border-[var(--line)] bg-[var(--paper)] p-4 shadow-xl">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--muted)]">
              Admin
            </p>
            <h2 className="mt-2 text-2xl font-semibold text-[var(--ink)]">Control center</h2>
          </div>
          <Button size="sm" variant="ghost" onPress={onClose}>
            Close
          </Button>
        </div>

        <section className="mt-5 rounded-lg border border-[var(--line)] bg-white p-4">
          <h3 className="text-sm font-semibold text-[var(--ink)]">Actions</h3>
          {!hasFeeds ? (
            <p className="mt-2 text-sm leading-6 text-[var(--muted)]">
              Add and save at least one RSS feed before running a manual fetch job.
            </p>
          ) : null}
          <div className="mt-3 flex flex-wrap gap-2">
            <Button isDisabled={!hasFeeds} size="sm" onPress={onRunFeedSync}>
              Run fetch + classify
            </Button>
            <Button size="sm" variant="outline" onPress={onRefreshReport}>
              Refresh report
            </Button>
            <Button size="sm" variant="outline" onPress={onReclassifyRecent}>
              Reclassify recent 50
            </Button>
            <Button size="sm" variant="outline" onPress={onReclassifyFeedback}>
              Reclassify feedback papers
            </Button>
            <Button size="sm" variant="outline" onPress={onReclassifyAll}>
              Reclassify all
            </Button>
            <Button size="sm" variant="secondary" onPress={onGenerateProposal}>
              Generate profile proposal
            </Button>
          </div>
        </section>

        <section className="mt-4 rounded-lg border border-[var(--line)] bg-white p-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-sm font-semibold text-[var(--ink)]">Feed subscriptions</h3>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="outline" onPress={onAddFeed}>
                Add feed
              </Button>
              <Button size="sm" isDisabled={feedsSaving} onPress={onSaveFeeds}>
                {feedsSaving ? "Saving..." : "Save feeds"}
              </Button>
            </div>
          </div>
          <div className="mt-3 space-y-3">
            {feeds.length === 0 ? (
              <p className="text-sm text-[var(--muted)]">No RSS feeds configured yet.</p>
            ) : (
              feeds.map((item, index) => (
                <Card key={`${item.url}-${index}`} className="border border-[var(--line)]">
                  <Card.Content className="space-y-3">
                    <label className="block text-sm font-medium text-[var(--ink)]">
                      Journal name
                      <input
                        className="mt-2 w-full rounded-md border border-[var(--line)] px-3 py-2 text-sm"
                        value={item.journal}
                        onChange={(event) => onFeedChange(index, "journal", event.target.value)}
                      />
                    </label>
                    <label className="block text-sm font-medium text-[var(--ink)]">
                      RSS URL
                      <input
                        className="mt-2 w-full rounded-md border border-[var(--line)] px-3 py-2 text-sm"
                        value={item.url}
                        onChange={(event) => onFeedChange(index, "url", event.target.value)}
                      />
                    </label>
                    <div className="flex justify-end">
                      <Button size="sm" variant="ghost" onPress={() => onRemoveFeed(index)}>
                        Remove
                      </Button>
                    </div>
                  </Card.Content>
                </Card>
              ))
            )}
          </div>
        </section>

        <section className="mt-4 rounded-lg border border-[var(--line)] bg-white p-4">
          <h3 className="text-sm font-semibold text-[var(--ink)]">Current profile</h3>
          {!profile ? (
            <p className="mt-3 text-sm text-[var(--muted)]">No applied profile yet.</p>
          ) : (
            <div className="mt-3 space-y-3">
              <div className="flex flex-wrap gap-2">
                <Chip size="sm" variant="secondary">
                  {profile.meta.name}
                </Chip>
                <Chip size="sm" variant="secondary">
                  v{profile.meta.version}
                </Chip>
                <Chip size="sm" variant="secondary">
                  {profile.topic_taxonomy.length} tags
                </Chip>
              </div>
              <p className="text-sm leading-6 text-[var(--body)]">{profile.scope}</p>
            </div>
          )}
        </section>

        <section className="mt-4 rounded-lg border border-[var(--line)] bg-white p-4">
          <h3 className="text-sm font-semibold text-[var(--ink)]">Profile proposals</h3>
          <div className="mt-3 space-y-3">
            {proposals.length === 0 ? (
              <p className="text-sm text-[var(--muted)]">No profile proposals yet.</p>
            ) : (
              proposals.map((proposal) => (
                <Card key={proposal.id} className="border border-[var(--line)]">
                  <Card.Header className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <Chip size="sm" variant="secondary">
                        {proposal.state}
                      </Chip>
                      <Chip size="sm" variant="secondary">
                        {proposal.model}
                      </Chip>
                    </div>
                    <span className="text-xs text-[var(--muted)]">{proposal.created_at.slice(0, 10)}</span>
                  </Card.Header>
                  <Card.Content className="max-h-[78vh] overflow-y-auto pr-1">
                    <ProfileProposalPreview proposal={proposal} />
                  </Card.Content>
                  <Card.Footer className="flex flex-wrap gap-2">
                    <Button
                      isDisabled={proposal.state !== "pending"}
                      size="sm"
                      onPress={() => onApplyProposal(proposal.id)}
                    >
                      Apply
                    </Button>
                    <Button
                      isDisabled={proposal.state !== "pending"}
                      size="sm"
                      variant="ghost"
                      onPress={() => onRejectProposal(proposal.id)}
                    >
                      Reject
                    </Button>
                  </Card.Footer>
                </Card>
              ))
            )}
          </div>
        </section>

        <section className="mt-4 rounded-lg border border-[var(--line)] bg-white p-4">
          <h3 className="text-sm font-semibold text-[var(--ink)]">Feedback queue</h3>
          <div className="mt-3 space-y-3">
            {feedback.length === 0 ? (
              <p className="text-sm text-[var(--muted)]">No feedback submitted yet.</p>
            ) : (
              feedback.map((item) => (
                <Card key={item.id} className="border border-[var(--line)]">
                  <Card.Content className="space-y-2">
                    <p className="text-sm font-semibold text-[var(--ink)]">{item.paper_title}</p>
                    <p className="text-sm text-[var(--muted)]">
                      {relevanceLabel[item.original_relevance]} {" -> "} {relevanceLabel[item.corrected_relevance]}
                    </p>
                    {item.note ? <p className="text-sm leading-6 text-[var(--body)]">{item.note}</p> : null}
                    <div className="flex flex-wrap gap-2">
                      <Chip size="sm" variant="secondary">
                        {item.state}
                      </Chip>
                      {item.used_in_profile ? (
                        <Chip color="success" size="sm" variant="soft">
                          Used in profile
                        </Chip>
                      ) : null}
                      <Button
                        size="sm"
                        variant="ghost"
                        onPress={() => onDeleteFeedback(item.id)}
                      >
                        Delete
                      </Button>
                    </div>
                  </Card.Content>
                </Card>
              ))
            )}
          </div>
        </section>

        <section className="mt-4 rounded-lg border border-[var(--line)] bg-white p-4">
          <h3 className="text-sm font-semibold text-[var(--ink)]">Jobs</h3>
          <div className="mt-3 space-y-3">
            {jobs.length === 0 ? (
              <p className="text-sm text-[var(--muted)]">No jobs yet.</p>
            ) : (
              jobs.map((job) => (
                <Card key={job.id} className="border border-[var(--line)]">
                  <Card.Content className="space-y-2">
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-sm font-semibold text-[var(--ink)]">{job.job_type}</span>
                      <Chip size="sm" variant="secondary">
                        {job.status}
                      </Chip>
                    </div>
                    <p className="text-sm leading-6 text-[var(--body)]">{statusMessage(job)}</p>
                  </Card.Content>
                </Card>
              ))
            )}
          </div>
        </section>
      </aside>
    </div>
  );
}

// 应用根组件负责衔接数据加载、筛选状态、后台任务和三栏式阅读界面。
export function App() {
  const [report, setReport] = React.useState<Report>(() => loadEmbeddedReport() ?? EMPTY_REPORT);
  const [profile, setProfile] = React.useState<ClassificationProfile | null>(null);
  const [feeds, setFeeds] = React.useState<FeedSubscription[]>([]);
  const [feedsLoaded, setFeedsLoaded] = React.useState(false);
  const [loadError, setLoadError] = React.useState<string | null>(null);
  const [notice, setNotice] = React.useState<string | null>(null);
  const [query, setQuery] = React.useState("");
  const [relevance, setRelevance] = React.useState<RelevanceFilter>("all");
  const [topic, setTopic] = React.useState("all");
  const [journal, setJournal] = React.useState("all");
  const [dateFilter, setDateFilter] = React.useState<DateFilter>("30d");
  const [readFilter, setReadFilter] = React.useState<ReadFilter>("unread");
  const [selectedId, setSelectedId] = React.useState<number | null>(null);
  const [feedbackRecords, setFeedbackRecords] = React.useState<FeedbackRecord[]>([]);
  const [profileProposals, setProfileProposals] = React.useState<ProfileProposal[]>([]);
  const [jobs, setJobs] = React.useState<JobInfo[]>([]);
  const [adminOpen, setAdminOpen] = React.useState(false);
  const [feedbackPaper, setFeedbackPaper] = React.useState<Paper | null>(null);
  const [feedbackValue, setFeedbackValue] = React.useState<Relevance>("indirect");
  const [feedbackNote, setFeedbackNote] = React.useState("");
  const [feedsSaving, setFeedsSaving] = React.useState(false);
  const [zoteroPaper, setZoteroPaper] = React.useState<Paper | null>(null);
  const [zoteroCollections, setZoteroCollections] = React.useState<ZoteroCollectionOption[]>([]);
  const [zoteroCollectionKey, setZoteroCollectionKey] = React.useState("");
  const [zoteroLoading, setZoteroLoading] = React.useState(false);
  const [zoteroSaving, setZoteroSaving] = React.useState(false);
  const [zoteroError, setZoteroError] = React.useState<string | null>(null);
  const [busy, setBusy] = React.useState(false);
  const deferredQuery = React.useDeferredValue(query);

  const refreshProfile = React.useCallback(async () => {
    const next = await fetchCurrentProfile();
    setProfile(next.profile);
    return next.profile;
  }, []);

  const refreshReport = React.useCallback(async () => {
    try {
      const next = await fetchLatestReport();
      React.startTransition(() => setReport(next));
      setLoadError(null);
    } catch (error) {
      const embedded = loadEmbeddedReport();
      if (embedded) {
        React.startTransition(() => setReport(embedded));
        setLoadError("Using embedded report data because the API is unavailable.");
        return;
      }
      throw error;
    }
  }, []);

  const refreshFeedback = React.useCallback(async () => {
    setFeedbackRecords(await fetchFeedback());
  }, []);

  const refreshFeeds = React.useCallback(async () => {
    const nextFeeds = await fetchFeedSubscriptions();
    setFeeds(nextFeeds);
    setFeedsLoaded(true);
  }, []);

  const refreshProposals = React.useCallback(async () => {
    setProfileProposals(await fetchProfileProposals());
  }, []);

  const refreshAll = React.useCallback(async () => {
    try {
      const currentProfile = await refreshProfile();
      await Promise.all([refreshProposals(), refreshFeedback(), refreshFeeds()]);
      if (currentProfile) {
        await refreshReport();
      }
      setLoadError(null);
    } catch (error) {
      setLoadError((error as Error).message);
    }
  }, [refreshFeedback, refreshFeeds, refreshProfile, refreshProposals, refreshReport]);

  React.useEffect(() => {
    void refreshAll();
  }, [refreshAll]);

  React.useEffect(() => {
    if (profile && feedsLoaded && feeds.length === 0) {
      setAdminOpen(true);
    }
  }, [feeds.length, feedsLoaded, profile]);

  const runningJobs = React.useMemo(
    () => jobs.filter((job) => job.status === "queued" || job.status === "running"),
    [jobs],
  );
  const bootstrapJob = React.useMemo(
    () => jobs.find((job) => job.job_type === "profile-bootstrap") ?? null,
    [jobs],
  );
  const onboardingBusy =
    busy ||
    bootstrapJob?.status === "queued" ||
    bootstrapJob?.status === "running";

  React.useEffect(() => {
    if (runningJobs.length === 0) {
      return;
    }
    const timer = window.setInterval(() => {
      Promise.all(runningJobs.map((job) => fetchJob(job.id)))
        .then((updatedJobs) => {
          setJobs((current) => {
            const byId = new Map(current.map((job) => [job.id, job]));
            updatedJobs.forEach((job) => byId.set(job.id, job));
            return Array.from(byId.values()).sort((left, right) =>
              left.created_at < right.created_at ? 1 : -1,
            );
          });
          const failedJob = updatedJobs.find((job) => job.status === "failed" && job.error);
          if (failedJob?.error) {
            setLoadError(failedJob.error);
          }
          if (updatedJobs.some((job) => job.status === "completed")) {
            void refreshAll();
          }
        })
        .catch((error) => setLoadError((error as Error).message));
    }, 2500);
    return () => window.clearInterval(timer);
  }, [refreshAll, runningJobs]);

  const tags = React.useMemo(
    () => profile?.topic_taxonomy.map((item) => item.id).sort() ?? [],
    [profile],
  );
  const journals = React.useMemo(
    () => Array.from(new Set(report.papers.map((paper) => paper.journal).filter(Boolean) as string[])).sort(),
    [report.papers],
  );

  const filteredBase = React.useMemo(
    () =>
      report.papers.filter((paper) => {
        const haystack = [
          paper.title,
          paper.classification.translated_title_zh ?? "",
          paper.abstract ?? "",
          paper.journal ?? "",
          paper.authors?.join(" ") ?? "",
          paper.classification.topic_tags.join(" "),
          paper.feedback_status?.note ?? "",
        ]
          .join(" ")
          .toLowerCase();
        const matchesQuery = !deferredQuery || haystack.includes(deferredQuery.toLowerCase());
        const matchesTopic = topic === "all" || paper.classification.topic_tags.includes(topic);
        const matchesJournal = journal === "all" || paper.journal === journal;
        const dateValue = paperDate(paper);
        const matchesRead =
          readFilter === "all" ||
          (readFilter === "read" ? Boolean(paper.read_at) : !paper.read_at);
        const matchesDate = matchesDateFilter(dateValue, report.report_date, dateFilter);
        return matchesQuery && matchesTopic && matchesJournal && matchesRead && matchesDate;
      }),
    [dateFilter, deferredQuery, journal, readFilter, report.papers, report.report_date, topic],
  );

  const filtered = React.useMemo(
    () =>
      filteredBase.filter(
        (paper) => relevance === "all" || paper.classification.relevance === relevance,
      ),
    [filteredBase, relevance],
  );

  const needsFeedSetup = Boolean(profile && feedsLoaded && feeds.length === 0);
  const hasNoFetchedPapers = !needsFeedSetup && feeds.length > 0 && report.papers.length === 0;
  const visibleBase = React.useMemo(
    () => (needsFeedSetup ? [] : filteredBase),
    [filteredBase, needsFeedSetup],
  );
  const visibleList = React.useMemo(
    () => (needsFeedSetup ? [] : filtered),
    [filtered, needsFeedSetup],
  );
  const visibleTotals = React.useMemo(() => relevanceCounts(visibleBase), [visibleBase]);

  React.useEffect(() => {
    if (visibleList.length === 0) {
      setSelectedId(null);
      return;
    }
    if (!selectedId || !visibleList.some((paper) => paper.id === selectedId)) {
      setSelectedId(visibleList[0].id);
    }
  }, [selectedId, visibleList]);

  React.useEffect(() => {
    if (!zoteroPaper) {
      return;
    }
    let cancelled = false;
    setZoteroLoading(true);
    setZoteroError(null);
    setZoteroCollections([]);
    setZoteroCollectionKey("");

    void fetchZoteroCollections()
      .then((payload) => {
        if (cancelled) {
          return;
        }
        setZoteroCollections(payload.collections);
        setZoteroCollectionKey(payload.default_collection_key ?? "");
      })
      .catch((error) => {
        if (cancelled) {
          return;
        }
        setZoteroError((error as Error).message);
      })
      .finally(() => {
        if (!cancelled) {
          setZoteroLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [zoteroPaper]);

  const selectedPaper = visibleList.find((paper) => paper.id === selectedId) ?? null;

  const updatePaper = (paperId: number, updater: (paper: Paper) => Paper) => {
    setReport((current) => ({
      ...current,
      papers: current.papers.map((paper) => (paper.id === paperId ? updater(paper) : paper)),
    }));
  };

  const persistReadStatus = async (paperId: number) => {
    const paper = report.papers.find((item) => item.id === paperId);
    if (!paper || paper.read_at) {
      return;
    }
    const optimisticReadAt = new Date().toISOString();
    updatePaper(paperId, (current) => ({...current, read_at: optimisticReadAt}));
    try {
      const status = await markPaperRead(paperId);
      updatePaper(paperId, (current) => ({...current, read_at: status.read_at}));
    } catch (error) {
      updatePaper(paperId, (current) => ({...current, read_at: null}));
      setLoadError((error as Error).message);
    }
  };

  const selectPaper = (paper: Paper) => {
    setSelectedId(paper.id);
  };

  const openZoteroModal = (paper: Paper) => {
    setZoteroPaper(paper);
    setZoteroError(null);
  };

  const handleSaveToZotero = async () => {
    if (!zoteroPaper) {
      return;
    }
    try {
      setZoteroSaving(true);
      const status = await saveToZotero(zoteroPaper.id, zoteroCollectionKey || null);
      updatePaper(zoteroPaper.id, (current) => ({...current, zotero_status: status}));
      if (status.saved) {
        setNotice("Saved to Zotero.");
        setZoteroPaper(null);
        setZoteroError(null);
        return;
      }
      setZoteroError(status.last_error ?? "Zotero save updated.");
    } catch (error) {
      setZoteroError((error as Error).message);
      setLoadError((error as Error).message);
    } finally {
      setZoteroSaving(false);
    }
  };

  const openFeedbackModal = (paper: Paper) => {
    setFeedbackPaper(paper);
    setFeedbackValue(paper.feedback_status?.corrected_relevance ?? paper.classification.relevance);
    setFeedbackNote(paper.feedback_status?.note ?? "");
  };

  const submitFeedback = async () => {
    if (!feedbackPaper) {
      return;
    }
    try {
      await createFeedback({
        paper_id: feedbackPaper.id,
        corrected_relevance: feedbackValue,
        note: feedbackNote.trim() || undefined,
      });
      setFeedbackPaper(null);
      setFeedbackNote("");
      await Promise.all([refreshReport(), refreshFeedback()]);
      setNotice("Feedback saved.");
    } catch (error) {
      setLoadError((error as Error).message);
    }
  };

  const handleDeleteFeedback = async (feedbackId: number) => {
    try {
      await deleteFeedback(feedbackId);
      await Promise.all([refreshReport(), refreshFeedback(), refreshProposals()]);
      setNotice("Feedback deleted.");
    } catch (error) {
      setLoadError((error as Error).message);
    }
  };

  const registerJob = (job: JobInfo, openAdmin = true) => {
    setJobs((current) => [job, ...current.filter((item) => item.id !== job.id)]);
    if (openAdmin) {
      setAdminOpen(true);
    }
  };

  const handleFeedChange = (index: number, field: "journal" | "url", value: string) => {
    setFeeds((current) =>
      current.map((item, itemIndex) =>
        itemIndex === index ? {...item, [field]: value} : item,
      ),
    );
  };

  const handleAddFeed = () => {
    setFeeds((current) => [...current, {journal: "", url: ""}]);
  };

  const handleRemoveFeed = (index: number) => {
    setFeeds((current) => current.filter((_item, itemIndex) => itemIndex !== index));
  };

  const handleSaveFeeds = async () => {
    const cleaned = feeds
      .map((item) => ({journal: item.journal.trim(), url: item.url.trim()}))
      .filter((item) => item.journal || item.url);
    if (cleaned.some((item) => !item.journal || !item.url)) {
      setLoadError("Each feed needs both a journal name and an RSS URL.");
      return;
    }
    try {
      setFeedsSaving(true);
      const saved = await saveFeedSubscriptions(cleaned);
      setFeeds(saved);
      setFeedsLoaded(true);
      setNotice("Feed subscriptions saved.");
      setLoadError(null);
    } catch (error) {
      setLoadError((error as Error).message);
    } finally {
      setFeedsSaving(false);
    }
  };

  const handleBootstrap = async (interestDescription: string, name: string) => {
    try {
      setBusy(true);
      registerJob(
        await bootstrapProfile({interest_description: interestDescription, name}),
        false,
      );
      setNotice("Initial profile generation started.");
    } catch (error) {
      setLoadError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleGenerateProposal = async () => {
    try {
      registerJob(await launchProfileProposalGeneration());
      setNotice("Profile proposal job started.");
    } catch (error) {
      setLoadError((error as Error).message);
    }
  };

  const handleApplyProposal = async (id: number) => {
    try {
      setBusy(true);
      await applyProfileProposal(id);
      await refreshAll();
      setNotice("Profile proposal applied.");
    } catch (error) {
      setLoadError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleRejectProposal = async (id: number) => {
    try {
      await rejectProfileProposal(id);
      await refreshProposals();
      setNotice("Profile proposal rejected.");
    } catch (error) {
      setLoadError((error as Error).message);
    }
  };

  const handleRunAdminJob = async (path: "/api/admin/run" | "/api/admin/report/latest") => {
    try {
      registerJob(await launchAdminJob(path));
      setNotice("Job started.");
    } catch (error) {
      setLoadError((error as Error).message);
    }
  };

  const handleReclassify = async (scope: "recent" | "feedback" | "all") => {
    try {
      registerJob(await launchReclassifyJob({scope, limit: scope === "all" ? 500 : 50}));
      setNotice("Reclassification job started.");
    } catch (error) {
      setLoadError((error as Error).message);
    }
  };

  if (!profile) {
    return (
      <>
        {loadError ? (
          <div className="mx-auto mt-4 max-w-5xl rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900">
            {loadError}
          </div>
        ) : null}
        {notice ? (
          <div className="mx-auto mt-4 max-w-5xl rounded-md border border-emerald-300 bg-emerald-50 p-3 text-sm text-emerald-900">
            {notice}
          </div>
        ) : null}
        <Onboarding
          busy={onboardingBusy}
          jobs={jobs}
          proposals={profileProposals}
          onBootstrap={handleBootstrap}
          onApplyProposal={handleApplyProposal}
        />
      </>
    );
  }

  return (
    <main className="min-h-screen bg-[var(--paper)] text-[var(--ink)]">
      <AdminPanel
        open={adminOpen}
        profile={profile}
        hasFeeds={feeds.length > 0}
        feeds={feeds}
        feedsSaving={feedsSaving}
        feedback={feedbackRecords}
        jobs={jobs}
        proposals={profileProposals}
        onClose={() => setAdminOpen(false)}
        onFeedChange={handleFeedChange}
        onAddFeed={handleAddFeed}
        onRemoveFeed={handleRemoveFeed}
        onSaveFeeds={() => void handleSaveFeeds()}
        onGenerateProposal={() => void handleGenerateProposal()}
        onApplyProposal={(id) => void handleApplyProposal(id)}
        onRejectProposal={(id) => void handleRejectProposal(id)}
        onRunFeedSync={() => void handleRunAdminJob("/api/admin/run")}
        onReclassifyRecent={() => void handleReclassify("recent")}
        onReclassifyFeedback={() => void handleReclassify("feedback")}
        onReclassifyAll={() => void handleReclassify("all")}
        onDeleteFeedback={(id) => void handleDeleteFeedback(id)}
        onRefreshReport={() => void handleRunAdminJob("/api/admin/report/latest")}
      />
      <FeedbackModal
        paper={feedbackPaper}
        value={feedbackValue}
        note={feedbackNote}
        onValueChange={setFeedbackValue}
        onNoteChange={setFeedbackNote}
        onClose={() => setFeedbackPaper(null)}
        onSubmit={() => void submitFeedback()}
      />
      <ZoteroSaveModal
        paper={zoteroPaper}
        collections={zoteroCollections}
        selectedCollectionKey={zoteroCollectionKey}
        loading={zoteroLoading}
        saving={zoteroSaving}
        error={zoteroError}
        onCollectionChange={setZoteroCollectionKey}
        onClose={() => setZoteroPaper(null)}
        onSubmit={() => void handleSaveToZotero()}
      />

      <div className="mx-auto grid max-w-[1500px] gap-4 px-4 py-4 lg:grid-cols-[260px_minmax(0,1fr)_360px]">
        <aside className="space-y-4 rounded-lg border border-[var(--line)] bg-white p-4 lg:sticky lg:top-4 lg:h-[calc(100vh-2rem)] lg:overflow-auto">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--muted)]">
              SciRSSAgent
            </p>
            <h1 className="mt-2 text-2xl font-semibold">Paper review</h1>
            <p className="mt-2 text-sm leading-6 text-[var(--muted)]">
              {report.report_date} · {visibleList.length} shown · {needsFeedSetup ? 0 : report.papers.length} total
            </p>
            <p className="mt-1 text-sm leading-6 text-[var(--muted)]">
              {profile.meta.name} · v{profile.meta.version}
            </p>
          </div>

          <div className="grid grid-cols-3 gap-2 text-center">
            {(["direct", "indirect", "unrelated"] as Relevance[]).map((item) => (
              <div key={item} className="rounded-md border border-[var(--line)] p-2">
                <div className={`text-lg font-semibold ${relevanceTone[item].text}`}>
                  {visibleTotals[item] ?? 0}
                </div>
                <div className="text-[11px] uppercase text-[var(--muted)]">{item}</div>
              </div>
            ))}
          </div>

          <div className="space-y-3">
            <label className="block text-sm font-medium text-[var(--ink)]">
              Journal
              <select
                className={`${nativeSelectClassName()} mt-2`}
                value={journal}
                onChange={(event) => setJournal(event.target.value)}
              >
                <option value="all">All journals</option>
                {journals.map((item) => (
                  <option key={item} value={item}>
                    {item}
                  </option>
                ))}
              </select>
            </label>

            <label className="block text-sm font-medium text-[var(--ink)]">
              Topic
              <select
                className={`${nativeSelectClassName()} mt-2`}
                value={topic}
                onChange={(event) => setTopic(event.target.value)}
              >
                <option value="all">All topics</option>
                {tags.map((item) => (
                  <option key={item} value={item}>
                    {tagLabel(item, profile)}
                  </option>
                ))}
              </select>
            </label>

            <label className="block text-sm font-medium text-[var(--ink)]">
              Date
              <select
                className={`${nativeSelectClassName()} mt-2`}
                value={dateFilter}
                onChange={(event) => setDateFilter(event.target.value as DateFilter)}
              >
                <option value="all">All dates</option>
                <option value="today">Today</option>
                <option value="7d">Last 7 days</option>
                <option value="30d">Last 30 days</option>
                <option value="180d">Last 6 months</option>
              </select>
            </label>

            <label className="block text-sm font-medium text-[var(--ink)]">
              Read status
              <select
                className={`${nativeSelectClassName()} mt-2`}
                value={readFilter}
                onChange={(event) => setReadFilter(event.target.value as ReadFilter)}
              >
                <option value="unread">Unread</option>
                <option value="read">Read</option>
                <option value="all">All</option>
              </select>
            </label>
          </div>

          <Button
            fullWidth
            variant="outline"
            onPress={() => {
              setTopic("all");
              setJournal("all");
              setDateFilter("30d");
              setReadFilter("unread");
              setRelevance("all");
              setQuery("");
            }}
          >
            Reset filters
          </Button>
        </aside>

        <section className="min-w-0 space-y-4">
          <div className="rounded-lg border border-[var(--line)] bg-white p-4">
            {loadError ? (
              <div className="mb-4 rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900">
                {loadError}
              </div>
            ) : null}
            {notice ? (
              <div className="mb-4 rounded-md border border-emerald-300 bg-emerald-50 p-3 text-sm text-emerald-900">
                {notice}
              </div>
            ) : null}
            {report.errors.length ? (
              <div className="mb-4 rounded-md border border-rose-300 bg-rose-50 p-3 text-sm text-rose-900">
                {report.errors.map((item) => (
                  <div key={item}>{item}</div>
                ))}
              </div>
            ) : null}

            <div className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
              <label className="block w-full xl:max-w-xl">
                <span className="text-sm font-medium text-[var(--ink)]">Search</span>
                <input
                  className="mt-2 w-full rounded-md border border-[var(--line)] px-3 py-2 text-sm"
                  placeholder="Search title, abstract, author, journal"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
              </label>

              <div className="flex flex-col gap-3 xl:items-end">
                <Button size="sm" variant="secondary" onPress={() => setAdminOpen(true)}>
                  Open admin
                </Button>
                <div className="flex flex-wrap gap-2">
                  {relevanceTabs.map((tab) => (
                    <Button
                      key={tab.id}
                      size="sm"
                      variant={relevance === tab.id ? "secondary" : "outline"}
                      onPress={() => setRelevance(tab.id)}
                    >
                      {tab.label}{" "}
                      {tab.id === "all" ? visibleBase.length : visibleTotals[tab.id]}
                    </Button>
                  ))}
                </div>
              </div>
            </div>
          </div>

          {needsFeedSetup ? (
            <EmptyStateCard
              eyebrow="Feed setup"
              title="Add RSS feeds before reviewing papers"
              body="No RSS feed subscriptions are saved yet. Open Admin, add one or more journal feeds, save them, and then run fetch manually or wait for your scheduled job."
              actions={
                <>
                  <Button
                    size="sm"
                    variant="secondary"
                    onPress={() => {
                      if (feeds.length === 0) {
                        handleAddFeed();
                      }
                      setAdminOpen(true);
                    }}
                  >
                    Add first feed
                  </Button>
                  <Button size="sm" variant="outline" onPress={() => setAdminOpen(true)}>
                    Open admin
                  </Button>
                </>
              }
            />
          ) : hasNoFetchedPapers ? (
            <EmptyStateCard
              eyebrow="Waiting for fetch"
              title="Feeds are ready, but no papers have been fetched yet"
              body="Save is complete. You can run a manual fetch right now, or let your scheduled task populate the library automatically."
              actions={
                <>
                  <Button
                    size="sm"
                    variant="secondary"
                    onPress={() => void handleRunAdminJob("/api/admin/run")}
                  >
                    Run fetch + classify
                  </Button>
                  <Button size="sm" variant="outline" onPress={() => setAdminOpen(true)}>
                    Open admin
                  </Button>
                </>
              }
            />
          ) : visibleList.length === 0 ? (
            <EmptyStateCard
              eyebrow="No results"
              title="No papers match the current filters"
              body="Try broadening the journal, topic, date, or read-status filters to bring more papers back into view."
              actions={
                <Button
                  size="sm"
                  variant="outline"
                  onPress={() => {
                    setTopic("all");
                    setJournal("all");
                    setDateFilter("30d");
                    setReadFilter("unread");
                    setRelevance("all");
                    setQuery("");
                  }}
                >
                  Reset filters
                </Button>
              }
            />
          ) : (
            <div className="overflow-hidden rounded-lg border border-[var(--line)] bg-[var(--paper)] px-1 py-1">
              <Virtuoso
                className="min-h-[420px]"
                computeItemKey={(index) => visibleList[index].id}
                increaseViewportBy={{bottom: 480, top: 240}}
                initialTopMostItemIndex={0}
                style={{height: "calc(100vh - 13rem)"}}
                totalCount={visibleList.length}
                itemContent={(index) => {
                  const paper = visibleList[index];
                  return (
                    <div className="px-1 py-1.5">
                      <PaperCard
                        paper={paper}
                        profile={profile}
                        isSelected={paper.id === selectedId}
                        isUnread={!paper.read_at}
                        onSelect={() => selectPaper(paper)}
                      />
                    </div>
                  );
                }}
              />
            </div>
          )}
        </section>

        <DetailPanel
          paper={needsFeedSetup ? null : selectedPaper}
          profile={profile}
          isUnread={Boolean(selectedPaper && !selectedPaper.read_at)}
          onMarkRead={() => selectedPaper && void persistReadStatus(selectedPaper.id)}
          onMarkWrong={() => selectedPaper && openFeedbackModal(selectedPaper)}
          onSave={() => selectedPaper && openZoteroModal(selectedPaper)}
        />
      </div>
    </main>
  );
}
