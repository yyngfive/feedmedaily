import {Card} from "@heroui/react";

import type {ClassificationProfile} from "../../types";

function RuleSection({items, title}: {items: string[]; title: string}) {
  return (
    <section className="space-y-2">
      <h3 className="text-sm font-semibold uppercase tracking-[0.12em] text-muted">{title}</h3>
      <ul className="list-disc space-y-2 pl-5 text-sm leading-6 text-(--body)">
        {items.map((item) => (
          <li key={`${title}-${item}`}>{item}</li>
        ))}
      </ul>
    </section>
  );
}

export function ProfileRulesDocument({
  profile,
  title = "Current classification profile",
}: {
  profile: ClassificationProfile;
  title?: string;
}) {
  return (
    <Card className="border border-(--line) bg-(--paper-accent)">
      <Card.Header className="flex flex-col items-start gap-2">
        <p className="text-sm font-semibold text-(--ink)">{title}</p>
        <div className="space-y-1">
          <h2 className="text-2xl font-semibold text-(--ink)">
            {profile.meta.name} · v{profile.meta.version}
          </h2>
          <p className="text-sm text-muted">
            {profile.meta.updated_at.slice(0, 10)} · created {profile.meta.created_at.slice(0, 10)}
          </p>
        </div>
      </Card.Header>
      <Card.Content className="space-y-6">
        <RuleSection items={profile.relevance_rules.direct} title="Direct" />
        <RuleSection items={profile.relevance_rules.indirect} title="Indirect" />
        <RuleSection items={profile.relevance_rules.unrelated} title="Unrelated" />
      </Card.Content>
    </Card>
  );
}
