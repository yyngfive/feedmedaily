# Changelog

This changelog is grouped by version number and records each version relative to the previous released version.

The latest released version is `0.5.0`. The next planned release is `0.5.1`, so unreleased product changes should be added under `0.5.1` until that version ships.

## 0.5.1 (Unreleased)

Changes since `0.5.0`:

## 0.5.0 (2026-07-09)

Changes since `0.4.0`:

### Added

- Added a Dashboard targeted sync control so selected RSS feeds can be refreshed without running a full manual sync.
- Added a review-list action to mark the selected paper and all visible papers above it as read.
- Expanded the bundled RSS feed catalog with additional publisher and journal feeds.

### Fixed

- Fixed Zotero collection loading for large libraries, added folder-depth display in the save picker, and improved saved item author/date metadata.
- Fixed PNAS/RSS author parsing so affiliation text is no longer stored as authors, and kept the paper detail actions visible when metadata is unusually long.
- Moved paper detail authors into their own scrollable block so long author lists do not crowd the abstract.
- Fixed the detail panel read action so a read paper can be marked unread again.
- Fixed review-list selection changes so the newly selected paper scrolls into view after read actions update the visible list.
- Simplified the feed subscriptions section so saved feeds default to a compact read-only list with editing tucked behind an Edit button.
- Fixed Settings drawer and checkbox interactions so opening Settings or selecting lower list items no longer scrolls the underlying review page or jumps the drawer unexpectedly.
- Unified form control radius across inputs, selects, select popovers, and checkbox controls for a more consistent UI.

## 0.4.0 (2026-07-06)

Changes since `0.3.4`:

### Changed

- Reworked the Settings drawer into Dashboard, Feeds, Profile, Model, and App pages so sync jobs, feed editing, profile review, model credentials, and app/runtime settings are no longer crowded into one config surface.
- Refined Settings spacing and status layout: Dashboard now groups runtime/update metadata, shows latest sync results and warnings in a structured block, Feeds uses stable editable rows, and Profile rule editing uses one multiline editor per rule class.
- Unified Web form controls around HeroUI components for more consistent inputs, selects, textareas, and checkboxes.
- Added a publisher-grouped RSS catalog to Settings > Feeds, with release builds refreshing the bundled catalog from `yyngfive/sci-rss-list`.
- Fixed the active profile path at `data/classification_profile.json` and removed the profile-path setting from local configuration.

### Fixed

- Changed protected-feed sync recovery to continue inside the same feed-fetch pass after host verification, so verified publishers no longer make the progress loop restart from feed 1 or refetch already completed feeds.
- Fixed the protected-feed verifier so ACS/other same-host feeds continue after a human Cloudflare check even when WebView2 lands on the first XML page without delivering the response body event.
- Fixed intermittent ChemRxiv verifier stalls by retrying failed protected-feed navigations automatically instead of requiring repeated manual refreshes.
- Reduced the protected-feed verification wait from 20 minutes to 10 minutes before skipping a feed and continuing the sync.

## 0.3.4 (2026-07-01)

Changes since `0.3.3`:

### Changed

- Removed the deprecated C# protected-feed verifier source now that the Go native verifier is the maintained build and runtime path.

### Added

- Added review workflow controls for filtering Mark wrong feedback, selecting multiple journals, sorting by date/journal/confidence, and marking the current visible result set as read.

### Fixed

- Restored the release `update.json` asset generation so older installed clients that still read the GitHub Releases `latest/download/update.json` URL can receive update notifications after the DNS TXT update flow is adopted.
- Improved classifier stability so transient LLM request failures, malformed JSON responses, and title-translation failures no longer waste an otherwise successful sync. Failed classifier batches now retry and then fall back to single-paper classification where possible.
- Fixed profile feedback reclassification semantics so applying a proposal only consumes accepted feedback, rejected feedback remains available, and manual profile saves automatically start a feedback-paper reclassification job.
- Fixed the Web status bar's `Open Data`, `Open Logs`, and `Open Install` buttons so local folders use the same Windows ShellExecute path as the tray menu instead of the less reliable URL file handler path.
- Fixed abstract image rendering for publisher feeds that provide relative/protocol-relative URLs or require article-page referrers, and removed the duplicate abstract-images dropdown from the paper detail panel.
- Fixed Web scheduler saves targeting the wrong tray instance by scoping tray mutexes and reload notifications to the current config directory.

## 0.3.3 (2026-06-21)

Changes since `0.3.2`:

### Changed

- Changed protected-feed recovery from an ACS-only path into a host-scoped verification session model for all challenge-gated publishers. Protected feeds now reuse one persistent host session per site, so later feeds on the same host can ride the same verified state within a run and across later runs.
- Renamed and moved the native protected-feed helper from the ACS-specific `tools/FeedMeDailyACSVerifier` project to the runtime component project `native/FeedMeDailyProtectedVerifier`, with release builds now packaging it under `FeedMeDailyProtectedVerifier`.
- Migrated the protected-feed verifier runtime from the C# helper to a Go native WebView2 helper while keeping the packaged binary path and callback protocol unchanged. Source and release builds no longer require the .NET SDK for the default protected-feed verifier, and the old Wails verifier is no longer built or packaged.
- Removed the legacy Python backend package and old pytest suite now that the Go tray plus Go backend service are the maintained runtime path.
- Changed packaged update checks to read the fixed DNS TXT record `feedmedaily-update.stassenger.top` instead of a configurable JSON manifest URL.

### Added

- Added a native WebView2 protected-feed helper as the default verifier path for protected hosts. It can keep one persistent host profile, capture multiple same-host feed XML documents in one session, and hand those XML bodies back to the existing Go sync pipeline without switching to the normal Go HTTP client mid-verification.
- Added a standalone `Save Settings` action to first-run onboarding, so users can save LLM, Zotero, and local settings before generating the initial profile.
- Added a PowerShell release helper that updates the DNS TXT release record through Aliyun OpenAPI without requiring the Aliyun CLI.

### Fixed

- Fixed first-run release onboarding so saving a shared DeepSeek/API key refreshes the running backend settings immediately. Initial profile generation now sees the just-saved `SCIRSS_PROFILE_API_KEY` without requiring an app restart.
- Fixed source-mode duplicate verifier launches so one `verification_id` can no longer start multiple helper processes for the same protected host. Duplicate callbacks are now acknowledged and ignored cleanly instead of generating follow-up `404` noise after the first successful XML capture.
- Fixed native verifier stalls so a protected-feed helper that cannot capture XML reports `needs_user` after a short watchdog interval, writes its own diagnostic log under `logs/protected-verifier/`, and is terminated by the backend if the verification request eventually times out.
- Fixed the admin `Reopen Verification Window` action so stale protected-feed verifier process records are cleared before relaunching, and the backend now launches the visible WinForms/WebView2 verifier window instead of accidentally applying hidden-window launcher settings to the verifier process itself.
## 0.3.2 (2026-06-15)

Changes since `0.3.1`:

### Changed

- Replaced the old first-run onboarding page with the new compact onboarding flow. First-run setup now uses split basic/advanced settings, a shared LLM API key with optional per-role overrides, and an editable initial profile review before acceptance.
- Changed onboarding acceptance so accepting the initial proposal now applies it and immediately saves the user's edited draft as the live classification profile.
- Added manual `Check for updates` actions in Settings and the footer status bar so release builds can force-refresh the update manifest on demand, while keeping the normal background update check cached.
- Changed paper classification to apply unrelated exclusions before direct and indirect rules, with a non-persisted decision trace in the model response contract to reduce false-positive indirect matches from surface profile terms.
- Changed protected-feed verification to keep the current Wails/WebView verifier as the first path for auto-drop-to-XML feeds, while adding a system-browser plus manual XML fallback for Cloudflare human-verification loops such as recent ACS RSS challenges.

### Fixed

- Fixed source-mode tray startup so `.env` or environment-based `SCIRSS_SERVER_HOST` and `SCIRSS_SERVER_PORT` values are respected when launching the local backend.
- Fixed the then-configurable release-mode update manifest setting so saved changes took effect immediately without restarting the local service.
- Fixed Windows external URL opening so feed links with query parameters like `&feed=rss&jc=jacsat` are opened intact instead of being truncated before the default browser receives them.

### Added

- Added a protected-feed browser fallback flow in Settings: users can now reopen the in-app verifier, open the protected feed in their normal browser, and manually submit the final RSS/Atom/RDF XML back into the paused sync job without restarting the whole run.
- Added host-scoped persistent WebView2 verifier profiles under local app data so Cloudflare-style human-verification state can survive across protected-feed retries instead of starting from a blank temporary browser session every time.

## 0.3.1 (2026-06-03)

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
- Fixed SQLite lock regressions introduced by the first long-lived-store refactor by separating the local API server into read and write store roles and enabling WAL plus busy-timeout pragmas for better local concurrency.
- Fixed review-side `Mark as read` and feedback mutations so they no longer block on a full report refresh, and removed the remaining post-read card-list flicker by committing reconcile report data and local read overrides in the same UI update.

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
