# PCCP Current State

**Last updated:** DARI transport wired, both listeners working

## DARI Protocol Status

### Transport (wired into data path):
- ✅ Relay DARI listener on :8444 (accepts Harness connections)
- ✅ PIA DARI listener on :9444 (accepts Relay connections)  
- ✅ Relay paper_client.go connects to PIA via DARI when PCCP_PIA_DARI_ADDR set
- ✅ HTTP fallback for dev when PCCP_PIA_DARI_ADDR not set

### Deployed:
- ✅ PIA running on patricks-mint (:9090 HTTP, :9444 DARI)
- ✅ Connected to vLLM Qwen3 MoE (10.200.82.233:8033)
- ✅ Control Plane on localhost (:8080)
- ✅ Relay on localhost (:8090 HTTP, :8444 DARI)

### Open Source Deliverables:
- ✅ adapters/vllm/ — Reusable vLLM adapter
- ✅ adapters/sglang/ — Reusable SGLang adapter
- ✅ sdk/piapi/ — PIA SDK (as .txt documentation)
- ✅ sdk/examples/ — Example PIA (as .txt documentation)
- ✅ registry/ — Protocol registries (messages, profiles, errors, crypto)
- ✅ DARI.md — Adoption documentation

## Comprehensive Audit Gaps (from previous session)

### CRITICAL (now addressed):
1. ✅ DARI is now the inference transport (was HTTP)
2. ⚠️ DARI AI Semantic v2 defined but not used in data path yet
3. ⚠️ Legacy HTTP path still exists as fallback
4. ⚠️ No DARI streaming (single request/response only)

### MODERATE:
5. ❌ No hot signed state cache in Relay
6. ❌ No account sharing detection
7. ❌ No wallboard mode
8. ⚠️ Graduated response not enforced
9. ⚠️ Global search partial

## Statistics
- 152 tests passing | 48+ packages | Build OK
- PIA on patricks-mint verified with Qwen3 MoE
