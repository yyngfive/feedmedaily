import {Button, Disclosure} from "@heroui/react";
import React from "react";

// 设置页统一使用 HeroUI Disclosure，保持折叠触发、焦点和动画行为一致。
export function AdminDisclosure({
  children,
  defaultExpanded = false,
  meta,
  title,
}: {
  children: React.ReactNode;
  defaultExpanded?: boolean;
  meta?: React.ReactNode;
  title: string;
}) {
  const [isExpanded, setIsExpanded] = React.useState(defaultExpanded);

  return (
    <Disclosure className="rounded-md border border-(--line) bg-(--paper-accent) p-1" isExpanded={isExpanded} onExpandedChange={setIsExpanded}>
      <Disclosure.Heading>
        <Button className="w-full justify-between border-none bg-transparent px-3 py-2.5 hover:bg-transparent data-[hovered=true]:bg-transparent" slot="trigger" variant="ghost">
          <span className="flex min-w-0 items-center gap-2 text-sm font-semibold text-(--ink)">
            {title}
            {meta}
          </span>
          <Disclosure.Indicator className="shrink-0 text-muted" />
        </Button>
      </Disclosure.Heading>
      <Disclosure.Content>
        <Disclosure.Body className="mx-3 border-t border-(--line) px-0 pt-4 pb-3">
          {children}
        </Disclosure.Body>
      </Disclosure.Content>
    </Disclosure>
  );
}
