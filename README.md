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
make dev-relay    # PAPER Relay on :8090
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
┌───────────────┐      PAPER (QUIC/TCP)  ┌──────────────────────────────┐              ┌────────────┐
│ Patty Code    │ ─────────────────────► │   Control Plane (admin)     │              │            │
│  Harness      │                        │ ┌──────────────────────────┐ │     PAPER    │   PIA      │
│  (HARNESS     │                        │ │   PAPER Relay (data)     │ │ ───────────► │ (INFERENCE)│
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
| PAPER Relay | 8090 | Data plane: auth, lease validation, model routing, evidence emission |
| PIA | 9090 | Patty Inference Agent: lease verification, OpenAI-compatible proxy to vLLM |

## Tech Stack

| Layer | Technology |
|---|---|
| Backend | Go 1.26 |
| Frontend | React 18 + TypeScript + Vite + Tailwind CSS |
| ORM | GORM (PostgreSQL / SQLite) |
| Protocol | PAPER (CBOR + COSE-Sign1, QUIC/TCP transport) |
| Crypto | Ed25519, SHA-256 |
| Default Language | Korean (ko-KR) with i18n support |

## Project Structure

```
pccp/
├── cmd/                    # Binary entrypoints
│   ├── pccp-server/        # Control Plane
│   ├── pccp-relay/         # PAPER Relay
│   └── pccp-pia/           # Patty Inference Agent
├── internal/
│   ├── paper/              # PAPER protocol library (CBOR, COSE, framing, state machine)
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
├── conformance/            # PAPER conformance suite
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
go test ./internal/paper/ -v

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
- [PAPER Protocol Spec](docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md)

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
