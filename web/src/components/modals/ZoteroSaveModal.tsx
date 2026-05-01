import {Button} from "@heroui/react";

import {ModalShell} from "../common/ModalShell";
import {SelectField} from "../common/SelectField";
import {StatusBanner} from "../common/StatusBanner";
import type {Paper, ZoteroCollectionOption} from "../../types";

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
  if (!paper) {
    return null;
  }
  const selectedCollectionLabel =
    collections.find((item) => item.key === selectedCollectionKey)?.path_label ??
    "No collection (save to library only)";

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
      <SelectField
        disabled={loading || saving}
        label="Save into collection"
        options={[
          {value: "", label: "No collection (save to library only)"},
          ...collections.map((item) => ({value: item.key, label: item.path_label})),
        ]}
        value={selectedCollectionKey}
        onChange={onCollectionChange}
      />
      <div className="space-y-2 rounded-md border border-(--line) bg-(--paper) p-3 text-sm">
        <p className="font-medium text-(--ink)">
          This sends a complete journal article record to Zotero, not just a DOI.
        </p>
        <div className="space-y-1 text-muted">
          <p>Title: {paper.title}</p>
          <p>Journal: {paper.journal || paper.feed_title || "Unknown journal"}</p>
          <p>DOI: {paper.doi || "—"}</p>
          <p>URL: {paper.url}</p>
          <p>Tags: {["scirssagent", paper.classification.relevance, ...paper.classification.topic_tags].join(", ") || "—"}</p>
          <p>Collection: {selectedCollectionLabel}</p>
        </div>
      </div>

      {loading ? <StatusBanner tone="info">Loading Zotero collections...</StatusBanner> : null}
      {error ? <StatusBanner tone="danger">{error}</StatusBanner> : null}
    </ModalShell>
  );
}
