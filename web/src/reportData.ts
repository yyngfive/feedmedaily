import type { Report } from "./types";

declare global {
  interface Window {
    __SCIRSS_REPORT__?: Report;
  }
}

export function loadEmbeddedReport(): Report | null {
  return window.__SCIRSS_REPORT__ ?? null;
}

export function reportDataUrl(): string {
  return new URL("./report-data.js", window.location.href).toString();
}

export function tagLabel(tag: string): string {
  return tag.replaceAll("_", " ");
}

