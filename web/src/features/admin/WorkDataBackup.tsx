import {Button} from "@heroui/react";
import React from "react";
import {createBackup, fetchBackups, type BackupEntry} from "../../api/client";
import type {JobInfo} from "../../shared/types";

export function WorkDataBackup({jobs, onJob}: {jobs: JobInfo[]; onJob: (job: JobInfo) => void}) {
  const [entries, setEntries] = React.useState<BackupEntry[]>([]);
  const [exists, setExists] = React.useState<boolean | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const [creating, setCreating] = React.useState(false);
  const [revision, setRevision] = React.useState(0);
  const job = jobs.find((item) => item.job_type === "backup");
  const busy = jobs.some((item) => ["sync", "reclassify", "cleanup", "cleanup-review", "backup"].includes(item.job_type) && (["queued", "running"].includes(item.status) || (item.job_type === "sync" && item.status === "waiting_for_user")));
  React.useEffect(() => {
    let cancelled = false;
    void fetchBackups().then((result) => {if (!cancelled) {setEntries(result.backups); setExists(result.database_exists); setError(null);}})
      .catch((err: Error) => {if (!cancelled) {setError(err.message); setExists(null);}});
    return () => {cancelled = true;};
  }, [job?.id, job?.status, revision]);
  const run = async () => {
    if (creating || busy || !exists) return;
    setCreating(true); setError(null);
    try {onJob(await createBackup());}
    catch (err) {setError(err instanceof Error ? err.message : "Could not create a backup.");}
    finally {setCreating(false);}
  };
  return <section className="mt-4 space-y-3 border-t border-(--line) pt-4">
    <h3 className="text-sm font-semibold">Work data backup</h3>
    <p className="text-sm text-muted">Includes papers, classifications, feedback, profile, and subscriptions. Model keys and app settings are not included.</p>
    <div className="flex flex-wrap items-center gap-2">
      <Button size="sm" isDisabled={creating || busy || !exists} onPress={() => void run()}>{creating || (job && ["queued", "running"].includes(job.status)) ? "Creating backup…" : "Create backup"}</Button>
      {exists === false ? <span className="text-sm text-muted">No database exists yet.</span> : busy && job?.status !== "running" && job?.status !== "queued" ? <span className="text-sm text-muted">Wait for the active job to finish.</span> : null}
    </div>
    {job ? <p role="status" className={job.status === "failed" ? "text-sm text-danger" : "text-sm text-muted"}>{job.error || job.message || job.status}</p> : null}
    {error ? <p role="alert" className="text-sm text-danger">{error} <Button size="sm" variant="ghost" onPress={() => setRevision((n) => n + 1)}>Retry</Button></p> : null}
    {exists === null && !error ? <p className="text-sm text-muted">Loading backups…</p> : null}
    {entries.length ? <ul className="space-y-2">
      {entries.map((entry) => <li key={entry.id} className="flex flex-wrap items-center justify-between gap-2 text-sm">
        <span>{new Date(entry.created_at).toLocaleString()} · {(entry.size / 1024 / 1024).toFixed(2)} MB</span>
        <a className="rounded-md px-2 py-1 text-(--accent) underline focus-visible:outline" href={`/api/admin/backups/${encodeURIComponent(entry.id)}/download`} download>Download</a>
      </li>)}
    </ul> : exists && !error ? <p className="text-sm text-muted">No work data backups yet.</p> : null}
    <p className="text-sm text-muted">Create backup writes a ZIP to your local data directory. Download copies that ZIP to your computer; it does not change app settings. Manual backups stay there until you remove them, and each ZIP includes restore instructions.</p>
  </section>;
}
