# Harness F — Latency & Streaming Benchmark (`patty-code-pccp` + `pccp`)

> **Origin:** operator request (2026-08-14): "check whether DARI can be much faster than the typical Responses/messages format, and comparable with the WS method Codex uses — faster network latency *and* security. Compare the numbers and put them into the arXiv paper."
>
> Grounded in: `internal/provider/dari/dari.go` (AI_OPEN → single AI_COMPLETE; connector already decodes `AI_TOKEN_CHUNK`), `pccp/internal/relay/paper_listener.go governAIOpen` (forwards inference, emits one AI_COMPLETE — **no token streaming on the governed path**), `internal/dari/messages.go` (AI_TOKEN_CHUNK 0x0402 defined, unused relay-side), and the comparison target `~/projects/codex/codex-rs/core/src/client.rs` (Responses API over a **cached, prewarmed WebSocket** — lazy open, reused across turns, `response.create` with `generate=false` prewarm — with SSE/eventsource HTTP fallback).

## What exists (honest baseline)
- DARI transport is a **persistent TLS 1.3 connection with binary CBOR framing** — no per-request HTTP upgrade, no SSE text parsing, no JSON-over-headers. Connection reuse exists (`connect()` pings and reuses).
- Auth is once-per-connection (transcript-bound AUTH_PROOF); leases/epochs are pushed on SESSION_OPEN — no per-request handshake cost.
- **But the governed path is not streaming**: the relay buffers the full inference and returns one AI_COMPLETE. TTFT (time-to-first-token) is therefore *whole-request*, strictly worse than any streaming method. The connector's AI_TOKEN_CHUNK decoder is dead code relay-side.
- No benchmark exists anywhere; no numbers for the paper.

## The gaps

### F1. No token streaming through the governed relay
- `governAIOpen` waits for the full forwarder response before replying. *Fix:* stream — forwarder yields deltas; relay wraps each delta in `AI_TOKEN_CHUNK` (0x0402, CBOR) and emits `AI_COMPLETE` at end, carrying governance (lease check, DLP, metering) per-chunk where cheap and per-exchange where not.

### F2. Connection prewarm / reuse parity with Codex WS
- Codex prewarms its WebSocket between turns. *Fix:* connector prewarms the DARI connection + session (SESSION_OPEN at idle end of turn, lease acquire) so turn-start cost is ~zero; measure with/without.

### F3. No benchmark harness or numbers
- *Fix:* a reproducible benchmark (`cmd/bench-paper` or `internal/bench`) that measures, over N runs:
  1. **TTFT** — first token at the connector,
  2. **inter-token latency** (p50/p95),
  3. **total completion time** for fixed token counts,
  4. **turn-start overhead** (connect+auth+session-setup vs warm reuse),
  5. **bytes-on-wire** per exchange (CBOR vs SSE text framing overhead).
- Three arms on identical localhost topology and identical canned model output:
  - **A. DARI** (this stack, streaming after F1),
  - **B. Responses/SSE** — mock Responses endpoint over HTTP SSE (the "typical" method),
  - **C. Codex-style WS Responses** — mock Responses endpoint over a cached WebSocket (mirroring `client.rs` behavior; reference implementation notes from `~/projects/codex`).
- All arms hit a **canned token source** (deterministic schedule) so the network/protocol stack is what's measured, not model speed.

### F4. Manuscript artifact
- *Fix:* methodology + results table (markdown → LaTeX) checked into `docs/`, reporting: per-arm TTFT/ITL/total/overhead/bytes, hardware+topology note, and the honest caveat that localhost favors persistent connections; security properties column (per-request auth vs per-connection transcript-bound auth; binary signed framing vs text). Numbers only land in the paper once F1 ships — measuring the non-streaming path would misrepresent DARI.

## Implementation notes
- F1 is the substantive work (relay streaming + connector passthrough + DLP inspector already chunk-capable). F2 is small. F3 is a bench package + mocks; keep it deterministic (fixed RNG schedule, no sleeps beyond the schedule).
- Depends on: Harness A wiring being live end-to-end (trust bundle, lease issuance) — the bench drives the real governed path, not a fake.

## Acceptance
- A streaming governed exchange reaches the connector token-by-token (AI_TOKEN_CHUNK observed in order, AI_COMPLETE last with usage);
- bench runs all three arms and prints the table; medians stable across runs (±10%);
- `docs/` contains the methodology + numbers artifact ready for the arXiv section;
- no regression: full test suites green in both repos.
