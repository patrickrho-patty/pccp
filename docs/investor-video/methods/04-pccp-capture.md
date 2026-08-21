# Method 04 — PCCP Enterprise and Sovereign Capture

## Source truth

PCCP is the governance, identity, security, provenance, communication, and operational backbone for Patty Code. The same conceptual kernel supports Enterprise and Government/Sovereign profiles. Government is a deployment/security profile, not a separate fork.

Use only `/Users/patrickrho/projects/pccp` as the source of truth, but build and stage inside the disposable copy from Method 02.

## Current fixture caveat

`internal/demo/enterprise.go` provides a rich, deterministic `SeedEnterprise` graph and comments that an operator should run `pccp-demo-seed`. In the inspected revision, no `cmd/pccp-demo-seed` exists and the `Makefile` does not build it. Do not invoke or document that missing command as if it exists.

The production agent has two valid options:

1. Use the supported bootstrap/API flows to create enough synthetic state.
2. In the disposable copy only, add a temporary local seeding entry point that opens the isolated database and calls `demo.SeedEnterprise`; review it, run it, and never copy it back to the source checkout.

Option 2 is preferred for the richer console. The helper must accept the database path as an explicit argument, reject paths outside the recorded capture root, and print no credential values. No tracked PCCP file is changed.

## Build the disposable copy

```bash
cd "$capture_root/pccp"
make build
```

Use the current prerequisites from PCCP `README.md`: Go 1.26+, Node.js 22+, pnpm, and SQLite for development.

## Isolated database and process start

Use a database path inside the recorded capture root and new, capture-only secrets supplied as process environment values. Never reuse or inspect repository `.env` files.

```bash
pccp_db="$capture_root/pccp-state/pccp.db"
mkdir -p "$capture_root/pccp-state" "$capture_root/logs"
capture_jwt="$(openssl rand -hex 32)"
capture_cp_token="$(openssl rand -hex 32)"

PCCP_DB_DRIVER=sqlite \
PCCP_DB_DSN="$pccp_db" \
PCCP_HTTP_ADDR=127.0.0.1:8080 \
PCCP_PROFILE=enterprise \
PCCP_JWT_SECRET="$capture_jwt" \
PCCP_CP_TOKEN="$capture_cp_token" \
PCCP_CA_KEY="$capture_root/pccp-state/ca.key" \
PCCP_CA_CERT="$capture_root/pccp-state/ca.cert" \
PCCP_STORAGE_DIR="$capture_root/pccp-state/storage" \
./bin/pccp-server >"$capture_root/logs/server.log" 2>&1 &
server_pid=$!
```

Keep `capture_jwt` and `capture_cp_token` in the process shell only. Do not print them, persist them in a sidecar, or reuse them outside this disposable run.

Start Relay and mock PIA only if the selected shots need a live governed exchange. Bind locally and record their PIDs. Do not expose any service publicly.

Before opening the UI, verify health and the expected organization profile through supported endpoints. Never record setup failures or terminal secrets.

## Enterprise shot manifest

Use a temporary browser profile at 3840×2160. Authenticate with capture-only fixture credentials and then record:

| ID | Route/state | Duration | Editorial purpose |
|---|---|---:|---|
| ENT01 | Dashboard overview | 6–8 s | Organization can see users, harnesses, active sessions, endpoints |
| ENT02 | Active session detail | 6–8 s | Human request bound to user, harness, repo, model, and policy |
| ENT03 | Policy/approval | 5–7 s | Consequential action is separately governed |
| ENT04 | Security/DLP | 5–7 s | Korean PII/secret control shown without real sensitive data |
| ENT05 | Provenance/evidence | 6–8 s | Request-to-action evidence and receipt |

The final film uses approximately 10–15 seconds of these shots.

## Sovereign shot manifest

Seed or bootstrap a separate isolated database using `PCCP_PROFILE=sovereign`. Do not merely relabel an Enterprise screenshot in post.

| ID | Route/state | Duration | Editorial purpose |
|---|---|---:|---|
| SOV01 | Profile/deployment overview | 6–8 s | Government/Sovereign posture is authentic |
| SOV02 | Local endpoint/model state | 5–7 s | Local approved inference infrastructure |
| SOV03 | Restricted network/update posture | 5–7 s | Closed/air-gapped operation without fake certification |

Use no public-agency seals or names. Do not show the Patty internal operations console.

## Capture truth checks

- Dashboard numbers come from the isolated database, never from post animation.
- No panel is labeled “live” if it is deterministic fixture data.
- The film may call the system operational product proof, but may not say a customer currently operates it.
- Korean PII, ISMS-P-oriented evidence, closed-network, and KCMVP-aware controls are positioning/capability language—not certifications.
- Any feature marked planned or not live in the current console is excluded from the proof montage.

## Shutdown

Terminate only `server_pid`, `relay_pid`, and `pia_pid` captured by this run. Verify ports 8080, 8090, and 9090 no longer answer before cleanup.
