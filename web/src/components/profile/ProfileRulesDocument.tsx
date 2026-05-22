import {Button, Card} from "@heroui/react";
import React from "react";

import type {ClassificationProfile, ProfileFewShot, Relevance, TopicDefinition} from "../../types";

type DraftTopic = {
  id: string;
  label: string;
};

type DraftFewShot = {
  title: string;
  relevance: Relevance;
  tags: string;
  rationale: string;
};

type ProfileDraft = {
  name: string;
  scope: string;
  directRules: string;
  indirectRules: string;
  unrelatedRules: string;
  topics: DraftTopic[];
  fewShots: DraftFewShot[];
};

function listToMultiline(items: string[]) {
  return items.join("\n");
}

function multilineToList(value: string) {
  return value
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function tagsToInput(tags: string[]) {
  return tags.join(", ");
}

function inputToTags(value: string) {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function createDraft(profile: ClassificationProfile): ProfileDraft {
  return {
    name: profile.meta.name,
    scope: profile.scope,
    directRules: listToMultiline(profile.relevance_rules.direct),
    indirectRules: listToMultiline(profile.relevance_rules.indirect),
    unrelatedRules: listToMultiline(profile.relevance_rules.unrelated),
    topics: profile.topic_taxonomy.map((item) => ({id: item.id, label: item.label})),
    fewShots: profile.few_shots.map((item) => ({
      title: item.title,
      relevance: item.relevance,
      tags: tagsToInput(item.tags),
      rationale: item.rationale,
    })),
  };
}

function topicSummary(items: TopicDefinition[]) {
  if (items.length === 0) {
    return [];
  }
  return items.map((item) => `${item.label} (${item.id})`);
}

function fewShotSummary(items: ProfileFewShot[]) {
  if (items.length === 0) {
    return [];
  }
  return items;
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
        direct: multilineToList(draft.directRules),
        indirect: multilineToList(draft.indirectRules),
        unrelated: multilineToList(draft.unrelatedRules),
      },
      topic_taxonomy: draft.topics
        .filter((item) => item.id.trim() || item.label.trim())
        .map((item) => ({id: item.id.trim(), label: item.label.trim()})),
      few_shots: draft.fewShots
        .filter(
          (item) =>
            item.title.trim() ||
            item.tags.trim() ||
            item.rationale.trim() ||
            item.relevance !== "direct",
        )
        .map((item) => ({
          title: item.title.trim(),
          relevance: item.relevance,
          tags: inputToTags(item.tags),
          rationale: item.rationale.trim(),
        })),
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

            <div className="grid gap-4 lg:grid-cols-3">
              <DraftField hint="One rule per line." title="Direct rules">
                <textarea
                  className="min-h-40 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm leading-6 text-(--ink)"
                  value={draft.directRules}
                  onChange={(event) =>
                    setDraft((current) => ({...current, directRules: event.target.value}))
                  }
                />
              </DraftField>
              <DraftField hint="One rule per line." title="Indirect rules">
                <textarea
                  className="min-h-40 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm leading-6 text-(--ink)"
                  value={draft.indirectRules}
                  onChange={(event) =>
                    setDraft((current) => ({...current, indirectRules: event.target.value}))
                  }
                />
              </DraftField>
              <DraftField hint="One rule per line." title="Unrelated rules">
                <textarea
                  className="min-h-40 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm leading-6 text-(--ink)"
                  value={draft.unrelatedRules}
                  onChange={(event) =>
                    setDraft((current) => ({...current, unrelatedRules: event.target.value}))
                  }
                />
              </DraftField>
            </div>

            <section className="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h3 className="text-sm font-semibold text-(--ink)">Topic taxonomy</h3>
                  <p className="text-sm leading-6 text-muted">Keep a compact ID and a readable label for each topic.</p>
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  onPress={() =>
                    setDraft((current) => ({
                      ...current,
                      topics: [...current.topics, {id: "", label: ""}],
                    }))
                  }
                >
                  Add topic
                </Button>
              </div>
              {draft.topics.length === 0 ? (
                <p className="text-sm text-muted">No topic taxonomy entries yet.</p>
              ) : (
                <div className="space-y-3">
                  {draft.topics.map((item, index) => (
                    <div key={`topic-${index}`} className="grid gap-3 rounded-lg border border-(--line) p-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
                      <input
                        className="w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink)"
                        placeholder="topic_id"
                        value={item.id}
                        onChange={(event) =>
                          setDraft((current) => ({
                            ...current,
                            topics: current.topics.map((topic, topicIndex) =>
                              topicIndex === index ? {...topic, id: event.target.value} : topic,
                            ),
                          }))
                        }
                      />
                      <input
                        className="w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink)"
                        placeholder="Readable label"
                        value={item.label}
                        onChange={(event) =>
                          setDraft((current) => ({
                            ...current,
                            topics: current.topics.map((topic, topicIndex) =>
                              topicIndex === index ? {...topic, label: event.target.value} : topic,
                            ),
                          }))
                        }
                      />
                      <Button
                        size="sm"
                        variant="ghost"
                        onPress={() =>
                          setDraft((current) => ({
                            ...current,
                            topics: current.topics.filter((_topic, topicIndex) => topicIndex !== index),
                          }))
                        }
                      >
                        Remove
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </section>

            <section className="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h3 className="text-sm font-semibold text-(--ink)">Few-shot examples</h3>
                  <p className="text-sm leading-6 text-muted">
                    Up to two examples. Tags are optional and should be comma-separated.
                  </p>
                </div>
                <Button
                  isDisabled={draft.fewShots.length >= 2}
                  size="sm"
                  variant="outline"
                  onPress={() =>
                    setDraft((current) => ({
                      ...current,
                      fewShots: [
                        ...current.fewShots,
                        {title: "", relevance: "direct", tags: "", rationale: ""},
                      ],
                    }))
                  }
                >
                  Add example
                </Button>
              </div>
              {draft.fewShots.length === 0 ? (
                <p className="text-sm text-muted">No few-shot examples yet.</p>
              ) : (
                <div className="space-y-3">
                  {draft.fewShots.map((item, index) => (
                    <div key={`few-shot-${index}`} className="space-y-3 rounded-lg border border-(--line) p-3">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <p className="text-sm font-semibold text-(--ink)">Example {index + 1}</p>
                        <Button
                          size="sm"
                          variant="ghost"
                          onPress={() =>
                            setDraft((current) => ({
                              ...current,
                              fewShots: current.fewShots.filter((_shot, shotIndex) => shotIndex !== index),
                            }))
                          }
                        >
                          Remove
                        </Button>
                      </div>
                      <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_180px]">
                        <input
                          className="w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink)"
                          placeholder="Paper title"
                          value={item.title}
                          onChange={(event) =>
                            setDraft((current) => ({
                              ...current,
                              fewShots: current.fewShots.map((shot, shotIndex) =>
                                shotIndex === index ? {...shot, title: event.target.value} : shot,
                              ),
                            }))
                          }
                        />
                        <select
                          className="w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink)"
                          value={item.relevance}
                          onChange={(event) =>
                            setDraft((current) => ({
                              ...current,
                              fewShots: current.fewShots.map((shot, shotIndex) =>
                                shotIndex === index
                                  ? {...shot, relevance: event.target.value as Relevance}
                                  : shot,
                              ),
                            }))
                          }
                        >
                          <option value="direct">Direct</option>
                          <option value="indirect">Indirect</option>
                          <option value="unrelated">Unrelated</option>
                        </select>
                      </div>
                      <input
                        className="w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink)"
                        placeholder="Tags (comma-separated)"
                        value={item.tags}
                        onChange={(event) =>
                          setDraft((current) => ({
                            ...current,
                            fewShots: current.fewShots.map((shot, shotIndex) =>
                              shotIndex === index ? {...shot, tags: event.target.value} : shot,
                            ),
                          }))
                        }
                      />
                      <textarea
                        className="min-h-28 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm leading-6 text-(--ink)"
                        placeholder="Why this example matters"
                        value={item.rationale}
                        onChange={(event) =>
                          setDraft((current) => ({
                            ...current,
                            fewShots: current.fewShots.map((shot, shotIndex) =>
                              shotIndex === index ? {...shot, rationale: event.target.value} : shot,
                            ),
                          }))
                        }
                      />
                    </div>
                  ))}
                </div>
              )}
            </section>
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

            <section className="space-y-2">
              <h3 className="text-sm font-semibold uppercase tracking-[0.12em] text-muted">Topic Taxonomy</h3>
              {topicSummary(profile.topic_taxonomy).length === 0 ? (
                <p className="text-sm text-muted">No topic taxonomy defined.</p>
              ) : (
                <ul className="space-y-2 text-sm leading-6 text-(--body)">
                  {topicSummary(profile.topic_taxonomy).map((item) => (
                    <li key={item}>{item}</li>
                  ))}
                </ul>
              )}
            </section>

            <section className="space-y-3">
              <h3 className="text-sm font-semibold uppercase tracking-[0.12em] text-muted">Few-Shot Examples</h3>
              {fewShotSummary(profile.few_shots).length === 0 ? (
                <p className="text-sm text-muted">No few-shot examples yet.</p>
              ) : (
                <div className="space-y-3">
                  {fewShotSummary(profile.few_shots).map((item, index) => (
                    <div key={`${item.title}-${index}`} className="rounded-lg border border-(--line) p-3">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <p className="text-sm font-semibold text-(--ink)">{item.title}</p>
                        <span className="text-xs font-semibold uppercase tracking-[0.14em] text-muted">
                          {item.relevance}
                        </span>
                      </div>
                      {item.tags.length > 0 ? (
                        <p className="mt-2 text-sm text-muted">Tags: {item.tags.join(", ")}</p>
                      ) : null}
                      <p className="mt-2 text-sm leading-6 text-(--body)">{item.rationale}</p>
                    </div>
                  ))}
                </div>
              )}
            </section>
          </>
        )}
      </Card.Content>
    </Card>
  );
}
