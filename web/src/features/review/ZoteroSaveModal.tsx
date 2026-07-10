import React, {type Key} from "react";
import {Button, Input, Label, ListBox, ListBoxItem, TextField} from "@heroui/react";

import {ModalShell} from "../../shared/components/ModalShell";
import {StatusBanner} from "../../shared/components/StatusBanner";
import type {Paper, ZoteroCollectionOption} from "../../shared/types";

const libraryCollectionKey = "__library__";

export function ZoteroSaveModal({
  collections,
  error,
  loading,
  onClose,
  onCollectionChange,
  onSubmit,
  paper,
  saving,
  selectedCollectionKey,
}: {
  collections: ZoteroCollectionOption[];
  error: string | null;
  loading: boolean;
  onClose: () => void;
  onCollectionChange: (value: string) => void;
  onSubmit: () => void;
  paper: Paper | null;
  saving: boolean;
  selectedCollectionKey: string;
}) {
  const [query, setQuery] = React.useState("");
  if (!paper) {
    return null;
  }
  const selectedCollectionLabel =
    collections.find((item) => item.key === selectedCollectionKey)?.path_label ??
    "No collection (save to library only)";
  const authorsLabel = paper.authors?.length ? paper.authors.join(", ") : "Unknown authors";
  const dateLabel = paper.published_date ?? "Unknown date";
  const yearLabel = paper.published_date?.slice(0, 4) ?? "Unknown year";

  return (
    <ModalShell
      eyebrow="Zotero"
      footer={
        <>
          <Button size="sm" variant="ghost" onPress={onClose}>
            Cancel
          </Button>
          <Button isDisabled={loading || saving} size="sm" onPress={onSubmit}>
            {saving ? "Saving..." : "Save to Zotero"}
          </Button>
        </>
      }
      onClose={onClose}
      title={paper.title}
    >
      <ZoteroCollectionPicker
        collections={collections}
        disabled={loading || saving}
        label="Save into collection"
        query={query}
        value={selectedCollectionKey}
        onQueryChange={setQuery}
        onChange={onCollectionChange}
      />
      <div className="space-y-2 rounded-md border border-(--line) bg-(--paper) p-3 text-sm">
        <div className="space-y-1 text-muted">
          <p>Title: {paper.title}</p>
          <p>Journal: {paper.journal || paper.feed_title || "Unknown journal"}</p>
          <p>Authors: {authorsLabel}</p>
          <p>Year/Date: {yearLabel} / {dateLabel}</p>
          <p>DOI: {paper.doi || "—"}</p>
          <p>URL: {paper.url}</p>
          <p>Collection: {selectedCollectionLabel}</p>
        </div>
      </div>

      {loading ? <StatusBanner tone="info">Loading Zotero collections...</StatusBanner> : null}
      {error ? <StatusBanner tone="danger">{error}</StatusBanner> : null}
    </ModalShell>
  );
}

function ZoteroCollectionPicker({
  collections,
  disabled,
  label,
  onChange,
  onQueryChange,
  query,
  value,
}: {
  collections: ZoteroCollectionOption[];
  disabled: boolean;
  label: string;
  onChange: (value: string) => void;
  onQueryChange: (value: string) => void;
  query: string;
  value: string;
}) {
  const normalizedQuery = query.trim().toLowerCase();
  const visibleCollections = React.useMemo(() => {
    if (!normalizedQuery) {
      return collections;
    }
    return collections.filter((item) =>
      `${item.name} ${item.path_label}`.toLowerCase().includes(normalizedQuery),
    );
  }, [collections, normalizedQuery]);
  const selectedKeys = React.useMemo(() => new Set([value || libraryCollectionKey]), [value]);
  const handleSelectionChange = (keys: "all" | Set<Key>) => {
    if (disabled || keys === "all") {
      return;
    }
    const nextKey = Array.from(keys)[0];
    onChange(nextKey === libraryCollectionKey ? "" : String(nextKey ?? ""));
  };
  const showLibraryOption =
    !normalizedQuery || "no collection save to library only".includes(normalizedQuery);

  return (
    <div className="space-y-2">
      <TextField isDisabled={disabled} value={query} onChange={onQueryChange}>
        <Label>{label}</Label>
        <Input className="w-full" placeholder="Search collections" />
      </TextField>
      <ListBox
        aria-label={label}
        className={`max-h-72 overflow-y-auto rounded-md border border-(--line) bg-(--paper) p-1 ${
          disabled ? "pointer-events-none opacity-60" : ""
        }`}
        selectedKeys={selectedKeys}
        selectionMode="single"
        onSelectionChange={handleSelectionChange}
      >
        {showLibraryOption ? (
          <ListBoxItem id={libraryCollectionKey} textValue="No collection (save to library only)">
            <span className="text-sm">No collection (save to library only)</span>
          </ListBoxItem>
        ) : null}
        {visibleCollections.map((item) => {
          const parentLabel = item.path_label === item.name ? "" : item.path_label.replace(` / ${item.name}`, "");
          return (
            <ListBoxItem
              key={item.key}
              id={item.key}
              style={{paddingLeft: `${0.75 + item.depth * 1.25}rem`}}
              textValue={item.path_label}
            >
              <span className="flex min-w-0 flex-col">
                <span className="truncate text-sm">{item.name}</span>
                {parentLabel ? <span className="truncate text-xs text-muted">{parentLabel}</span> : null}
              </span>
            </ListBoxItem>
          );
        })}
        {!showLibraryOption && visibleCollections.length === 0 ? (
          <ListBoxItem id="__empty__" isDisabled textValue="No matching collections">
            <span className="text-sm text-muted">No matching collections</span>
          </ListBoxItem>
        ) : null}
      </ListBox>
    </div>
  );
}
