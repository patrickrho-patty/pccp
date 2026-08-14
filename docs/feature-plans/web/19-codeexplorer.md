# 19 — 코드 탐색기 · CodeExplorer (`web/src/pages/CodeExplorer.tsx`)

> Vertical read: component → `/api/repositories`, `/api/provenance/repos/{id}{,/stats}` → `handleGetRepoProvenance/ChangeSets/Spans/Stats` → `models/provenance.go ChangeSet/ProvenanceSpan`. Verified the write-side: `CreateChangeSet`/`CreateProvenanceSpan` have **zero callers**.

## What this page actually is
The **code-provenance explorer** (§19) — browse changesets and line-level spans to see, per code region, who/which harness/model produced it, AI-vs-human attribution, and drill into the originating session. The "click AI code → see grounded provenance" promise (DoD #8).

## Current vertical — read-side real, write-side absent
| Layer | Reality |
|---|---|
| Component | 3 tabs (changesets/spans/files); repo picker; stats (total/AI%/lines); per-change detail; per-file span grouping |
| Read endpoints | real — query `ChangeSet`/`ProvenanceSpan` by repo |
| **Write-side** | **`CreateChangeSet` and `CreateProvenanceSpan` are never called anywhere** → the tables stay empty → the explorer permanently shows "no data" |

➡️ The page is correctly built but starved: nothing in the system creates provenance. The harness emits no evidence to PCCP (HARNESS B), and the relay records only `ActionEnvelope`s (via `RecordAction`), not changesets/spans.

## Gaps — grounded
**A. No provenance is ever produced.** *Fix:* the harness must emit `ChangeSet`+`ProvenanceSpan` (line-level, content-addressed) over DARI as it edits files; the relay records them. Without this, §19 doesn't exist (ties to HARNESS B + Domain 1–2).
**B. Attribution-state / AST-fingerprint logic unimplemented.** `AttributionState`/`ASTFingerprint` are stored but nothing computes them (§19.3/§19.4). *Fix:* compute attribution (AI_GENERATED vs AI_THEN_HUMAN_EDITED…) from edit provenance; AST-anchor spans so they survive rename/move (§19.2/§19.5).
**C. No real file browser** — repos are metadata (Repositories C2); can't browse files/branches, so "click code → provenance" has no code to click.
**D. No change-impact intelligence** (§20) — blast radius of a span; no click-span→harness-replay (§19.1).
**E. No detail pages/deep links; spans not filterable/sortable; heatmap lacks legend.**

## UX improvements (grounded)
1. Page is empty in practice (A) — needs an honest empty-state ("provenance appears as governed sessions produce code").
2. No file browser (C).
3. Tabs (changes/spans/files) without guidance.
4. Confidence shown as a number, no visual; attribution badge not explained.
5. No deep-link to a span/changeset.
6. Heatmap non-interactive, no legend.
7. No search across files; stats not clickable.
8. No favorites; no export; no diff view.
9. Session links generic (not to the specific session/exchange).
10. No responsive layout.

## Intended-features coverage (vs WEB_FEATURE_GAPS §21 — 10 features)
1. Real provenance data (wire the path) → **A** ✅
2. Line-level attribution hover → session/user/model (§19) → **B** ✅
3. AI-vs-human diff + surviving-code metrics (§19.4) → **B** (attribution) ✅
4. Change-impact intelligence (§20) → **D** ✅
5. Click code block → harness replay (§19.1) → **D** ✅
6. Repo/file tree (real git browse) → **C** ✅
7. Attribution confidence + override → **B** (confidence) + **add** override.
8. Commit-binding navigation (§18.6) → **D** (commit binding) ✅
9. Export attribution report → **add**; export report.
10. Filter by attribution/model/author/confidence → **E** ✅

## Sequencing
Phase 1 (data): A (harness emits + relay records changesets/spans) — the §19 enabler; B (compute attribution/AST).
Phase 2 (exploration): C (real file browser), D (impact + replay), detail pages/deep links, filters.
Phase 3 (usability): heatmap legend, export, search, responsive.
