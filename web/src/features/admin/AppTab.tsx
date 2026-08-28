import {Button, Spinner} from "@heroui/react";
import React from "react";

import {fetchZoteroCollections} from "../../api/client";
import {SelectField} from "../../shared/components/SelectField";
import type {AppMeta, AppUpdate, SchedulerSettings, SettingsConfigField, SettingsConfigUpdate, ZoteroCollectionOption} from "../../shared/types";
import {SettingsConfigEditor, type SettingsConfigEditorHandle} from "./SettingsConfigEditor";

function fieldValue(fields: SettingsConfigField[], key: string) {
  return fields.find((field) => field.key === key)?.value ?? "";
}

export function AppTab({
  appMeta,
  appUpdate,
  configFields,
  configSaving,
  onDeleteScheduler,
  onSaveConfig,
  onSaveScheduler,
  scheduler,
  schedulerSaving,
}: {
  appMeta: AppMeta | null;
  appUpdate: AppUpdate | null;
  configFields: SettingsConfigField[];
  configSaving: boolean;
  onDeleteScheduler: () => Promise<void>;
  onSaveConfig: (fields: Record<string, SettingsConfigUpdate>) => Promise<void>;
  onSaveScheduler: (dailyTime: string) => Promise<void>;
  scheduler: SchedulerSettings | null;
  schedulerSaving: boolean;
}) {
  const zoteroRef = React.useRef<SettingsConfigEditorHandle | null>(null);
  const localAppRef = React.useRef<SettingsConfigEditorHandle | null>(null);
  const zoteroFields = configFields.filter((field) => field.section === "Zotero");
  const collectionField = zoteroFields.find((field) => field.key === "SCIRSS_ZOTERO_COLLECTION_KEY") ?? null;
  const zoteroConnectionFields = zoteroFields.filter((field) => field.key !== "SCIRSS_ZOTERO_COLLECTION_KEY");
  const localAppFields = configFields.filter((field) => field.section === "Local app");
  const apiKeyField = zoteroFields.find((field) => field.key === "SCIRSS_ZOTERO_API_KEY");
  const zoteroConfigured = Boolean(apiKeyField?.configured && fieldValue(zoteroFields, "SCIRSS_ZOTERO_LIBRARY_ID").trim());
  const [configureZotero, setConfigureZotero] = React.useState(!zoteroConfigured);
  const [collections, setCollections] = React.useState<ZoteroCollectionOption[]>([]);
  const [collectionKey, setCollectionKey] = React.useState(collectionField?.value ?? "");
  const [collectionsLoading, setCollectionsLoading] = React.useState(false);
  const [zoteroError, setZoteroError] = React.useState<string | null>(null);
  const [schedulerTime, setSchedulerTime] = React.useState("12:30");
  const schedulerAdvisory = scheduler?.advisory?.trim() ?? "";

  React.useEffect(() => setCollectionKey(collectionField?.value ?? ""), [collectionField?.value]);
  React.useEffect(() => setSchedulerTime(scheduler?.scheduled_time ?? "12:30"), [scheduler?.scheduled_time]);

  const loadCollections = React.useCallback(async () => {
    if (!zoteroConfigured) return;
    setCollectionsLoading(true);
    setZoteroError(null);
    try {
      const response = await fetchZoteroCollections();
      setCollections(response.collections);
      setCollectionKey((current) => current || response.default_collection_key || "");
    } catch (error) {
      setCollections([]);
      setZoteroError(error instanceof Error ? error.message : "Could not connect to Zotero.");
    } finally {
      setCollectionsLoading(false);
    }
  }, [zoteroConfigured]);

  React.useEffect(() => { void loadCollections(); }, [loadCollections]);

  const saveZotero = async () => {
    await onSaveConfig({
      ...(zoteroRef.current?.getPayload() ?? {}),
      ...(collectionField ? {[collectionField.key]: {value: collectionKey}} : {}),
    });
    await loadCollections();
  };

  return (
    <div className="space-y-6">
      <div className="border-b border-(--line) pb-4">
        <h2 className="text-xl font-semibold text-(--ink)">App</h2>
        <p className="mt-1 text-sm text-muted">Connect Zotero and configure local automation.</p>
      </div>

      <section className="border-b border-(--line) pb-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div><div className="flex flex-wrap items-center gap-2"><h3 className="text-sm font-semibold text-(--ink)">Zotero</h3><span className={`rounded-full px-2 py-0.5 text-xs font-medium ${zoteroError ? "bg-rose-100 text-rose-800" : zoteroConfigured ? "bg-emerald-100 text-emerald-800" : "bg-slate-100 text-slate-700"}`}>{zoteroError ? "Connection error" : zoteroConfigured ? "Connected" : "Needs setup"}</span></div><p className="mt-1 text-sm text-muted">Save papers to a personal or group library.</p></div>
          <Button size="sm" variant="outline" onPress={() => setConfigureZotero((current) => !current)}>{configureZotero ? "Hide configuration" : "Configure"}</Button>
        </div>
        {zoteroError ? <p className="mt-3 text-sm text-rose-700">{zoteroError}</p> : null}
        {configureZotero ? (
          <div className="mt-4 rounded-md border border-(--line) bg-(--paper-accent) p-4">
            <SettingsConfigEditor ref={zoteroRef} fields={zoteroConnectionFields} hideGroupTitles saving={configSaving} showHeader={false} showSaveAction={false} title="Zotero" onSave={onSaveConfig} />
            {zoteroConfigured ? (
              <div className="mt-4 border-t border-(--line) pt-4">
                <SelectField disabled={collectionsLoading} label="Default collection" options={[{label: "Library root", value: ""}, ...collections.map((collection) => ({label: collection.path_label || collection.name, value: collection.key, depth: collection.depth}))]} value={collectionKey} onChange={setCollectionKey} />
              </div>
            ) : <p className="mt-3 text-sm text-muted">Save the connection first, then choose a default collection.</p>}
            <div className="mt-4 flex flex-wrap gap-2">
              <Button isDisabled={configSaving} size="sm" onPress={() => void saveZotero()}>{configSaving ? "Saving..." : "Save Zotero"}</Button>
              {zoteroConfigured ? <Button isDisabled={collectionsLoading} size="sm" variant="ghost" onPress={() => void loadCollections()}>{collectionsLoading ? <span className="inline-flex items-center gap-2"><Spinner color="current" size="sm" />Connecting...</span> : "Test connection"}</Button> : null}
            </div>
          </div>
        ) : null}
      </section>

      <section className="border-b border-(--line) pb-6">
        <div className="flex flex-wrap items-start justify-between gap-3"><div><h3 className="text-sm font-semibold text-(--ink)">Scheduled sync</h3><p className="mt-1 text-sm text-muted">{scheduler?.installed ? `Enabled daily at ${scheduler.scheduled_time}.` : "Automatic sync is disabled."}</p></div>{scheduler?.installed ? <span className="rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-800">Enabled</span> : null}</div>
        {schedulerAdvisory ? <div className="mt-3 rounded-md border border-amber-300 bg-amber-50 px-3 py-3 text-sm text-amber-900"><p className="font-medium">Automatic scheduling is unavailable on this platform.</p><p className="mt-1 leading-6">{schedulerAdvisory}</p></div> : null}
        <div className="mt-3 flex flex-wrap items-end gap-3">
          <label className="block w-52"><span className="text-sm font-medium text-(--ink)">Daily time</span><input className="mt-2 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink)" type="time" value={schedulerTime} onChange={(event) => setSchedulerTime(event.target.value)} /></label>
          <Button isDisabled={schedulerSaving || !schedulerTime} size="sm" onPress={() => void onSaveScheduler(schedulerTime)}>{schedulerSaving ? "Saving..." : scheduler?.installed ? "Update schedule" : "Enable schedule"}</Button>
          {scheduler?.installed ? <Button isDisabled={schedulerSaving} size="sm" variant="ghost" onPress={() => void onDeleteScheduler()}>Disable schedule</Button> : null}
        </div>
        {scheduler?.installed ? <details className="mt-4 rounded-md border border-(--line)"><summary className="cursor-pointer list-none px-3 py-2 text-sm font-medium text-(--ink)">Schedule details</summary><div className="space-y-1 border-t border-(--line) p-3 text-sm text-muted"><p>Task: {scheduler.task_name}</p><p>State: {scheduler.state ?? "Unknown"}</p><p>Next run: {scheduler.next_run_time ? new Date(scheduler.next_run_time).toLocaleString() : "Not scheduled"}</p><p>Last run: {scheduler.last_run_time ? new Date(scheduler.last_run_time).toLocaleString() : "Never"}</p><p>Last result: {scheduler.last_result ?? "Unknown"}</p>{scheduler.command ? <p className="break-all">Command: <code>{scheduler.command}</code></p> : null}</div></details> : null}
      </section>

      <details className="rounded-md border border-(--line)">
        <summary className="cursor-pointer list-none px-4 py-3 text-sm font-semibold text-(--ink)">Advanced local app</summary>
        <div className="border-t border-(--line) p-4"><p className="mb-3 text-sm text-muted">Host and port changes take effect after the local service restarts.</p><SettingsConfigEditor ref={localAppRef} fields={localAppFields} hideGroupTitles saving={configSaving} showHeader={false} showSaveAction={false} title="Local app" onSave={onSaveConfig} /><Button className="mt-4" isDisabled={configSaving} size="sm" onPress={() => void onSaveConfig(localAppRef.current?.getPayload() ?? {})}>{configSaving ? "Saving..." : "Save local app"}</Button></div>
      </details>

      <details className="rounded-md border border-(--line)">
        <summary className="cursor-pointer list-none px-4 py-3 text-sm font-semibold text-(--ink)">About and diagnostics</summary>
        <div className="space-y-2 border-t border-(--line) p-4 text-sm text-muted">{appMeta ? <><p className="font-medium text-(--ink)">{appMeta.name} v{appMeta.version}</p><p>Mode: {appMeta.mode}</p><p className="break-all">Data: <code>{appMeta.data_dir}</code></p><p className="break-all">Logs: <code>{appMeta.logs_dir}</code></p></> : <p>App metadata is unavailable.</p>}<p>Update: {appUpdate?.latest_version ? `v${appUpdate.latest_version}` : appUpdate?.status ?? "unknown"}</p><p>Full runtime details and update controls are available on Dashboard.</p></div>
      </details>
    </div>
  );
}
