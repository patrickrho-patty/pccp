# PCCP Current State — v2 Implementation

**Last updated:** v2 migration M1-M4 complete, M5+ remaining

## v2 Milestone Status

| Milestone | Status | Description |
|---|---|---|
| M0 — Inventory | ✅ | v1 codebase fully inventoried |
| M1 — Kernel Schemas | ✅ | CatalogModel, CatalogEpoch, Account, Subscription, AccountCapacityLease |
| M2 — Model Catalog | ✅ | Server-authoritative catalog service + paper.models/1 + API + UI |
| M3 — PAPER AI v2 | ✅ | Full semantic IR (tools, multimodal, streaming, cache, structured output) |
| M4 — Public Cloud | ✅ | Accounts, subscriptions, capacity leases, work slots, risk domains |
| M5 — Relay Data Plane | 🔄 | PAPER native listener exists, needs hot state caches + fair scheduler |
| M6 — Account Integrity | 📋 | Risk domain separation implemented, needs detection signals |
| M7 — SRE Console | 📋 | Not started |
| M8 — Enterprise Regression | 📋 | v1 features still work, need catalog integration |

## Key v2 Deliverables Complete

1. **Server-Authoritative Model Catalog** (§10A):
   - CatalogModel/CatalogEpoch/ModelDescriptor schemas
   - catalog service with epoch generation, validation, withdraw/announce
   - paper.models/1 protocol extension (10 message types)
   - API: /api/catalog/* (6 endpoints)
   - UI: ModelCatalog.tsx page with Korean labels

2. **PAPER AI Semantic v2** (§10B):
   - AIRequestV2, AIInputItem, AIContentPart (multimodal)
   - ToolDescriptorV2 with 6 placement types
   - ToolChoiceMode (auto/none/required/specific/subset)
   - 12 FinishReason types
   - UsageV2 with cache accounting
   - 11 streaming event types
   - 13 built-in coding tool classes

3. **Public Cloud** (§10C):
   - Account/Subscription lifecycle
   - AccountCapacityLease with Ed25519 signing
   - Agent Work Slots (semantic concurrency)
   - 4 separate risk domains
   - Plan configs (free/developer/pro/team/enterprise)
   - API: /api/public/* (6 endpoints)

## Statistics

- **146 tests** passing (0 failing)
- **47 Go packages** (v2 added: catalog, publiccloud)
- **20 React pages** (v2 added: ModelCatalog)
- **~24,000 lines** Go + **1,600 lines** TS/React
- Build OK, Vet OK, 23 demo checks passing
