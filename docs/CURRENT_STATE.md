# PCCP Current State

**Last updated:** 2026-08-11 (post-audit)

## Final Statistics

- **~21,000 lines** of Go code across **43 packages** (84 source files)
- **~1,450 lines** of TypeScript/React (19 pages)
- **134 tests** passing (0 failing) across **19 test packages**
- **23 end-to-end demo checks** passing
- **148 REST API endpoints** across **40 route groups**
- **37 GORM domain models** (all with JSON tags)
- **51 PAPER message types** covering all 13 registry ranges

## Architecture Decision: cmd/ + internal/ (not src/relay/pia/)

The MASTER_PLAN suggested `src/`, `relay/`, `pia/` at the repo root.
We chose the Go-standard `cmd/` + `internal/` layout instead because:
- `cmd/` is the idiomatic Go location for binary entrypoints
- `internal/` prevents external import of internal packages
- Each component (Control Plane, Relay, PIA) is a separate binary under `cmd/`
- Shared protocol library, models, and services live in `internal/`

This is a deliberate and better decision per the plan's rule:
"Steering off of the plan is absolutely ok as long as the new plan is significantly better."

## All Signed Objects Use COSE-Sign1

Per PAPER spec, all signed objects use COSE-Sign1 (RFC 8152):
- ✅ PeerCredential (paper/peer.go SignWith/VerifySignature)
- ✅ ActionEnvelope (provenance/service.go RecordAction)
- ✅ CapabilityLease (policy/service.go IssueCapabilityLease)
- ✅ EvidenceReceipt (provenance/service.go IssueEvidenceReceipt)

## PAPER Transport

Both transports are implemented and tested:
- ✅ TLS/TCP with PAPER preface and CBOR framing (transport.go)
- ✅ QUIC with control stream and lane multiplexing (quic.go)
- ✅ PAPER native relay listener (relay/paper_listener.go)
- ✅ PAPER reference client (paper/client.go)

## All 12 PAPER Protocol Invariants Tested

conformance/conformance_test.go tests all 12 invariants from PAPER Appendix E.

## All 326 JSON Tags Added

All request/response struct fields across all 43 packages have JSON tags
for proper API serialization/deserialization.
