# PCCP Implementation Plan — Phase 0

> Vertical-slice implementation plan for Phase 0: Contracts & Trust Foundation.

## Technology Stack Decisions

| Layer | Choice | Rationale |
|---|---|---|
| Backend language | Go 1.26 | Matches harness, protocol transport, enterprise-grade |
| Frontend framework | React 18 + TypeScript + Vite | Industry standard, Korean-first i18n |
| ORM | GORM | Multi-RDBMS (PostgreSQL + SQLite), mature, widely used |
| Dev database | SQLite | Zero-config, easy switching to PostgreSQL |
| Prod database | PostgreSQL 16 | Enterprise-grade, JSON/JSONB support, row-level security |
| HTTP framework | Chi | Lightweight, idiomatic Go, middleware-friendly |
| CBOR | fxamacker/cbor | Deterministic encoding, RFC 8949 compliant |
| COSE signing | veraison/go-cose | RFC 8152 compliant COSE-Sign1 |
| QUIC | quic-go/quic-go | De facto Go QUIC implementation |
| Crypto | Ed25519, SHA-256 | Per DARI cryptographic profile |
| CSS framework | Tailwind CSS | Utility-first, rapid Korean-first UI |

## Component Architecture

```
pccp/
├── go.mod
├── Makefile
├── cmd/
│   ├── pccp-server/     # Control Plane API server (HTTP, admin)
│   ├── pccp-relay/      # DARI Relay (QUIC/TCP data plane)
│   └── pccp-pia/        # Patty Inference Agent
├── internal/
│   ├── paper/           # DARI protocol library
│   │   ├── framing.go   # 32-byte record framing
│   │   ├── cbor.go      # Deterministic CBOR encoding
│   │   ├── cose.go      # COSE-Sign1 envelope
│   │   ├── messages.go  # Message types, registry
│   │   ├── conn.go      # Connection state machine
│   │   ├── auth.go      # Challenge-response auth
│   │   ├── peer.go      # Peer credentials (PPC)
│   │   └── crypto.go    # SHA-256 content addressing
│   ├── models/          # GORM domain models
│   ├── db/              # Database initialization, migration
│   ├── identity/        # User auth, harness enrollment
│   ├── registry/        # Model registry, PMP, endpoints
│   ├── policy/          # Policy epochs, capability leases
│   ├── provenance/      # Provenance spine, ChangeSet, evidence
│   ├── audit/           # ActionEnvelope, audit stream
│   ├── relay/           # Relay business logic
│   ├── pia/             # PIA business logic
│   ├── api/             # HTTP API handlers (admin)
│   └── config/          # Configuration management
├── web/                 # React admin UI (Korean-first)
├── migrations/          # Database migrations
├── test/                # Integration tests
└── docs/                # Existing PRDs + implementation docs
```

## Phase 0 Slices (in build order)

### Slice 1: Project scaffold + schema foundation
- Go module, directory structure, Makefile
- CBOR encoding, COSE signing, content addressing
- Record framing (32-byte prelude)

### Slice 2: Domain models + database
- GORM models for all Phase 0 entities
- Database init with PostgreSQL/SQLite support
- Auto-migration

### Slice 3: Identity & enrollment
- Organization, User models + API
- Harness enrollment flow, PPC issuance
- User authentication (JWT-based for admin)

### Slice 4: Model registry & PMP
- ModelPackage schema, signing, verification
- InferenceEndpoint, EndpointAttestation, EndpointLease
- PMP manifest format

### Slice 5: Policy engine
- Policy epochs
- Capability leases (signed authorization objects)

### Slice 6: DARI protocol core
- Connection state machine (HELLO → AUTH → READY)
- Peer credential verification
- Working sessions, governed exchanges
- Evidence receipts

### Slice 7: PIA (Patty Inference Agent)
- PIA enrollment, attestation
- Endpoint lease management
- Local serving bridge (OpenAI-compatible API proxy)

### Slice 8: Relay (Gateway)
- AuthN, lease validation, policy epoch binding
- Endpoint routing (reject without valid EndpointLease)
- Evidence emission

### Slice 9: Provenance & audit
- ActionEnvelope, audit stream
- Provenance spine (content-addressed DAG)
- ChangeSet with lineage
- Evidence receipts

### Slice 10: Control Plane API + React UI
- REST API for all admin operations
- React admin console (Korean-first)
- End-to-end demo flow

## Success Criteria
The Phase 0 demo: admin enrolls 김개발 + harness, assigns to project, permits Patty-KoCoder-v1, starts session on repo/payment-service branch feature/refund, sends Korean coding request, routes to attested endpoint, records edit against Git state, opens Control to see full provenance chain.
