# PCCP API Reference

> **Scope (revised 2026-08-21).** This reference documents the core control-plane entity APIs and
> the data-plane entry points. The full REST surface is larger — 451 registered routes live in
> `internal/api/server.go`, which is the source of truth for exact paths; per-screen usage is
> mapped in [FEATURE_DOCUMENTATION.md](FEATURE_DOCUMENTATION.md).

## Authentication

All API endpoints (except `/api/auth/*`) require a JWT bearer token in the `Authorization` header.

```
Authorization: Bearer <token>
```

### POST /api/auth/bootstrap
Initial setup — creates the first organization and admin account. Default credentials come from
`PCCP_ADMIN_EMAIL` / `PCCP_ADMIN_PASSWORD` (defaults `admin@patty.dev` / `changeme`).

**Request:**
```json
{
  "email": "admin@patty.dev",
  "password": "changeme",
  "org_name": "Patty Enterprise"
}
```

### POST /api/auth/login
Authenticate and receive a JWT token.

**Request:**
```json
{
  "email": "admin@patty.dev",
  "password": "changeme"
}
```

**Response:**
```json
{
  "token": "eyJhbGc..."
}
```

---

## Organizations

### GET /api/organizations
List all organizations.

### POST /api/organizations
Create a new organization.

### GET /api/organizations/{id}
Get a specific organization.

---

## Users

### GET /api/users
List users in the authenticated user's organization.

### POST /api/users
Create a new user.

**Request:**
```json
{
  "organization_id": "org-uuid",
  "email": "kim@patty.dev",
  "name": "Kim Gaebal",
  "name_ko": "김개발",
  "title": "시니어 개발자"
}
```

---

## Harnesses

### GET /api/harnesses
List enrolled harnesses.

### POST /api/harnesses/enroll
Enroll a new harness instance. The harness must generate an Ed25519 key pair locally and submit its public key.

### POST /api/harnesses/{id}/revoke
Revoke a harness enrollment.

---

## Projects & Repositories

### GET /api/projects
List projects.

### POST /api/projects
Create a project with allowed model classes.

### GET /api/repositories
List repositories.

### POST /api/repositories
Register a repository under a project.

### POST /api/repositories/{id}/baselines
Create a repository baseline (immutable Git state reference).

---

## Sessions

### GET /api/sessions
List sessions.

### POST /api/sessions
Open a new working session.

### POST /api/sessions/{id}/close
Close a session.

### GET /api/sessions/{id}/provenance
Get the full provenance chain for a session.

---

## Model Registry

### GET /api/models
List model packages.

### POST /api/models
Register a new Patty Model Package (PMP).

### POST /api/models/{id}/publish
Publish a model package.

### POST /api/models/{id}/recall
Recall a model package (invalidates all endpoint leases).

---

## Endpoints

### GET /api/endpoints
List inference endpoints.

### POST /api/endpoints/enroll
Enroll a PIA as an inference endpoint.

### POST /api/endpoints/{id}/lease
Issue an endpoint lease.

---

## Policy

### GET /api/policy/epochs
List policy epochs.

### POST /api/policy/epochs
Create a new policy epoch.

### POST /api/policy/leases
Issue a capability lease for a session.

---

## Audit

### GET /api/audit
List recent audit events.

---

## Model Catalog

### GET /api/catalog
Get the current model catalog snapshot (server-authoritative model discovery).

### POST /api/catalog/refresh
Advance the catalog epoch and republish the snapshot to connected harnesses.

---

## SRE / Public Operations

### GET /api/sre/probes
Health probes for the service operations console (control plane, relay, scheduler, endpoint fleet).

---

## Scheduler API (port 8455)

The model traffic director exposes an HTTP admin/gateway surface (`PCCP_SCHED_HTTP_ADDR`) plus a
DARI worker listener on `:8445`. Admin auth is via `PCCP_SCHED_ADMIN_TOKEN` when set.

### GET /healthz
Scheduler liveness.

### GET /api/v1/{workers,fleet,queue,routing,stages,pd,kvdir,cache,programs,shadow,batch,scaling,perf}
Read-only operational views backing the web console panels: worker registry and load, queue
depths, routing decisions, stage queues, P/D capacity balance, KV directory occupancy, program
tool-pause state, shadow/canary rollout state (canary status is part of the shadow view), batch
jobs, autoscaling, and performance/SLO views.

---

## Relay API (port 8090)

### POST /v1/exchanges
Open a governed exchange. The Relay validates the capability lease, policy epoch, and model authorization before returning ALLOW/DENY.

### POST /v1/inference
Route an inference request to a PIA endpoint with a valid lease.

### POST /v1/exchanges/{id}/close
Close an exchange and issue an evidence receipt.

---

## PIA API (port 9090)

### GET /health
PIA health check including lease status.

### GET /v1/models
List available models.

### POST /v1/chat/completions
OpenAI-compatible inference endpoint. Requires a valid endpoint lease.
