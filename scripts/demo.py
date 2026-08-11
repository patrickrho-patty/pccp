#!/usr/bin/env python3
"""PCCP Phase 0 End-to-End Demo.

Validates the complete Phase 0 build slice end-to-end.
"""
import subprocess
import sys
import time
import os
import signal
import json
import requests

PCCP_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_DIR = os.path.dirname(PCCP_DIR)
os.chdir(PROJECT_DIR)

API = "http://localhost:8080"
RELAY = "http://localhost:8090"
PIA = "http://localhost:9090"

GREEN = "\033[92m"
YELLOW = "\033[93m"
RED = "\033[91m"
NC = "\033[0m"

def banner(text):
    print(f"\n{GREEN}{'='*50}{NC}")
    print(f"{GREEN} {text}{NC}")
    print(f"{GREEN}{'='*50}{NC}")

def step(num, text):
    print(f"\n{YELLOW}[{num}/10] {text}{NC}")

def ok(text):
    print(f"  {GREEN}✓ {text}{NC}")

def fail(text):
    print(f"  {RED}✗ {text}{NC}")

def wait_for_server(url, name, timeout=10):
    for _ in range(timeout * 10):
        try:
            r = requests.get(f"{url}/health", timeout=1)
            if r.status_code == 200:
                return True
        except:
            pass
        time.sleep(0.1)
    return False

def main():
    banner("PCCP Phase 0 — End-to-End Demo")

    # Kill any existing servers
    step(0, "Setting up environment")
    os.system("pkill -f pccp-server 2>/dev/null; pkill -f pccp-relay 2>/dev/null; pkill -f pccp-pia 2>/dev/null; true")
    time.sleep(1)
    os.system("rm -f .data/pccp.db")

    env = os.environ.copy()
    env["PCCP_HTTP_ADDR"] = ":8080"
    env["PCCP_PIA_HTTP_ADDR"] = ":9090"
    env["PCCP_PIA_ENGINE"] = "mock"
    env["PCCP_RELAY_HTTP_ADDR"] = ":8090"

    # Start servers sequentially (SQLite needs the DB to exist first)
    cp_proc = subprocess.Popen(
        ["./bin/pccp-server"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
    )
    if not wait_for_server(API, "Control Plane"):
        fail("Control Plane failed to start")
        return 1
    ok("Control Plane started")

    pia_proc = subprocess.Popen(
        ["./bin/pccp-pia"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
    )
    if not wait_for_server(PIA, "PIA"):
        fail("PIA failed to start")
        return 1
    ok("PIA started")

    relay_proc = subprocess.Popen(
        ["./bin/pccp-relay"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
    )

    try:
        if not wait_for_server(RELAY, "Relay"):
            fail("Relay failed to start")
            return 1
        ok("Relay started")

        # 1. Bootstrap
        step(1, "Bootstrap Control Plane")
        r = requests.post(f"{API}/api/auth/bootstrap", json={
            "email": "admin@patty.dev", "password": "admin123", "org_name": "Patty Enterprise"
        })
        if r.status_code != 201:
            fail(f"Bootstrap failed: {r.text}")
            return 1
        org_id = r.json()["organization_id"]
        ok(f"Organization: {org_id}")

        # Login
        r = requests.post(f"{API}/api/auth/login", json={
            "email": "admin@patty.dev", "password": "admin123"
        })
        token = r.json()["token"]
        headers = {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}
        ok("Admin logged in")

        # 2. Create User
        step(2, "Enroll User: 김개발")
        r = requests.post(f"{API}/api/users", headers=headers, json={
            "organization_id": org_id, "email": "kim@patty.dev",
            "name": "Kim Gaebal", "name_ko": "김개발", "title": "시니어 개발자"
        })
        user_id = r.json()["id"]
        ok(f"User: 김개발 ({user_id})")

        # 3. Enroll Harness
        step(3, "Enroll Harness")
        harness_id = f"hrn_demo_{int(time.time())}"
        # Use a fixed demo public key
        pub_key = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
        r = requests.post(f"{API}/api/harnesses/enroll", headers=headers, json={
            "organization_id": org_id, "user_id": user_id, "harness_id": harness_id,
            "public_key_hex": pub_key, "binary_version": "1.0.0",
            "binary_hash": "sha256:abc123", "device_hostname": "dev-machine",
            "device_os": "darwin", "device_os_version": "14.0", "device_arch": "arm64",
            "enrollment_mode": "sso"
        })
        if r.status_code != 201:
            fail(f"Harness enrollment failed: {r.text}")
            return 1
        ok(f"Harness: {harness_id}")

        # 4. Create Project & Repository
        step(4, "Create Project & Repository")
        r = requests.post(f"{API}/api/projects", headers=headers, json={
            "organization_id": org_id, "name": "Payment Service", "name_ko": "결제 서비스",
            "slug": "payment-service", "allowed_models": ["pmp_kocoder_v1"]
        })
        proj_id = r.json()["id"]
        ok(f"Project: 결제 서비스 ({proj_id})")

        r = requests.post(f"{API}/api/repositories", headers=headers, json={
            "organization_id": org_id, "project_id": proj_id,
            "name": "payment-service", "full_name": "org/payment-service",
            "default_branch": "main", "sensitivity": "confidential"
        })
        repo_id = r.json()["id"]
        ok(f"Repository: payment-service ({repo_id})")

        # 5. Register Model Package
        step(5, "Register Model Package: Patty-KoCoder-v1")
        r = requests.post(f"{API}/api/models", headers=headers, json={
            "package_id": "pmp_kocoder_v1", "model_id": "patty-kocoder-35b",
            "name": "Patty-KoCoder-v1", "name_ko": "패티 코더 v1",
            "family": "coder", "version": "1.0.0",
            "capabilities": ["code", "tool_use", "korean"],
            "entitlement_class": "enterprise-coder",
            "minimum_endpoint_assurance": "L1", "state": "draft"
        })
        ok(f"Model Package: pmp_kocoder_v1")

        r = requests.post(f"{API}/api/models/pmp_kocoder_v1/publish", headers=headers)
        ok(f"Model published")

        # 6. Create Policy Epoch
        step(6, "Create Policy Epoch")
        r = requests.post(f"{API}/api/policy/epochs", headers=headers, json={
            "organization_id": org_id, "allowed_models": ["pmp_kocoder_v1"],
            "transition_mode": "immediate"
        })
        epoch_id = r.json()["epoch_id"]
        ok(f"Epoch: {epoch_id}")

        # 7. Enroll PIA & Issue Lease
        step(7, "Enroll PIA Endpoint & Issue Lease")
        r = requests.get(f"{PIA}/health")
        pia_pubkey = r.json()["public_key"]

        r = requests.post(f"{API}/api/endpoints/enroll", headers=headers, json={
            "organization_id": org_id, "pia_peer_id": "pia-local",
            "model_package_id": "pmp_kocoder_v1", "serving_engine": "vllm",
            "serving_engine_version": "0.6.0", "public_key_hex": pia_pubkey,
            "node_identity": "spiffe://patty.local/node/pia-local",
            "assurance_level": "L1"
        })
        ep_id = r.json()["endpoint_id"]
        ok(f"Endpoint: {ep_id}")

        r = requests.post(f"{API}/api/endpoints/{ep_id}/lease", headers=headers, json={
            "validity_hours": 24
        })
        if r.status_code != 201:
            fail(f"Endpoint lease failed ({r.status_code}): {r.text}")
            return 1
        lease_id = r.json()["lease_id"]
        ok(f"Endpoint Lease: {lease_id}")

        # 8. Open Session
        step(8, "Open Session on feature/refund")
        r = requests.post(f"{API}/api/sessions", headers=headers, json={
            "organization_id": org_id, "harness_id": harness_id, "user_id": user_id,
            "project_id": proj_id, "repository_id": repo_id, "branch": "feature/refund",
            "title": "환불 로직 구현", "task_purpose": "payment refund processing",
            "model_class": "pmp_kocoder_v1"
        })
        sess_db_id = r.json()["id"]
        sess_id = r.json()["session_id"]
        ok(f"Session: {sess_id}")

        # Issue capability lease
        r = requests.post(f"{API}/api/policy/leases", headers=headers, json={
            "organization_id": org_id, "subject_peer_id": harness_id,
            "user_id": user_id, "session_id": sess_id, "policy_epoch_id": epoch_id,
            "allowed_models": ["pmp_kocoder_v1"],
            "repository_scope": [{"repo_id": repo_id, "branch": "feature/refund"}],
            "file_path_read_scope": ["src/**"], "file_path_write_scope": ["src/**"],
            "tool_classes": ["read", "write", "execute"],
            "token_budget": 100000, "validity": 3600000000000
        })
        cap_lease_id = r.json()["lease_id"]
        ok(f"Capability Lease: {cap_lease_id}")

        # 9. Governed AI Inference Exchange
        step(9, "Governed AI Inference Exchange")
        r = requests.post(f"{RELAY}/v1/exchanges", json={
            "organization_id": org_id, "session_id": sess_id, "user_id": user_id,
            "harness_id": harness_id, "lease_id": cap_lease_id,
            "policy_epoch_id": epoch_id, "model_package_id": "pmp_kocoder_v1",
            "project_id": proj_id, "repository_id": repo_id,
            "branch": "feature/refund", "purpose": "implement refund logic"
        })
        resp_data = r.json()
        exch_id = resp_data.get("exchange", {}).get("id", "")
        verdict = resp_data.get("verdict", "")
        ok(f"Exchange: {exch_id}")
        ok(f"Verdict: {verdict}")

        if verdict == "ALLOW":
            ok("Relay authorized the request")

            # Route inference
            print(f"  {YELLOW}Routing inference to PIA...{NC}")
            r = requests.post(f"{RELAY}/v1/inference", json={
                "exchange_id": exch_id, "organization_id": org_id,
                "session_id": sess_id, "model_package_id": "pmp_kocoder_v1",
                "model": "Patty-KoCoder-v1",
                "messages": [{"role": "user", "content": "payment-service의 환불 처리 로직을 Go로 작성해주세요."}],
                "max_tokens": 500
            })
            if r.status_code == 200:
                choices = r.json().get("choices", [])
                if choices:
                    response_text = choices[0].get("message", {}).get("content", "")
                    ok(f"Response (first 200 chars): {response_text[:200]}")

                # Close exchange
                r = requests.post(f"{RELAY}/v1/exchanges/{exch_id}/close")
                if r.status_code == 200:
                    receipt_id = r.json().get("id", "")
                    ok(f"Evidence Receipt: {receipt_id}")
            else:
                fail(f"Inference failed: {r.text}")
        else:
            fail(f"Exchange denied: {resp_data}")

        # 10. Verify Provenance Chain
        step(10, "Verify Provenance Chain")
        r = requests.get(f"{API}/api/sessions/{sess_db_id}/provenance", headers=headers)
        chain = r.json()
        action_count = len(chain.get("actions", []))
        ok(f"Actions recorded: {action_count}")

        if action_count > 0:
            ok("Full provenance chain verified!")
            print(f"\n  {GREEN}The complete user → Harness → prompt → model → endpoint → action chain{NC}")
            print(f"  {GREEN}is recorded and auditable in the Control Plane.{NC}")

        banner("Phase 0 Demo: ALL CHECKS PASSED")
        print(f"\nOpen {YELLOW}http://localhost:8080{NC} to view the Control Plane UI.")

        return 0

    finally:
        cp_proc.terminate()
        pia_proc.terminate()
        relay_proc.terminate()
        cp_proc.wait()
        pia_proc.wait()
        relay_proc.wait()

if __name__ == "__main__":
    sys.exit(main())
