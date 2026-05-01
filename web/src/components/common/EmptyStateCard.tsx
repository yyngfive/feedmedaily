import {Card} from "@heroui/react";
import React from "react";

export function EmptyStateCard({
  actions,
  body,
  eyebrow,
  title,
}: {
  actions?: React.ReactNode;
  body: string;
  eyebrow: string;
  title: string;
}) {
  return (
    <Card className="border border-[var(--line)] bg-white p-8 text-center">
      <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--muted)]">
        {eyebrow}
      </p>
      <h2 className="mt-3 text-xl font-semibold text-[var(--ink)]">{title}</h2>
      <p className="mx-auto mt-3 max-w-2xl text-sm leading-7 text-[var(--muted)]">{body}</p>
      {actions ? <div className="mt-5 flex flex-wrap justify-center gap-2">{actions}</div> : null}
    </Card>
  );
}
