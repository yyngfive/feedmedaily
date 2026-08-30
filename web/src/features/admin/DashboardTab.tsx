import {Button, Chip, Spinner} from "@heroui/react";
import React from "react";

import {fetchLLMUsage, fetchReclassifyOptions, type ReclassifyOptions, type ReclassifyScope} from "../../api/client";
import {CheckboxRow, TextAreaField, TextInputField} from "../../shared/components/FormFields";
import type {FeedSubscription, JobInfo, LLMUsageRecord, LLMUsageSummary} from "../../shared/types";
import {AdminDisclosure} from "./AdminDisclosure";

function formatJobTime(value?: string | null) {
  if (!value || Number.isNaN(Date.parse(value))) {
    return "Not available";
  }
  return new Date(value).toLocaleString();
}

function jobResultNumber(job: JobInfo, key: string) {
  const value = job.result?.[key];
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function jobResultErrors(job: JobInfo) {
  const value = job.result?.errors;
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string" && item.trim().length > 0)
    : [];
}

function formatTokenCount(value: number) {
  return new Intl.NumberFormat().format(value);
}

function usageCostLabel(usage: Pick<LLMUsageSummary, "estimated_cost_cny" | "pricing_status">) {
  return usage.pricing_status === "estimated" && usage.estimated_cost_cny
    ? `Estimated ¥${usage.estimated_cost_cny}`
    : "Cost unavailable";
}

function LatestJobPanel({feeds, job}: {feeds: FeedSubscription[]; job: JobInfo}) {
  const feedNames = new Map(feeds.map((feed) => [feed.url.trim(), feed.journal.trim()]));
  const errors = jobResultErrors(job).map((value) => {
    const divider = value.indexOf(": ");
    const url = divider < 0 ? "" : value.slice(0, divider).trim();
    return {
      detail: divider < 0 ? value.trim() : value.slice(divider + 2).trim(),
      label: url ? feedNames.get(url) || url : "Other warning",
      url,
    };
  });

  return (
    <div className="mt-3 text-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <p className="font-medium text-(--ink)">{job.job_type} · {job.status}</p>
        <div className="text-left text-muted sm:text-right">
          <p>Started: {formatJobTime(job.started_at)}</p>
          <p>Finished: {formatJobTime(job.finished_at)}</p>
        </div>
      </div>
      {job.llm_usage ? (
        <p className="mt-3 text-sm text-muted">
          {usageCostLabel(job.llm_usage)} · hit {formatTokenCount(job.llm_usage.prompt_cache_hit_tokens)} · miss {formatTokenCount(job.llm_usage.prompt_cache_miss_tokens)} · output {formatTokenCount(job.llm_usage.completion_tokens)}
        </p>
      ) : null}
      {job.job_type === "sync" ? (
        <dl className="mt-3 grid grid-cols-2 gap-x-6 gap-y-3 border-y border-(--line) py-3 text-muted sm:grid-cols-5">
          {(["fetched", "inserted", "updated", "classified"] as const).map((key) => (
            <div key={key}>
              <dt className="text-xs capitalize">{key}</dt>
              <dd className="mt-1 font-semibold text-(--ink)">{jobResultNumber(job, key)}</dd>
            </div>
          ))}
          <div>
            <dt className="text-xs">Warnings</dt>
            <dd className="mt-1 font-semibold text-(--ink)">{job.warning_count ?? errors.length}</dd>
          </div>
        </dl>
      ) : null}
      {job.error ? <p className="mt-3 text-rose-700">{job.error}</p> : null}
      {errors.length > 0 ? (
        <div className="mt-3"><AdminDisclosure title={`Warnings (${errors.length})`}>
          <div className="space-y-3">{errors.map((item, index) => (
            <div key={`${item.url}-${index}`}>
              <p className="font-medium text-(--ink)">{item.label}</p>
              {item.url ? <p className="break-all text-xs text-muted">{item.url}</p> : null}
              <p className="mt-1 leading-6 text-(--body)">{item.detail}</p>
            </div>
          ))}</div>
        </AdminDisclosure></div>
      ) : job.message ? (
        <p className="mt-3 leading-6 text-(--body)">{job.message}</p>
      ) : null}
    </div>
  );
}

function VerificationPanel({
  job,
  onOpenInBrowser,
  onReopen,
  onSubmitXML,
  submitting,
  submitError,
  xml,
  onXMLChange,
}: {
  job: JobInfo;
  onOpenInBrowser: () => void;
  onReopen: () => void;
  onSubmitXML: () => void;
  submitting: boolean;
  submitError: string | null;
  xml: string;
  onXMLChange: (value: string) => void;
}) {
  return (
    <section className="rounded-md border border-amber-300/70 bg-amber-50 px-3 py-3 text-sm text-amber-950">
      <p className="font-semibold">
        {(job.verification_journal?.trim() || "This feed")} needs manual verification
      </p>
      <p className="mt-1 leading-6">
        If Cloudflare keeps looping, open the feed in your browser, finish the check, and paste the final RSS/XML here.
      </p>
      <div className="mt-3 flex flex-wrap gap-2">
        <Button size="sm" variant="secondary" onPress={onReopen}>Reopen verification window</Button>
        <Button size="sm" variant="outline" onPress={onOpenInBrowser}>Open in browser</Button>
        {job.verification_feed_url ? (
          <code className="self-center break-all text-xs text-amber-900/80">{job.verification_feed_url}</code>
        ) : null}
      </div>
      <div className="mt-3 space-y-2">
        <label className="block text-xs font-semibold uppercase tracking-[0.14em] text-amber-900/80">
          Paste final RSS/XML
        </label>
        <TextAreaField
          hideLabel
          className="font-mono text-xs leading-5"
          label="Paste final RSS/XML"
          placeholder="Paste the raw RSS, Atom, or RDF XML source here."
          rows={8}
          value={xml}
          onChange={onXMLChange}
        />
        {submitError ? (
          <p className="text-sm leading-6 text-rose-700">{submitError}</p>
        ) : (
          <p className="text-xs leading-5 text-amber-900/80">
            The sync stays paused until valid XML is submitted or the job times out.
          </p>
        )}
        <Button isDisabled={submitting || !xml.trim()} size="sm" onPress={onSubmitXML}>
          <span className="inline-flex items-center gap-2">
            {submitting ? <Spinner color="current" size="sm" /> : null}
            Submit XML
          </span>
        </Button>
      </div>
    </section>
  );
}

export function DashboardTab({
  feeds,
  hasFeeds,
  jobs,
  onOpenVerificationInBrowser,
  onReclassify,
  onRunSync,
  onStopJob,
  onStartVerification,
  onSubmitVerificationXML,
  verificationSubmitting,
  verificationSubmitError,
}: {
  feeds: FeedSubscription[];
  hasFeeds: boolean;
  jobs: JobInfo[];
  onOpenVerificationInBrowser: (job: JobInfo) => void;
  onReclassify: (scope: ReclassifyScope, limit?: number) => Promise<void> | void;
  onRunSync: (feedURLs?: string[]) => void;
  onStopJob: (jobID: string, jobType: "sync" | "reclassify") => Promise<void> | void;
  onStartVerification: (job: JobInfo) => void;
  onSubmitVerificationXML: (job: JobInfo, xml: string) => Promise<void> | void;
  verificationSubmitting: boolean;
  verificationSubmitError: string | null;
}) {
  const [verificationXML, setVerificationXML] = React.useState("");
  const [syncFeedQuery, setSyncFeedQuery] = React.useState("");
  const [selectedSyncFeedURLs, setSelectedSyncFeedURLs] = React.useState<string[]>([]);
  const [llmUsage, setLLMUsage] = React.useState<LLMUsageRecord[]>([]);
  const [llmUsageError, setLLMUsageError] = React.useState<string | null>(null);
  const [stoppingSync, setStoppingSync] = React.useState(false);
  const [reclassifyScope, setReclassifyScope] = React.useState<ReclassifyScope>("today");
  const [reclassifyLimit, setReclassifyLimit] = React.useState("0");
  const [reclassifyOptions, setReclassifyOptions] = React.useState<ReclassifyOptions | null>(null);
  const [reclassifyOptionsError, setReclassifyOptionsError] = React.useState<string | null>(null);
  const [reclassifying, setReclassifying] = React.useState(false);
  const [stoppingReclassify, setStoppingReclassify] = React.useState(false);
  const latestJob = jobs[0] ?? null;
  const activeSyncJob = jobs.find((job) => job.job_type === "sync" && ["queued", "running", "waiting_for_user"].includes(job.status)) ?? null;
  const activeReclassifyJob = jobs.find((job) => job.job_type === "reclassify" && ["queued", "running"].includes(job.status)) ?? null;
  const verificationJob = jobs.find((job) => job.status === "waiting_for_user" && job.verification_required) ?? null;
  const savedSyncFeedURLs = React.useMemo(() => feeds.map((feed) => feed.url.trim()).filter(Boolean), [feeds]);
  const syncFeedMatches = React.useMemo(() => {
    const query = syncFeedQuery.trim().toLowerCase();
    return feeds.filter((feed) => feed.url.trim() && (!query || `${feed.journal} ${feed.url}`.toLowerCase().includes(query)));
  }, [feeds, syncFeedQuery]);

  React.useEffect(() => setVerificationXML(""), [verificationJob?.id]);
  React.useEffect(() => {
    let cancelled = false;
    const since = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString();
    void fetchLLMUsage(since).then((items) => {
      if (!cancelled) { setLLMUsage(items); setLLMUsageError(null); }
    }).catch(() => {
      if (!cancelled) setLLMUsageError("Could not load LLM usage.");
    });
    return () => { cancelled = true; };
  }, [latestJob?.finished_at]);
  React.useEffect(() => {
    let cancelled = false;
    void fetchReclassifyOptions().then((options) => {
      if (cancelled) return;
      setReclassifyOptions(options);
      setReclassifyOptionsError(null);
      setReclassifyLimit((current) => String(Math.min(Number.parseInt(current, 10) || 0, options.paper_count)));
    }).catch(() => {
      if (!cancelled) setReclassifyOptionsError("Could not load the database paper count.");
    });
    return () => { cancelled = true; };
  }, [latestJob?.finished_at]);
  React.useEffect(() => {
    const savedURLs = new Set(savedSyncFeedURLs);
    setSelectedSyncFeedURLs((current) => current.filter((url) => savedURLs.has(url)));
  }, [savedSyncFeedURLs]);

  const runSync = () => {
    const selected = selectedSyncFeedURLs.filter((url) => savedSyncFeedURLs.includes(url));
    onRunSync(selected.length > 0 && selected.length < savedSyncFeedURLs.length ? selected : undefined);
  };

  const stopSync = () => {
    if (!activeSyncJob || activeSyncJob.cancel_requested || stoppingSync) return;
    setStoppingSync(true);
    void Promise.resolve(onStopJob(activeSyncJob.id, "sync")).catch(() => undefined).finally(() => setStoppingSync(false));
  };

  const stopReclassify = () => {
    if (!activeReclassifyJob || activeReclassifyJob.cancel_requested || stoppingReclassify) return;
    setStoppingReclassify(true);
    void Promise.resolve(onStopJob(activeReclassifyJob.id, "reclassify")).catch(() => undefined).finally(() => setStoppingReclassify(false));
  };

  const parsedReclassifyLimit = Number.parseInt(reclassifyLimit, 10);
  const reclassifyLimitIsInteger = /^\d+$/.test(reclassifyLimit.trim());
  const reclassifyPaperCount = reclassifyOptions?.paper_count ?? null;
  const reclassifyLimitValid = reclassifyScope !== "count" || (
    reclassifyPaperCount !== null
    && reclassifyLimitIsInteger
    && Number.isInteger(parsedReclassifyLimit)
    && parsedReclassifyLimit >= 0
    && parsedReclassifyLimit <= reclassifyPaperCount
  );
  React.useEffect(() => {
    // 指定数量范围需要按最新入库拆分已分类/未分类，随输入防抖刷新。
    if (reclassifyScope !== "count" || !reclassifyLimitValid) return;
    const timer = window.setTimeout(() => {
      fetchReclassifyOptions(parsedReclassifyLimit).then((options) => {
        setReclassifyOptions(options);
      }).catch(() => undefined);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [reclassifyScope, reclassifyLimitValid, parsedReclassifyLimit]);
  const reclassifyPreview = (() => {
    if (reclassifyScope === "feedback") {
      return "Papers linked to your profile feedback will be reclassified.";
    }
    if (reclassifyOptions === null) {
      return null;
    }
    if (reclassifyScope === "today") {
      const total = reclassifyOptions.today_paper_count;
      return `${total} papers entered today: ${reclassifyOptions.today_classified_count} classified, ${reclassifyOptions.today_unclassified_count} unclassified. This job will process all ${total}.`;
    }
    if (reclassifyScope === "all") {
      const total = reclassifyOptions.paper_count;
      return `${total} papers in the database: ${reclassifyOptions.classified_paper_count} classified, ${reclassifyOptions.unclassified_paper_count} unclassified. This job will process all ${total}.`;
    }
    if (reclassifyScope === "unclassified") {
      const total = reclassifyOptions.unclassified_paper_count;
      return `${total} papers have no classification yet. This job will process all ${total}.`;
    }
    const total = reclassifyOptions.count_paper_count;
    return `${total} newest papers selected: ${reclassifyOptions.count_classified_count} classified, ${reclassifyOptions.count_unclassified_count} unclassified. This job will process all ${total}.`;
  })();
  const confirmReclassify = () => {
    if (!reclassifyLimitValid || reclassifying || activeReclassifyJob) return;
    setReclassifying(true);
    const limit = reclassifyScope === "count" ? parsedReclassifyLimit : 0;
    void Promise.resolve(onReclassify(reclassifyScope, limit)).finally(() => setReclassifying(false));
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-(--line) pb-4">
        <h2 className="text-xl font-semibold text-(--ink)">Dashboard</h2>
        {activeSyncJob ? <Chip color="warning" size="sm" variant="soft">Sync · {activeSyncJob.status}</Chip> : <Chip color="success" size="sm" variant="soft">Ready</Chip>}
      </div>

      {verificationJob ? <VerificationPanel job={verificationJob} submitting={verificationSubmitting} submitError={verificationSubmitError} xml={verificationXML} onXMLChange={setVerificationXML} onOpenInBrowser={() => onOpenVerificationInBrowser(verificationJob)} onReopen={() => onStartVerification(verificationJob)} onSubmitXML={() => void onSubmitVerificationXML(verificationJob, verificationXML)} /> : null}

      <section className="border-b border-(--line) pb-6">
        <h3 className="text-sm font-semibold text-(--ink)">Sync</h3>
        {!hasFeeds ? <p className="mt-2 text-sm leading-6 text-muted">Add and save at least one RSS feed before running a manual sync.</p> : null}
        <div className="mt-3 flex flex-wrap gap-2">
          <Button isDisabled={!hasFeeds || Boolean(activeSyncJob) || Boolean(activeReclassifyJob)} size="sm" onPress={runSync}>{activeSyncJob ? "Sync running" : activeReclassifyJob ? "Reclassification running" : "Sync now"}</Button>
          {activeSyncJob ? <Button isDisabled={stoppingSync || Boolean(activeSyncJob.cancel_requested)} size="sm" variant="danger" onPress={stopSync}>{stoppingSync || activeSyncJob.cancel_requested ? "Stopping…" : "Stop sync"}</Button> : null}
        </div>
        {hasFeeds ? (
          <div className="mt-4"><AdminDisclosure title="Target specific feeds">
              <div className="flex flex-wrap items-end gap-2">
                <TextInputField hideLabel className="min-w-60 flex-1" label="Search feeds for targeted sync" placeholder="Search feeds" value={syncFeedQuery} onChange={setSyncFeedQuery} />
                <Button size="sm" variant="outline" onPress={() => setSelectedSyncFeedURLs(savedSyncFeedURLs)}>Select all</Button>
                <Button isDisabled={selectedSyncFeedURLs.length === 0} size="sm" variant="ghost" onPress={() => setSelectedSyncFeedURLs([])}>Clear</Button>
              </div>
              <div className="mt-2 max-h-52 space-y-0.5 overflow-y-auto pr-1">
                {syncFeedMatches.length === 0 ? <p className="rounded-md border border-(--line) px-3 py-3 text-sm text-muted">No feed matches.</p> : syncFeedMatches.map((feed) => {
                  const url = feed.url.trim();
                  return <CheckboxRow key={feed.client_id ?? url} checked={selectedSyncFeedURLs.includes(url)} className="rounded-md px-2 py-1 text-sm" onChange={() => setSelectedSyncFeedURLs((current) => current.includes(url) ? current.filter((item) => item !== url) : [...current, url])}><span className="font-medium text-(--ink)">{feed.journal}</span></CheckboxRow>;
                })}
              </div>
          </AdminDisclosure></div>
        ) : null}
        <div className="mt-4"><AdminDisclosure title="Reclassify papers">
          <div className="space-y-3">
            <div className="flex flex-wrap gap-2" role="group" aria-label="Reclassification range">
              {([
                ["today", "Today"],
                ["feedback", "Feedback papers"],
                ["all", "All papers"],
                ["count", "Specific count"],
                ["unclassified", "Unclassified papers"],
              ] as Array<[ReclassifyScope, string]>).map(([scope, label]) => (
                <Button key={scope} aria-pressed={reclassifyScope === scope} size="sm" variant={reclassifyScope === scope ? "secondary" : "outline"} onPress={() => setReclassifyScope(scope)}>{label}</Button>
              ))}
            </div>
            {reclassifyScope === "count" ? (
              <TextInputField
                className="max-w-72"
                description={reclassifyPaperCount === null ? "Loading database paper count…" : `Enter 0–${reclassifyPaperCount}. Newest papers are selected first.`}
                disabled={reclassifyPaperCount === null}
                inputMode="numeric"
                label="Number of papers"
                type="number"
                value={reclassifyLimit}
                onChange={setReclassifyLimit}
              />
            ) : null}
            {reclassifyOptionsError ? <p className="text-sm text-rose-700">{reclassifyOptionsError}</p> : null}
            {!reclassifyLimitValid ? <p className="text-sm text-rose-700">Enter a whole number from 0 to {reclassifyPaperCount ?? 0}.</p> : null}
            {reclassifyPreview ? <p className="text-sm leading-6 text-muted">{reclassifyPreview}</p> : null}
            <div className="flex flex-wrap gap-2">
              <Button isDisabled={!reclassifyLimitValid || reclassifying || Boolean(activeReclassifyJob) || Boolean(activeSyncJob)} size="sm" onPress={confirmReclassify}>
                {reclassifying || activeReclassifyJob ? "Reclassifying…" : activeSyncJob ? "Sync running" : "Confirm reclassification"}
              </Button>
              {activeReclassifyJob ? (
                <Button isDisabled={stoppingReclassify || Boolean(activeReclassifyJob.cancel_requested)} size="sm" variant="danger" onPress={stopReclassify}>
                  {stoppingReclassify || activeReclassifyJob.cancel_requested ? "Stopping…" : "Stop reclassification"}
                </Button>
              ) : null}
            </div>
          </div>
        </AdminDisclosure></div>
      </section>

      <section className="border-b border-(--line) pb-6">
        <h3 className="text-sm font-semibold text-(--ink)">Latest activity</h3>
        {latestJob ? <LatestJobPanel feeds={feeds} job={latestJob} /> : <p className="mt-3 text-sm text-muted">No tracked jobs yet.</p>}
      </section>

      <AdminDisclosure title="Cost and usage · last 3 days">
          {llmUsageError ? <p className="text-sm text-rose-700">{llmUsageError}</p> : llmUsage.length === 0 ? <p className="text-sm text-muted">No completed LLM jobs in this window.</p> : (
            <div className="overflow-x-auto"><table className="w-full min-w-[45rem] text-left text-xs"><thead className="border-b border-(--line) text-muted"><tr><th className="py-2 pr-3 font-medium">Time</th><th className="py-2 pr-3 font-medium">Job</th><th className="py-2 pr-3 font-medium">Model</th><th className="py-2 pr-3 font-medium">Requests</th><th className="py-2 pr-3 font-medium">Tokens · hit / miss / output</th><th className="py-2 font-medium">Cost</th></tr></thead><tbody>{llmUsage.map((item) => <tr key={item.job_id} className="border-b border-(--line)/70 last:border-0"><td className="py-2 pr-3 whitespace-nowrap text-muted">{formatJobTime(item.completed_at)}</td><td className="py-2 pr-3 text-(--ink)">{item.job_type} · {item.status}</td><td className="py-2 pr-3 text-muted">{item.model || "Unknown"}</td><td className="py-2 pr-3 text-muted">{formatTokenCount(item.request_count)}</td><td className="py-2 pr-3 whitespace-nowrap text-muted">{formatTokenCount(item.prompt_cache_hit_tokens)} / {formatTokenCount(item.prompt_cache_miss_tokens)} / {formatTokenCount(item.completion_tokens)}</td><td className="py-2 whitespace-nowrap text-(--ink)">{usageCostLabel(item)}</td></tr>)}</tbody></table></div>
          )}
      </AdminDisclosure>
    </div>
  );
}
