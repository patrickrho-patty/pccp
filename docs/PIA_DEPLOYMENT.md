# PIA Deployment Guide — patricks-mint (GPU Machine)

## Prerequisites

- Go 1.26+ installed
- vLLM running with Qwen3 MoE on GPU0+GPU1
- GitHub credentials configured (private repo access)

## Setup

```bash
# Create project directory
mkdir -p /data/projects
cd /data/projects

# Clone the PCCP repo
git clone https://github.com/patrickrho-patty/pccp.git
cd pccp

# Build the PIA binary
go build -o bin/pccp-pia ./cmd/pccp-pia/

# Verify vLLM is running and get the model name
curl http://localhost:8081/v1/models

# The vLLM endpoint should be at localhost:8081 (or wherever vLLM is running)
# No API key is needed — the endpoint is open.
```

## Running PIA

```bash
# Set environment variables
export PCCP_DB_DRIVER=sqlite
export PCCP_DB_DSN=/data/projects/pccp/.data/pia.db
export PCCP_PIA_HTTP_ADDR=:9090
export PCCP_PIA_ENGINE=vllm
export PCCP_PIA_SERVING_URL=http://localhost:8081
export PCCP_PIA_PEER_ID=pia-mint-01
export PCCP_PIA_ASSURANCE=L1
export PCCP_CP_URL=http://<control-plane-host>:8080

# Start PIA (connected to vLLM, will forward requests to Qwen3 MoE)
./bin/pccp-pia

# To auto-enroll with the control plane:
./bin/pccp-pia --enroll --org <org-id> --model pmp_qwen3_moe_v1
```

## Verifying PIA

```bash
# Health check
curl http://localhost:9090/health

# Test inference (PIA forwards to vLLM)
curl -X POST http://localhost:9090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3-moe",
    "messages": [{"role": "user", "content": "안녕하세요"}]
  }'
```

## Architecture

```
Harness (developer machine)
    ↓ PAPER
Relay (Control Plane)
    ↓ PAPER / HTTP
PIA (patricks-mint, this machine)
    ↓ localhost
vLLM (GPU0 + GPU1)
    ↓
Qwen3 MoE model
```

The PIA is the ONLY component that talks to vLLM directly.
All other components talk to the PIA through the Relay.
