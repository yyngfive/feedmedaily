import {Button, Card} from "@heroui/react";
import React from "react";

import type {SettingsConfigUpdate} from "../../shared/types";
import type {SchedulerSettings, SettingsConfigField} from "../../shared/types";
import {SettingsConfigEditor} from "./SettingsConfigEditor";

export function AppTab({
  configFields,
  configSaving,
  onDeleteScheduler,
  onSaveConfig,
  onSaveScheduler,
  scheduler,
  schedulerSaving,
}: {
  configFields: SettingsConfigField[];
  configSaving: boolean;
  onDeleteScheduler: () => Promise<void>;
  onSaveConfig: (fields: Record<string, SettingsConfigUpdate>) => Promise<void>;
  onSaveScheduler: (dailyTime: string) => Promise<void>;
  scheduler: SchedulerSettings | null;
  schedulerSaving: boolean;
}) {
  const [schedulerTime, setSchedulerTime] = React.useState("10:00");
  const schedulerAdvisory = scheduler?.advisory?.trim() ?? "";

  React.useEffect(() => setSchedulerTime(scheduler?.scheduled_time ?? "10:00"), [scheduler?.scheduled_time]);

  return (
    <div className="mt-5 space-y-5">
      <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
        <Card.Header><h3 className="text-xl font-semibold text-(--ink)">App</h3></Card.Header>
        <Card.Content className="space-y-6">
          <SettingsConfigEditor
            fields={configFields}
            intro={<p>Environment variables override local stored values until the override is removed.</p>}
            saving={configSaving}
            saveLabel="Save app settings"
            title="App settings"
            onSave={onSaveConfig}
          />
          <section className="border-b border-(--line) pb-5">
            <h3 className="text-sm font-semibold text-(--ink)">Scheduled sync</h3>
            {schedulerAdvisory ? (
              <div className="mt-3 rounded-md border border-amber-300 bg-amber-50 px-3 py-3 text-sm text-amber-900">
                <p className="font-medium">Automatic scheduling is unavailable on this platform.</p>
                <p className="mt-1 leading-6">{schedulerAdvisory}</p>
              </div>
            ) : null}
            <div className="mt-3 grid gap-3 md:grid-cols-[220px_minmax(0,1fr)]">
              <label className="block">
                <span className="text-sm font-medium text-(--ink)">Daily time</span>
                <input className="mt-2 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink)" type="time" value={schedulerTime} onChange={(event) => setSchedulerTime(event.target.value)} />
              </label>
              <div className="rounded-md border border-(--line) p-3 text-sm">
                {scheduler?.installed ? (
                  <>
                    <p className="text-(--ink)">Enabled as <span className="font-semibold">{scheduler.task_name}</span></p>
                    <p className="mt-1 text-muted">State: {scheduler.state ?? "Unknown"}</p>
                    <p className="mt-1 text-muted">Next run: {scheduler.next_run_time ? new Date(scheduler.next_run_time).toLocaleString() : "Not scheduled"}</p>
                    <p className="mt-1 text-muted">Last run: {scheduler.last_run_time ? new Date(scheduler.last_run_time).toLocaleString() : "Never"}</p>
                    <p className="mt-1 text-muted">Last result: {scheduler.last_result ?? "Unknown"}</p>
                    {scheduler.command ? <p className="mt-1 break-all text-muted">Suggested command: <code>{scheduler.command}</code></p> : null}
                  </>
                ) : <p className="text-muted">Daily sync is currently disabled.</p>}
              </div>
            </div>
            <div className="mt-3 flex flex-wrap gap-2">
              <Button isDisabled={schedulerSaving || !schedulerTime} size="sm" onPress={() => void onSaveScheduler(schedulerTime)}>
                {schedulerSaving ? "Saving..." : scheduler?.installed ? "Update daily sync" : "Enable daily sync"}
              </Button>
              <Button isDisabled={schedulerSaving || !scheduler?.installed} size="sm" variant="ghost" onPress={() => void onDeleteScheduler()}>Remove task</Button>
            </div>
          </section>
        </Card.Content>
      </Card>
    </div>
  );
}
