import {Button, Card, Spinner} from "@heroui/react";
import React from "react";

import {CheckboxRow, TextAreaField, TextInputField} from "../../shared/components/FormFields";
import type {AppMeta, AppUpdate, FeedSubscription, JobInfo} from "../../shared/types";

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
    <div className="mt-3 rounded-md border border-(--line) bg-(--paper) p-3 text-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="font-semibold text-(--ink)">Latest job</p>
          <p className="mt-1 text-muted">{job.job_type} · {job.status}</p>
        </div>
        <div className="text-left text-muted sm:text-right">
          <p>Started: {formatJobTime(job.started_at)}</p>
          <p>Finished: {formatJobTime(job.finished_at)}</p>
        </div>
      </div>
      {job.job_type === "sync" ? (
        <dl className="mt-3 grid gap-2 text-muted sm:grid-cols-5">
          {(["fetched", "inserted", "updated", "classified"] as const).map((key) => (
            <div key={key} className="rounded-md border border-(--line) px-3 py-2">
              <dt className="capitalize">{key}</dt>
              <dd className="mt-1 font-semibold text-(--ink)">{jobResultNumber(job, key)}</dd>
            </div>
          ))}
          <div className="rounded-md border border-(--line) px-3 py-2">
            <dt>Warnings</dt>
            <dd className="mt-1 font-semibold text-(--ink)">{job.warning_count ?? errors.length}</dd>
          </div>
        </dl>
      ) : null}
      {job.error ? <p className="mt-3 text-rose-700">{job.error}</p> : null}
      {errors.length > 0 ? (
        <div className="mt-3 space-y-2">
          <p className="font-medium text-(--ink)">Warnings</p>
          {errors.map((item, index) => (
            <div key={`${item.url}-${index}`} className="rounded-md border border-(--line) px-3 py-2">
              <p className="font-medium text-(--ink)">{item.label}</p>
              {item.url ? <p className="break-all text-xs text-muted">{item.url}</p> : null}
              <p className="mt-1 leading-6 text-(--body)">{item.detail}</p>
            </div>
          ))}
        </div>
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
  appMeta,
  appUpdate,
  appUpdateChecking,
  feeds,
  hasFeeds,
  jobs,
  onCheckForUpdates,
  onOpenVerificationInBrowser,
  onReclassifyAll,
  onReclassifyFeedback,
  onReclassifyRecent,
  onRunSync,
  onStartVerification,
  onSubmitVerificationXML,
  verificationSubmitting,
  verificationSubmitError,
}: {
  appMeta: AppMeta | null;
  appUpdate: AppUpdate | null;
  appUpdateChecking: boolean;
  feeds: FeedSubscription[];
  hasFeeds: boolean;
  jobs: JobInfo[];
  onCheckForUpdates: () => void;
  onOpenVerificationInBrowser: (job: JobInfo) => void;
  onReclassifyAll: () => void;
  onReclassifyFeedback: () => void;
  onReclassifyRecent: () => void;
  onRunSync: (feedURLs?: string[]) => void;
  onStartVerification: (job: JobInfo) => void;
  onSubmitVerificationXML: (job: JobInfo, xml: string) => Promise<void> | void;
  verificationSubmitting: boolean;
  verificationSubmitError: string | null;
}) {
  const [verificationXML, setVerificationXML] = React.useState("");
  const [syncFeedQuery, setSyncFeedQuery] = React.useState("");
  const [selectedSyncFeedURLs, setSelectedSyncFeedURLs] = React.useState<string[]>([]);
  const latestJob = jobs[0] ?? null;
  const activeSyncJob = jobs.find((job) => job.job_type === "sync" && ["queued", "running", "waiting_for_user"].includes(job.status)) ?? null;
  const verificationJob = jobs.find((job) => job.status === "waiting_for_user" && job.verification_required) ?? null;
  const lastCheckedLabel = appUpdate && !Number.isNaN(Date.parse(appUpdate.checked_at))
    ? new Date(appUpdate.checked_at).toLocaleString()
    : null;
  const savedSyncFeedURLs = React.useMemo(() => feeds.map((feed) => feed.url.trim()).filter(Boolean), [feeds]);
  const syncFeedMatches = React.useMemo(() => {
    const query = syncFeedQuery.trim().toLowerCase();
    return feeds.filter((feed) => feed.url.trim() && (!query || `${feed.journal} ${feed.url}`.toLowerCase().includes(query)));
  }, [feeds, syncFeedQuery]);

  React.useEffect(() => setVerificationXML(""), [verificationJob?.id]);
  React.useEffect(() => {
    const savedURLs = new Set(savedSyncFeedURLs);
    setSelectedSyncFeedURLs((current) => current.filter((url) => savedURLs.has(url)));
  }, [savedSyncFeedURLs]);

  const runSync = () => {
    const selected = selectedSyncFeedURLs.filter((url) => savedSyncFeedURLs.includes(url));
    onRunSync(selected.length > 0 && selected.length < savedSyncFeedURLs.length ? selected : undefined);
  };

  return (
    <div className="mt-5 space-y-5">
      <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
        <Card.Header><h3 className="text-xl font-semibold text-(--ink)">Dashboard</h3></Card.Header>
        <Card.Content className="space-y-6">
          <section className="border-b border-(--line) py-5">
            {!hasFeeds ? <p className="mb-3 text-sm leading-6 text-muted">Add and save at least one RSS feed before running a manual sync.</p> : null}
            <div className="flex flex-wrap gap-2">
              <Button isDisabled={!hasFeeds || Boolean(activeSyncJob)} size="sm" onPress={runSync}>{activeSyncJob ? "Sync running" : "Sync"}</Button>
              <Button size="sm" variant="outline" onPress={onReclassifyRecent}>Reclassify recent 50</Button>
              <Button size="sm" variant="outline" onPress={onReclassifyFeedback}>Reclassify feedback papers</Button>
              <Button size="sm" variant="ghost" onPress={onReclassifyAll}>Reclassify all</Button>
            </div>
            {hasFeeds ? (
              <div className="mt-4 w-full">
                <div className="flex flex-wrap items-end gap-2">
                  <TextInputField hideLabel className="min-w-60 flex-1" label="Search feeds for targeted sync" placeholder="Search feeds" value={syncFeedQuery} onChange={setSyncFeedQuery} />
                  <Button size="sm" variant="outline" onPress={() => setSelectedSyncFeedURLs(savedSyncFeedURLs)}>Select all</Button>
                  <Button isDisabled={selectedSyncFeedURLs.length === 0} size="sm" variant="ghost" onPress={() => setSelectedSyncFeedURLs([])}>Clear</Button>
                </div>
                <div className="mt-3 max-h-52 space-y-2 overflow-y-auto pr-1">
                  {syncFeedMatches.length === 0 ? <p className="rounded-md border border-(--line) px-3 py-3 text-sm text-muted">No feed matches.</p> : syncFeedMatches.map((feed) => {
                    const url = feed.url.trim();
                    return (
                      <CheckboxRow key={feed.client_id ?? url} checked={selectedSyncFeedURLs.includes(url)} className="rounded-md border border-(--line) px-3 py-2 text-sm" onChange={() => setSelectedSyncFeedURLs((current) => current.includes(url) ? current.filter((item) => item !== url) : [...current, url])}>
                        <span className="min-w-0"><span className="font-medium text-(--ink)">{feed.journal}</span><span className="mt-1 block break-all text-xs text-muted">{feed.url}</span></span>
                      </CheckboxRow>
                    );
                  })}
                </div>
              </div>
            ) : null}
          </section>
          <section className="border-b border-(--line) pb-5">
            <h3 className="text-sm font-semibold text-(--ink)">Service</h3>
            <p className="mt-2 text-sm leading-6 text-muted">
              {appMeta ? appMeta.process_running ? "Backend process is running." : "Backend process state is unknown." : "Release metadata is unavailable."}
              {appMeta?.server_url ? <span className="ml-2">{appMeta.server_url}</span> : null}
            </p>
            {latestJob ? <LatestJobPanel feeds={feeds} job={latestJob} /> : <p className="mt-3 text-sm text-muted">No tracked jobs yet.</p>}
          </section>
          <section>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h3 className="text-sm font-semibold text-(--ink)">Runtime</h3>
              {appMeta ? <span className="text-sm text-muted">{appMeta.name} v{appMeta.version} · {appMeta.mode}</span> : null}
            </div>
            {appMeta ? (
              <div className="mt-3 space-y-2 text-sm leading-6 text-muted">
                <p className="break-all">Server: <code>{appMeta.server_url ?? "Unavailable"}</code></p>
                <p className="break-all">Install dir: <code>{appMeta.install_dir}</code></p>
                <p className="break-all">Static dir: <code>{appMeta.static_dir}</code></p>
                <p className="break-all">Data: <code>{appMeta.data_dir}</code></p>
                <p className="break-all">Logs: <code>{appMeta.logs_dir}</code></p>
                <p className="break-all">Config: <code>{appMeta.config_dir ?? "Unavailable"}</code></p>
              </div>
            ) : <p className="mt-3 text-sm text-muted">Release metadata is unavailable.</p>}
          </section>
          <section className="border-t border-(--line) pt-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h3 className="text-sm font-semibold text-(--ink)">Update check</h3>
              <Button isDisabled={appUpdateChecking} size="sm" variant="outline" onPress={onCheckForUpdates}>
                <span className="inline-flex items-center gap-2">{appUpdateChecking ? <Spinner color="current" size="sm" /> : null}{appUpdateChecking ? "Checking..." : "Check for updates"}</span>
              </Button>
            </div>
            {appUpdate ? (
              <div className="mt-3 text-sm leading-6 text-muted">
                <p>Status: {appUpdate.status}</p>
                {appUpdate.latest_version ? <p>Latest version: {appUpdate.latest_version}</p> : null}
                {lastCheckedLabel ? <p>Last checked: {lastCheckedLabel}</p> : null}
                {appUpdate.detail ? <p>{appUpdate.detail}</p> : null}
                <div className="mt-3 flex flex-wrap gap-2">
                  {appUpdate.download_url ? <a className="rounded-md border border-(--line) px-3 py-2 text-sm text-(--ink)" href={appUpdate.download_url} rel="noreferrer" target="_blank">Download installer</a> : null}
                  {appUpdate.release_notes_url ? <a className="rounded-md border border-(--line) px-3 py-2 text-sm text-(--ink)" href={appUpdate.release_notes_url} rel="noreferrer" target="_blank">Release notes</a> : null}
                </div>
              </div>
            ) : <p className="mt-3 text-sm text-muted">Update information is unavailable.</p>}
          </section>
          {verificationJob ? (
            <VerificationPanel job={verificationJob} submitting={verificationSubmitting} submitError={verificationSubmitError} xml={verificationXML} onXMLChange={setVerificationXML} onOpenInBrowser={() => onOpenVerificationInBrowser(verificationJob)} onReopen={() => onStartVerification(verificationJob)} onSubmitXML={() => void onSubmitVerificationXML(verificationJob, verificationXML)} />
          ) : null}
        </Card.Content>
      </Card>
    </div>
  );
}
