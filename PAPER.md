# PAPER Protocol — Open Source Repository

> **Patty AI Provenance & Enforcement Relay**
>
> PAPER is an open communication protocol for governed AI inference. It connects AI coding agents to model endpoints through a verifiable, policy-enforced, provenance-tracked data path.

## Quick Start

### Build a PIA (Patty Inference Agent)

A PIA bridges any model serving engine (vLLM, SGLang, TGI) to the PAPER protocol.

#### Using the Go SDK

```go
package main

import (
    "context"
    "piapi"
)

type MyAdapter struct{}

func (a *MyAdapter) Complete(ctx context.Context, req *piapi.Request) (*piapi.Response, error) {
    // Call your model serving engine here
    return &piapi.Response{
        Content:      "Model output here",
        FinishReason: "COMPLETED",
        Usage:        piapi.Usage{InputTokens: 10, OutputTokens: 20},
    }, nil
}

func (a *MyAdapter) HealthCheck(ctx context.Context) error { return nil }
func (a *MyAdapter) ModelID() string                        { return "my-model" }

func main() {
    pia := piapi.New("my-pia-01", &MyAdapter{})
    pia.Listen(":9444") // PAPER protocol listener
}
```

#### Using the vLLM Adapter

```go
import "vllmadapter"

func main() {
    client := vllmadapter.New("http://localhost:8000")
    // Use with PIA...
}
```

### Connect as a Harness

```go
import "paper"

client, err := paper.DialClient(ctx, paper.ClientConfig{
    Addr:       "relay.example.com:8444",
    PeerID:     "my-harness-01",
    Profile:    paper.ProfileHarness,
    PrivateKey: privateKey,
    Credential: peerCredential,
})
```

## Protocol Overview

```
Harness ←──PAPER──→ Relay ←──PAPER──→ PIA ←──local──→ vLLM/SGLang
```

| Concept | Description |
|---|---|
| **PAPER** | Binary protocol over TLS/TCP or QUIC using CBOR records |
| **Harness** | The AI coding agent (developer's machine) |
| **Relay** | Horizontally scalable data plane with governance enforcement |
| **PIA** | Patty Inference Agent bridging PAPER to local serving engines |
| **Peer Credential (PPC)** | COSE-signed credential binding identity to a public key |
| **Capability Lease** | Signed authorization for what a session can do |
| **Policy Epoch** | Versioned, immutable policy set |
| **Governed Exchange** | A single AI request/response with full provenance |

## Repository Layout

```
pccp/
├── internal/paper/          # PAPER protocol library (Go)
│   ├── framing.go           # 32-byte record framing
│   ├── cbor.go              # Deterministic CBOR encoding
│   ├── cose.go              # COSE-Sign1 envelopes
│   ├── transport.go         # TLS/TCP transport
│   ├── quic.go              # QUIC transport
│   ├── conn.go              # Connection state machine
│   ├── client.go            # Reference PAPER client
│   ├── ai_v2.go             # PAPER AI Semantic v2 (tools, multimodal, streaming)
│   ├── models.go            # paper.models/1 catalog extension
│   └── handshake.go         # All PAPER message types (CBOR)
├── adapters/
│   ├── vllm/                # Reusable vLLM adapter
│   └── sglang/              # Reusable SGLang adapter
├── sdk/
│   ├── piapi/               # PIA SDK (build your own inference agent)
│   └── examples/            # Example implementations
├── registry/
│   ├── messages.csv         # PAPER message type registry
│   ├── profiles.csv         # Peer profiles
│   ├── errors.csv           # Error code families
│   └── crypto.csv           # Cryptographic profiles
├── conformance/             # Conformance test suite
└── docs/plans/PAPER/        # Full protocol specification
```

## Building a Custom Adapter

Any inference engine can be connected to PAPER by implementing the `EngineAdapter` interface:

```go
type EngineAdapter interface {
    Complete(ctx context.Context, req *Request) (*Response, error)
    HealthCheck(ctx context.Context) error
    ModelID() string
}
```

The adapter is responsible for:
- Receiving normalized PAPER AI requests
- Calling the local serving engine API
- Returning normalized responses with usage accounting

The PAPER protocol handles:
- Authentication and authorization
- Policy enforcement
- Provenance tracking
- Evidence generation
- Streaming
- Tool governance

## Supported Serving Engines

| Engine | Status | Adapter |
|---|---|---|
| vLLM | ✅ Ready | `adapters/vllm/` |
| SGLang | ✅ Ready | `adapters/sglang/` |
| TGI | 📋 Planned | Implement `EngineAdapter` |
| Custom | ✅ Any | Implement `EngineAdapter` |

## Key Design Principles

1. **PAPER is the sole Harness protocol** — no OpenAI/Anthropic HTTP fallback
2. **Server-authoritative model discovery** — Harness receives models from PCCP
3. **Policy before inference** — governance decisions happen before model dispatch
4. **Provenance by default** — every action produces evidence
5. **PIA isolation** — serving engines are never directly reachable

## License

Open source. See LICENSE.
