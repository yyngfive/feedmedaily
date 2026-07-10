import {Button} from "@heroui/react";
import React from "react";

export function ModalShell({
  children,
  eyebrow,
  footer,
  onClose,
  title,
}: {
  children: React.ReactNode;
  eyebrow: string;
  footer: React.ReactNode;
  onClose: () => void;
  title: string;
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/35 p-4">
      <div className="w-full max-w-xl rounded-lg border border-[var(--line)] bg-(--paper-accent) p-5 shadow-xl">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--muted)]">
              {eyebrow}
            </p>
            <h2 className="mt-2 text-lg font-semibold text-[var(--ink)]">{title}</h2>
          </div>
          <Button size="sm" variant="ghost" onPress={onClose}>
            Close
          </Button>
        </div>

        <div className="mt-4 space-y-4">{children}</div>

        <div className="mt-5 flex justify-end gap-2">{footer}</div>
      </div>
    </div>
  );
}
