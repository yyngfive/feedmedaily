import {Button, Card} from "@heroui/react";
import React from "react";

import {feedCatalog} from "../../data/feedCatalog";
import {CheckboxRow, TextInputField} from "../../shared/components/FormFields";
import type {FeedSubscription} from "../../shared/types";

export function FeedsTab({
  feeds,
  feedsSaving,
  onAddFeed,
  onAddFeeds,
  onFeedChange,
  onRemoveFeed,
  onSaveFeeds,
}: {
  feeds: FeedSubscription[];
  feedsSaving: boolean;
  onAddFeed: () => void;
  onAddFeeds: (feeds: FeedSubscription[]) => void;
  onFeedChange: (index: number, field: "journal" | "url", value: string) => void;
  onRemoveFeed: (index: number) => void;
  onSaveFeeds: () => void;
}) {
  const [newFeedJournal, setNewFeedJournal] = React.useState("");
  const [newFeedURL, setNewFeedURL] = React.useState("");
  const [catalogPublisher, setCatalogPublisher] = React.useState("All");
  const [catalogQuery, setCatalogQuery] = React.useState("");
  const [selectedCatalogURLs, setSelectedCatalogURLs] = React.useState<string[]>([]);
  const [feedsEditing, setFeedsEditing] = React.useState(false);
  const catalogPublishers = React.useMemo(() => ["All", ...Array.from(new Set(feedCatalog.map((item) => item.publisher))).sort()], []);
  const existingFeedURLs = React.useMemo(() => new Set(feeds.map((feed) => feed.url.trim()).filter(Boolean)), [feeds]);
  const catalogMatches = React.useMemo(() => {
    const query = catalogQuery.trim().toLowerCase();
    return feedCatalog.filter((item) =>
      (catalogPublisher === "All" || item.publisher === catalogPublisher) &&
      (!query || `${item.journal} ${item.publisher} ${item.subjects.join(" ")}`.toLowerCase().includes(query)),
    );
  }, [catalogPublisher, catalogQuery]);
  const selectedCatalogFeeds = React.useMemo(() => feedCatalog.filter((item) => selectedCatalogURLs.includes(item.url)), [selectedCatalogURLs]);

  const addSelectedCatalogFeeds = () => {
    onAddFeeds(selectedCatalogFeeds.map((item) => ({journal: item.journal, url: item.url})));
    setSelectedCatalogURLs([]);
    setFeedsEditing(true);
  };
  const addDraftFeed = () => {
    const journal = newFeedJournal.trim();
    const url = newFeedURL.trim();
    if (!journal || !url) return;
    const nextIndex = feeds.length;
    onAddFeed();
    window.setTimeout(() => {
      onFeedChange(nextIndex, "journal", journal);
      onFeedChange(nextIndex, "url", url);
    }, 0);
    setNewFeedJournal("");
    setNewFeedURL("");
    setFeedsEditing(true);
  };

  return (
    <div className="mt-5 space-y-5">
      <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
        <Card.Header><h3 className="text-xl font-semibold text-(--ink)">Feeds</h3></Card.Header>
        <Card.Content className="space-y-5">
          <section className="border-b border-(--line) pb-5">
            <div className="mt-3 rounded-md border border-(--line) bg-(--paper) p-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="text-sm font-medium text-(--ink)">Catalog</p>
                <a className="text-sm text-muted underline-offset-3 hover:underline" href="https://github.com/yyngfive/sci-rss-list" rel="noreferrer" target="_blank">Not Listed?</a>
              </div>
              <TextInputField hideLabel className="mt-3 w-full" label="Search feed catalog" placeholder="Search journal, publisher, or subject" value={catalogQuery} onChange={setCatalogQuery} />
              <div className="mt-3 flex max-h-32 flex-wrap gap-2 overflow-y-auto pr-1">
                {catalogPublishers.map((publisher) => <Button key={publisher} size="sm" variant={catalogPublisher === publisher ? "secondary" : "outline"} onPress={() => setCatalogPublisher(publisher)}>{publisher}</Button>)}
              </div>
              <div className="mt-3 max-h-72 space-y-2 overflow-y-auto pr-1">
                {catalogMatches.length === 0 ? <p className="rounded-md border border-(--line) px-3 py-3 text-sm text-muted">No catalog matches.</p> : catalogMatches.map((item) => {
                  const selected = selectedCatalogURLs.includes(item.url);
                  const exists = existingFeedURLs.has(item.url.trim());
                  return (
                    <CheckboxRow key={item.url} checked={selected} className="rounded-md border border-(--line) px-3 py-2 text-sm" disabled={exists} onChange={() => setSelectedCatalogURLs((current) => current.includes(item.url) ? current.filter((value) => value !== item.url) : [...current, item.url])}>
                      <span className="min-w-0"><span className="flex flex-wrap items-center gap-2"><span className="font-medium text-(--ink)">{item.journal}</span>{exists ? <span className="text-warning">Added</span> : null}</span><span className="mt-1 block break-all text-xs text-muted">{item.url}</span></span>
                    </CheckboxRow>
                  );
                })}
              </div>
              <Button className="mt-3" isDisabled={selectedCatalogFeeds.length === 0} size="sm" onPress={addSelectedCatalogFeeds}>Add selected</Button>
            </div>
            <div className="mt-3 grid gap-3 md:grid-cols-[minmax(180px,0.7fr)_minmax(260px,1fr)_auto]">
              <TextInputField hideLabel label="New feed journal name" placeholder="Journal name" value={newFeedJournal} onChange={setNewFeedJournal} />
              <TextInputField hideLabel label="New feed URL" placeholder="RSS URL" type="url" value={newFeedURL} onChange={setNewFeedURL} />
              <Button isDisabled={!newFeedJournal.trim() || !newFeedURL.trim()} size="sm" onPress={addDraftFeed}>{feeds.length === 0 ? "Add first feed" : "Add feed"}</Button>
            </div>
          </section>
          <section>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h3 className="text-sm font-semibold text-(--ink)">Feed subscriptions ({feeds.length})</h3>
              <div className="flex flex-wrap gap-2">
                {feeds.length > 0 ? <Button size="sm" variant="outline" onPress={() => setFeedsEditing((current) => !current)}>{feedsEditing ? "Cancel" : "Edit"}</Button> : null}
                {feedsEditing ? <Button size="sm" isDisabled={feedsSaving} onPress={onSaveFeeds}>{feedsSaving ? "Saving..." : "Save feeds"}</Button> : null}
              </div>
            </div>
            <div className="mt-3 space-y-2">
              {feeds.length === 0 ? <p className="rounded-md border border-(--line) px-3 py-4 text-sm text-muted">No RSS feeds configured yet.</p> : !feedsEditing ? (
                <div className="max-h-[58vh] space-y-2 overflow-y-auto pr-1">
                  {feeds.map((item, index) => <div key={item.client_id ?? String(index)} className="rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm"><p className="font-medium text-(--ink)">{item.journal || "Untitled feed"}</p><p className="mt-1 break-all text-xs text-muted">{item.url}</p></div>)}
                </div>
              ) : (
                <>
                  <div className="hidden grid-cols-[minmax(160px,0.7fr)_minmax(280px,1fr)_96px] gap-3 px-3 text-sm font-semibold text-(--ink) md:grid"><span>Name</span><span>URL</span><span>Action</span></div>
                  <div className="max-h-[58vh] space-y-2 overflow-y-auto pr-1">
                    {feeds.map((item, index) => (
                      <div key={item.client_id ?? String(index)} className="grid gap-3 rounded-md border border-(--line) bg-(--paper) p-3 md:grid-cols-[minmax(160px,0.7fr)_minmax(280px,1fr)_96px]">
                        <TextInputField hideLabel className="w-full" label={`Feed name ${index + 1}`} value={item.journal} onChange={(value) => onFeedChange(index, "journal", value)} />
                        <TextInputField hideLabel className="w-full" label={`Feed URL ${index + 1}`} type="url" value={item.url} onChange={(value) => onFeedChange(index, "url", value)} />
                        <Button size="sm" variant="ghost" onPress={() => onRemoveFeed(index)}>Delete</Button>
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
  );
}
