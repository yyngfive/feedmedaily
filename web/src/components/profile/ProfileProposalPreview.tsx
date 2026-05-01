import {Card} from "@heroui/react";

import type {ProfileProposal} from "../../types";

function RuleSection({items, title}: {items: string[]; title: string}) {
  return (
    <section className="space-y-2">
      <h3 className="text-sm font-semibold uppercase tracking-[0.12em] text-muted">{title}</h3>
      {items.length === 0 ? (
        <p className="text-sm text-muted">No changes proposed.</p>
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

export function ProfileProposalPreview({proposal}: {proposal: ProfileProposal}) {
  const profile = proposal.proposed_profile;
  const delta = proposal.rule_delta;

  return (
    <Card className="border border-(--line) bg-(--paper-accent)">
      <Card.Header className="flex flex-col items-start gap-2">
        <p className="text-sm font-semibold text-(--ink)">Pending profile changes</p>
        <div className="space-y-1">
          <h2 className="text-2xl font-semibold text-(--ink)">
            {profile.meta.name} · v{profile.meta.version}
          </h2>
          <p className="text-sm text-muted">
            {proposal.created_at.slice(0, 10)} · {proposal.state}
          </p>
        </div>
      </Card.Header>
      <Card.Content className="space-y-6">
        <p className="text-sm leading-6 text-(--body)">{delta.summary}</p>
        {delta.scope_rewrite ? (
          <section className="space-y-2">
            <h3 className="text-sm font-semibold uppercase tracking-[0.12em] text-muted">
              Scope rewrite
            </h3>
            <p className="text-sm leading-6 text-(--body)">{delta.scope_rewrite}</p>
          </section>
        ) : null}
        <RuleSection items={delta.direct_rule_additions} title="Direct" />
        <RuleSection items={delta.indirect_rule_additions} title="Indirect" />
        <RuleSection items={delta.unrelated_rule_additions} title="Unrelated" />
      </Card.Content>
    </Card>
  );
}
