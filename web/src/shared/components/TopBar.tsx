import {Button} from "@heroui/react";

import {StatusBanner} from "./StatusBanner";
import type {UiMessage} from "../../app/messages";

export function TopBar({
  message,
  onOpenAdmin,
  onToggleTheme,
  resolvedTheme,
  usingSystemTheme,
}: {
  message: UiMessage | null;
  onOpenAdmin: () => void;
  onToggleTheme: () => void;
  resolvedTheme: "light" | "dark";
  usingSystemTheme: boolean;
}) {
  const themeLabel = resolvedTheme === "dark" ? "Dark" : "Light";

  return (
    <header className="z-30 h-16 flex-none border-b border-(--line) bg-(--paper)">
      <div className="mx-auto flex h-full max-w-375 items-center gap-4 px-4">
        <div className="min-w-0 shrink-0">
          <p className="text-[10px] font-semibold uppercase tracking-[0.18em] text-muted">Feed</p>
          <h1 className="text-xl font-semibold leading-6 text-(--ink)">FeedMeDaily</h1>
        </div>

        <div className="min-w-0 flex-1">
          {message ? (
            <StatusBanner className="truncate" compact tone={message.tone}>
              {message.text}
            </StatusBanner>
          ) : null}
        </div>

        <div className="flex shrink-0 items-center gap-2">
          <Button size="sm" variant="outline" onPress={onToggleTheme}>
            {themeLabel}
            {usingSystemTheme ? " (Auto)" : ""}
          </Button>
          <Button size="sm" variant="secondary" onPress={onOpenAdmin}>
            Settings
          </Button>
        </div>
      </div>
    </header>
  );
}
