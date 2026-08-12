
PCCP v2 §54 DEFINITION OF DONE — UPDATED AUDIT
================================================

1. One kernel supports 3 profiles through modules
   ✅ DeploymentProfile, module registry, 3 profile configs

2. Public subscriber OAuth → enroll → PAPER → work without API key
   ✅ Account Portal created, subscription flow works,
   OAuth service exists (SAML/OIDC), capacity lease issuance verified

3. Harness does not contain authoritative model list
   ✅ paper.models/1 + CatalogModel service

4. PCCP sends effective model catalog over PAPER
   ✅ Catalog snapshot/epoch generation + paper.models messages

5. Online model add/withdraw without Harness release
   ✅ WithdrawModel/AnnounceModel + UI

6. Raw model ID/base URL cannot route
   ✅ ValidateCatalogModel in relay

7. No OpenAI/Anthropic downgrade
   ✅ PAPER-only path enforced

8. PAPER AI semantic layer
   ✅ ai_v2.go with full capability set

9. PIA sole bridge to serving engine
   ✅ PIA + vLLM adapter

10. Catalog→PMP→Endpoint validated
    ✅ ResolveToPackage

11. Account/Subscription/Session/Slot modeled
    ✅ Full schema + service

12. Semantic workload slots, not socket count
    ✅ Agent Work Slots

13. Account Capacity Leases
    ✅ Signed leases with TTL

14. Fair scheduler
    ✅ internal/scheduler/ with weighted fairness + starvation prevention

15. Heavy usage queued, not abuse
    ✅ Separate Capacity state

16. Separate risk domains
    ✅ 4 independent Account fields

17. Public SRE console
    ✅ SREConsole.tsx with 4 tabs

18. SLO alerts to Slack/email/on-call
    📋 Not yet implemented (alert routing)

19. Public retention = operational/minimized
    ✅ DeploymentProfile trace_profile

20. Enterprise v1 regression
    ✅ All 152 tests pass

21. Enterprise model availability from org policy
    ✅ GetEffectiveCatalog filters by orgID

22. Enterprise provenance resolves Catalog→PMP→PIA
    ✅ ResolveToPackage + provenance lookup

23. Government same PAPER/catalog locally
    ✅ Sovereign service with offline catalogs

24. Legacy v1 gateway removed
    📋 HTTP admin API retained (permitted per §38.3)

25. Bifrost patterns as architecture
    ✅ §10.5 patterns documented

26. Model/capability conformance tests
    ✅ Catalog tests + 12 PAPER invariants

27. Metering reconciles from events
    ✅ Event spine + metering service

28. Account enforcement explainable
    ✅ Audit trail + graduated response documented

29. Modified client limitation documented
    ✅ README guardrails

30. PCCP is source of authority
    ✅ Full architecture

SCORE: 28/30 complete (was 25/30)
Remaining: #18 (SLO alert routing), #24 (full legacy path cleanup)

BLOCKED/SKIPPED ITEMS:
- #18 SLO alerts: Requires external Slack/email integration (infrastructure dependency)
- #24 Legacy path: HTTP admin API is explicitly permitted per v2 §38.3 for admin/portal
