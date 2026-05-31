import {Button, Card, Input} from "@heroui/react";
import React from "react";

import type {ClassificationProfile} from "../../types";

type ProfileDraft = {
  name: string;
  scope: string;
  directRules: string[];
  indirectRules: string[];
  unrelatedRules: string[];
};

function createDraft(profile: ClassificationProfile): ProfileDraft {
  return {
    name: profile.meta.name,
    scope: profile.scope,
    directRules: [...profile.relevance_rules.direct],
    indirectRules: [...profile.relevance_rules.indirect],
    unrelatedRules: [...profile.relevance_rules.unrelated],
  };
}

function RuleSection({items, title}: {items: string[]; title: string}) {
  return (
    <section className="space-y-2">
      <h3 className="text-sm font-semibold uppercase tracking-[0.12em] text-muted">{title}</h3>
      {items.length === 0 ? (
        <p className="text-sm text-muted">No rules yet.</p>
      ) : (
        <ul className="list-disc space-y-2 pl-5 text-sm leading-6 text-(--body)">
          {items.map((item) => (
            <li key={`${title}-${item}`}>{item}</li>
          ))}
        </ul>
      )}
    </section>
  );
}

function RuleListEditor({
  items,
  onAdd,
  onChange,
  onRemove,
  title,
}: {
  items: string[];
  onAdd: () => void;
  onChange: (index: number, value: string) => void;
  onRemove: (index: number) => void;
  title: string;
}) {
  return (
    <section className="space-y-3 rounded-lg border border-(--line) bg-(--paper) p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 className="text-sm font-semibold text-(--ink)">{title}</h3>
          <p className="text-sm leading-6 text-muted">One rule per row. Keep rules compact and reason-based.</p>
        </div>
        <Button size="sm" variant="outline" onPress={onAdd}>
          Add rule
        </Button>
      </div>
      {items.length === 0 ? (
        <p className="text-sm text-muted">No rules yet.</p>
      ) : (
        <div className="space-y-3">
          {items.map((item, index) => (
            <div
              key={`${title}-${index}`}
              className="grid gap-3 rounded-lg border border-(--line) p-3 md:grid-cols-[minmax(0,1fr)_auto]"
            >
              <Input
                aria-label={`${title} rule ${index + 1}`}
                value={item}
                onChange={(event) => onChange(index, event.target.value)}
              />
              <Button size="sm" variant="ghost" onPress={() => onRemove(index)}>
                Delete
              </Button>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function DraftField({
  children,
  hint,
  title,
}: {
  children: React.ReactNode;
  hint?: string;
  title: string;
}) {
  return (
    <label className="block space-y-2">
      <span className="text-sm font-semibold text-(--ink)">{title}</span>
      {hint ? <p className="text-sm leading-6 text-muted">{hint}</p> : null}
      {children}
    </label>
  );
}

export function ProfileRulesDocument({
  editable = false,
  onSave,
  profile,
  saving = false,
  title = "Current classification profile",
}: {
  editable?: boolean;
  onSave?: (profile: ClassificationProfile) => Promise<void> | void;
  profile: ClassificationProfile;
  saving?: boolean;
  title?: string;
}) {
  const [editing, setEditing] = React.useState(false);
  const [draft, setDraft] = React.useState<ProfileDraft>(() => createDraft(profile));
  const editEnabled = editable && typeof onSave === "function";

  React.useEffect(() => {
    setDraft(createDraft(profile));
    setEditing(false);
  }, [profile]);

  const saveDraft = async () => {
    if (!onSave) {
      return;
    }
    const nextProfile: ClassificationProfile = {
      ...profile,
      meta: {
        ...profile.meta,
        name: draft.name.trim(),
      },
      scope: draft.scope.trim(),
      relevance_rules: {
        direct: draft.directRules.map((item) => item.trim()).filter(Boolean),
        indirect: draft.indirectRules.map((item) => item.trim()).filter(Boolean),
        unrelated: draft.unrelatedRules.map((item) => item.trim()).filter(Boolean),
      },
      topic_taxonomy: [],
      few_shots: [],
    };
    await onSave(nextProfile);
    setEditing(false);
  };

  const cancelEditing = () => {
    setDraft(createDraft(profile));
    setEditing(false);
  };

  return (
    <Card className="border border-(--line) bg-(--paper-accent)">
      <Card.Header className="flex flex-col items-start gap-3">
        <div className="flex w-full flex-wrap items-start justify-between gap-3">
          <div className="space-y-1">
            <p className="text-sm font-semibold text-(--ink)">{title}</p>
            <h2 className="text-2xl font-semibold text-(--ink)">
              {profile.meta.name} · v{profile.meta.version}
            </h2>
            <p className="text-sm text-muted">
              {profile.meta.updated_at.slice(0, 10)} · created {profile.meta.created_at.slice(0, 10)}
            </p>
          </div>
          {editEnabled ? (
            editing ? (
              <div className="flex flex-wrap gap-2">
                <Button isDisabled={saving} size="sm" variant="ghost" onPress={cancelEditing}>
                  Cancel
                </Button>
                <Button isDisabled={saving} size="sm" onPress={() => void saveDraft()}>
                  {saving ? "Saving..." : "Save profile"}
                </Button>
              </div>
            ) : (
              <Button isDisabled={saving} size="sm" onPress={() => setEditing(true)}>
                Edit profile
              </Button>
            )
          ) : null}
        </div>
      </Card.Header>
      <Card.Content className="space-y-6">
        {editing ? (
          <>
            <DraftField title="Profile name">
              <input
                className="w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink)"
                value={draft.name}
                onChange={(event) => setDraft((current) => ({...current, name: event.target.value}))}
              />
            </DraftField>

            <DraftField title="Scope">
              <textarea
                className="min-h-28 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm leading-6 text-(--ink)"
                value={draft.scope}
                onChange={(event) => setDraft((current) => ({...current, scope: event.target.value}))}
              />
            </DraftField>

            <div className="space-y-4">
              <RuleListEditor
                items={draft.directRules}
                title="Direct rules"
                onAdd={() =>
                  setDraft((current) => ({...current, directRules: [...current.directRules, ""]}))
                }
                onChange={(index, value) =>
                  setDraft((current) => ({
                    ...current,
                    directRules: current.directRules.map((item, itemIndex) =>
                      itemIndex === index ? value : item,
                    ),
                  }))
                }
                onRemove={(index) =>
                  setDraft((current) => ({
                    ...current,
                    directRules: current.directRules.filter((_item, itemIndex) => itemIndex !== index),
                  }))
                }
              />
              <RuleListEditor
                items={draft.indirectRules}
                title="Indirect rules"
                onAdd={() =>
                  setDraft((current) => ({...current, indirectRules: [...current.indirectRules, ""]}))
                }
                onChange={(index, value) =>
                  setDraft((current) => ({
                    ...current,
                    indirectRules: current.indirectRules.map((item, itemIndex) =>
                      itemIndex === index ? value : item,
                    ),
                  }))
                }
                onRemove={(index) =>
                  setDraft((current) => ({
                    ...current,
                    indirectRules: current.indirectRules.filter((_item, itemIndex) => itemIndex !== index),
                  }))
                }
              />
              <RuleListEditor
                items={draft.unrelatedRules}
                title="Unrelated rules"
                onAdd={() =>
                  setDraft((current) => ({...current, unrelatedRules: [...current.unrelatedRules, ""]}))
                }
                onChange={(index, value) =>
                  setDraft((current) => ({
                    ...current,
                    unrelatedRules: current.unrelatedRules.map((item, itemIndex) =>
                      itemIndex === index ? value : item,
                    ),
                  }))
                }
                onRemove={(index) =>
                  setDraft((current) => ({
                    ...current,
                    unrelatedRules: current.unrelatedRules.filter((_item, itemIndex) => itemIndex !== index),
                  }))
                }
              />
            </div>
          </>
        ) : (
          <>
            <section className="space-y-2">
              <h3 className="text-sm font-semibold uppercase tracking-[0.12em] text-muted">Scope</h3>
              <p className="text-sm leading-7 text-(--body)">{profile.scope}</p>
            </section>
            <RuleSection items={profile.relevance_rules.direct} title="Direct" />
            <RuleSection items={profile.relevance_rules.indirect} title="Indirect" />
            <RuleSection items={profile.relevance_rules.unrelated} title="Unrelated" />
          </>
        )}
      </Card.Content>
    </Card>
  );
}
