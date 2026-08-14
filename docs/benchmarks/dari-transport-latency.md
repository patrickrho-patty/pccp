# DARI Transport Latency & Streaming — Benchmark Methodology and Results

**Harness plan:** `docs/feature-plans/harness/F-latency-streaming.md` (F3/F4)
**Runner:** `cmd/pccp-bench` (`go run ./cmd/pccp-bench -turns 30 -tokens 128`)
**Date:** 2026-08-14 · **Host:** Apple M4 Pro, 14 logical CPUs, macOS, localhost loopback

## Question

Can a fully governed protocol (mutual authentication, capability leases, policy-epoch binding, inline DLP, evidence receipts, provenance) deliver interactive latency comparable to the ungoverned transports it replaces — the typical Responses/SSE HTTP call and the cached prewarmed WebSocket used by Codex-style clients?

## Arms

All three arms consume the same deterministic token schedule (128 tokens, 2 ms inter-token delay, 5 ms first-token delay) served by the same mock inference endpoint on the same host — the measured variable is the protocol stack, not the model.

| Arm | Transport | Per turn | Security carried |
|---|---|---|---|
| **DARI** | TLS 1.3 + binary CBOR framing, persistent connection | AI_OPEN → governed authorize → streamed AI_TOKEN_CHUNK → receipt push + ack → AI_COMPLETE | Mutual auth (COSE-Sign1 PPC + transcript-bound proof), capability lease, policy epoch, model catalog pin, inline DLP, evidence receipt + ack, provenance ingestion |
| **Responses/SSE** | HTTP/1.1 POST + `text/event-stream` | New TCP+HTTP request per turn | none |
| **WS Responses** | RFC 6455 WebSocket, cached across turns, `response.create` prewarm between turns (codex-rs `client.rs` parity) | Warm send | none |

## Method

- N=30 turns per arm; each turn is one full completion.
- TTFT measured at the client at the first content-bearing event; ITL between consecutive events; "Cold" = connect+handshake+session-setup amortized per turn-1 (PAPER/SSE re-connect per turn; DARI and WS reuse).
- Wire bytes counted at the application layer (framing + payloads), excluding TLS record overhead common to all arms.
- The DARI arm exercises the **real** governed loop: live relay binary, enrollment via the CA, AUTH_PROOF verification, SESSION_OPEN governance pushes (epoch → catalog → lease), per-exchange authorization, receipt issuance/ack.

## Results (median unless noted)

| Arm | TTFT ms | ITL p50 ms | ITL p95 ms | Total ms | Turn-start overhead ms | Bytes/turn |
|---|---|---|---|---|---|---|
| DARI (governed, streaming) | **8.38** | 2.51 | 2.57 | 328.46 | 4.07 (one-time; connection reused) | **5,271** |
| Responses/SSE | 6.47 | 2.52 | 2.55 | 323.69 | 6.49 (every turn) | 6,542 |
| WS Responses (prewarmed) | 6.41 | 2.52 | 2.56 | 323.50 | 0.57 (prewarm amortized) | 7,751 |

## Reading

- **Governance costs ~2 ms of TTFT and ~5 ms of total** on localhost — the DARI arm carries mutual auth, per-exchange authorization, DLP scanning, and a signed evidence-receipt round-trip on the same connection the tokens stream over, and still lands within ~2% of the ungoverned transports' total completion time.
- **Inter-token latency is statistically identical** across arms (p50 ≈ 2.5 ms): once streaming begins, the governed relay's per-chunk forwarding adds no measurable delay — the pipeline is transport-bound, not governance-bound.
- **Bytes on wire: DARI is the lightest** (−19% vs SSE, −32% vs WS JSON events) because of binary CBOR framing.
- **Turn-start:** WS prewarm is fastest per-turn (0.57 ms); DARI's 4 ms is a one-time cost (connection + session governance reused across turns; F2 prewarm keeps it warm), while SSE pays 6.5 ms every turn.
- **Honest caveat:** localhost eliminates real-network RTT, which favors persistent connections (DARI/WS) less than it would on a real network — on a WAN, DARI's avoidance of per-turn connection setup should widen its advantage over SSE, while the governance delta (~2 ms) is local compute and largely RTT-independent. Model inference time is excluded by construction (canned schedule).

## Reproduction

```bash
go run ./cmd/pccp-bench -turns 30 -tokens 128
```

Medians are stable across runs (±0.3 ms observed). The live-model validation (`DARI_LIVE_E2E_LIVE=1` in the connector repo, driving qwen3.6-35b-a3b through the same governed loop) confirms real-token streaming end-to-end (5–9 chunks observed per short completion).
