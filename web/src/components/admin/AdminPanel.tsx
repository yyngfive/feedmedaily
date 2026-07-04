import { Button, Card, Input, Spinner } from "@heroui/react";
import React from "react";

import { relevanceLabel } from "../../app/constants";
import { feedCatalog } from "../../feedCatalog";
import type { SettingsConfigUpdate } from "../../types";
import { ProfileProposalReview } from "../profile/ProfileProposalReview";
import { ProfileRulesDocument } from "../profile/ProfileRulesDocument";
import { SettingsConfigEditor } from "./SettingsConfigEditor";
import type {
  AppMeta,
  AppUpdate,
  ClassificationProfile,
  FeedSubscription,
  FeedbackRecord,
  JobInfo,
  ProfileProposal,
  SchedulerSettings,
  SettingsConfigField,
} from "../../types";

export type AdminTab = "dashboard" | "feeds" | "profile" | "model" | "app";

const adminTabs: Array<{ id: AdminTab; label: string }> = [
  { id: "dashboard", label: "Dashboard" },
  { id: "feeds", label: "Feeds" },
  { id: "profile", label: "Profile" },
  { id: "model", label: "Model" },
  { id: "app", label: "App" },
];

const modelSections = new Set(["Classifier model", "Profile model"]);
const appSections = new Set(["Zotero", "Local app"]);

function fieldsInSections(fields: SettingsConfigField[], sections: Set<string>) {
  return fields.filter((field) => sections.has(field.section));
}

function formatJobTime(value?: string | null) {
  if (!value || Number.isNaN(Date.parse(value))) {
    return "Not available";
  }
  return new Date(value).toLocaleString();
}

function jobResultNumber(job: JobInfo, key: string) {
  const value = job.result?.[key];
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  return 0;
}

function jobResultErrors(job: JobInfo) {
  const value = job.result?.errors;
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter((item): item is string => typeof item === "string" && item.trim().length > 0);
}

function splitJobError(value: string) {
  const divider = value.indexOf(": ");
  if (divider < 0) {
    return { detail: value.trim(), url: "" };
  }
  return {
    detail: value.slice(divider + 2).trim(),
    url: value.slice(0, divider).trim(),
  };
}

function LatestJobPanel({ feeds, job }: { feeds: FeedSubscription[]; job: JobInfo }) {
  const feedNames = new Map(feeds.map((feed) => [feed.url.trim(), feed.journal.trim()]));
  const errors = jobResultErrors(job).map((item) => {
    const parsed = splitJobError(item);
    return {
      detail: parsed.detail,
      label: parsed.url ? feedNames.get(parsed.url) || parsed.url : "Other warning",
      url: parsed.url,
    };
  });
  const isSync = job.job_type === "sync";

  return (
    <div className="mt-3 rounded-md border border-(--line) bg-(--paper) p-3 text-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="font-semibold text-(--ink)">Latest job</p>
          <p className="mt-1 text-muted">
            {job.job_type} · {job.status}
          </p>
        </div>
        <div className="text-left text-muted sm:text-right">
          <p>Started: {formatJobTime(job.started_at)}</p>
          <p>Finished: {formatJobTime(job.finished_at)}</p>
        </div>
      </div>
      {isSync ? (
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
        If Cloudflare keeps looping, open the feed in your browser, finish the check, and paste
        the final RSS/XML here.
      </p>
      <div className="mt-3 flex flex-wrap gap-2">
        <Button size="sm" variant="secondary" onPress={onReopen}>
          Reopen verification window
        </Button>
        <Button size="sm" variant="outline" onPress={onOpenInBrowser}>
          Open in browser
        </Button>
        {job.verification_feed_url ? (
          <code className="self-center break-all text-xs text-amber-900/80">
            {job.verification_feed_url}
          </code>
        ) : null}
      </div>
      <div className="mt-3 space-y-2">
        <label className="block text-xs font-semibold uppercase tracking-[0.14em] text-amber-900/80">
          Paste final RSS/XML
        </label>
        <textarea
          className="min-h-36 w-full rounded-md border border-amber-300/70 bg-white px-3 py-2 font-mono text-xs leading-5 text-amber-950 outline-none transition focus:border-amber-500"
          placeholder="Paste the raw RSS, Atom, or RDF XML source here."
          value={xml}
          onChange={(event) => onXMLChange(event.target.value)}
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

export function AdminPanel({
  activeTab,
  appMeta,
  appUpdate,
  appUpdateChecking,
  configFields,
  configSaving,
  feedback,
  feeds,
  feedsSaving,
  hasFeeds,
  jobs,
  onAddFeed,
  onAddFeeds,
  onApplyProposal,
  onCheckForUpdates,
  onClose,
  onDeleteFeedback,
  onFeedChange,
  onGenerateProposal,
  onStartVerification,
  onOpenVerificationInBrowser,
  onSubmitVerificationXML,
  onReclassifyAll,
  onReclassifyFeedback,
  onReclassifyRecent,
  onRejectProposal,
  onRemoveFeed,
  onRunSync,
  onSaveConfig,
  onSaveProfile,
  onSaveScheduler,
  onSaveFeeds,
  onTabChange,
  onDeleteScheduler,
  open,
  profile,
  profileSaving,
  proposalGenerating,
  proposals,
  scheduler,
  schedulerSaving,
  verificationSubmitting,
  verificationSubmitError,
}: {
  activeTab: AdminTab;
  appMeta: AppMeta | null;
  appUpdate: AppUpdate | null;
  appUpdateChecking: boolean;
  configFields: SettingsConfigField[];
  configSaving: boolean;
  feedback: FeedbackRecord[];
  feeds: FeedSubscription[];
  feedsSaving: boolean;
  hasFeeds: boolean;
  jobs: JobInfo[];
  onAddFeed: () => void;
  onAddFeeds: (feeds: FeedSubscription[]) => void;
  onApplyProposal: (
    id: number,
    selection?: { accepted_change_ids: string[]; rejected_change_ids: string[] },
  ) => Promise<void> | void;
  onCheckForUpdates: () => void;
  onClose: () => void;
  onDeleteFeedback: (id: number) => void;
  onFeedChange: (index: number, field: "journal" | "url", value: string) => void;
  onGenerateProposal: () => void;
  onStartVerification: (job: JobInfo) => void;
  onOpenVerificationInBrowser: (job: JobInfo) => void;
  onSubmitVerificationXML: (job: JobInfo, xml: string) => Promise<void> | void;
  onReclassifyAll: () => void;
  onReclassifyFeedback: () => void;
  onReclassifyRecent: () => void;
  onRejectProposal: (id: number) => void;
  onRemoveFeed: (index: number) => void;
  onRunSync: () => void;
  onSaveConfig: (fields: Record<string, SettingsConfigUpdate>) => Promise<void>;
  onSaveProfile: (profile: ClassificationProfile) => Promise<void> | void;
  onSaveScheduler: (dailyTime: string) => Promise<void>;
  onSaveFeeds: () => void;
  onTabChange: (tab: AdminTab) => void;
  onDeleteScheduler: () => Promise<void>;
  open: boolean;
  profile: ClassificationProfile | null;
  profileSaving: boolean;
  proposalGenerating: boolean;
  proposals: ProfileProposal[];
  scheduler: SchedulerSettings | null;
  schedulerSaving: boolean;
  verificationSubmitting: boolean;
  verificationSubmitError: string | null;
}) {
  const [schedulerTime, setSchedulerTime] = React.useState("10:00");
  const [verificationXML, setVerificationXML] = React.useState("");
  const [newFeedJournal, setNewFeedJournal] = React.useState("");
  const [newFeedURL, setNewFeedURL] = React.useState("");
  const [catalogQuery, setCatalogQuery] = React.useState("");
  const [selectedCatalogURLs, setSelectedCatalogURLs] = React.useState<string[]>([]);
  const pendingProposal = proposals.find((item) => item.state === "pending") ?? null;
  const openFeedback = feedback.filter((item) => item.state === "open");
  const latestJob = jobs[0] ?? null;
  const verificationJob =
    jobs.find((job) => job.status === "waiting_for_user" && job.verification_required) ?? null;
  const schedulerAdvisory = scheduler?.advisory?.trim() ?? "";
  const showSchedulerAdvisory = schedulerAdvisory.length > 0;
  const lastCheckedLabel =
    appUpdate && !Number.isNaN(Date.parse(appUpdate.checked_at))
      ? new Date(appUpdate.checked_at).toLocaleString()
      : null;
  const modelFields = React.useMemo(() => fieldsInSections(configFields, modelSections), [configFields]);
  const appFields = React.useMemo(() => fieldsInSections(configFields, appSections), [configFields]);

  React.useEffect(() => {
    setSchedulerTime(scheduler?.scheduled_time ?? "10:00");
  }, [scheduler?.scheduled_time]);

  React.useEffect(() => {
    setVerificationXML("");
  }, [verificationJob?.id]);

  const existingFeedURLs = React.useMemo(
    () => new Set(feeds.map((feed) => feed.url.trim()).filter(Boolean)),
    [feeds],
  );
  const catalogMatches = React.useMemo(() => {
    const query = catalogQuery.trim().toLowerCase();
    if (!query) {
      return feedCatalog;
    }
    return feedCatalog.filter((item) =>
      `${item.journal} ${item.publisher}`.toLowerCase().includes(query),
    );
  }, [catalogQuery]);
  const selectedCatalogFeeds = React.useMemo(
    () => feedCatalog.filter((item) => selectedCatalogURLs.includes(item.url)),
    [selectedCatalogURLs],
  );
  const addSelectedCatalogFeeds = () => {
    onAddFeeds(selectedCatalogFeeds.map((item) => ({journal: item.journal, url: item.url})));
    setSelectedCatalogURLs([]);
  };
  const toggleCatalogFeed = (url: string) => {
    setSelectedCatalogURLs((current) =>
      current.includes(url) ? current.filter((item) => item !== url) : [...current, url],
    );
  };

  const addDraftFeed = () => {
    const journal = newFeedJournal.trim();
    const url = newFeedURL.trim();
    if (!journal || !url) {
      return;
    }
    const nextIndex = feeds.length;
    onAddFeed();
    window.setTimeout(() => {
      onFeedChange(nextIndex, "journal", journal);
      onFeedChange(nextIndex, "url", url);
    }, 0);
    setNewFeedJournal("");
    setNewFeedURL("");
  };

  if (!open) {
    return null;
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-slate-900/20">
      <aside className="h-full w-full max-w-[min(1040px,96vw)] overflow-auto border-l border-(--line) bg-(--paper) p-5 shadow-xl">
        <div className="w-full">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0 space-y-3">
              <div>
                <h2 className="mt-2 mb-4 text-2xl font-semibold text-(--ink)">Settings</h2>
              </div>
              <div className="flex flex-wrap gap-2">
                {adminTabs.map((tab) => (
                  <Button
                    key={tab.id}
                    size="sm"
                    variant={activeTab === tab.id ? "secondary" : "outline"}
                    onPress={() => onTabChange(tab.id)}
                  >
                    {tab.label}
                  </Button>
                ))}
              </div>
            </div>
            <Button size="sm" variant="ghost" onPress={onClose}>
              Close
            </Button>
          </div>

          <div className="mt-5 space-y-5" hidden={activeTab !== "dashboard"}>
            <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
              <Card.Header>
                <h3 className="text-xl font-semibold text-(--ink)">Dashboard</h3>
              </Card.Header>
              <Card.Content className="space-y-6">

                <section className="border-b border-(--line) py-5">
                  {!hasFeeds ? (
                    <p className="mb-3 text-sm leading-6 text-muted">
                      Add and save at least one RSS feed before running a manual sync.
                    </p>
                  ) : null}
                  <div className="flex flex-wrap gap-2">
                    <Button isDisabled={!hasFeeds} size="sm" onPress={onRunSync}>
                      Sync
                    </Button>
                    <Button size="sm" variant="outline" onPress={onReclassifyRecent}>
                      Reclassify recent 50
                    </Button>
                    <Button size="sm" variant="outline" onPress={onReclassifyFeedback}>
                      Reclassify feedback papers
                    </Button>
                    <Button size="sm" variant="ghost" onPress={onReclassifyAll}>
                      Reclassify all
                    </Button>
                  </div>
                </section>
                <section className="border-b border-(--line) pb-5">
                  <h3 className="text-sm font-semibold text-(--ink)">Service</h3>
                  <p className="mt-2 text-sm leading-6 text-muted">
                    {appMeta
                      ? appMeta.process_running
                        ? "Backend process is running."
                        : "Backend process state is unknown."
                      : "Release metadata is unavailable."}
                    {appMeta?.server_url ? <span className="ml-2">{appMeta.server_url}</span> : null}
                  </p>
                  {latestJob ? (
                    <LatestJobPanel feeds={feeds} job={latestJob} />
                  ) : (
                    <p className="mt-3 text-sm text-muted">No tracked jobs yet.</p>
                  )}
                </section>

                <section>
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <h3 className="text-sm font-semibold text-(--ink)">Runtime</h3>
                    {appMeta ? (
                      <span className="text-sm text-muted">
                        {appMeta.name} v{appMeta.version} · {appMeta.mode}
                      </span>
                    ) : null}
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
                  ) : (
                    <p className="mt-3 text-sm text-muted">Release metadata is unavailable.</p>
                  )}
                </section>

                <section className="border-t border-(--line) pt-5">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <h3 className="text-sm font-semibold text-(--ink)">Update check</h3>
                    <Button
                      isDisabled={appUpdateChecking}
                      size="sm"
                      variant="outline"
                      onPress={onCheckForUpdates}
                    >
                      <span className="inline-flex items-center gap-2">
                        {appUpdateChecking ? <Spinner color="current" size="sm" /> : null}
                        {appUpdateChecking ? "Checking..." : "Check for updates"}
                      </span>
                    </Button>
                  </div>
                  {appUpdate ? (
                    <div className="mt-3 text-sm leading-6 text-muted">
                      <p>Status: {appUpdate.status}</p>
                      {appUpdate.latest_version ? <p>Latest version: {appUpdate.latest_version}</p> : null}
                      {lastCheckedLabel ? <p>Last checked: {lastCheckedLabel}</p> : null}
                      {appUpdate.detail ? <p>{appUpdate.detail}</p> : null}
                      <div className="mt-3 flex flex-wrap gap-2">
                        {appUpdate.download_url ? (
                          <a
                            className="rounded-md border border-(--line) px-3 py-2 text-sm text-(--ink)"
                            href={appUpdate.download_url}
                            rel="noreferrer"
                            target="_blank"
                          >
                            Download installer
                          </a>
                        ) : null}
                        {appUpdate.release_notes_url ? (
                          <a
                            className="rounded-md border border-(--line) px-3 py-2 text-sm text-(--ink)"
                            href={appUpdate.release_notes_url}
                            rel="noreferrer"
                            target="_blank"
                          >
                            Release notes
                          </a>
                        ) : null}
                      </div>
                    </div>
                  ) : (
                    <p className="mt-3 text-sm text-muted">Update information is unavailable.</p>
                  )}
                </section>

                {verificationJob ? (
                  <VerificationPanel
                    job={verificationJob}
                    submitting={verificationSubmitting}
                    submitError={verificationSubmitError}
                    xml={verificationXML}
                    onXMLChange={setVerificationXML}
                    onOpenInBrowser={() => onOpenVerificationInBrowser(verificationJob)}
                    onReopen={() => onStartVerification(verificationJob)}
                    onSubmitXML={() => void onSubmitVerificationXML(verificationJob, verificationXML)}
                  />
                ) : null}
              </Card.Content>
            </Card>
          </div>

          <div className="mt-5 space-y-5" hidden={activeTab !== "feeds"}>
            <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
              <Card.Header>
                <h3 className="text-xl font-semibold text-(--ink)">Feeds</h3>
              </Card.Header>
              <Card.Content className="space-y-5">
                <section className="border-b border-(--line) pb-5">
                  <h3 className="text-sm font-semibold text-(--ink)">Quick add</h3>
                  <div className="mt-3 grid gap-3 lg:grid-cols-[minmax(280px,0.95fr)_minmax(280px,1fr)]">
                    <div className="rounded-md border border-(--line) bg-(--paper) p-3">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <p className="text-sm font-medium text-(--ink)">Catalog</p>
                        <a
                          className="text-sm text-muted underline-offset-3 hover:underline"
                          href="https://github.com/yyngfive/sci-rss-list"
                          rel="noreferrer"
                          target="_blank"
                        >
                          More methods
                        </a>
                      </div>
                      <Input
                        aria-label="Search feed catalog"
                        className="mt-3"
                        placeholder="Search journal or publisher"
                        value={catalogQuery}
                        onChange={(event) => setCatalogQuery(event.target.value)}
                      />
                      <div className="mt-3 max-h-56 space-y-2 overflow-y-auto pr-1">
                        {catalogMatches.length === 0 ? (
                          <p className="rounded-md border border-(--line) px-3 py-3 text-sm text-muted">
                            No catalog matches.
                          </p>
                        ) : (
                          catalogMatches.map((item) => {
                            const selected = selectedCatalogURLs.includes(item.url);
                            const exists = existingFeedURLs.has(item.url.trim());
                            return (
                              <label
                                key={item.url}
                                className="grid cursor-pointer grid-cols-[auto_minmax(0,1fr)] gap-3 rounded-md border border-(--line) px-3 py-2 text-sm"
                              >
                                <input
                                  checked={selected}
                                  className="mt-1"
                                  disabled={exists}
                                  type="checkbox"
                                  onChange={() => toggleCatalogFeed(item.url)}
                                />
                                <span className="min-w-0">
                                  <span className="flex flex-wrap items-center gap-2">
                                    <span className="font-medium text-(--ink)">{item.journal}</span>
                                    <span className="text-xs text-muted">{item.publisher}</span>
                                    {exists ? <span className="text-xs text-muted">Added</span> : null}
                                  </span>
                                  <span className="mt-1 block break-all text-xs text-muted">
                                    {item.url}
                                  </span>
                                </span>
                              </label>
                            );
                          })
                        )}
                      </div>
                      <Button
                        className="mt-3"
                        isDisabled={selectedCatalogFeeds.length === 0}
                        size="sm"
                        onPress={addSelectedCatalogFeeds}
                      >
                        Add selected
                      </Button>
                    </div>
                    <div className="grid gap-3 md:grid-cols-[minmax(150px,0.7fr)_minmax(220px,1fr)_auto] lg:grid-cols-1">
                    <Input
                      aria-label="New feed journal name"
                      placeholder="Journal name"
                      value={newFeedJournal}
                      onChange={(event) => setNewFeedJournal(event.target.value)}
                    />
                    <Input
                      aria-label="New feed URL"
                      placeholder="RSS URL"
                      value={newFeedURL}
                      onChange={(event) => setNewFeedURL(event.target.value)}
                    />
                    <Button
                      isDisabled={!newFeedJournal.trim() || !newFeedURL.trim()}
                      size="sm"
                      onPress={addDraftFeed}
                    >
                      {feeds.length === 0 ? "Add first feed" : "Add feed"}
                    </Button>
                    </div>
                  </div>
                </section>

                <section>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="text-sm font-semibold text-(--ink)">Feed subscriptions</h3>
                    <Button size="sm" isDisabled={feedsSaving} onPress={onSaveFeeds}>
                      {feedsSaving ? "Saving..." : "Save feeds"}
                    </Button>
                  </div>
                  <div className="mt-3 space-y-2">
                    {feeds.length === 0 ? (
                      <p className="rounded-md border border-(--line) px-3 py-4 text-sm text-muted">
                        No RSS feeds configured yet.
                      </p>
                    ) : (
                      <>
                        <div className="hidden grid-cols-[minmax(160px,0.7fr)_minmax(280px,1fr)_96px] gap-3 px-3 text-sm font-semibold text-(--ink) md:grid">
                          <span>Name</span>
                          <span>URL</span>
                          <span>Action</span>
                        </div>
                        <div className="max-h-[58vh] space-y-2 overflow-y-auto pr-1">
                          {feeds.map((item, index) => (
                            <div
                              key={item.client_id ?? String(index)}
                              className="grid gap-3 rounded-md border border-(--line) bg-(--paper) p-3 md:grid-cols-[minmax(160px,0.7fr)_minmax(280px,1fr)_96px]"
                            >
                              <Input
                                aria-label={`Feed name ${index + 1}`}
                                className="w-full"
                                value={item.journal}
                                onChange={(event) => onFeedChange(index, "journal", event.target.value)}
                              />
                              <Input
                                aria-label={`Feed URL ${index + 1}`}
                                className="w-full"
                                value={item.url}
                                onChange={(event) => onFeedChange(index, "url", event.target.value)}
                              />
                              <Button size="sm" variant="ghost" onPress={() => onRemoveFeed(index)}>
                                Delete
                              </Button>
                            </div>
                          ))}
                        </div>
                      </>
                    )}
                  </div>
                </section>
              </Card.Content>
            </Card>
          </div>

          <div className="mt-5 space-y-5" hidden={activeTab !== "profile"}>
            <div className="space-y-4">
              <div className="flex justify-end">
                <Button
                  isDisabled={proposalGenerating}
                  size="sm"
                  variant="secondary"
                  onPress={onGenerateProposal}
                >
                  <span className="inline-flex items-center gap-2">
                    {proposalGenerating ? <Spinner color="current" size="sm" /> : null}
                    Generate profile proposal
                  </span>
                </Button>
              </div>
              {pendingProposal ? (
                profile ? (
                  <ProfileProposalReview
                    proposal={pendingProposal}
                    onApplySelection={onApplyProposal}
                    onRejectProposal={onRejectProposal}
                  />
                ) : (
                  <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
                    <Card.Content className="p-4 text-sm text-muted">
                      No current profile available for proposal review.
                    </Card.Content>
                  </Card>
                )
              ) : profile ? (
                <ProfileRulesDocument
                  editable
                  onSave={onSaveProfile}
                  profile={profile}
                  saving={profileSaving}
                />
              ) : (
                <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
                  <Card.Content className="p-4 text-sm text-muted">No profile available yet.</Card.Content>
                </Card>
              )}

              <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
                <Card.Header className="flex flex-wrap items-center justify-between gap-2">
                  <h3 className="text-sm font-semibold text-(--ink)">Feedback queue</h3>
                  <span className="text-sm text-muted">{openFeedback.length} open</span>
                </Card.Header>
                <Card.Content>
                  <div className="overflow-hidden rounded-md border border-(--line)">
                    {openFeedback.length === 0 ? (
                      <p className="px-3 py-4 text-sm text-muted">No feedback submitted yet.</p>
                    ) : (
                      <table className="w-full border-collapse text-sm">
                        <thead className="bg-(--paper)">
                          <tr className="text-left text-(--ink)">
                            <th className="px-3 py-2 font-semibold">Paper</th>
                            <th className="px-3 py-2 font-semibold">From</th>
                            <th className="px-3 py-2 font-semibold">To</th>
                            <th className="px-3 py-2 font-semibold">Note</th>
                            <th className="px-3 py-2 font-semibold">Created</th>
                            <th className="w-20 px-3 py-2 font-semibold">Action</th>
                          </tr>
                        </thead>
                        <tbody>
                          {openFeedback.map((item) => (
                            <tr key={item.id} className="border-t border-(--line) align-top">
                              <td className="px-3 py-2 text-(--ink)">{item.paper_title}</td>
                              <td className="px-3 py-2 text-muted">
                                {relevanceLabel[item.original_relevance]}
                              </td>
                              <td className="px-3 py-2 text-muted">
                                {relevanceLabel[item.corrected_relevance]}
                              </td>
                              <td className="px-3 py-2 leading-6 text-(--body)">
                                {item.note?.trim() ? item.note : "-"}
                              </td>
                              <td className="px-3 py-2 text-muted">
                                {new Date(item.created_at).toLocaleDateString()}
                              </td>
                              <td className="px-3 py-2">
                                <Button size="sm" variant="ghost" onPress={() => onDeleteFeedback(item.id)}>
                                  Delete
                                </Button>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}
                  </div>
                </Card.Content>
              </Card>
            </div>
          </div>

          <div className="mt-5 space-y-5" hidden={activeTab !== "model"}>
            <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
              <Card.Header>
                <h3 className="text-xl font-semibold text-(--ink)">Model</h3>
              </Card.Header>
              <Card.Content>
                <SettingsConfigEditor
                  fields={modelFields}
                  saving={configSaving}
                  saveLabel="Save model settings"
                  title="Model settings"
                  onSave={onSaveConfig}
                />
              </Card.Content>
            </Card>
          </div>

          <div className="mt-5 space-y-5" hidden={activeTab !== "app"}>
            <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
              <Card.Header>
                <h3 className="text-xl font-semibold text-(--ink)">App</h3>
              </Card.Header>
              <Card.Content className="space-y-6">
                <SettingsConfigEditor
                  fields={appFields}
                  intro={
                    <p>
                      Environment variables override local stored values until the override is removed.
                    </p>
                  }
                  saving={configSaving}
                  saveLabel="Save app settings"
                  title="App settings"
                  onSave={onSaveConfig}
                />

                <section className="border-b border-(--line) pb-5">
                  <h3 className="text-sm font-semibold text-(--ink)">Scheduled sync</h3>
                  {showSchedulerAdvisory ? (
                    <div className="mt-3 rounded-md border border-amber-300 bg-amber-50 px-3 py-3 text-sm text-amber-900">
                      <p className="font-medium">Automatic scheduling is unavailable on this platform.</p>
                      <p className="mt-1 leading-6">{schedulerAdvisory}</p>
                    </div>
                  ) : null}
                  <div className="mt-3 grid gap-3 md:grid-cols-[220px_minmax(0,1fr)]">
                    <label className="block">
                      <span className="text-sm font-medium text-(--ink)">Daily time</span>
                      <input
                        className="mt-2 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink)"
                        type="time"
                        value={schedulerTime}
                        onChange={(event) => setSchedulerTime(event.target.value)}
                      />
                    </label>
                    <div className="rounded-md border border-(--line) p-3 text-sm">
                      {scheduler?.installed ? (
                        <>
                          <p className="text-(--ink)">
                            Enabled as <span className="font-semibold">{scheduler.task_name}</span>
                          </p>
                          <p className="mt-1 text-muted">State: {scheduler.state ?? "Unknown"}</p>
                          <p className="mt-1 text-muted">
                            Next run: {scheduler.next_run_time ? new Date(scheduler.next_run_time).toLocaleString() : "Not scheduled"}
                          </p>
                          <p className="mt-1 text-muted">
                            Last run: {scheduler.last_run_time ? new Date(scheduler.last_run_time).toLocaleString() : "Never"}
                          </p>
                          <p className="mt-1 text-muted">Last result: {scheduler.last_result ?? "Unknown"}</p>
                          {scheduler.command ? (
                            <p className="mt-1 break-all text-muted">
                              Suggested command: <code>{scheduler.command}</code>
                            </p>
                          ) : null}
                        </>
                      ) : (
                        <p className="text-muted">Daily sync is currently disabled.</p>
                      )}
                    </div>
                  </div>
                  <div className="mt-3 flex flex-wrap gap-2">
                    <Button
                      isDisabled={schedulerSaving || !schedulerTime}
                      size="sm"
                      onPress={() => void onSaveScheduler(schedulerTime)}
                    >
                      {schedulerSaving ? "Saving..." : scheduler?.installed ? "Update daily sync" : "Enable daily sync"}
                    </Button>
                    <Button
                      isDisabled={schedulerSaving || !scheduler?.installed}
                      size="sm"
                      variant="ghost"
                      onPress={() => void onDeleteScheduler()}
                    >
                      Remove task
                    </Button>
                  </div>
                </section>

              </Card.Content>
            </Card>
          </div>
        </div>
      </aside>
    </div>
  );
}
