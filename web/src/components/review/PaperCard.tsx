import {Card, Chip} from "@heroui/react";
import React from "react";

import {relevanceLabel, relevanceTone} from "../../app/constants";
import {authorsLine, feedbackLabel, paperDate} from "../../app/utils";
import type {Paper} from "../../types";

export function PaperCard({
  isSelected,
  isUnread,
  onSelect,
  paper,
}: {
  isSelected: boolean;
  isUnread: boolean;
  onSelect: () => void;
  paper: Paper;
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
      className={`border-l-4 ${tone.ring} ${isSelected ? "outline outline-(--accent)" : ""}`}
    >
      <div
        className="block w-full cursor-pointer text-left"
        role="button"
        tabIndex={0}
        onClick={onSelect}
        onKeyDown={handleSelectKeyDown}
      >
        <Card.Header className="gap-3">
          <div className="flex flex-1 flex-wrap items-center gap-2 my-1">
            {isUnread ? (
              <span aria-label="Unread" className="size-2 rounded-full bg-(--unread)" title="Unread"></span>
            ) : null}
            <span className={`text-sm font-semibold ${tone.text}`}>
              {Math.round(paper.classification.confidence * 100)}%
            </span>
            <Chip color={tone.chip} size="sm" variant="soft">
              {relevanceLabel[paper.classification.relevance]}
            </Chip>
            {feedbackText ? (
              <Chip color="danger" size="sm" variant="soft">
                {feedbackText}
              </Chip>
            ) : null}
          </div>

        </Card.Header>
        <Card.Content className="gap-3">
          <div>
            <Card.Title className="line-clamp-2 text-lg leading-6">{paper.title}</Card.Title>
            {paper.classification.translated_title_zh ? (
              <Card.Description className="mt-1 line-clamp-2 text-base">
                {paper.classification.translated_title_zh}
              </Card.Description>
            ) : null}
          </div>
          <p className="text-base text-muted">{paper.journal || "Unknown journal"}</p>
          <p className="text-sm text-muted">
            {paperDate(paper)} · {authorsLine(paper)}
          </p>
        </Card.Content>
      </div>
    </Card>
  );
}
