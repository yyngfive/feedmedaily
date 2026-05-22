# Changelog

This changelog is grouped by version number and records each version relative to the previous released version.

The latest released version is `0.2.1`. The next planned release is `0.2.2`, so unreleased product changes should be added under `0.2.2` until that version ships.

## 0.2.2 (Unreleased)

Changes since `0.2.1`:

### Fixed

- Restored Nature-family RSS ingestion after upstream feeds switched to RSS 1.0 / RDF roots such as `rdf:RDF`. The Go feed parser now accepts RDF-backed RSS feeds, keeps the existing RSS normalization path, and preserves multiple `dc:creator` authors instead of only the first author.
- Fixed the review sidebar `Last Update` label so it no longer changes on page refresh or job polling. The UI now shows the latest real feed refresh timestamp derived from stored paper fetch metadata, rather than the time when `/api/report/latest` was read.
- Hardened RSS fetching against intermittent upstream blocking by adding richer request headers, limited retry handling for `403`/`429`/`5xx`, and challenge-page detection without relying on the old Windows-only platform fetch fallback.
- Improved RSS metadata extraction across publisher-specific feeds, including bioRxiv author parsing, ACS TOC graphic fallback, and detailed Elsevier/ScienceDirect `description` parsing for author/date/source metadata.

### Changed

- Refactored the Go feed pipeline into layered fetch client, generic parser, and small publisher-extractor stages, and moved OpenAlex/Crossref back to a dedicated metadata-enrich layer.
- Changed metadata enrichment so already-complete RSS entries no longer trigger routine OpenAlex/Crossref lookups; external metadata is now requested only when DOI, authors, journal, or usable abstract content is missing.

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
