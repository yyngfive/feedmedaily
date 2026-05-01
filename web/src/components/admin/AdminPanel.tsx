import {Button, Input, Spinner} from "@heroui/react";
import React from "react";

import {relevanceLabel} from "../../app/constants";
import type {SettingsConfigUpdate} from "../../types";
import {ProfileProposalPreview} from "../profile/ProfileProposalPreview";
import {ProfileRulesDocument} from "../profile/ProfileRulesDocument";
import {SettingsConfigEditor} from "./SettingsConfigEditor";
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

export type AdminTab = "config" | "feeds" | "profile-feedback";

const adminTabs: Array<{id: AdminTab; label: string}> = [
  {id: "config", label: "Config"},
  {id: "feeds", label: "Feeds"},
  {id: "profile-feedback", label: "Profile + Feedback"},
];

export function AdminPanel({
  activeTab,
  appMeta,
  appUpdate,
  configFields,
  configSaving,
  feedback,
  feeds,
  feedsSaving,
  hasFeeds,
  jobs,
  onAddFeed,
  onApplyProposal,
  onClose,
  onDeleteFeedback,
  onFeedChange,
  onGenerateProposal,
  onReclassifyAll,
  onReclassifyFeedback,
  onReclassifyRecent,
  onRefreshReport,
  onRejectProposal,
  onRemoveFeed,
  onRunFeedSync,
  onSaveConfig,
  onSaveScheduler,
  onSaveFeeds,
  onTabChange,
  onDeleteScheduler,
  open,
  profile,
  proposalGenerating,
  proposals,
  scheduler,
  schedulerSaving,
}: {
  activeTab: AdminTab;
  appMeta: AppMeta | null;
  appUpdate: AppUpdate | null;
  configFields: SettingsConfigField[];
  configSaving: boolean;
  feedback: FeedbackRecord[];
  feeds: FeedSubscription[];
  feedsSaving: boolean;
  hasFeeds: boolean;
  jobs: JobInfo[];
  onAddFeed: () => void;
  onApplyProposal: (id: number) => void;
  onClose: () => void;
  onDeleteFeedback: (id: number) => void;
  onFeedChange: (index: number, field: "journal" | "url", value: string) => void;
  onGenerateProposal: () => void;
  onReclassifyAll: () => void;
  onReclassifyFeedback: () => void;
  onReclassifyRecent: () => void;
  onRefreshReport: () => void;
  onRejectProposal: (id: number) => void;
  onRemoveFeed: (index: number) => void;
  onRunFeedSync: () => void;
  onSaveConfig: (fields: Record<string, SettingsConfigUpdate>) => Promise<void>;
  onSaveScheduler: (dailyTime: string) => Promise<void>;
  onSaveFeeds: () => void;
  onTabChange: (tab: AdminTab) => void;
  onDeleteScheduler: () => Promise<void>;
  open: boolean;
  profile: ClassificationProfile | null;
  proposalGenerating: boolean;
  proposals: ProfileProposal[];
  scheduler: SchedulerSettings | null;
  schedulerSaving: boolean;
}) {
  if (!open) {
    return null;
  }
  const pendingProposal = proposals.find((item) => item.state === "pending") ?? null;
  const openFeedback = feedback.filter((item) => item.state === "open");
  const [schedulerTime, setSchedulerTime] = React.useState("10:00");

  React.useEffect(() => {
    setSchedulerTime(scheduler?.scheduled_time ?? "10:00");
  }, [scheduler?.scheduled_time]);

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-slate-900/20">
      <aside className="h-full w-full max-w-[min(1180px,94vw)] overflow-auto border-l border-(--line) bg-(--paper) p-4 shadow-xl">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-3">
            <div>
              <h2 className="mt-2 text-2xl font-semibold text-(--ink)">Settings</h2>
              <p className="mt-1 text-sm leading-6 text-muted">
                Keep secrets local, manage feeds, and review profile changes from one place.
              </p>
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

        {activeTab === "config" ? (
          <div className="mt-5 space-y-4">
            <section className="rounded-lg border border-(--line) bg-(--paper-accent) p-4">
              <h3 className="text-sm font-semibold text-(--ink)">Actions</h3>
              {!hasFeeds ? (
                <p className="mt-2 text-sm leading-6 text-muted">
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
            </section>

            <SettingsConfigEditor
              fields={configFields}
              intro={
                <p className="text-sm leading-6 text-muted">
                  If a value says it comes from the system environment, that source currently wins
                  over the local stored value.
                </p>
              }
              saving={configSaving}
              onSave={onSaveConfig}
            />

            <section className="rounded-lg border border-(--line) bg-(--paper-accent) p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h3 className="text-sm font-semibold text-(--ink)">Release runtime</h3>
                  <p className="mt-1 text-sm leading-6 text-muted">
                    FeedMeDaily release builds store app data under the per-user data directory and
                    serve the already-built web app locally.
                  </p>
                </div>
              </div>
              {appMeta ? (
                <div className="mt-3 grid gap-3 md:grid-cols-2">
                  <div className="rounded-lg border border-(--line) p-3">
                    <p className="text-xs font-semibold uppercase tracking-[0.16em] text-muted">
                      App
                    </p>
                    <p className="mt-2 text-sm text-(--ink)">
                      {appMeta.name} · v{appMeta.version}
                    </p>
                    <p className="mt-1 text-sm text-muted">Mode: {appMeta.mode}</p>
                    <p className="mt-1 break-all text-sm text-muted">
                      Install dir: <code>{appMeta.install_dir}</code>
                    </p>
                    <p className="mt-1 break-all text-sm text-muted">
                      Static dir: <code>{appMeta.static_dir}</code>
                    </p>
                  </div>
                  <div className="rounded-lg border border-(--line) p-3">
                    <p className="text-xs font-semibold uppercase tracking-[0.16em] text-muted">
                      User data
                    </p>
                    <p className="mt-2 break-all text-sm text-muted">
                      Data: <code>{appMeta.data_dir}</code>
                    </p>
                    <p className="mt-1 break-all text-sm text-muted">
                      Logs: <code>{appMeta.logs_dir}</code>
                    </p>
                    <p className="mt-1 break-all text-sm text-muted">
                      Reports: <code>{appMeta.reports_dir}</code>
                    </p>
                  </div>
                </div>
              ) : (
                <p className="mt-3 text-sm text-muted">Release metadata is unavailable.</p>
              )}
            </section>

            <section className="rounded-lg border border-(--line) bg-(--paper-accent) p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h3 className="text-sm font-semibold text-(--ink)">Update check</h3>
                  <p className="mt-1 text-sm leading-6 text-muted">
                    This build can check an optional remote manifest and surface the next installer
                    download without auto-updating.
                  </p>
                </div>
              </div>
              {appUpdate ? (
                <div className="mt-3 rounded-lg border border-(--line) p-3 text-sm">
                  <p className="text-(--ink)">
                    Current version: <span className="font-semibold">{appUpdate.current_version}</span>
                  </p>
                  <p className="mt-1 text-muted">Status: {appUpdate.status}</p>
                  {appUpdate.latest_version ? (
                    <p className="mt-1 text-muted">Latest version: {appUpdate.latest_version}</p>
                  ) : null}
                  {appUpdate.detail ? <p className="mt-1 text-muted">{appUpdate.detail}</p> : null}
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

            <section className="rounded-lg border border-(--line) bg-(--paper-accent) p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h3 className="text-sm font-semibold text-(--ink)">Scheduled sync</h3>
                  <p className="mt-1 text-sm leading-6 text-muted">
                    FeedMeDaily can use Windows Task Scheduler to run the daily fetch and classify
                    cycle even when the app is closed.
                  </p>
                </div>
              </div>
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
                <div className="rounded-lg border border-(--line) p-3 text-sm">
                  {scheduler?.installed ? (
                    <>
                      <p className="text-(--ink)">
                        Installed as <span className="font-semibold">{scheduler.task_name}</span>
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
                          Command: <code>{scheduler.command}</code>
                        </p>
                      ) : null}
                    </>
                  ) : (
                    <p className="text-muted">
                      No Windows scheduler task is installed yet.
                    </p>
                  )}
                </div>
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                <Button
                  isDisabled={schedulerSaving || !schedulerTime}
                  size="sm"
                  onPress={() => void onSaveScheduler(schedulerTime)}
                >
                  {schedulerSaving ? "Saving..." : scheduler?.installed ? "Update task" : "Create task"}
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
          </div>
        ) : null}

        {activeTab === "feeds" ? (
          <section className="mt-5 rounded-lg border border-(--line) bg-(--paper-accent) p-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <h3 className="text-sm font-semibold text-(--ink)">Feed subscriptions</h3>
                <p className="mt-1 text-sm leading-6 text-muted">
                  These rows are stored in <code>data/rss_feeds.json</code>, which is local app state.
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button size="sm" variant="outline" onPress={onAddFeed}>
                  Add row
                </Button>
                <Button size="sm" isDisabled={feedsSaving} onPress={onSaveFeeds}>
                  {feedsSaving ? "Saving..." : "Save feeds"}
                </Button>
              </div>
            </div>
            <div className="mt-3 max-h-[68vh] overflow-auto rounded-lg border border-(--line)">
              <table className="w-full border-collapse text-sm">
                <thead className="sticky top-0 bg-(--paper)">
                  <tr className="text-left text-(--ink)">
                    <th className="px-3 py-2 font-semibold">Name</th>
                    <th className="px-3 py-2 font-semibold">URL</th>
                    <th className="w-20 px-3 py-2 font-semibold">Action</th>
                  </tr>
                </thead>
                <tbody>
                  {feeds.length === 0 ? (
                    <tr>
                      <td className="px-3 py-4 text-muted" colSpan={3}>
                        No RSS feeds configured yet.
                      </td>
                    </tr>
                  ) : (
                    feeds.map((item, index) => (
                      <tr key={`${item.url}-${index}`} className="border-t border-(--line)">
                        <td className="px-3 py-2 align-top">
                          <Input
                            aria-label={`Feed name ${index + 1}`}
                            className="w-full"
                            value={item.journal}
                            onChange={(event) =>
                              onFeedChange(index, "journal", event.target.value)
                            }
                          />
                        </td>
                        <td className="px-3 py-2 align-top">
                          <Input
                            aria-label={`Feed URL ${index + 1}`}
                            className="w-full"
                            value={item.url}
                            onChange={(event) => onFeedChange(index, "url", event.target.value)}
                          />
                        </td>
                        <td className="px-3 py-2 align-top">
                          <Button size="sm" variant="ghost" onPress={() => onRemoveFeed(index)}>
                            Delete
                          </Button>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </section>
        ) : null}

        {activeTab === "profile-feedback" ? (
          <div className="mt-5 space-y-4">
            <section className="rounded-lg border border-(--line) bg-(--paper-accent) p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-sm font-semibold text-(--ink)">Classification profile</h3>
                <Button
                  isDisabled={proposalGenerating}
                  size="sm"
                  variant="secondary"
                  onPress={onGenerateProposal}
                >
                  <span className="inline-flex items-center gap-2">
                    {proposalGenerating ? <Spinner color="current" size="sm" /> : null}
                    Generate proposal
                  </span>
                </Button>
              </div>
              <div className="mt-3 space-y-3">
                {pendingProposal ? (
                  <>
                    <ProfileProposalPreview proposal={pendingProposal} />
                    <div className="flex flex-wrap gap-2">
                      <Button
                        isDisabled={pendingProposal.state !== "pending"}
                        size="sm"
                        onPress={() => onApplyProposal(pendingProposal.id)}
                      >
                        Apply
                      </Button>
                      <Button
                        isDisabled={pendingProposal.state !== "pending"}
                        size="sm"
                        variant="ghost"
                        onPress={() => onRejectProposal(pendingProposal.id)}
                      >
                        Reject
                      </Button>
                    </div>
                  </>
                ) : profile ? (
                  <ProfileRulesDocument profile={profile} />
                ) : (
                  <p className="text-sm text-muted">No profile available yet.</p>
                )}
              </div>
            </section>

            <section className="rounded-lg border border-(--line) bg-(--paper-accent) p-4">
              <div className="flex items-center justify-between gap-2">
                <h3 className="text-sm font-semibold text-(--ink)">Feedback queue</h3>
                <span className="text-sm text-muted">{jobs.length} tracked job(s)</span>
              </div>
              <div className="mt-3 overflow-hidden rounded-lg border border-(--line)">
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
                            {item.note?.trim() ? item.note : "—"}
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
            </section>
          </div>
        ) : null}
      </aside>
    </div>
  );
}
