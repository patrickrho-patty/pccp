# 15 — 샌드박스 · Sandboxes (`web/src/pages/Sandboxes.tsx`)

> Vertical read: component → `fetch /api/sandboxes{,/{id}/destroy,/{id}/snapshot}` → `server.go handleListSandboxes/handleCreateSandbox/handleDestroySandbox/handleForensicSnapshot` (1631–1675) → `sandbox.CreateSandbox`. `Sandbox` model lives in the `sandbox` package.

## What this page actually is
**Runtime & sandbox control** (§31) — configuring and launching isolated execution environments (container/microvm/remote) for sensitive work, with network policy, resource limits, and forensic snapshots. The point is that harness code execution happens in a governed, isolated runtime.

## Current vertical — the runtime is fictional
| Layer | Reality |
|---|---|
| Component | create form (mode docker/microvm/remote, image, session link, CPU/mem, network policy) with `modeInfo` isolation descriptions; list; snapshot + destroy actions |
| `sandbox.CreateSandbox` | **records a `Sandbox` definition only** — comment: *"In a real implementation, this would provision via Docker/containerd/Kubernetes. For now, we record the sandbox definition."* `Status` set `pending`, **never advanced to running**; nothing is provisioned |
| Destroy / Snapshot | DB status flips; snapshot is metadata-only (no real capture) |
| Network policy / resource limits / image digest | stored as JSON, **never enforced** (no runtime to enforce against) |

➡️ The entire §31 capability — actual isolated execution — is absent. The page configures a sandbox that never runs.

## Gaps — grounded
**A. No real runtime provisioning.** *Fix:* integrate a real runtime (containerd/Docker/K8s for container mode; Firecracker/gVisor for microvm; remote-host for remote) — `Create` provisions, status → `running`, `Destroy` tears down. This is the whole feature.
**B. Nothing binds harness execution to a sandbox** (§31.2 remote-sandbox baseline). *Fix:* a session configured for sensitive work must dispatch tool execution into its sandbox; local execution becomes an explicit, audited exception (§31.4).
**C. Network policy / resource limits unenforced.** *Fix:* apply at the runtime (network broker §17.4 + cgroup/limit hooks).
**D. Image allowlist + signing** (§31.1) — `ImageDigest` stored but not verified against an allowlist.
**E. Forensic snapshot is metadata-only.** *Fix:* real disk/memory capture for incident response (§15.4, §40.3).

## UX improvements (grounded)
1. "생성" silently records a row that never runs (A) — the page implies a working sandbox.
2. `modeInfo` describes isolation levels that don't actually differ (no provisioning per mode).
3. Image/CPU/mem free-text — needs an image picker/allowlist + validation (no slider/units guidance).
4. No sandbox detail / terminal/console view (nothing to show — no runtime).
5. Snapshot gives only an `alert('…생성됨')` — no artifact.
6. No filter by mode/status/session; no bulk destroy.
7. No resource-usage gauge (nothing to gauge).
8. No session lifecycle binding shown (B).
9. No favorites; no sub-menu (active/snapshots/images).
10. No empty-state; no destroy confirmation beyond the click.
11. No export; no responsive layout.
12. Network policy select is a single value (restricted/none/...) — no per-host rules editor.

## Intended-features coverage (vs WEB_FEATURE_GAPS §13 — 10 features)
1. Real sandbox runtime → **A** ✅
2. Runtime-mode enforcement (local/remote/container) → **A** (per-mode provisioning) ✅
3. Network-policy actual enforcement → **C** ✅
4. Resource limits actual enforcement → **C** ✅
5. Snapshot/restore real → **E** ✅
6. Session↔sandbox lifecycle binding → **B** ✅
7. Image allowlist + signing verification → **D** ✅
8. Sandbox fleet health → **add**; fleet health view once runtimes exist.
9. Per-sandbox evidence/provenance → **add**; tie sandbox to provenance/evidence.
10. Auto-teardown on session close → **add**; lifecycle hook on session close.

## Sequencing
Phase 1 (make it real — the feature): A (real provisioning for ≥1 mode), B (bind harness execution to sandbox), C (enforce network/resource).
Phase 2 (trust): D (image allowlist/signing), E (real forensic snapshot).
Phase 3 (ops): terminal view, resource gauges, server query, bulk ops.
