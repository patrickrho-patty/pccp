# Harness B — Provenance & Evidence (`patty-code-pccp`)

> Grounded in: `internal/evidence/{evidence,child}.go` (Ledger: Record/Summary/MergeChild/receipt-progress — rich, **no Export/Emit/Send/Persist**), `internal/capability` (Audit decision ledger), and the PCCP side `internal/provenance` (`CreateChangeSet`/`CreateProvenanceSpan` have **zero callers**; `models/provenance.go`).

## What exists (honest baseline)
- A **rich local evidence Ledger**: records receipts, verification progress, successful commands, completed reviews, mutation-risk classification, background-lease tracking, child-summary merge (for subagents). 5.7K LOC, tested.
- A **capability Audit** ledger (`RecordDecision/Decline/Route/Gate/Skill/MCPProxy`).
- PCCP has rich provenance models (`ChangeSet` with §19.3 attribution states + confidence; `ProvenanceSpan` with §19.4 AST fingerprints; `ActionEnvelope`; `EvidenceReceipt`) — but nothing populates them.

## The gaps (provenance is local-only → PCCP's provenance product is empty)

### B1. Evidence emits nothing to PCCP
- The Ledger has no export/emit/send; it's in-memory/local. PCCP's `CodeExplorer`/`Provenance` are permanently empty (CodeExplorer A). *Fix:* a provenance-emission path: as the harness edits files, build `ChangeSet` + line-level `ProvenanceSpan` and push to the Relay (new PAPER `PROVENANCE`/`CHANGESET` records — none exist in `paperproto` today); Relay records via `provenance.CreateChangeSet/CreateProvenanceSpan` (currently uncalled).

### B2. No content-addressed, rename-safe attribution
- `ChangeSet.AttributionState` (AI_GENERATED / AI_THEN_HUMAN_EDITED / …) and `ProvenanceSpan.ASTFingerprint` are stored but **never computed**. *Fix:* compute attribution from edit provenance; AST-anchor spans so they survive rename/move (§19.2/§19.5); add a survival test.

### B3. No evidence-receipt acknowledgment loop
- PCCP `IssueEvidenceReceipt` exists; the harness never receives/retains an ack. *Fix:* per protected action → signed receipt → PCCP ack → harness retains as tamper-evidence (§40.3, §39.6).

### B4. No prompt→tool→file→commit attribution chain in the harness UI / no replay
- Devs can't see "this code came from prompt X on model Y" (§19.1); no replay-from-provenance (§14.3). *Fix:* surface the chain author-side; replay reconstructs context+model through the governed pipeline.

## Implementation notes
- The harness already classifies mutations and tracks commands/reviews locally — B1 is largely *transmit what the Ledger already knows*, structured as ChangeSet/Span, not new detection.
- Depends on Harness A (PAPER record types) and CodeExplorer A (PCCP write-side).

## Acceptance
- After a governed session edits code, an admin clicking that code in `CodeExplorer` sees the originating session/user/model/exchange + AI/human attribution; spans survive a file rename; receipts verify.
