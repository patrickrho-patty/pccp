# Feature-Plans Review — Coverage Audit

**Reviewed:** every doc in `docs/feature-plans/` against the intended item lists in `WEB_FEATURE_GAPS.md` (10 features + ~15 improvements per page), `HARNESS_FEATURE_GAPS.md` (26 features), and the conversation's explicit asks.

## Outcome summary
- **All 26 web pages + 5 harness sections have a plan** (no page missing).
- **Every plan is grounded** in that page's code vertical (component→api→handlers→service→model) — verified by file:line citations; stale gap-doc claims were corrected at write time (e.g., Tools dup-bug-fixed, LiveView SSE-not-mounted).
- This review found and fixed 3 classes of incompleteness (below).

## Fixes applied in this review
1. **Added `web/00-cross-cutting.md`** — the 14 global improvements (favorites, sub-menus, animations, server-side pagination, clickable stats, EntitySelect, detail pages, ⌘K, theme/density, ConfirmDialog, unified search, empty-states, keyboard nav, responsive) were orphaned in `WEB_FEATURE_GAPS.md` with no plan. Now captured as a shared-infra plan that lifts all pages.
2. **Rewrote 6 genuinely-thin plans to full depth** (each now enumerates all intended features + ~14–15 improvements): `09-fleet`, `10-sre`, `11-servicecommandcenter`, `12-subscribermanagement`, `20-provenance`, `23-dashboard`. These had folded ~10 features into 3–5 prose paragraphs and dropped specifics (e.g., SRE's incident-timeline/regional-health/on-call; Fleet's containment-verification/approvals/broadcast-to-affected; ServiceCommand's support-timeline/refund/T&S-queue/abuse-feed).
3. **Residual folded items** — the remaining consolidated plans (`14-tools`, `15-sandboxes`, `16-analytics`, `17-audit`, `18-modelinfra`, `19-codeexplorer`, `21-liveview`, `22-enterprisefeatures`, `24-accountportal`) use grouped A–E gap paragraphs that cover most intended features but fold a few into "Phase 3 / advanced" sequencing. Each now ends with an **Intended-features coverage** appendix mapping all 10 intended features to where they're handled, so nothing is silently dropped.

## Per-page coverage (after fixes)
| Page | Plan | Intended features | Improvements | Status |
|---|---|---|---|---|
| Users | 01 | 12 (A/B/C) | 18 | ✅ full |
| Sessions | 02 | 12 | 15 | ✅ full |
| Harnesses | 03 | 10 | 16 | ✅ full |
| Projects | 04 | 10 | 15 | ✅ full |
| Repositories | 05 | 11 | 15 | ✅ full |
| Security | 06 | 9 | 15 | ✅ full |
| Policy | 07 | 11 | 15 | ✅ full |
| Compliance | 08 | 10 | 15 | ✅ full |
| Fleet | 09 | 12 (rewritten) | 15 | ✅ full |
| SRE | 10 | 12 (rewritten) | 14 | ✅ full |
| ServiceCommandCenter | 11 | 12 (rewritten) | 14 | ✅ full |
| SubscriberManagement | 12 | 13 (rewritten) | 14 | ✅ full |
| Communications | 13 | 11 | 14 | ✅ full |
| Tools | 14 | 5 groups + coverage appendix | 14 | ✅ (appendix added) |
| Sandboxes | 15 | 5 groups + appendix | 12 | ✅ (appendix added) |
| Analytics | 16 | 5 groups + appendix | 10 | ✅ (appendix added) |
| Audit | 17 | 5 groups + appendix | 14 | ✅ (appendix added) |
| ModelInfra | 18 | 5 groups + appendix | 12 | ✅ (appendix added) |
| CodeExplorer | 19 | 5 groups + appendix | 10 | ✅ (appendix added) |
| Provenance | 20 | 10 (rewritten) | 14 | ✅ full |
| LiveView | 21 | 5 groups + appendix | 9 | ✅ (appendix added) |
| EnterpriseFeatures | 22 | 4 groups + appendix | 9 | ✅ (appendix added) |
| Dashboard | 23 | 11 (rewritten) | 14 | ✅ full |
| AccountPortal | 24 | 5 groups + appendix | 10 | ✅ (appendix added) |
| Login | 25 | 5 (trivial) | 7 | ✅ appropriate |
| Bootstrap | 26 | 4 (trivial) | 5 | ✅ appropriate |
| Cross-cutting | 00 | 14 (new) | — | ✅ added |
| Harness A–E | harness/* | 26 features across 5 sections | — | ✅ full |

## Residual notes (not bugs — by design)
- Improvement counts on consolidated pages are lower than 15 because the repeating global UX items (favorites/sub-menu/animation/empty-state/responsive/keyboard-nav) now live once in `00-cross-cutting.md` rather than being duplicated per page (DRY). Each page retains its page-specific improvements.
- Two gap-doc claims were intentionally **corrected** (not dropped) because the code contradicts them: Tools "seeds duplicates" → actually deduped (RegisterTool + DB verified); LiveView "scripted fakery" → actually SSE-wired but the route isn't mounted + no emitter.
- Harness plans (A–E) collectively cover all 26 HARNESS_FEATURE_GAPS features, grounded in `patty-code-pccp` source + PCCP comms code.
