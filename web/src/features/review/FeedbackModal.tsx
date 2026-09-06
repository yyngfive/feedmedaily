import {Button} from "@heroui/react";
import React from "react";

import {TextAreaField, TextInputField} from "../../shared/components/FormFields";
import {ModalShell} from "../../shared/components/ModalShell";
import {SelectField} from "../../shared/components/SelectField";
import type {Paper, Relevance, ReportTopics} from "../../shared/types";

const feedbackOptions = [
  {value: "direct", label: "Direct"},
  {value: "indirect", label: "Indirect"},
  {value: "unrelated", label: "Unrelated"},
] as const;

export function FeedbackModal({
  createTopic,
  note,
  onClose,
  onNoteChange,
  onSubmit,
  onTopicChange,
  onValueChange,
  paper,
  topics,
  topicValue,
  value,
}: {
  createTopic: (label: string) => Promise<{id: string; label: string} | null>;
  note: string;
  onClose: () => void;
  onNoteChange: (value: string) => void;
  onSubmit: () => void;
  onTopicChange: (value: string) => void;
  onValueChange: (value: Relevance) => void;
  paper: Paper | null;
  topics: ReportTopics | null | undefined;
  topicValue: string;
  value: Relevance;
}) {
  const [newTopicLabel, setNewTopicLabel] = React.useState("");
  const [creatingTopic, setCreatingTopic] = React.useState(false);
  React.useEffect(() => setNewTopicLabel(""), [paper?.id]);
  if (!paper) {
    return null;
  }

  // 主题下拉：无主题 + 注册表全部主题。孤儿主题（已删除）不出现在选项里。
  const topicOptions = [
    {value: "", label: "No topic"},
    ...(topics?.items ?? []).map((topic) => ({value: topic.id, label: topic.label})),
  ];
  const knownTopic = topicOptions.some((option) => option.value === topicValue);
  const submitNewTopic = async () => {
    const label = newTopicLabel.trim();
    if (!label || creatingTopic) return;
    setCreatingTopic(true);
    const created = await createTopic(label);
    setCreatingTopic(false);
    if (created) {
      onTopicChange(created.id);
      setNewTopicLabel("");
    }
  };

  return (
    <ModalShell
      eyebrow="Mark wrong"
      footer={
        <>
          <Button size="sm" variant="ghost" onPress={onClose}>
            Cancel
          </Button>
          <Button size="sm" onPress={onSubmit}>
            Save feedback
          </Button>
        </>
      }
      onClose={onClose}
      title={paper.title}
    >
      <SelectField
        label="Correct label"
        options={[...feedbackOptions]}
        value={value}
        onChange={(nextValue) => onValueChange(nextValue as Relevance)}
      />
      <SelectField
        label="Topic"
        options={topicOptions}
        value={knownTopic ? topicValue : ""}
        onChange={onTopicChange}
      />
      <div className="flex items-end gap-2">
        <div className="flex-1">
          <TextInputField
            label="New topic"
            placeholder="Create a topic for the profile"
            value={newTopicLabel}
            onChange={setNewTopicLabel}
          />
        </div>
        <Button
          isDisabled={!newTopicLabel.trim() || creatingTopic}
          isIconOnly
          size="sm"
          onPress={() => void submitNewTopic()}
        >
          {creatingTopic ? "…" : "Add"}
        </Button>
      </div>
      <TextAreaField
        label="Note"
        placeholder="Why should this be classified differently?"
        rows={5}
        value={note}
        onChange={onNoteChange}
      />
    </ModalShell>
  );
}
