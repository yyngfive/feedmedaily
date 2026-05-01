import React from "react";

const toneClasses = {
  danger: "border-rose-300 bg-rose-50 text-rose-900",
  info: "border-sky-300 bg-sky-50 text-sky-900",
  success: "border-emerald-300 bg-emerald-50 text-emerald-900",
  warning: "border-amber-300 bg-amber-50 text-amber-900",
} as const;

export function StatusBanner({
  children,
  className = "",
  tone,
}: {
  children: React.ReactNode;
  className?: string;
  tone: keyof typeof toneClasses;
}) {
  return (
    <div className={`rounded-md border p-3 text-sm ${toneClasses[tone]} ${className}`.trim()}>
      {children}
    </div>
  );
}
