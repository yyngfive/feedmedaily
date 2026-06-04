# FeedMeDaily Current Roadmap

Last Updated: 2026-06-04
Status: Active

## Summary

This document tracks the next product and engineering priorities for the current FeedMeDaily runtime. It focuses on unfinished work only.

Current baseline:

- `feedmedailyd` is the production local backend
- `feedmedaily-tray` is the Windows app shell
- the React UI is the primary review and settings surface
- first-run onboarding now uses split basic/advanced settings plus an editable initial profile review

## Priority Areas

### 1. Profile maintenance experience

Goal: make the active profile easier to maintain after the initial onboarding flow.

Planned work:

- add AI-assisted profile simplification without expanding research scope
- improve how the current profile and pending changes are reviewed in the UI
- keep the profile editor document-oriented and easy to scan

Constraints:

- preserve profile validation and versioning semantics
- do not reintroduce paper-level topic tags without a separate product and storage redesign

### 2. Feed discovery and job progress clarity

Goal: make feed setup easier and long-running work more understandable.

Planned work:

- add a built-in feed list and links to official RSS discovery pages
- present clearer progress for feed fetch, metadata enrichment, and classification jobs
- keep protected-feed recovery understandable from the main UI

Constraints:

- preserve the layered Go feed pipeline
- keep long-running actions in background jobs with visible status

### 3. Review workflow ergonomics

Goal: reduce friction in the main paper review loop.

Planned work:

- add `mark wrong` filtering
- support multi-select journal filters
- add sort options for journal, date, and confidence
- add a bulk `mark all read` action for the current result set

Constraints:

- preserve the three-column layout baseline
- keep paper cards summary-only and the primary actions in the detail panel

### 4. Settings and onboarding polish

Goal: make first-run configuration and ongoing settings edits denser and easier to understand.

Planned work:

- add clearer onboarding help and external guidance links where useful without re-expanding the page chrome
- keep the compact split onboarding flow easy to scan while tightening wording around proposal acceptance and follow-up steps
- improve job status continuity and other high-value feedback messaging in the UI

Constraints:

- configuration editing remains local-first
- secret values stay write-only from the frontend perspective

### 5. Review-path scaling follow-up

Goal: keep the paper review surface responsive as local libraries grow larger.

Planned work:

- measure whether the current batched SQLite report path and non-blocking first-screen loading are sufficient on larger real libraries
- if needed, split list and detail data more aggressively or add server-side pagination and filtering for the report payload
- continue treating app-update and other admin/status requests as non-critical background hydration

Constraints:

- preserve the current `/api/report/latest` contract unless a clear follow-up redesign is intentionally scheduled
- keep the card list on the first-screen critical path and avoid reintroducing admin-request blocking

### 6. Zotero and packaging follow-up

Goal: close the main remaining integration and distribution gaps.

Planned work:

- fix remaining Zotero collection-tree and folder-hierarchy issues
- reduce visible runtime startup flashing where possible
- improve packaged-update guidance and cross-platform follow-up documentation

Constraints:

- Zotero integration continues to use the Zotero Web API with an in-app collection picker
- Windows remains the primary packaged platform

## Suggested Delivery Order

1. Profile simplification and maintenance UX
2. Feed discovery and job progress improvements
3. Review filters, sorting, and bulk-read actions
4. Settings and onboarding polish
5. Review-path scaling follow-up
6. Zotero follow-up
7. Packaging and cross-platform follow-up

## Working Rules

- Keep `ARCHITECTURE.md` as the canonical current-architecture document.
- Keep `CHANGELOG.md` for user-visible release history.
- Treat this roadmap as the current prioritized plan; keep `TODO.md` as a lighter backlog and scratch list.
- When system boundaries or UI ownership change, update both this document and `ARCHITECTURE.md` in the same change.
