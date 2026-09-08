import {Card, Chip} from "@heroui/react";
import React from "react";

import {relevanceLabel, relevanceTone} from "../../app/constants";
import {authorsLine, feedbackLabel, paperDate, paperTopicID, paperTopicState, topicLabelFor} from "../../app/utils";
import {TOPIC_NONE} from "../../shared/types";
import type {Paper, ReportTopics} from "../../shared/types";

export function PaperCard({
  isSelected,
  isUnread,
  onSelect,
  paper,
  topics,
}: {
  isSelected: boolean;
  isUnread: boolean;
  onSelect: () => void;
  paper: Paper;
  topics: ReportTopics | null | undefined;
}) {
  const tone = relevanceTone[paper.classification.relevance];
  const feedbackText = feedbackLabel(paper);
  // 主题 chip：有主题判定的 related 论文一定显示——真实主题显示 label，
  // 哨兵 none 与孤儿 id（注册表已不含该 id）统一显示“未归类”，与侧栏
  // 计数、Topic 过滤的口径保持一致；未判定与 unrelated 不显示 chip。
  const topicState = paperTopicState(paper);
  const topicID = paperTopicID(paper, topics);
  const topicLabel = topicID ? topicLabelFor(topicID, topics) : null;
  const originalTopicText =
    topicState === null || topicState === "unprocessed"
      ? null
      : topicID && topicLabel
        ? topicLabel
        : "Unassigned";
  // 待生效的主题纠正（最近一条 open feedback）：仅在最新分类尚未满足纠正时，
  // chip 追加“原值 -> 纠正值”；落实后分类本身显示纠正值，chip 回到普通主题形态。
  // 未落实判定用原始值比较（空数组按哨兵 none），与后端对账口径一致。
  const pendingTopic = paper.feedback_status?.corrected_topic ?? null;
  const currentTopicRaw = paper.classification.topic_tags?.[0] ?? TOPIC_NONE;
  const topicPending = pendingTopic != null && currentTopicRaw !== pendingTopic;
  const pendingTopicText = !topicPending
    ? null
    : pendingTopic === TOPIC_NONE
      ? "No topic"
      : topicLabelFor(pendingTopic, topics) ?? "Unassigned";
  const topicChipText = pendingTopicText
    ? `${originalTopicText ?? (topicState === "unprocessed" ? "Not processed" : null) ?? "Unassigned"} -> ${pendingTopicText}`
    : originalTopicText;
  const handleSelectKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onSelect();
    }
  };

  return (
    <Card
      className="paper-card" data-selected={isSelected}
    >
      <div
        className="flex w-full cursor-pointer flex-col gap-3 text-left outline-none"
        role="button"
        aria-pressed={isSelected}
        tabIndex={0}
        onClick={onSelect}
        onKeyDown={handleSelectKeyDown}
      >
        <Card.Header className="order-2 gap-3">
          <div className="flex flex-1 flex-wrap items-center gap-2">
            {isUnread ? (
              <span aria-label="Unread" className="size-2 rounded-full bg-(--unread)" title="Unread"></span>
            ) : null}
            <span className="order-last text-xs text-muted" title="Classification confidence">
              {Math.round(paper.classification.confidence * 100)}%
            </span>
            <Chip color={tone.chip} size="sm" variant="soft">
              {relevanceLabel[paper.classification.relevance]}
            </Chip>
            {topicChipText ? (
              <Chip color="default" size="sm" variant="soft">
                {topicChipText}
              </Chip>
            ) : null}
            {feedbackText ? (
              <Chip color="danger" size="sm" variant="soft">
                {feedbackText}
              </Chip>
            ) : null}
          </div>

        </Card.Header>
        <Card.Content className="gap-3">
          <div>
            <Card.Title className="line-clamp-2 text-lg font-semibold leading-6">{paper.title}</Card.Title>
            {paper.classification.translated_title_zh ? (
              <Card.Description className="mt-1 line-clamp-2 text-base leading-7">
                {paper.classification.translated_title_zh}
              </Card.Description>
            ) : null}
          </div>
          <p className="text-sm text-muted">{paper.journal || "Unknown journal"}</p>
          <p className="text-sm text-muted">
            {paperDate(paper)} · {authorsLine(paper)}
          </p>
        </Card.Content>
      </div>
    </Card>
  );
}
