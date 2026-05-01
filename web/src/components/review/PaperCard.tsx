import {Card, Chip} from "@heroui/react";
import React from "react";

import {relevanceLabel, relevanceTone} from "../../app/constants";
import {authorsLine, feedbackLabel, paperDate, sentence} from "../../app/utils";
import {tagLabel} from "../../reportData";
import type {ClassificationProfile, Paper} from "../../types";

export function PaperCard({
  isSelected,
  isUnread,
  onSelect,
  paper,
  profile,
}: {
  isSelected: boolean;
  isUnread: boolean;
  onSelect: () => void;
  paper: Paper;
  profile: ClassificationProfile | null;
}) {
  const tone = relevanceTone[paper.classification.relevance];
  const feedbackText = feedbackLabel(paper);
  const handleSelectKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onSelect();
    }
  };

  return (
    <Card
      className={`border-l-4 ${tone.ring} ${isSelected ? "outline outline-2 outline-[var(--accent)]" : ""}`}
    >
      <div
        className="block w-full cursor-pointer text-left"
        role="button"
        tabIndex={0}
        onClick={onSelect}
        onKeyDown={handleSelectKeyDown}
      >
        <Card.Header className="gap-3">
          <div className="flex flex-1 flex-wrap items-center gap-2">
            {isUnread ? (
              <span
                aria-label="Unread"
                className="size-2 rounded-full bg-[var(--unread)]"
                title="Unread"
              />
            ) : null}
            <Chip color={tone.chip} size="sm" variant="soft">
              {relevanceLabel[paper.classification.relevance]}
            </Chip>
            {paper.classification.topic_tags.slice(0, 2).map((tag) => (
              <Chip key={tag} size="sm" variant="secondary">
                {tagLabel(tag, profile)}
              </Chip>
            ))}
            {feedbackText ? (
              <Chip color="danger" size="sm" variant="soft">
                {feedbackText}
              </Chip>
            ) : null}
          </div>
          <span className={`text-sm font-semibold ${tone.text}`}>
            {Math.round(paper.classification.confidence * 100)}%
          </span>
        </Card.Header>
        <Card.Content className="gap-3">
          <div>
            <Card.Title className="line-clamp-2 text-lg leading-6">{paper.title}</Card.Title>
            {paper.classification.translated_title_zh ? (
              <Card.Description className="mt-1 line-clamp-2 text-sm">
                {paper.classification.translated_title_zh}
              </Card.Description>
            ) : null}
          </div>
          <p className="text-sm text-[var(--muted)]">
            {paper.journal || "Unknown journal"} · {paperDate(paper)} · {authorsLine(paper)}
          </p>
          <p className="line-clamp-2 text-sm leading-6 text-[var(--body)]">
            {sentence(paper.abstract ?? paper.classification.reason)}
          </p>
          <p className="line-clamp-2 text-sm leading-6 text-[var(--body)]">
            <span className="font-semibold text-[var(--ink)]">Why relevant:</span>{" "}
            {paper.classification.reason}
          </p>
        </Card.Content>
      </div>
    </Card>
  );
}
