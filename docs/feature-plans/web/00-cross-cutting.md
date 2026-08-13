# 00 — Cross-Cutting Web Improvements (apply to every page)

> Source: the cross-cutting list in `WEB_FEATURE_GAPS.md`. These are global UX/architecture items not owned by any single page; each page's own doc references the relevant ones. Tracked here so they aren't lost.

## What this is
Platform-level improvements that recur on every page. Implementing them once (shared components, app shell, design system) lifts all 26 pages rather than fixing them one-by-one.

## Gaps — grounded
**A1. No favorites / pinning anywhere.** *Fix:* a shared `useFavorites(entity)` hook + star control; pinned items sort first; persisted per operator.
**A2. No left sub-menus — nav is flat.** *Fix:* group pages (조직/Users·Roles·Departments·Contractors; 거버넌스/Policy·Security·Compliance; 운영/Fleet·Sessions·Live; 모델/ModelInfra·Catalog; 커뮤니케이션; 분석; 감사) into collapsible sub-menus.
**A3. No animations/transitions** (only a pulse dot). *Fix:* a motion system (page enter, modal/expand height, row hover, toasts) — "smooth like butter," not 1980-static. Respect `prefers-reduced-motion`.
**A4. Server-side pagination/filtering absent everywhere** (client slices full lists). *Fix:* a shared `useServerTable(endpoint, {filters, sort, page})` hook; every list page uses it (unblocks each page's "server query" gap).
**A5. Numbers/stats not clickable** anywhere *(your explicit example)*. *Fix:* a `<StatCard to={filteredRoute}>` component; every dashboard/count becomes a drill-down.
**A6. Hardcoded `<select>`s pervade** (departments, models, plans, SCM, runtime, severity, auth). *Fix:* a shared `<EntitySelect entity=…>` backed by admin-configurable entities (BusinessUnit, Catalog, Plan, SCM connector, etc.) — removes the per-page hardcoded-option debt.
**A7. No detail pages** for any entity (inline expand only) — breaks deep-linking/sharing. *Fix:* a `<EntityDetail>` route pattern `/{entity}/:id` for users/harnesses/projects/repos/sessions/models/endpoints/findings.
**A8. No global command palette / ⌘K.** *Fix:* ⌘K to search entities + jump to pages/actions (ties to the §6.4 global-search gap).
**A9. No dark/light + density toggles.** *Fix:* theme tokens already exist (CSS vars); add the switchers.
**A10. Cancel/OK/Submit + modal consistency.** *Fix:* a `<ConfirmDialog>` + `<Modal>` with sticky header, max-height, responsive, and a consistent button order; every destructive action routes through it (fixes the per-page "no confirmation" gaps).
**A11. Search is per-page fetch-and-filter**, not unified; no user→1:1-chat launch *(your example)*. *Fix:* unified search service + cross-entity actions (e.g., from a user, start a 1:1 chat / view their sessions).
**A12. No empty-state guidance / onboarding** on any page. *Fix:* a shared `<EmptyState>` with first-action CTAs per entity.
**A13. No keyboard navigation / shortcuts.** *Fix:* a shortcut layer (j/k rows, enter to open, ⌘K, / to search).
**A14. Responsive/mobile layouts largely absent.** *Fix:* table→card breakpoints; a responsive grid system.

## Sequencing
Phase 1 (shared infra — unblocks every page): A4 (server-table hook), A5 (StatCard), A6 (EntitySelect), A7 (detail route), A10 (ConfirmDialog/Modal), A12 (EmptyState). Build once, adopt across pages.
Phase 2 (app shell): A1 (favorites), A2 (sub-menus), A8 (⌘K), A9 (theme/density), A13 (shortcuts).
Phase 3 (polish): A3 (motion), A11 (unified search + cross-actions), A14 (responsive).
