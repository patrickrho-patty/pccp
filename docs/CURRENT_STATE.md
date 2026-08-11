# PCCP Current State

> **This file is the agent's handover.** At the end of every session, update this file to reflect what was done, what is in flight, and what the next session should pick up. The agent on the next session reads this file before doing anything else.

**Last updated:** 2026-08-11 (repo init)

---

## Phase 0 — Contracts & Trust Foundation

> Goal: one enrolled user + one enrolled Harness can send one governed request only to an attested Patty endpoint, and the complete action is auditable.

### Build slice checklist (from PRD §Appendix H)

- [ ] (1) `Organization`, `User`, `Harness`, `Project`, `Repository`, `Session`, `Action` schemas (CBOR, signed).
- [ ] (2) User SSO + independent Harness enrollment / certificate.
- [ ] (3) Signed `ActionEnvelope` and audit stream.
- [ ] (4) `ModelPackage`, `InferenceEndpoint`, `EndpointAttestation`, `EndpointLease` schemas.
- [ ] (5) PIA in front of one vLLM deployment.
- [ ] (6) Signed Patty model manifest verification.
- [ ] (7) Relay that rejects any endpoint without a valid `EndpointLease`.
- [ ] (8) One repository baseline + one model request correlated end-to-end.
- [ ] (9) One code patch written as `ChangeSet` with provenance to user/Harness/model.
- [ ] (10) Minimal Control UI showing user, Harness, session, repo/branch, model endpoint, request, and signed timeline.

### Phase 0 demo

> An enterprise admin enrolls 김개발 and his managed Patty Code Harness, assigns him to Project A, permits only `Patty-KoCoder-v1`, starts a session on `repo/payment-service` branch `feature/refund`, sends one Korean coding request, routes it only to an attested Patty endpoint, records the resulting edit against exact Git state, and then opens Control to see the complete user → Harness → prompt → model → file → diff → policy → commit provenance chain.

### Decisions made (Phase 0)

_TBD — populate as decisions land._

### Open questions (Phase 0)

- Language choice for the paper protocol library. (Existing Harness is Go 1.25; the worktree in `patty-code-pccp/` is the obvious model. Lean toward Go unless there's a reason.)
- Web framework for the Control Plane admin UI. (Likely TBD until (10) is in scope.)
- Database for the Control Plane. (TBD until (1) and (3) force the choice.)
- Harness dual-stack window: how long does the OpenAI-compatible path stay in `patty-code-pccp` once PAPER is live? (Recommend: drop it as soon as PAPER is functional, behind a `--legacy-provider` flag.)

### In-flight / blocker notes

_None yet._

---

## Phase 1 — Enterprise Harness Fleet + Gateway

Not started.

### Gate
Admins can enroll/revoke Harnesses, manage users, restrict models, trace one session to repository state.

---

## Phase 2 — Provenance + Security Enforcement

Not started.

### Gate
Click a code span and recover full session/user/Harness/model/context/tools/policies/tests; critical incident can isolate session/Harness.

---

## Phase 3 — Communications + Enterprise Operations

Not started.

### Gate
Enterprise can run PCCP as the central developer-AI operations hub without external chat for necessary engineering coordination.

---

## Phase 4 — Work Intelligence + Advanced Analytics

Not started.

### Gate
An evaluation scorecard can be explained entirely from underlying signed work evidence and requires human finalization.

---

## Phase 5 — Private/Sovereign Hardening

Not started.

### Gate
Representative Government/Sovereign environment can install, license, attest endpoints, operate, audit, and update without public internet.

---

## Phase 6 — Scale + Ecosystem

Not started.

### Gate
Large-group tenancy, MCP registry, advanced connectors, certification-readiness.

---

## Session log

| Date | Session | Outcome | Files touched |
|---|---|---|---|
| 2026-08-11 | repo init | Created repo, transferred PRDs, wrote `MASTER_PLAN.md`, `README.md`, this file. No code yet. | `docs/MASTER_PLAN.md`, `docs/CURRENT_STATE.md`, `README.md`, `.gitignore` |
