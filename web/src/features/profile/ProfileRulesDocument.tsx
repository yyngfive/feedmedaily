import {Button, Card, Chip} from "@heroui/react";
import React from "react";

import {TextAreaField, TextInputField} from "../../shared/components/FormFields";
import {SelectField} from "../../shared/components/SelectField";
import {toProfileRule} from "../../app/utils";
import type {ClassificationProfile, TopicDefinition} from "../../shared/types";

type ProfileRuleDraft = {
  text: string;
  topics: string[];
};

type ProfileDraft = {
  name: string;
  scope: string;
  directRules: ProfileRuleDraft[];
  indirectRules: ProfileRuleDraft[];
  unrelatedRules: ProfileRuleDraft[];
  topics: TopicDefinition[];
};

function draftRules(rules: unknown[]): ProfileRuleDraft[] {
  return rules.map((rule) => {
    const normalized = toProfileRule(rule);
    return {text: normalized.text, topics: [...normalized.topics]};
  });
}

function createDraft(profile: ClassificationProfile): ProfileDraft {
  return {
    name: profile.meta.name,
    scope: profile.scope,
    directRules: draftRules(profile.relevance_rules.direct),
    indirectRules: draftRules(profile.relevance_rules.indirect),
    unrelatedRules: draftRules(profile.relevance_rules.unrelated),
    topics: profile.topic_taxonomy.map((topic) => ({...topic})),
  };
}

function TopicTagChips({
  label,
  topics,
  topicLabels,
}: {
  label: string;
  topics: string[];
  topicLabels: Map<string, string>;
}) {
  if (topics.length === 0) {
    return null;
  }
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="text-xs uppercase tracking-[0.12em] text-muted">{label}</span>
      {topics.map((topicID) => (
        <Chip key={topicID} color="default" size="sm" variant="soft">
          {topicLabels.get(topicID) ?? "未归类"}
        </Chip>
      ))}
    </div>
  );
}

function RuleSection({items, title, topicLabels}: {items: unknown[]; title: string; topicLabels: Map<string, string>}) {
  return (
    <section className="space-y-2">
      <h3 className="text-sm font-semibold uppercase tracking-[0.12em] text-muted">{title}</h3>
      {items.length === 0 ? (
        <p className="text-sm text-muted">No rules yet.</p>
      ) : (
        <ul className="space-y-2 text-sm leading-6 text-(--body)">
          {items.map((item) => {
            const rule = toProfileRule(item);
            return (
              <li key={`${title}-${rule.text}`} className="space-y-1">
                <span>{rule.text}</span>
                <TopicTagChips label="Topics" topics={rule.topics} topicLabels={topicLabels} />
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

function RuleListEditor({
  allowTopics,
  items,
  onChange,
  title,
  topicLabels,
  topics,
}: {
  allowTopics: boolean;
  items: ProfileRuleDraft[];
  onChange: (items: ProfileRuleDraft[]) => void;
  title: string;
  topicLabels: Map<string, string>;
  topics: TopicDefinition[];
}) {
  // unrelated 不携带主题标签：保持原始整体多行文本框，一行一条规则。
  if (!allowTopics) {
    return (
      <section className="space-y-2">
        <h3 className="text-sm font-semibold text-(--ink)">{title}</h3>
        <TextAreaField
          hideLabel
          label={title}
          rows={9}
          value={items.map((item) => item.text).join("\n")}
          onChange={(value) =>
            onChange(value.split(/\r?\n/).map((line) => ({text: line, topics: []})))
          }
        />
      </section>
    );
  }
  // direct/indirect 需要按条打标：每条规则一个多行文本框，主题用单选下拉
  //（每条规则最多归属一个主题），Remove 与下拉同行靠右，无逐条边框盒子。
  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold text-(--ink)">{title}</h3>
        <Button
          size="sm"
          variant="outline"
          onPress={() => onChange([...items, {text: "", topics: []}])}
        >
          Add rule
        </Button>
      </div>
      {items.length === 0 ? (
        <p className="text-sm text-muted">No rules yet.</p>
      ) : (
        <div className="space-y-3">
          {items.map((rule, index) => {
            const topicOptions = [
              {value: "", label: "No topic"},
              ...topics.map((topic) => ({value: topic.id, label: topic.label})),
            ];
            const selectedTopic = rule.topics[0] ?? "";
            const knownTopic = selectedTopic === "" || topicOptions.some((option) => option.value === selectedTopic);
            return (
              <div key={`${title}-${index}`} className="space-y-1.5">
                <TextAreaField
                  hideLabel
                  label={title}
                  rows={2}
                  value={rule.text}
                  onChange={(text) =>
                    onChange(items.map((item, i) => (i === index ? {...item, text} : item)))
                  }
                />
                <div className="flex items-end gap-2">
                  <div className="w-56">
                    <SelectField
                      label="Topic"
                      options={topicOptions}
                      value={knownTopic ? selectedTopic : ""}
                      onChange={(value) =>
                        onChange(
                          items.map((item, i) =>
                            i === index ? {...item, topics: value ? [value] : []} : item,
                          ),
                        )
                      }
                    />
                  </div>
                  {topics.length === 0 && rule.topics.length > 0 ? (
                    <p className="flex-1 pb-2 text-xs text-muted">
                      Removed topic: {topicLabels.get(rule.topics[0]) ?? rule.topics[0]}
                    </p>
                  ) : null}
                  <Button
                    className="ml-auto"
                    size="sm"
                    variant="ghost"
                    onPress={() => onChange(items.filter((_, i) => i !== index))}
                  >
                    Remove
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}

function TopicsEditor({
  onChange,
  topics,
}: {
  onChange: (topics: TopicDefinition[]) => void;
  topics: TopicDefinition[];
}) {
  const [newLabel, setNewLabelLabel] = React.useState("");
  const addTopic = () => {
    const label = newLabel.trim();
    if (!label) return;
    if (topics.some((topic) => topic.label.toLowerCase() === label.toLowerCase())) return;
    // 新主题的 id 由后端在保存时分配；草稿用空 id 占位。
    onChange([...topics, {id: "", label}]);
    setNewLabelLabel("");
  };
  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold text-(--ink)">Topics</h3>
      </div>
      <p className="text-sm leading-6 text-muted">
        Topics are optional buckets for related papers, assigned through topic tags on direct and
        indirect rules. Renaming updates every paper automatically; deleted topics leave old papers
        unassigned until the next classification.
      </p>
      {topics.length === 0 ? (
        <p className="text-sm text-muted">No topics yet.</p>
      ) : (
        <div className="space-y-2">
          {topics.map((topic, index) => (
            <div key={topic.id || `new-${topic.label}`} className="flex items-center gap-2">
              <div className="flex-1">
                <TextInputField
                  hideLabel
                  label="Topic label"
                  value={topic.label}
                  onChange={(value) =>
                    onChange(topics.map((item, i) => (i === index ? {...item, label: value} : item)))
                  }
                />
              </div>
              <Button
                size="sm"
                variant="danger"
                onPress={() => onChange(topics.filter((_, i) => i !== index))}
              >
                Delete
              </Button>
            </div>
          ))}
        </div>
      )}
      <div className="flex items-end gap-2">
        <div className="flex-1">
          <TextInputField
            hideLabel
            label="New topic"
            placeholder="New topic label"
            value={newLabel}
            onChange={setNewLabelLabel}
          />
        </div>
        <Button isDisabled={!newLabel.trim()} size="sm" variant="outline" onPress={addTopic}>
          Add topic
        </Button>
      </div>
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
    <div className="block space-y-2">
      <span className="text-sm font-semibold text-(--ink)">{title}</span>
      {hint ? <p className="text-sm leading-6 text-muted">{hint}</p> : null}
      {children}
    </div>
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

  const topicLabels = React.useMemo(
    () => new Map(profile.topic_taxonomy.map((topic) => [topic.id, topic.label])),
    [profile.topic_taxonomy],
  );
  const usedTopicIDs = React.useMemo(() => {
    const ids = new Set<string>();
    for (const section of [
      profile.relevance_rules.direct,
      profile.relevance_rules.indirect,
    ]) {
      for (const item of section) {
        for (const id of toProfileRule(item).topics) {
          ids.add(id);
        }
      }
    }
    return ids;
  }, [profile.relevance_rules]);

  const saveDraft = async () => {
    if (!onSave) {
      return;
    }
    const cleanRules = (rules: ProfileRuleDraft[]) =>
      rules
        .map((rule) => ({text: rule.text.trim(), topics: rule.topics.filter(Boolean)}))
        .filter((rule) => rule.text);
    // 草稿里空 id 的新主题由后端保存时分配系统 id。
    const draftTopics = draft.topics
      .map((topic) => ({id: topic.id.trim(), label: topic.label.trim()}))
      .filter((topic) => topic.label);
    const nextProfile: ClassificationProfile = {
      ...profile,
      meta: {
        ...profile.meta,
        name: draft.name.trim(),
      },
      scope: draft.scope.trim(),
      relevance_rules: {
        direct: cleanRules(draft.directRules),
        indirect: cleanRules(draft.indirectRules),
        unrelated: cleanRules(draft.unrelatedRules),
      },
      topic_taxonomy: draftTopics,
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
              <TextInputField
                hideLabel
                label="Profile name"
                value={draft.name}
                onChange={(value) => setDraft((current) => ({...current, name: value}))}
              />
            </DraftField>

            <DraftField title="Scope">
              <TextAreaField
                hideLabel
                label="Scope"
                rows={5}
                value={draft.scope}
                onChange={(value) => setDraft((current) => ({...current, scope: value}))}
              />
            </DraftField>

            <TopicsEditor
              topics={draft.topics}
              onChange={(topics) => setDraft((current) => ({...current, topics}))}
            />

            <div className="space-y-4">
              <RuleListEditor
                allowTopics
                items={draft.directRules}
                title="Direct rules"
                topicLabels={topicLabels}
                topics={draft.topics.filter((topic) => topic.label)}
                onChange={(directRules) => setDraft((current) => ({...current, directRules}))}
              />
              <RuleListEditor
                allowTopics
                items={draft.indirectRules}
                title="Indirect rules"
                topicLabels={topicLabels}
                topics={draft.topics.filter((topic) => topic.label)}
                onChange={(indirectRules) => setDraft((current) => ({...current, indirectRules}))}
              />
              <RuleListEditor
                allowTopics={false}
                items={draft.unrelatedRules}
                title="Unrelated rules"
                topicLabels={topicLabels}
                topics={[]}
                onChange={(unrelatedRules) => setDraft((current) => ({...current, unrelatedRules}))}
              />
            </div>
          </>
        ) : (
          <>
            <section className="space-y-2">
              <h3 className="text-sm font-semibold uppercase tracking-[0.12em] text-muted">Scope</h3>
              <p className="text-sm leading-7 text-(--body)">{profile.scope}</p>
            </section>
            {profile.topic_taxonomy.length > 0 ? (
              <section className="space-y-2">
                <h3 className="text-sm font-semibold uppercase tracking-[0.12em] text-muted">Topics</h3>
                <div className="flex flex-wrap gap-1.5">
                  {profile.topic_taxonomy.map((topic) => (
                    <Chip
                      key={topic.id}
                      color="default"
                      size="sm"
                      variant={usedTopicIDs.has(topic.id) ? "soft" : "tertiary"}
                    >
                      {topic.label}
                      {usedTopicIDs.has(topic.id) ? "" : " · unused"}
                    </Chip>
                  ))}
                </div>
              </section>
            ) : null}
            <RuleSection items={profile.relevance_rules.direct} title="Direct" topicLabels={topicLabels} />
            <RuleSection items={profile.relevance_rules.indirect} title="Indirect" topicLabels={topicLabels} />
            <RuleSection items={profile.relevance_rules.unrelated} title="Unrelated" topicLabels={topicLabels} />
          </>
        )}
      </Card.Content>
    </Card>
  );
}
