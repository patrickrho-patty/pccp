# Harness D — Enterprise Workflow Governance (`patty-code-pccp`)

> Grounded in: the **0-files finding** for change-freeze / coding-standard / acknowledgement / forced-version / recall / change-control-board in the harness, and the PCCP side `internal/korean` (`InitiateChangeFreeze`, `IsChangeFrozen`, `SetForcedHarnessVersion`, `EmergencyModelRecall` — exist but stubs, not enforced) + `EnterpriseHarnessFeature` tracker (`code_review §33.4`, `coding_standards §33.11`, `mandatory_ack §33.6`, `exception_workflow §33.8` — catalogued, unimplemented).

## What exists (honest baseline)
- The harness has `control` (turn admission), `hook` (extensible pre/post hooks), `guardian`, `skill` — a strong substrate to hang workflow gates on.
- PCCP models the intent (`ChangeFreeze`, `ModelRecall`, forced-version, `EnterpriseHarnessFeature` rows) but enforcement is absent on both sides.

## The gaps (the Korean enterprise differentiators aren't enforced client-side)

### D1. Mandatory policy-acknowledgement gate (§33.6)
- 0 harness files. *Fix:* on policy-epoch change requiring ack, the harness blocks configured high-risk workflows until the dev acknowledges (Korean summary in-IDE); reports ack to PCCP.

### D2. Change-control-board submission (§33.4)
- *Fix:* harness auto-submits high-risk changes (crypto/payment/PII-adjacent, new MCP, new network dest, new dependency) to the PCCP change-control queue; blocks merge until approved.

### D3. Change-freeze / critical-period compliance (§33.13)
- PCCP `IsChangeFrozen` exists but the harness doesn't call it. *Fix:* harness checks freeze state for the target branch/repo; allows read/review/test, blocks AI write/export, requires elevated approval; surfaces the freeze reason clearly.

### D4. Coding-standard & architecture packs (§33.11)
- *Fix:* enforce org coding standards (framework versions, package boundaries, forbidden deps, naming/API conventions) via `hook`/`guardian`, with **plain-Korean explanations** when a rule blocks.

### D5. Forced harness version & release-ring (§33.10)
- *Fix:* harness checks its build against the PCCP-declared minimum version/ring on connect; self-updates or refuses to start if below minimum / on a blocked build.

### D6. Emergency model recall (§33.9)
- *Fix:* on a signed recall advisory, harness suspends the recalled model, surfaces affected sessions/commits, offers the declared replacement.

## Implementation notes
- These are gates/hooks on existing `control`/`hook`/`guardian`; the rules + state live in PCCP (`korean`, policy) and are pushed via the policy epoch (Harness A4). The `EnterpriseHarnessFeature` tracker (web 22) becomes meaningful once these report status/violations back (web 22 B).

## Acceptance
- A frozen branch rejects AI writes with a Korean reason; an un-acked policy blocks high-risk work; a sub-minimum build refuses to start; a recalled model is suspended with affected work listed.
