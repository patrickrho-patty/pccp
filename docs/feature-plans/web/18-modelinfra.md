# 18 — 모델 인프라 · ModelInfra (`web/src/pages/ModelInfra.tsx`)

> Vertical read: component (3 tabs: catalog/packages/endpoints) → `/api/catalog/{models,epoch,seed,withdraw,announce}` + `/api/models{,/{id}/publish,recall}` + `/api/endpoints{,/enroll,/{id}/lease,drain}` → `models/model_registry.go ModelPackage/InferenceEndpoint`, `catalog` service. Cross-checked PMP trust (PIA) and catalog push (§10A).

## What this page actually is
**Model & endpoint operations** (§9, §10A, §11, §30) — the server-authoritative model catalog (what users choose), Patty Model Packages (signed, content-addressed artifacts), and inference endpoints/PIAs (deployments with leases). The trust core of the product.

## Current vertical (what exists) — rich & mostly real
| Layer | Reality |
|---|---|
| Catalog tab | `/api/catalog/models` + `epoch` display, seed, withdraw/announce — **real** catalog epoch lifecycle |
| Packages tab | register/publish/recall `ModelPackage`; model is **very rich**: `WeightsMerkleRoot`, shard/tokenizer/config/chat-template digests, quant, serving engines, capabilities, **`ManifestDigest` + `Signature` (COSE-Sign1)**, state lifecycle |
| Endpoints tab | enroll/lease/drain `InferenceEndpoint` (PIA peer, build digest, assurance) |

## Gaps — grounded
**A. PMP signature stored but never verified.** `ModelPackage.Signature`/`ManifestDigest` are captured, but PIA never verifies the package at load (MISSING_ITEMS P.1) and nothing validates the digest chain on publish. *Fix:* verify signature+digests at publish and at PIA load; reject on mismatch (§9.4).
**B. Catalog epoch isn't pushed to harnesses** (§10A) — displayed here, but no code sends `paper.models/1` snapshots to connected harnesses. *Fix:* push epoch+catalog to harnesses on change/connect.
**C. No model-recall impact analysis** (§33.9) — recall flips state but doesn't enumerate affected sessions/commits/endpoints.
**D. No canary/ring assignment** (§33.10), no routing-policy editor (§10.6/§30.4), no data-residency router config (§30.5), no KV/cache utilization per endpoint (§10.10) — gpuops has metrics but not surfaced/wired here.
**E. No detail pages** for package/endpoint; TTFT/active-requests not sortable; no filter by family/state/assurance.

## UX improvements (grounded)
1. Catalog/packages/endpoints on 3 tabs is good — but no cross-nav (a package → its endpoints → its sessions).
2. PMP "게시" has no verification status shown (A) — admin can't tell if signature validated.
3. Epoch refresh/withdraw lack impact preview/diff.
4. "기본 모델 등록" free-text IDs → pick from catalog.
5. TTFT/active-requests not sortable; health dot without drill-down.
6. No package/endpoint detail page.
7. No filter by family/state/assurance; no bulk drain/publish.
8. No favorites; no export; no empty-state.
9. Artifact/digest fields raw, not copyable; no sub-menu.
10. Endpoint lease status not actionable (renew/revoke inline).
11. No assurance-level explanation (L1/L2/…).
12. No responsive layout.

## Intended-features coverage (vs WEB_FEATURE_GAPS §16 — 10 features)
1. PMP signature/attestation viewer (§9.4) → **A** ✅
2. Endpoint assurance editor + verification (§9.6) → **D** ✅
3. Catalog-epoch publish/recall + affected-endpoint preview (§10A.8) → **B** + preview **add**
4. Model recall impact analysis (§33.9) → **C** ✅
5. Routing-policy editor (§10.6/§30.4) → **D** ✅
6. GPU/replica capacity planner (§30.3) → **D** (gpuops) ✅
7. Endpoint drain/cordon + live in-flight count → **D** ✅
8. Canary/rollout ring assignment (§33.10) → **D** ✅
9. Data-residency router config (§30.5) → **D** ✅
10. KV/cache utilization per endpoint (§10.10) → **D** ✅

## Sequencing
Phase 1 (trust): A (verify PMP signature+digests at publish + PIA load), B (push catalog to harnesses) — the §9.4/§10A core; coordinate with PIA + protocol plans.
Phase 2 (ops): C (recall impact), D (canary/routing/residency/cache), detail pages, server query/sort/filter.
Phase 3 (usability): cross-nav, copyable digests, assurance help, bulk ops.
