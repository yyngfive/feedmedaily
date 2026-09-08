import type {TopicDefinition} from "../../shared/types";

export type DraftTopic = TopicDefinition & {draftKey: string};

// 新主题还没有持久化 id，也需要稳定身份来区分删除、重命名和列表移动。
export function relabelRuleTopics<T extends {topics: string[]}>(
  rules: T[], oldTopics: DraftTopic[], newTopics: DraftTopic[],
): T[] {
  const nextByKey = new Map(newTopics.map((topic) => [topic.draftKey, topic]));
  const labels = new Map(oldTopics.map((topic) => [
    topic.label.toLowerCase(), nextByKey.get(topic.draftKey)?.label ?? null,
  ]));
  return rules.map((rule) => ({
    ...rule,
    topics: rule.topics.flatMap((label) => {
      const next = labels.get(label.toLowerCase());
      return next === null ? [] : [next ?? label];
    }),
  }));
}
