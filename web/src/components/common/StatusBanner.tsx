import React from "react";

const toneClasses = {
  danger: "border-[var(--danger-line)] bg-[var(--danger-bg)] text-[var(--danger-ink)]",
  info: "border-[var(--info-line)] bg-[var(--info-bg)] text-[var(--info-ink)]",
  success: "border-[var(--success-line)] bg-[var(--success-bg)] text-[var(--success-ink)]",
  warning: "border-[var(--warning-line)] bg-[var(--warning-bg)] text-[var(--warning-ink)]",
} as const;

export type StatusTone = keyof typeof toneClasses;

export function StatusBanner({
  children,
  className = "",
  compact = false,
  tone,
}: {
  children: React.ReactNode;
  className?: string;
  compact?: boolean;
  tone: StatusTone;
}) {
  return (
    <div
      className={`rounded-md border text-sm ${compact ? "px-3 py-1.5" : "p-3"} ${toneClasses[tone]} ${className}`.trim()}
    >
      {children}
    </div>
  );
}
