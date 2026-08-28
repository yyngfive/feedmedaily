import {Button} from "@heroui/react";
import React from "react";

import {feedCatalog} from "../../data/feedCatalog";
import {CheckboxRow, TextInputField} from "../../shared/components/FormFields";
import {SelectField} from "../../shared/components/SelectField";
import type {FeedSubscription} from "../../shared/types";

function cloneFeeds(feeds: FeedSubscription[]): FeedSubscription[] {
  return feeds.map((feed) => ({...feed}));
}

function draftID() {
  return typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random()}`;
}

// Feed 设置使用独立草稿，只有保存后才更新阅读工作区的数据。
export function FeedsTab({feeds, feedsSaving, onSaveFeeds}: {
  feeds: FeedSubscription[];
  feedsSaving: boolean;
  onSaveFeeds: (feeds: FeedSubscription[]) => Promise<boolean | void> | boolean | void;
}) {
  const [draftFeeds, setDraftFeeds] = React.useState<FeedSubscription[]>(() => cloneFeeds(feeds));
  const [editing, setEditing] = React.useState(false);
  const [adding, setAdding] = React.useState(false);
  const [newFeedJournal, setNewFeedJournal] = React.useState("");
  const [newFeedURL, setNewFeedURL] = React.useState("");
  const [catalogPublisher, setCatalogPublisher] = React.useState("All");
  const [catalogQuery, setCatalogQuery] = React.useState("");
  const [selectedCatalogURLs, setSelectedCatalogURLs] = React.useState<string[]>([]);

  React.useEffect(() => {
    if (!editing) setDraftFeeds(cloneFeeds(feeds));
  }, [editing, feeds]);

  const catalogPublishers = React.useMemo(() => ["All", ...Array.from(new Set(feedCatalog.map((item) => item.publisher))).sort()], []);
  const existingFeedURLs = React.useMemo(() => new Set(draftFeeds.map((feed) => feed.url.trim()).filter(Boolean)), [draftFeeds]);
  const catalogMatches = React.useMemo(() => {
    const query = catalogQuery.trim().toLowerCase();
    return feedCatalog.filter((item) =>
      (catalogPublisher === "All" || item.publisher === catalogPublisher) &&
      (!query || `${item.journal} ${item.publisher} ${item.subjects.join(" ")}`.toLowerCase().includes(query)),
    );
  }, [catalogPublisher, catalogQuery]);

  const addFeeds = (items: FeedSubscription[]) => {
    setDraftFeeds((current) => {
      const urls = new Set(current.map((item) => item.url.trim()).filter(Boolean));
      return [...current, ...items.filter((item) => item.url.trim() && !urls.has(item.url.trim())).map((item) => ({...item, client_id: item.client_id ?? draftID()}))];
    });
    setEditing(true);
    setAdding(false);
    setSelectedCatalogURLs([]);
  };

  const addCustomFeed = () => {
    const journal = newFeedJournal.trim();
    const url = newFeedURL.trim();
    if (!journal || !url || existingFeedURLs.has(url)) return;
    addFeeds([{journal, url}]);
    setNewFeedJournal("");
    setNewFeedURL("");
  };

  const cancelEditing = () => {
    setDraftFeeds(cloneFeeds(feeds));
    setEditing(false);
    setAdding(false);
    setSelectedCatalogURLs([]);
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
            <Button isDisabled={!newFeedJournal.trim() || !newFeedURL.trim() || existingFeedURLs.has(newFeedURL.trim())} size="sm" onPress={addCustomFeed}>Add feed</Button>
          </div>
        </section>
        <section>
          <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_220px]">
            <TextInputField hideLabel label="Search feed catalog" placeholder="Search journal, publisher, or subject" value={catalogQuery} onChange={setCatalogQuery} />
            <SelectField hideLabel label="Publisher" options={catalogPublishers.map((publisher) => ({label: publisher, value: publisher}))} value={catalogPublisher} onChange={setCatalogPublisher} />
          </div>
          <div className="mt-3 max-h-[54vh] divide-y divide-(--line) overflow-y-auto border-y border-(--line)">
            {catalogMatches.length === 0 ? <p className="py-4 text-sm text-muted">No catalog matches.</p> : catalogMatches.map((item) => {
              const exists = existingFeedURLs.has(item.url.trim());
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
            {draftFeeds.map((item, index) => <div key={item.client_id ?? String(index)} className="grid gap-1 py-3 md:grid-cols-[minmax(160px,0.65fr)_minmax(0,1fr)] md:gap-4"><p className="font-medium text-(--ink)">{item.journal || "Untitled feed"}</p><p className="truncate text-sm text-muted" title={item.url}>{item.url}</p></div>)}
          </div>
        ) : (
          <div className="mt-3 max-h-[68vh] divide-y divide-(--line) overflow-y-auto border-y border-(--line)">
            {draftFeeds.map((item, index) => (
              <div key={item.client_id ?? String(index)} className="grid gap-3 py-3 md:grid-cols-[minmax(160px,0.65fr)_minmax(240px,1fr)_auto]">
                <TextInputField hideLabel label={`Feed name ${index + 1}`} value={item.journal} onChange={(journal) => setDraftFeeds((current) => current.map((feed, feedIndex) => feedIndex === index ? {...feed, journal} : feed))} />
                <TextInputField hideLabel label={`Feed URL ${index + 1}`} type="url" value={item.url} onChange={(url) => setDraftFeeds((current) => current.map((feed, feedIndex) => feedIndex === index ? {...feed, url} : feed))} />
                <Button size="sm" variant="ghost" onPress={() => setDraftFeeds((current) => current.filter((_feed, feedIndex) => feedIndex !== index))}>Remove</Button>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
