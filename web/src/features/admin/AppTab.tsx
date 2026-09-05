import {Button, Chip, Spinner} from "@heroui/react";
import React from "react";

import {fetchZoteroCollections} from "../../api/client";
import {SelectField} from "../../shared/components/SelectField";
import type {AppMeta, AppUpdate, SchedulerSettings, SettingsConfigField, SettingsConfigUpdate, ZoteroCollectionOption} from "../../shared/types";
import {AdminDisclosure} from "./AdminDisclosure";
import {SettingsConfigEditor, type SettingsConfigEditorHandle} from "./SettingsConfigEditor";

function fieldValue(fields: SettingsConfigField[], key: string) {
  return fields.find((field) => field.key === key)?.value ?? "";
}

export function AppTab({appMeta, appUpdate, appUpdateChecking, configFields, configSaving, onCheckForUpdates, onDeleteScheduler, onSaveConfig, onSaveScheduler, scheduler, schedulerSaving}: {
  appMeta: AppMeta | null;
  appUpdate: AppUpdate | null;
  appUpdateChecking: boolean;
  configFields: SettingsConfigField[];
  configSaving: boolean;
  onCheckForUpdates: () => void;
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
  const [collections, setCollections] = React.useState<ZoteroCollectionOption[]>([]);
  const [collectionKey, setCollectionKey] = React.useState(collectionField?.value ?? "");
  const [collectionsLoading, setCollectionsLoading] = React.useState(false);
  const [zoteroError, setZoteroError] = React.useState<string | null>(null);
  const [schedulerTime, setSchedulerTime] = React.useState("12:30");
  const schedulerAdvisory = scheduler?.advisory?.trim() ?? "";
  const lastCheckedLabel = appUpdate?.checked_at && !Number.isNaN(Date.parse(appUpdate.checked_at)) ? new Date(appUpdate.checked_at).toLocaleString() : null;

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

  const zoteroStatus = zoteroError ? "Connection error" : zoteroConfigured ? "Connected" : "Needs setup";
  const zoteroColor = zoteroError ? "danger" : zoteroConfigured ? "success" : "default";

  return (
    <div className="space-y-6">
      <div className="border-b border-(--line) pb-4">
        <h2 className="text-xl font-semibold text-(--ink)">App</h2>
        <p className="mt-1 text-sm text-muted">Updates, integrations, and local automation.</p>
      </div>

      <section className="border-b border-(--line) pb-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold text-(--ink)">About and updates</h3>
            {appMeta ? <p className="mt-1 text-sm text-muted">{appMeta.name} v{appMeta.version} · {appMeta.mode}</p> : null}
          </div>
          <Button isDisabled={appUpdateChecking} size="sm" variant="outline" onPress={onCheckForUpdates}><span className="inline-flex items-center gap-2">{appUpdateChecking ? <Spinner color="current" size="sm" /> : null}{appUpdateChecking ? "Checking..." : "Check for updates"}</span></Button>
        </div>
        {appUpdate ? (
          <div className="mt-3 text-sm leading-6 text-muted">
            <p className="font-medium capitalize text-(--ink)">{appUpdate.has_update && appUpdate.latest_version ? `Version ${appUpdate.latest_version} is available` : appUpdate.status.replaceAll("_", " ")}</p>
            {lastCheckedLabel ? <p>Last checked: {lastCheckedLabel}</p> : null}
            {appUpdate.detail ? <p>{appUpdate.detail}</p> : null}
            <div className="mt-3 flex flex-wrap gap-2">
              {appUpdate.has_update && appUpdate.download_url ? <Button size="sm" onPress={() => window.open(appUpdate.download_url!, "_blank", "noopener,noreferrer")}>Download installer</Button> : null}
              {appUpdate.release_notes_url ? <Button size="sm" variant="outline" onPress={() => window.open(appUpdate.release_notes_url!, "_blank", "noopener,noreferrer")}>Release notes</Button> : null}
              <Button size="sm" variant="outline" onPress={() => window.open('https://github.com/yyngfive/feedmedaily/blob/main/README.md', "_blank", "noopener,noreferrer")}>User Manual</Button>
            </div>
          </div>
        ) : <p className="mt-3 text-sm text-muted">Update information is unavailable.</p>}
        {appMeta ? (
          <dl className="mt-4 grid gap-x-6 gap-y-2 border-t border-(--line) pt-4 text-sm text-muted sm:grid-cols-2">
            <div><dt className="text-xs">Server</dt><dd className="break-all text-(--ink)"><code>{appMeta.server_url ?? "Unavailable"}</code></dd></div>
            <div><dt className="text-xs">Install directory</dt><dd className="break-all text-(--ink)"><code>{appMeta.install_dir}</code></dd></div>
            <div><dt className="text-xs">Data</dt><dd className="break-all text-(--ink)"><code>{appMeta.data_dir}</code></dd></div>
            <div><dt className="text-xs">Logs</dt><dd className="break-all text-(--ink)"><code>{appMeta.logs_dir}</code></dd></div>
            <div><dt className="text-xs">Static files</dt><dd className="break-all text-(--ink)"><code>{appMeta.static_dir}</code></dd></div>
            <div><dt className="text-xs">Config</dt><dd className="break-all text-(--ink)"><code>{appMeta.config_dir ?? "Unavailable"}</code></dd></div>
          </dl>
        ) : <p className="mt-4 text-sm text-muted">App metadata is unavailable.</p>}
      </section>

      <div className="space-y-4">
        <AdminDisclosure meta={<Chip color={zoteroColor} size="sm" variant="soft">{zoteroStatus}</Chip>} title="Zotero">
          {zoteroError ? <p className="mb-4 text-sm text-rose-700">{zoteroError}</p> : null}
          <SettingsConfigEditor ref={zoteroRef} fields={zoteroConnectionFields} hideGroupTitles saving={configSaving} showHeader={false} showSaveAction={false} title="Zotero" onSave={onSaveConfig} />
          {zoteroConfigured ? <div className="mt-4 border-t border-(--line) pt-4"><SelectField disabled={collectionsLoading} label="Default collection" options={[{label: "Library root", value: ""}, ...collections.map((collection) => ({label: collection.path_label || collection.name, value: collection.key, depth: collection.depth}))]} value={collectionKey} onChange={setCollectionKey} /></div> : <p className="mt-3 text-sm text-muted">Save the connection first, then choose a default collection.</p>}
          <div className="mt-4 flex flex-wrap gap-2">
            <Button isDisabled={configSaving} size="sm" onPress={() => void saveZotero()}>{configSaving ? "Saving..." : "Save Zotero"}</Button>
            {zoteroConfigured ? <Button isDisabled={collectionsLoading} size="sm" variant="ghost" onPress={() => void loadCollections()}>{collectionsLoading ? <span className="inline-flex items-center gap-2"><Spinner color="current" size="sm" />Connecting...</span> : "Test connection"}</Button> : null}
          </div>
        </AdminDisclosure>

        <AdminDisclosure meta={scheduler?.installed ? <Chip color="success" size="sm" variant="soft">Enabled</Chip> : undefined} title="Scheduled sync">
          <p className="text-sm text-muted">{scheduler?.installed ? `Runs daily at ${scheduler.scheduled_time}.` : "Automatic sync is disabled."}</p>
          {schedulerAdvisory ? <div className="mt-3 rounded-md border border-amber-300 bg-amber-50 px-3 py-3 text-sm text-amber-900"><p className="font-medium">Automatic scheduling is unavailable on this platform.</p><p className="mt-1 leading-6">{schedulerAdvisory}</p></div> : null}
          <div className="mt-3 flex flex-wrap items-end gap-3">
            <label className="block w-52"><span className="text-sm font-medium text-(--ink)">Daily time</span><input className="mt-2 w-full rounded-md border border-(--line) bg-(--paper-accent) px-3 py-2 text-sm text-(--ink)" type="time" value={schedulerTime} onChange={(event) => setSchedulerTime(event.target.value)} /></label>
            <Button isDisabled={schedulerSaving || !schedulerTime} size="sm" onPress={() => void onSaveScheduler(schedulerTime)}>{schedulerSaving ? "Saving..." : scheduler?.installed ? "Update schedule" : "Enable schedule"}</Button>
            {scheduler?.installed ? <Button isDisabled={schedulerSaving} size="sm" variant="danger" onPress={() => void onDeleteScheduler()}>Disable schedule</Button> : null}
          </div>
          {scheduler?.installed ? <p className="mt-4 text-sm text-muted">Last run: {scheduler.last_run_time ? new Date(scheduler.last_run_time).toLocaleString() : "Never"}</p> : null}
        </AdminDisclosure>

        <AdminDisclosure title="Local app">
          <p className="mb-3 text-sm text-muted">Host and port changes take effect after the local service restarts.</p>
          <SettingsConfigEditor ref={localAppRef} fields={localAppFields} hideGroupTitles saving={configSaving} showHeader={false} showSaveAction={false} title="Local app" onSave={onSaveConfig} />
          <Button className="mt-4" isDisabled={configSaving} size="sm" onPress={() => void onSaveConfig(localAppRef.current?.getPayload() ?? {})}>{configSaving ? "Saving..." : "Save local app"}</Button>
        </AdminDisclosure>
      </div>
    </div>
  );
}
