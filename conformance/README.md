# PAPER Conformance Suite

This directory contains the PAPER protocol conformance tests.

Per PAPER §70, the conformance suite covers:

1. **Framing** — record encode/decode, 32-byte prelude, limits
2. **Authentication** — challenge-response, replay protection, credential validation
3. **Leases** — capability lease validation, expiry, revocation
4. **AI/PIA** — inference exchange, model package verification, endpoint lease
5. **Tools** — tool proposal authorization
6. **Collaboration** — chat/presence/file independent lane classes
7. **Provenance** — spine node digest verification, evidence receipt validation

## Protocol Invariants (PAPER Appendix E)

1. No protected action without authenticated peer.
2. No protected action outside a valid Capability Lease.
3. No protected action evaluated under an unknown Policy Epoch.
4. No model invocation against an unapproved PMP/Endpoint Lease.
5. No tool proposal grants authority by itself.
6. No collaboration payload becomes model context implicitly.
7. No transport fallback changes authorization semantics.
8. No completed side effect is automatically duplicated after reconnect.
9. Every protected exchange terminates with verifiable evidence or explicit evidence failure.
10. Every provenance node digest changes if any canonicalized causal content changes.
11. A peer profile cannot emit privileged messages assigned to another profile.
12. Administrative communication and administrative enforcement are separate message classes.

## Running

```bash
go test ./conformance/... -v
```
