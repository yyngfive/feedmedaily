import {Chip} from "@heroui/react";

import {relevanceLabel} from "../../app/constants";
import type {ProfileProposal} from "../../types";

function RuleList({title, items}: {title: string; items: string[]}) {
  return (
    <div className="space-y-2">
      <h4 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
        {title}
      </h4>
      {items.length === 0 ? (
        <p className="text-sm text-[var(--muted)]">No rules.</p>
      ) : (
        <ul className="list-disc space-y-1 pl-5 text-sm leading-6 text-[var(--body)]">
          {items.map((item) => (
            <li key={`${title}-${item}`}>{item}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

function CompactTagList({
  emptyLabel,
  items,
  title,
}: {
  emptyLabel: string;
  items: Array<{id: string; label: string}>;
  title: string;
}) {
  return (
    <div className="space-y-2">
      <h4 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
        {title}
      </h4>
      {items.length === 0 ? (
        <p className="text-sm text-[var(--muted)]">{emptyLabel}</p>
      ) : (
        <div className="flex flex-wrap gap-2">
          {items.map((item) => (
            <Chip key={item.id} size="sm" variant="secondary">
              {item.label} · {item.id}
            </Chip>
          ))}
        </div>
      )}
    </div>
  );
}

export function ProfileProposalPreview({proposal}: {proposal: ProfileProposal}) {
  const profile = proposal.proposed_profile;
  const delta = proposal.rule_delta;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Chip size="sm" variant="secondary">
          {proposal.state}
        </Chip>
        <Chip size="sm" variant="secondary">
          {proposal.model}
        </Chip>
        <Chip size="sm" variant="secondary">
          v{profile.meta.version}
        </Chip>
      </div>
      <p className="text-sm leading-6 text-[var(--body)]">{delta.summary}</p>
      {delta.scope_rewrite ? (
        <div className="rounded-md border border-[var(--line)] p-3">
          <h4 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
            Scope Rewrite
          </h4>
          <p className="mt-2 text-sm leading-6 text-[var(--body)]">{delta.scope_rewrite}</p>
        </div>
      ) : null}
      <div className="grid gap-4 xl:grid-cols-3">
        <div className="rounded-md border border-[var(--line)] p-3">
          <RuleList title="Direct additions" items={delta.direct_rule_additions} />
        </div>
        <div className="rounded-md border border-[var(--line)] p-3">
          <RuleList title="Indirect additions" items={delta.indirect_rule_additions} />
        </div>
        <div className="rounded-md border border-[var(--line)] p-3">
          <RuleList title="Unrelated additions" items={delta.unrelated_rule_additions} />
        </div>
      </div>
      <div className="grid gap-4 xl:grid-cols-2">
        <div className="rounded-md border border-[var(--line)] p-3">
          <CompactTagList
            emptyLabel="No tag additions."
            items={delta.tag_additions}
            title="Tag additions"
          />
        </div>
        <div className="rounded-md border border-[var(--line)] p-3">
          <h4 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
            Tag removals
          </h4>
          {delta.tag_removals.length === 0 ? (
            <p className="mt-2 text-sm text-[var(--muted)]">No tag removals.</p>
          ) : (
            <div className="mt-2 flex flex-wrap gap-2">
              {delta.tag_removals.map((item) => (
                <Chip key={item} size="sm" variant="secondary">
                  {item}
                </Chip>
              ))}
            </div>
          )}
        </div>
      </div>
      <div className="rounded-md border border-[var(--line)] p-3">
        <CompactTagList
          emptyLabel="No tags in merged profile."
          items={profile.topic_taxonomy}
          title="Merged tags"
        />
      </div>
      {profile.few_shots.length ? (
        <div className="rounded-md border border-[var(--line)] p-3">
          <h4 className="text-xs font-semibold uppercase tracking-[0.12em] text-[var(--muted)]">
            Retained few-shot examples
          </h4>
          <div className="mt-3 space-y-3">
            {profile.few_shots.map((item) => (
              <div
                key={`${item.title}-${item.relevance}`}
                className="rounded-md border border-[var(--line)] p-3"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <Chip size="sm" variant="secondary">
                    {relevanceLabel[item.relevance]}
                  </Chip>
                  {item.tags.map((tag) => (
                    <Chip key={`${item.title}-${tag}`} size="sm" variant="secondary">
                      {tag}
                    </Chip>
                  ))}
                </div>
                <p className="mt-2 text-sm font-medium leading-6 text-[var(--ink)]">
                  {item.title}
                </p>
                <p className="mt-1 text-sm leading-6 text-[var(--body)]">{item.rationale}</p>
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}
