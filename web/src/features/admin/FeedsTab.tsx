import {Button} from "@heroui/react";
import React from "react";

import {emailFeedCatalog} from "../../data/emailFeedCatalog";
import {feedCatalog} from "../../data/feedCatalog";
import {CheckboxRow, TextInputField} from "../../shared/components/FormFields";
import {SelectField} from "../../shared/components/SelectField";
import type {FeedSubscription, SettingsConfigField, SettingsConfigUpdate} from "../../shared/types";

function cloneFeeds(feeds: FeedSubscription[]): FeedSubscription[] {
  return feeds.map((feed) => ({...feed}));
}

function draftID() {
  return typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random()}`;
}

// 私有邮件源以稳定标识参与去重与定向同步，真实 URL 不进前端。
function feedIdentity(feed: FeedSubscription) {
  return feed.email_source ? `email:${feed.email_source}` : feed.url.trim();
}

// Feed 设置使用独立草稿，只有保存后才更新阅读工作区的数据。
export function FeedsTab({feeds, feedsSaving, onSaveFeeds, configFields, configSaving, onSaveConfig}: {
  feeds: FeedSubscription[];
  feedsSaving: boolean;
  onSaveFeeds: (feeds: FeedSubscription[]) => Promise<boolean | void> | boolean | void;
  configFields: SettingsConfigField[];
  configSaving: boolean;
  onSaveConfig: (fields: Record<string, SettingsConfigUpdate>) => Promise<void> | void;
}) {
  const [draftFeeds, setDraftFeeds] = React.useState<FeedSubscription[]>(() => cloneFeeds(feeds));
  const [editing, setEditing] = React.useState(false);
  const [adding, setAdding] = React.useState(false);
  const [newFeedJournal, setNewFeedJournal] = React.useState("");
  const [newFeedURL, setNewFeedURL] = React.useState("");
  const [catalogPublisher, setCatalogPublisher] = React.useState("All");
  const [catalogQuery, setCatalogQuery] = React.useState("");
  const [selectedCatalogURLs, setSelectedCatalogURLs] = React.useState<string[]>([]);
  const [selectedEmailSourceIDs, setSelectedEmailSourceIDs] = React.useState<string[]>([]);
  const [emailFeedURLDraft, setEmailFeedURLDraft] = React.useState("");

  React.useEffect(() => {
    if (!editing) setDraftFeeds(cloneFeeds(feeds));
  }, [editing, feeds]);

  const catalogPublishers = React.useMemo(() => ["All", ...Array.from(new Set(feedCatalog.map((item) => item.publisher))).sort()], []);
  const existingIdentities = React.useMemo(() => new Set(draftFeeds.map((feed) => feedIdentity(feed)).filter(Boolean)), [draftFeeds]);
  const catalogMatches = React.useMemo(() => {
    const query = catalogQuery.trim().toLowerCase();
    return feedCatalog.filter((item) =>
      (catalogPublisher === "All" || item.publisher === catalogPublisher) &&
      (!query || `${item.journal} ${item.publisher} ${item.subjects.join(" ")}`.toLowerCase().includes(query)),
    );
  }, [catalogPublisher, catalogQuery]);
  const emailConfigKeys = React.useMemo(() => Array.from(new Set(emailFeedCatalog.map((entry) => entry.configKey))), []);
  const emailConfigFields = React.useMemo(
    () => emailConfigKeys.map((key) => configFields.find((field) => field.key === key)).filter((field): field is SettingsConfigField => Boolean(field)),
    [configFields, emailConfigKeys],
  );
  const emailFeedConfigured = emailConfigFields.length === emailConfigKeys.length && emailConfigFields.every((field) => field.configured);

  const addFeeds = (items: FeedSubscription[]) => {
    setDraftFeeds((current) => {
      const identities = new Set(current.map((item) => feedIdentity(item)).filter(Boolean));
      return [...current, ...items.filter((item) => feedIdentity(item) && !identities.has(feedIdentity(item))).map((item) => ({...item, client_id: item.client_id ?? draftID()}))];
    });
    setEditing(true);
    setAdding(false);
    setSelectedCatalogURLs([]);
    setSelectedEmailSourceIDs([]);
  };

  const addCustomFeed = () => {
    const journal = newFeedJournal.trim();
    const url = newFeedURL.trim();
    if (!journal || !url || existingIdentities.has(url)) return;
    addFeeds([{journal, url}]);
    setNewFeedJournal("");
    setNewFeedURL("");
  };

  const cancelEditing = () => {
    setDraftFeeds(cloneFeeds(feeds));
    setEditing(false);
    setAdding(false);
    setSelectedCatalogURLs([]);
    setSelectedEmailSourceIDs([]);
  };

  const saveEmailFeedURL = async () => {
    const value = emailFeedURLDraft.trim();
    if (!value) return;
    const fields: Record<string, SettingsConfigUpdate> = {};
    for (const key of emailConfigKeys) fields[key] = {value};
    await onSaveConfig(fields);
    setEmailFeedURLDraft("");
  };

  const saveFeeds = async () => {
    const saved = await onSaveFeeds(draftFeeds);
    if (saved !== false) setEditing(false);
  };

  if (adding) {
    return (
      <div className="space-y-5">
        <div className="flex flex-wrap items-start justify-between gap-3 border-b border-(--line) pb-4">
          <div><h2 className="text-xl font-semibold text-(--ink)">Add feeds</h2><p className="mt-1 text-sm text-muted">Choose journals from the catalog or add a custom RSS URL.</p></div>
          <Button size="sm" variant="ghost" onPress={() => setAdding(false)}>Back to subscriptions</Button>
        </div>
        <section className="border-b border-(--line) pb-5">
          <h3 className="text-sm font-semibold text-(--ink)">Custom feed</h3>
          <div className="mt-3 grid gap-3 md:grid-cols-[minmax(160px,0.7fr)_minmax(240px,1fr)_auto]">
            <TextInputField hideLabel label="Journal name" placeholder="Journal name" value={newFeedJournal} onChange={setNewFeedJournal} />
            <TextInputField hideLabel label="RSS URL" placeholder="https://example.com/feed.xml" type="url" value={newFeedURL} onChange={setNewFeedURL} />
            <Button isDisabled={!newFeedJournal.trim() || !newFeedURL.trim() || existingIdentities.has(newFeedURL.trim())} size="sm" onPress={addCustomFeed}>Add feed</Button>
          </div>
        </section>
        <section className="border-b border-(--line) pb-5">
          <h3 className="text-sm font-semibold text-(--ink)">Email alert feeds</h3>
          <p className="mt-1 text-sm text-muted">Journal emails aggregated through a private feed. FMD never shows this feed address; it is stored in settings like a password.</p>
          {!emailFeedConfigured ? (
            <div className="mt-3 grid gap-3 md:grid-cols-[minmax(240px,1fr)_auto]">
              <TextInputField hideLabel label="Email feed URL" placeholder="Private Atom/RSS URL of your kill-the-news feed" type="password" value={emailFeedURLDraft} onChange={setEmailFeedURLDraft} />
              <Button isDisabled={!emailFeedURLDraft.trim() || configSaving} size="sm" onPress={() => void saveEmailFeedURL()}>{configSaving ? "Saving..." : "Save access URL"}</Button>
            </div>
          ) : null}
          <div className="mt-3 divide-y divide-(--line)">
            {emailFeedCatalog.map((entry) => {
              const exists = draftFeeds.some((feed) => feed.email_source === entry.id);
              return (
                <CheckboxRow key={entry.id} checked={selectedEmailSourceIDs.includes(entry.id)} className="px-2 py-3 text-sm" disabled={!emailFeedConfigured || exists} onChange={() => setSelectedEmailSourceIDs((current) => current.includes(entry.id) ? current.filter((id) => id !== entry.id) : [...current, entry.id])}>
                  <span className="min-w-0"><span className="flex flex-wrap items-center gap-2"><span className="font-medium text-(--ink)">{entry.journal}</span><span className="text-xs text-muted">{entry.publisher}</span>{exists ? <span className="text-xs text-warning">Added</span> : !emailFeedConfigured ? <span className="text-xs text-warning">Configure access first</span> : null}</span><span className="mt-1 block text-xs text-muted">{entry.description}</span></span>
                </CheckboxRow>
              );
            })}
          </div>
          {selectedEmailSourceIDs.length > 0 ? (
            <div className="mt-3 flex justify-end">
              <Button size="sm" onPress={() => addFeeds(emailFeedCatalog.filter((entry) => selectedEmailSourceIDs.includes(entry.id)).map((entry) => ({journal: entry.journal, url: "", email_source: entry.id, private: true})))}>Add selected ({selectedEmailSourceIDs.length})</Button>
            </div>
          ) : null}
        </section>
        <section>
          <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_220px]">
            <TextInputField hideLabel label="Search feed catalog" placeholder="Search journal, publisher, or subject" value={catalogQuery} onChange={setCatalogQuery} />
            <SelectField hideLabel label="Publisher" options={catalogPublishers.map((publisher) => ({label: publisher, value: publisher}))} value={catalogPublisher} onChange={setCatalogPublisher} />
          </div>
          <div className="mt-3 max-h-[54vh] divide-y divide-(--line) overflow-y-auto border-y border-(--line)">
            {catalogMatches.length === 0 ? <p className="py-4 text-sm text-muted">No catalog matches.</p> : catalogMatches.map((item) => {
              const exists = existingIdentities.has(item.url.trim());
              return (
                <CheckboxRow key={item.url} checked={selectedCatalogURLs.includes(item.url)} className="px-2 py-3 text-sm" disabled={exists} onChange={() => setSelectedCatalogURLs((current) => current.includes(item.url) ? current.filter((url) => url !== item.url) : [...current, item.url])}>
                  <span className="min-w-0"><span className="flex flex-wrap items-center gap-2"><span className="font-medium text-(--ink)">{item.journal}</span><span className="text-xs text-muted">{item.publisher}</span>{exists ? <span className="text-xs text-warning">Added</span> : null}</span><span className="mt-1 block truncate text-xs text-muted">{item.url}</span></span>
                </CheckboxRow>
              );
            })}
          </div>
          <div className="mt-3 flex items-center justify-between gap-3">
            <a className="text-sm text-muted underline-offset-3 hover:underline" href="https://github.com/yyngfive/sci-rss-list" rel="noreferrer" target="_blank">Feed not listed?</a>
            <Button isDisabled={selectedCatalogURLs.length === 0} size="sm" onPress={() => addFeeds(feedCatalog.filter((item) => selectedCatalogURLs.includes(item.url)).map((item) => ({journal: item.journal, url: item.url})))}>Add selected ({selectedCatalogURLs.length})</Button>
          </div>
        </section>
      </div>
    );
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-(--line) pb-4">
        <div><h2 className="text-xl font-semibold text-(--ink)">Feeds</h2><p className="mt-1 text-sm text-muted">Manage the journals included in sync.</p></div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" onPress={() => setAdding(true)}>Add feeds</Button>
          {draftFeeds.length > 0 && !editing ? <Button size="sm" variant="ghost" onPress={() => setEditing(true)}>Edit</Button> : null}
          {editing ? <Button isDisabled={feedsSaving} size="sm" variant="ghost" onPress={cancelEditing}>Cancel</Button> : null}
          {editing ? <Button isDisabled={feedsSaving} size="sm" onPress={() => void saveFeeds()}>{feedsSaving ? "Saving..." : "Save feeds"}</Button> : null}
        </div>
      </div>
      <section>
        <div className="flex items-center justify-between gap-3"><h3 className="text-sm font-semibold text-(--ink)">Subscriptions</h3><span className="text-sm text-muted">{draftFeeds.length}</span></div>
        {draftFeeds.length === 0 ? <p className="mt-3 border-y border-(--line) py-5 text-sm text-muted">No RSS feeds configured yet.</p> : !editing ? (
          <div className="mt-3 max-h-[68vh] divide-y divide-(--line) overflow-y-auto border-y border-(--line)">
            {draftFeeds.map((item, index) => <div key={item.client_id ?? String(index)} className="grid gap-1 py-3 md:grid-cols-[minmax(160px,0.65fr)_minmax(0,1fr)] md:gap-4"><p className="font-medium text-(--ink)">{item.journal || "Untitled feed"}</p>{item.email_source ? <p className="text-sm text-muted">Email feed · URL stored in settings</p> : <p className="truncate text-sm text-muted" title={item.url}>{item.url}</p>}</div>)}
          </div>
        ) : (
          <div className="mt-3 max-h-[68vh] divide-y divide-(--line) overflow-y-auto border-y border-(--line)">
            {draftFeeds.map((item, index) => (
              <div key={item.client_id ?? String(index)} className="grid gap-3 py-3 md:grid-cols-[minmax(160px,0.65fr)_minmax(240px,1fr)_auto]">
                <TextInputField hideLabel label={`Feed name ${index + 1}`} value={item.journal} onChange={(journal) => setDraftFeeds((current) => current.map((feed, feedIndex) => feedIndex === index ? {...feed, journal} : feed))} />
                {item.email_source ? <p className="self-center text-sm text-muted">Private email feed · the URL is managed in settings and never shown.</p> : <TextInputField hideLabel label={`Feed URL ${index + 1}`} type="url" value={item.url} onChange={(url) => setDraftFeeds((current) => current.map((feed, feedIndex) => feedIndex === index ? {...feed, url} : feed))} />}
                <Button size="sm" variant="danger" onPress={() => setDraftFeeds((current) => current.filter((_feed, feedIndex) => feedIndex !== index))}>Remove</Button>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
