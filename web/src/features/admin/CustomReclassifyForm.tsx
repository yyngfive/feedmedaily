import {Button} from "@heroui/react";
import React from "react";
import {fetchJournalOptions, previewCustomReclassify, type CustomReclassifyPreview, type CustomReclassifyRequest, type JournalOption} from "../../api/client";
import {CheckboxRow, TextInputField} from "../../shared/components/FormFields";

export function CustomReclassifyForm({busy, onRun, stopButton}: {
  busy: boolean;
  onRun: (request: CustomReclassifyRequest) => Promise<void> | void;
  stopButton: React.ReactNode;
}) {
  const [options, setOptions] = React.useState<JournalOption[]>([]);
  const [timezone, setTimezone] = React.useState("");
  const [optionsError, setOptionsError] = React.useState<string | null>(null);
  const [reload, setReload] = React.useState(0);
  const [search, setSearch] = React.useState("");
  const [keys, setKeys] = React.useState<string[]>([]);
  const [from, setFrom] = React.useState("");
  const [to, setTo] = React.useState("");
  const [preview, setPreview] = React.useState<{signature: string; value: CustomReclassifyPreview} | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const [launchError, setLaunchError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const [revision, setRevision] = React.useState(0);
  const filter = React.useMemo(() => ({journal_keys: keys, date_from: from, date_to: to}), [keys, from, to]);
  const signature = JSON.stringify(filter);
  const validation = !keys.length && !from && !to ? "Select at least one journal or date boundary." : from && to && from > to ? "From must be on or before To." : null;
  const current = preview?.signature === signature ? preview.value : null;

  React.useEffect(() => {
    let cancelled = false;
    setOptionsError(null);
    void fetchJournalOptions().then((result) => {if (!cancelled) {setOptions(result.journals); setTimezone(result.timezone);}})
      .catch((err: Error) => {if (!cancelled) setOptionsError(err.message);});
    return () => {cancelled = true;};
  }, [reload]);

  React.useEffect(() => {
    let cancelled = false;
    setPreview(null); setError(null);
    if (validation) return;
    const timer = window.setTimeout(() => {
      void previewCustomReclassify(filter).then((value) => {if (!cancelled) setPreview({signature, value});})
        .catch((err: Error) => {if (!cancelled) setError(err.message);});
    }, 300);
    return () => {cancelled = true; window.clearTimeout(timer);};
  }, [filter, signature, validation, revision, busy]);

  const toggle = (key: string) => setKeys((old) => old.includes(key) ? old.filter((item) => item !== key) : [...old, key]);
  const run = async () => {
    if (!current?.total || busy || submitting || validation) return;
    setSubmitting(true); setError(null); setLaunchError(null);
    try {await onRun({...filter, fingerprint: current.fingerprint});}
    catch (err) {setLaunchError(err instanceof Error ? err.message : "Could not start reclassification."); setPreview(null);}
    finally {setSubmitting(false); setRevision((value) => value + 1);}
  };

  return <div className="space-y-3">
    <div className="flex items-end gap-2">
      <TextInputField className="min-w-0 flex-1" clearable label="Journals" placeholder="Search journals…" value={search} onChange={setSearch}/>
    </div>
    {keys.length ? <div className="flex flex-wrap items-center gap-2" aria-label="Selected journals">
      {keys.map((key) => <Button key={key} size="sm" variant="secondary" className="h-auto max-w-full whitespace-normal break-words text-left" aria-label={`Remove ${options.find((item) => item.key === key)?.label ?? key}`} onPress={() => toggle(key)}>{options.find((item) => item.key === key)?.label ?? "Unknown selection"} ×</Button>)}
      <Button size="sm" variant="ghost" onPress={() => setKeys([])}>Clear selection</Button>
    </div> : <p className="text-sm text-muted">All journals</p>}
    {optionsError ? <p role="alert" className="text-sm text-danger">{optionsError} <Button size="sm" variant="ghost" onPress={() => setReload((n) => n + 1)}>Retry</Button></p> : null}
    <div className="max-h-52 space-y-1 overflow-auto rounded-md border border-(--line) p-2" aria-label="Journal choices">
      {options.filter((option) => option.label.toLowerCase().includes(search.toLowerCase())).map((option) => <CheckboxRow key={option.key} checked={keys.includes(option.key)} onChange={() => toggle(option.key)}><span className="min-w-0 break-words">{option.label}</span></CheckboxRow>)}
      {!options.some((option) => option.label.toLowerCase().includes(search.toLowerCase())) ? <p className="text-sm text-muted">{timezone ? "No matching journals." : "Loading journals…"}</p> : null}
    </div>
    <p className="text-sm font-medium">Date added{timezone ? ` · ${timezone}` : ""}</p>
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <TextInputField label="From" type="date" value={from} onChange={setFrom}/>
      <TextInputField label="To" type="date" value={to} onChange={setTo}/>
    </div>
    <div aria-live="polite" className="text-sm text-muted">
      {validation ?? (current ? `${current.total} papers · ${current.classified} classified · ${current.unclassified} unclassified` : error ? "Preview unavailable." : "Loading preview…")}
      {current && current.total > 0 ? <p>Existing classifications will be updated.</p> : null}
    </div>
    {launchError ? <p role="alert" className="text-sm text-danger">{launchError}</p> : null}
    {error ? <p role="alert" className="text-sm text-danger">{error} <Button size="sm" variant="ghost" onPress={() => setRevision((n) => n + 1)}>Refresh preview</Button></p> : null}
    <div className="flex flex-wrap gap-2">
      <Button size="sm" isDisabled={busy || submitting || !current?.total || Boolean(validation) || Boolean(optionsError)} onPress={() => void run()}>{submitting ? "Starting…" : `Reclassify ${current?.total ?? 0} papers`}</Button>
      {stopButton}
    </div>
  </div>;
}
