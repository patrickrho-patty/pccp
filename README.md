# Patty Code Control Plane (PCCP)

The governance, identity, security, provenance, communication, and operational backbone of the Patty Code product line.

**한국어 기반 AI 코딩 거버넌스 플랫폼 / Korean-first AI coding governance platform**

## Quick Start

### Prerequisites
- Go 1.26+
- Node.js 22+ with pnpm
- SQLite (for dev) or PostgreSQL 16+ (for production)

### Build & Run

```bash
# Build everything
make build

# Run the end-to-end demo
python3 scripts/demo.py

# Or start individual components:
make dev-server   # Control Plane API + Admin UI on :8080
make dev-relay    # DARI Relay on :8090
make dev-pia      # Patty Inference Agent on :9090
```

### Using Docker

```bash
cd deployments/docker
docker compose up
```

Then open http://localhost:8080 and bootstrap with:
- Email: `admin@patty.dev`
- Password: `admin123`

## Architecture

```
developer machine                        PCCP                                         inference host
┌───────────────┐      DARI (QUIC/TCP)  ┌──────────────────────────────┐              ┌────────────┐
│ Patty Code    │ ─────────────────────► │   Control Plane (admin)     │              │            │
│  Harness      │                        │ ┌──────────────────────────┐ │     DARI    │   PIA      │
│  (HARNESS     │                        │ │   DARI Relay (data)     │ │ ───────────► │ (INFERENCE)│
│   profile)    │                        │ │  auth · lease · policy   │ │              │            │
└───────────────┘                        │ │  DLP · verdict · evidence│ │              └─────┬──────┘
                                        │ └──────────────────────────┘ │                    │
                                        └──────────────────────────────┘            localhost│
                                                                                       ▼
                                                                                      vLLM/SGLang
```

## Components

| Component | Port | Description |
|---|---|---|
| Control Plane | 8080 | REST API + React admin UI, org/user/harness/model/policy management |
| DARI Relay | 8090 | Data plane: auth, lease validation, model routing, evidence emission |
| PIA | 9090 | Patty Inference Agent: lease verification, OpenAI-compatible proxy to vLLM |

## Tech Stack

| Layer | Technology |
|---|---|
| Backend | Go 1.26 |
| Frontend | React 18 + TypeScript + Vite + Tailwind CSS |
| ORM | GORM (PostgreSQL / SQLite) |
| Protocol | DARI (CBOR + COSE-Sign1, QUIC/TCP transport) |
| Crypto | Ed25519, SHA-256 |
| Default Language | Korean (ko-KR) with i18n support |

## Project Structure

```
pccp/
├── cmd/                    # Binary entrypoints
│   ├── pccp-server/        # Control Plane
│   ├── pccp-relay/         # DARI Relay
│   └── pccp-pia/           # Patty Inference Agent
├── internal/
│   ├── paper/              # DARI protocol library (CBOR, COSE, framing, state machine)
│   ├── models/             # 30 GORM domain models
│   ├── db/                 # Database layer (PostgreSQL/SQLite)
│   ├── identity/           # User auth, harness enrollment, PPC issuance
│   ├── registry/           # Model registry, PMP signing, endpoint enrollment
│   ├── policy/             # Policy epochs, capability leases
│   ├── provenance/         # Provenance spine, ChangeSet, evidence chain
│   ├── relay/              # Relay business logic
│   ├── pia/                # PIA business logic
│   ├── api/                # REST API handlers
│   └── config/             # Configuration
├── web/                    # React admin UI
├── conformance/            # DARI conformance suite
├── deployments/            # Docker + Kubernetes
├── scripts/                # Demo and utility scripts
└── docs/                   # PRDs, specs, plans, API reference
```

## Development

### Running Tests

```bash
# Unit tests
go test ./...

# Protocol tests only
go test ./internal/dari/ -v

# Conformance suite
go test ./conformance/ -v

# Integration tests
go test ./test/ -v
```

### Database

Dev uses SQLite (`.data/pccp.db`). To switch to PostgreSQL:

```bash
export PCCP_DB_DRIVER=postgres
export PCCP_DB_DSN="host=localhost port=5432 user=pccp password=pccp dbname=pccp sslmode=disable"
```

### Frontend Development

```bash
cd web
pnpm install
pnpm dev    # Dev server on :3000 with proxy to :8080
pnpm build  # Build to web/dist/
```

## Documentation

- [Master Plan](docs/MASTER_PLAN.md) — Navigation document
- [Current State](docs/CURRENT_STATE.md) — Phase-by-phase status
- [Implementation Plan](docs/IMPLEMENTATION_PLAN.md) — Build order
- [API Reference](docs/API_REFERENCE.md) — REST API docs
- [Product PRD](docs/plans/Patty_Code_Control_Plane_PRD_v1.md)
- [DARI Protocol Spec](docs/plans/DARI/DARI_Protocol_Specification_v1.0.md)

## Guardrails (non-negotiable)

1. **One product, three profiles.** Never accept code that forks for Government. Different policy defaults, different deployment topology, same code.
2. **Schema before UI.** Every entity defined in the PRD becomes a signed schema before any dashboard renders it.
3. **Vertical slices, not horizontal layers.** Every increment must ship end-to-end through Harness → Relay → PIA → Control, even if the surface is tiny. No "build all the dashboards first".
4. **Evidence is part of the build.** Every protected action emits an event in the same commit the action is implemented. No retroactive "add logging later".
5. **Conformance is part of the protocol.** DARI comes with a conformance suite. Reference implementation must pass it. Independent implementations must be able to pass it.
6. **Open-source boundary is enforced.** What is open stays open; what is proprietary stays proprietary. The control plane itself is open source (see PRD §9.9). The trust boundary is the signed model package and the endpoint attestation, not the secrecy of the code.
7. **No phantom compliance.** Do not claim "CSAP compliant", "KISA certified", or "ISMS-P compliant" because a feature exists. Maps and evidence are the product; the certifications are the customer's process.
8. **No HTTP/REST/WebSocket for protocol traffic.** The protocol binds QUIC and TLS/TCP. If the network blocks QUIC, fall back to native TLS/TCP — never to REST. The HTTP API is for admin/control-plane only.
9. **Harness changes ship from the worktree.** `patty-code-pccp/` is a separate repo. Do not stage Harness files into this repo.
10. **No employee evaluation autonomy.** Work Intelligence produces rubric scores with evidence. A human finalization step is required for any consequential employment decision. Period.


## Phase Status

| Phase | Theme | Status |
|---|---|---|
| 0 | Contracts & Trust Foundation | ✅ Complete |
| 1 | Enterprise Fleet + Gateway | 🔄 In Progress |
| 2 | Provenance + Security Enforcement | 📋 Planned |
| 3 | Communications + Operations | 📋 Planned |
| 4 | Work Intelligence | 📋 Planned |
| 5 | Sovereign Hardening | 📋 Planned |
| 6 | Scale + Ecosystem | 📋 Planned |

## License

See LICENSE file.
