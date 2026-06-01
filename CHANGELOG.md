# Changelog

This changelog is grouped by version number and records each version relative to the previous released version.

The latest released version is `0.3.0`. The next planned release is `0.3.1`, so unreleased product changes should be added under `0.3.1` until that version ships.

## 0.3.1 (Unreleased)

Changes since `0.3.0`:

### Changed

- Changed feedback-driven profile proposal review to a git-style compact diff flow with per-change accept/reject controls, section-level change summaries, and explicit compactness signals so profile edits are easier to scan and less likely to hide rule growth.
- Tightened the feedback-driven profile proposal prompt to favor reason-first abstraction, broader rewrite/merge compaction, and clearer rejection of object-level rule sprawl; added review warnings for newly added rules that still look overly specific.
- Further tightened feedback-driven proposal generation so conflicting old rules must be rewritten or removed, merge/add duplicates are discouraged, and merged rules are expected to preserve important boundary coverage. Classification now uses only scope plus relevance rules, without few-shot examples, to reduce broad-example bias.
- Simplified profile maintenance to focus only on scope plus relevance rules: the current editor now uses row-based rule lists, tags/examples are cleared from new saves and proposals, and profile proposal generation can run in maintenance mode even when there is no open feedback.
- Added structured background-job progress reporting for sync, reclassify, and profile-generation flows. The UI now shows feed-by-feed fetch status, metadata/classification completion percentages, and step-based profile generation progress without adding a separate progress-bar UI.
- Changed the local API server to keep a long-lived SQLite store open across requests, batch-build `/api/report/latest`, and reuse a short-lived cached app-update status instead of repeatedly reopening the database or rechecking the remote manifest on every page load.

### Fixed

- Reduced homepage wait time for larger libraries by replacing the report page's earlier N+1 SQLite read pattern with indexed batch queries for the latest classification, feedback, and Zotero state per paper.
- Fixed the review app's first-screen loading flow so the paper card list no longer waits for update checks, scheduler state, settings, proposals, or feedback requests before it starts loading. Those admin/status requests now hydrate in the background.

## 0.3.0 (2026-05-25)

Changes since `0.2.1`:

### Added

- Added a Windows-only Wails verification window for Cloudflare-protected feeds. Protected RSS jobs can now pause for manual verification, capture RDF/XML directly inside that dedicated WebView2 session, and resume the same run after the verifier posts the captured feed back to the local backend.
- Added dedicated verifier-side local logging for protected-feed verification sessions, so release builds now record verifier startup, XML capture, backend callback, close-phase, and process-exit diagnostics in the user log directory.

### Fixed

- Restored Nature-family RSS ingestion after upstream feeds switched to RSS 1.0 / RDF roots such as `rdf:RDF`. The Go feed parser now accepts RDF-backed RSS feeds, keeps the existing RSS normalization path, and preserves multiple `dc:creator` authors instead of only the first author.
- Fixed the review sidebar `Last Update` label so it no longer changes on page refresh or job polling. The UI now shows the latest real feed refresh timestamp derived from stored paper fetch metadata, rather than the time when `/api/report/latest` was read.
- Hardened RSS fetching against intermittent upstream blocking by adding richer request headers, limited retry handling for `403`/`429`/`5xx`, and challenge-page detection without relying on the old Windows-only platform fetch fallback.
- Improved RSS metadata extraction across publisher-specific feeds, including bioRxiv author parsing, ACS TOC graphic fallback, and detailed Elsevier/ScienceDirect `description` parsing for author/date/source metadata.
- Fixed protected-feed verifier callback handling so duplicate XML callbacks are ignored cleanly, HTML-wrapped XML previews no longer fall through as unsupported `html` roots, and release-mode verifier windows now close reliably even when WebView2 does not perform a clean shutdown on its own.

### Changed

- Refactored the Go feed pipeline into layered fetch client, generic parser, and small publisher-extractor stages, and moved OpenAlex/Crossref back to a dedicated metadata-enrich layer.
- Changed metadata enrichment so already-complete RSS entries no longer trigger routine OpenAlex/Crossref lookups; external metadata is now requested only when DOI, authors, journal, or usable abstract content is missing.
- Changed admin run-job behavior so Cloudflare-protected feeds can move into a `waiting_for_user` state instead of failing immediately when manual verification is required.
- Replaced the earlier experimental Edge/C# verification helpers with a single Go-built verifier binary, and wired source builds, release packaging, and installer output to include `FeedMeDailyVerifier.exe`.
- Changed the protected-feed recovery flow so the verifier window now opens automatically, captures feed XML automatically when the protected page resolves, and lets the run continue with a fetch warning if verification never returns usable XML instead of forcing the whole job to fail.
- Changed `feedmedailyd` back into a daemon-only service entrypoint. Manual sync now consistently runs through the daemon job API instead of mixing command-mode and background-job paths.
- Added a Linux source-mode helper script for `serve`, `open`, `sync`, and `paths`, and aligned scheduler guidance around using that script with `cron` instead of direct daemon command flags.

## 0.2.1 (2026-05-21)

Changes since `0.2.0`:

### Changed

- Classification no longer emits paper-level `topic_tags`. The current Go classification path focuses on relevance, confidence, reason, recommended action, and translated titles.
- Classifier and profile-model requests now retry once with `thinking=disabled` when provider thinking/reasoning mode fails with timeout or gateway-style errors.

### Fixed

- Hardened Windows tray recovery after resume by moving tray refresh and balloon updates onto the UI thread, adding tray icon re-registration safeguards, and covering the behavior with Windows tray tests.

## 0.2.0 (2026-05-17)

Changes since `0.1.3`:

### Added

- Added the Go backend service `feedmedailyd` as the primary local app/API runtime.
- Added Go implementations for report, feedback, profile proposal, reclassify, admin jobs, Zotero collection listing, and Zotero save flows.
- Added editable local configuration support for classifier/profile/Zotero settings in the app.
- Added a detailed Go manual regression checklist and a UI click checklist for release verification.

### Changed

- Migrated the production runtime from the legacy Python-first backend path to a Go tray plus Go backend service architecture.
- Moved `Run Sync Now` and the daily fetch/classify pipeline onto the Go runtime, including feed fetch, metadata enrichment, classification, and report refresh.
- Switched runtime logging to readable daily `.log` files with richer feed, metadata, classifier, profile, Zotero, and job traces.
- Refreshed FeedMeDaily branding assets, favicons, release packaging, and installer wiring around the Go runtime.

### Fixed

- Improved tray launch UX, including tray-managed backend startup and reduced command-window flashing in the new Go runtime path.
- Fixed onboarding so the generated initial profile proposal refreshes back into the UI correctly.
- Fixed in-progress job message handling so current job state is preserved more consistently in the UI.
- Fixed several Go migration regressions in tray behavior, release runtime wiring, and RSS parsing.
