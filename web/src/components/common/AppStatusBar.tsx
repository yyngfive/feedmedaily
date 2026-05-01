import {Button, Chip} from "@heroui/react";

import type {AppMeta, AppUpdate} from "../../types";

function updateLabel(appUpdate: AppUpdate | null): string {
  if (!appUpdate) {
    return "Update: unknown";
  }
  if (appUpdate.has_update && appUpdate.latest_version) {
    return `Update: v${appUpdate.latest_version} available`;
  }
  if (appUpdate.status === "up_to_date") {
    return "Update: up to date";
  }
  if (appUpdate.status === "check_failed") {
    return "Update: check failed";
  }
  if (appUpdate.status === "not_configured") {
    return "Update: manifest not set";
  }
  return "Update: checking";
}

export function AppStatusBar({
  appMeta,
  appUpdate,
  busy,
  onExit,
  onOpenData,
  onOpenInstall,
  onOpenLogs,
}: {
  appMeta: AppMeta | null;
  appUpdate: AppUpdate | null;
  busy: boolean;
  onExit: () => void;
  onOpenData: () => void;
  onOpenInstall: () => void;
  onOpenLogs: () => void;
}) {
  return (
    <footer className="border-t border-(--line) bg-(--paper)/95 backdrop-blur">
      <div className="mx-auto flex max-w-375 flex-col gap-3 px-4 py-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex flex-wrap items-center gap-2 text-sm text-muted">
          <Chip size="sm" variant="soft">
            {appMeta ? `${appMeta.name} v${appMeta.version}` : "FeedMeDaily"}
          </Chip>
          <Chip size="sm" variant="secondary">
            {appMeta?.mode === "release" ? "Release mode" : "Source mode"}
          </Chip>
          <Chip
            color={appMeta?.process_running ? "success" : "default"}
            size="sm"
            variant="secondary"
          >
            {appMeta?.process_running ? "Service running" : "Service idle"}
          </Chip>
          <span>{updateLabel(appUpdate)}</span>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="outline" onPress={onOpenData}>
            Open Data
          </Button>
          <Button size="sm" variant="outline" onPress={onOpenLogs}>
            Open Logs
          </Button>
          <Button size="sm" variant="outline" onPress={onOpenInstall}>
            Open Install
          </Button>
          <Button isDisabled={busy} size="sm" variant="danger-soft" onPress={onExit}>
            {busy ? "Exiting..." : "Exit App"}
          </Button>
        </div>
      </div>
    </footer>
  );
}
