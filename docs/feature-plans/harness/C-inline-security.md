# Harness C — Inline Security Governance (`patty-code-pccp`)

> Grounded in: `internal/guardian` (reasoning-based safety assessment, critical-deny, fail-closed), `internal/permission` (bash/powershell policy rules, explicit-approval gates), `internal/shellsafe`, `internal/secrets`, and the **0-files finding** for client-side DLP/PII/injection. PCCP side: `internal/security` (CheckContext detection — real, but not on the path) + Policy page (localStorage-only).

## What exists (honest baseline)
- A real **guardian** safety reviewer (assessment → critical-deny → fail-closed retry semantics).
- Real **command authorization** (`permission`: bash/powershell rule matching, reusable allows, explicit-approval gates) and `shellsafe`.
- A `secrets` package (local).
- PCCP has a working DLP/PII/secrets/injection detector (`security.CheckContext`) and a DLP rule catalog — but it runs only on the **manual scanner**, not inline on the path.

## The gaps (no client-side data-loss / injection governance)

### C1. No client-side DLP / Korean-PII / secret redaction before send
- 0 harness files for DLP/PII/injection. Outgoing context (prompts, file contents, tool I/O) is **not** scanned before PAPER dispatch. Relying on the Relay alone is too late/coarse. *Fix:* harness runs the PCCP rule pack (synced via policy epoch — Harness A4) client-side pre-send; block/redact/mask per policy (§16.3/§16.5). Korean PII lexicon (RRN/사업자/계좌/카드) must be versioned + org-overridable.

### C2. No prompt-injection defense at the harness boundary
- Repo content, tool output, and MCP results are untrusted inputs; the harness doesn't treat them as data or detect override/jailbreak/indirect-injection patterns. *Fix:* extend `guardian` to treat external content as untrusted, detect injection, refuse to act on injected instructions (§16.4, §35.8).

### C3. Tool/MCP approval against PCCP
- `permission` enforces command rules **locally**; tools/MCP aren't checked against the PCCP tool registry/approval state (§17.1/§17.2). *Fix:* resolve tool/MCP calls against the PCCP registry + per-org approval; block unapproved.

### C4. Network broker & secret broker integration
- Outbound network and secret access are local, not CP-brokered (§17.4/§17.5). *Fix:* harness asks the PCCP network/secret broker for grants; never hardcodes keys/URLs; usage accounted.

### C5. Response inspection
- Only outbound context is scanned; model **responses** (exfil-in-output) aren't checked (§16.5). *Fix:* inspect responses for secrets/PII before surfacing.

## Implementation notes
- The detection engine already exists in PCCP (`security.CheckContext`); C1 is *push the rule pack to the harness + run it pre-send*. The governance framework (`guardian`/`permission`) is the natural home.
- Depends on Harness A4 (policy epoch) for rule-pack sync.

## Acceptance
- A prompt containing an RRN/AWS key is redacted/blocked before dispatch; an injected "ignore previous instructions" in repo content is refused; an unapproved tool/MCP is blocked; a model response leaking a secret is caught.
