
PCCP v2 KEY CHANGES (vs v1):
=============================

NEW CORE SUBSYSTEMS:
1. Server-Authoritative Model Catalog (§10A) — CatalogModel, ModelDescriptor, CatalogEpoch, dari.models/1 extension
2. DARI AI Semantic Contract v2 (§10B) — rich tools, multimodal, structured output, streaming, caching
3. Public Cloud Profile (§10C) — Subscription, OAuth, Fair Use, Capacity, Account Integrity, SRE
4. Kernel/Module Architecture (§1) — Profile modules, Class A/B/C extensions, DeploymentProfile
5. Agent Work Slots + Compute Load Units (§10C.3-4) — semantic concurrency, not socket count
6. Account Capacity Lease (§10C.5) — multi-Relay concurrency control
7. Account Integrity / Trust & Safety separation (§10C.9-11)
8. Public SRE Console (§10C, §7.1)
9. Server-Authoritative Model Discovery — no local model list in Harness

MIGRATION ROADMAP (§48):
M0: Inventory (done — we have the v1 codebase)
M1: Kernel profiles + new schemas (CatalogModel, ModelDescriptor, Account, Subscription)
M2: DARI model catalog + Harness authority migration
M3: DARI AI semantic v2 expansion
M4: Public OAuth + subscription + account portal
M5: Relay data plane + capacity authority + fair scheduler
M6: Account integrity + T&S + platform security
M7: Public SRE console
M8: Enterprise regression
M9: Legacy path removal

IMMEDIATE SLICE (Appendix H):
1. Add CatalogModel, ModelDescriptor, CatalogEpoch schemas
2. Map current Patty model to Catalog Model
3. Implement dari.models snapshot
4. Harness model selector from snapshot only
5. Remove user custom base URL/provider config
6. AI_OPEN sends Catalog Model ID + epoch
7. Relay validates catalog + maps to PMP
8. Use existing PIA/Endpoint Lease
9. Add Public Account/Subscription/Entitlement
10. Three-Harness registration policy
11. Five-work-slot account policy
12. Record normalized usage
13. Minimal Public operations page
14. Run Enterprise regression

DEFINITION OF DONE v2 (30 criteria) — key additions:
- Server-authoritative model catalog
- DARI-only Harness path (no OpenAI/Anthropic fallback)
- DARI AI semantic superset
- Agent Work Slots (not socket count)
- Account Capacity Lease
- Separate risk domains (Capacity/Integrity/T&S/Security)
- Public SRE console
- Enterprise regression passes
