import {Button} from "@heroui/react";

import {ModalShell} from "../common/ModalShell";
import {SelectField} from "../common/SelectField";
import type {Paper, Relevance} from "../../types";

const feedbackOptions = [
  {value: "direct", label: "Direct"},
  {value: "indirect", label: "Indirect"},
  {value: "unrelated", label: "Unrelated"},
] as const;

export function FeedbackModal({
  note,
  onClose,
  onNoteChange,
  onSubmit,
  onValueChange,
  paper,
  value,
}: {
  note: string;
  onClose: () => void;
  onNoteChange: (value: string) => void;
  onSubmit: () => void;
  onValueChange: (value: Relevance) => void;
  paper: Paper | null;
  value: Relevance;
}) {
  if (!paper) {
    return null;
  }

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
      <label className="block text-sm font-medium text-[var(--ink)]">
        Note
        <textarea
          className="mt-2 min-h-28 w-full rounded-md border border-[var(--line)] px-3 py-2 text-sm"
          placeholder="Why should this be classified differently?"
          value={note}
          onChange={(event) => onNoteChange(event.target.value)}
        />
      </label>
    </ModalShell>
  );
}
