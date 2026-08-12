# PCCP Current State — v2 Implementation

**Last updated:** PCCP v2 migration in progress (M1-M2 milestones)

## v2 Implementation Progress

### M0 — Inventory ✅ COMPLETE
- Full v1 codebase inventoried (46 packages, 148 API endpoints)
- v1 Gateway paths identified
- v1 Enterprise regression passing

### M1 — Shared Kernel Profiles and New Core Schemas ✅ COMPLETE
New schemas added (internal/models/catalog.go):
- `CatalogModel` — stable user-facing model identity (§10A)
- `CatalogEpoch` — versioned effective catalog per scope (§10A.5)
- `ModelDescriptor` — capability contract sent to Harness (§10A.6)
- `Account` — Public Cloud subscriber identity (§10C)
- `Subscription` — plan, status, entitlements (§10C.2)
- `AccountCapacityLease` — multi-Relay concurrency control (§10C.5)

### M2 — PAPER Model Catalog + Harness Authority ✅ IMPLEMENTED
- Server-Authoritative Model Catalog service (internal/catalog/)
- paper.models/1 PAPER extension (10 new message types)
- CatalogModel/CatalogEpoch generation and validation
- Model withdraw/announce lifecycle
- Default catalog seeded: Patty Code Standard + Pro
- 5 catalog tests passing

### M3 — PAPER AI Semantic v2 ✅ SPEC COMPLETE
- Full PAPER AI Semantic IR (internal/paper/ai_v2.go)
- Provider-neutral: tools, multimodal, structured output, streaming
- 12 normalized finish reasons
- Rich usage accounting (input/output/cache/reasoning)
- 11 streaming lifecycle events
- 13 built-in coding tool classes

### M4 — Public Cloud ✅ SERVICE COMPLETE
- Account/Subscription lifecycle (internal/publiccloud/)
- Capacity lease issuance and validation
- Agent Work Slot management (semantic concurrency)
- Separate risk domains: AccountIntegrity, TrustSafety, PlatformSecurity, Capacity
- Plan configs: free/developer/pro/team/enterprise
- 7 publiccloud tests passing

### Remaining M4-M8
- OAuth/OIDC PKCE flow for public users
- Account Portal UI (self-service harness management)
- Relay data plane with hot signed state caches
- Fair scheduler implementation
- Public SRE operations dashboard
- Enterprise regression with catalog integration

## Statistics

- **146 tests** passing (0 failing)
- **47 Go packages** (added: catalog, publiccloud)
- **~24,000 lines** Go code
- Build OK, Vet OK
