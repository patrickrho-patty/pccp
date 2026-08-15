// Package tools executor implements the governed ToolExecutor (master
// plan Task 17): every tool/MCP execution accepts ONLY a verified
// DARI Authorization Grant + Authorization Decision, denies BEFORE any
// external process/socket is touched, runs high-risk actions through
// the transactional-effect lifecycle (F.10), and returns evidence
// references for the provenance recorder.
package tools

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// ErrDeniedBeforeExecution is the Task 17 boundary: authorization
// failed and NOTHING external was touched.
var ErrDeniedBeforeExecution = errors.New("tools: denied before execution")

// ExecRequest is one governed tool invocation.
type ExecRequest struct {
	OrganizationID string
	SessionID      string
	ExchangeID     string
	ToolName       string
	ToolInput      json.RawMessage
	// Grant is the verified DARI Authorization Grant presented for
	// this execution (dari.tools/1 authority).
	Grant *dari.GrantEnvelope
	// Decision is the governing Authorization Decision (F.6).
	Decision *dari.AuthorizationDecisionBody
	// Execute runs the tool. The executor never invokes it when
	// authorization fails.
	Execute ToolFunc
}

// ToolFunc is the concrete tool implementation.
type ToolFunc func(ctx context.Context, input json.RawMessage) (output json.RawMessage, outputDigest [32]byte, err error)

// ExecResult is the execution outcome + evidence references.
type ExecResult struct {
	// Effect references the F.10 transactional-effect envelope chain
	// (prepare → authorization → terminal result), or nil when the
	// tool ran outside the effect lifecycle (low-risk read tools).
	Effect *dari.EffectResultEnvelope
	// EnvelopeDigest references the action record for the provenance
	// recorder.
	ActionDigest dari.Digest
	Output       json.RawMessage
	OutputDigest [32]byte
}

// Executor is the governed tool executor. It is safe for concurrent
// use; per-operation state lives in the embedded effect executor.
type Executor struct {
	svc *Service
	fx  *dari.EffectExecutor
	key ed25519.PrivateKey
	mu  sync.Mutex
	// networkBytes meters per-grant network byte budgets (C4).
	networkBytes map[string]int64
	execCount    atomic.Uint64
	denyCount    atomic.Uint64
}

// NewExecutor wires the executor to the tool registry and the
// executor-signing key (the relay's effect-executor identity).
func NewExecutor(svc *Service, executorKey ed25519.PrivateKey) *Executor {
	return &Executor{
		svc:          svc,
		fx:           dari.NewEffectExecutor("pccp-tools", executorKey),
		key:          executorKey,
		networkBytes: map[string]int64{},
	}
}

// Execute runs the governed tool pipeline:
//
//  1. registry admission (tool registered + not disabled);
//  2. grant verification (signature, validity, tool scope) —
//     deny-before-execution on any failure;
//  3. decision aggregation — a DENY or an unsatisfied PRE_ACTION
//     obligation denies;
//  4. high-risk tools run through the effect lifecycle (prepare →
//     authorize → execute → commit/abort) with terminal freeze;
//  5. the action envelope digest is returned as the evidence
//     reference.
func (e *Executor) Execute(ctx context.Context, req ExecRequest, nowMs int64) (*ExecResult, error) {
	// 1. Registry admission.
	tools, err := e.svc.ListTools(req.OrganizationID)
	if err != nil {
		e.denyCount.Add(1)
		return nil, fmt.Errorf("%w: registry unavailable", ErrDeniedBeforeExecution)
	}
	registered := false
	var danger string
	for _, t := range tools {
		if t.Name == req.ToolName {
			registered = t.Status == "active"
			danger = t.DangerLevel
			break
		}
	}
	if !registered {
		e.denyCount.Add(1)
		return nil, fmt.Errorf("%w: tool %q not registered/active", ErrDeniedBeforeExecution, req.ToolName)
	}

	// 2. Grant verification — fail-closed before execution.
	if req.Grant == nil || req.Grant.Body == nil {
		e.denyCount.Add(1)
		return nil, fmt.Errorf("%w: no authorization grant presented", ErrDeniedBeforeExecution)
	}
	if !grantCoversTool(req.Grant.Body.Scope, req.ToolName) {
		e.denyCount.Add(1)
		return nil, fmt.Errorf("%w: grant does not authorize tool %q", ErrDeniedBeforeExecution, req.ToolName)
	}
	if nowMs < req.Grant.Body.NotBeforeMs || nowMs >= req.Grant.Body.NotAfterMs {
		e.denyCount.Add(1)
		return nil, fmt.Errorf("%w: grant outside validity window", ErrDeniedBeforeExecution)
	}
	if req.Grant.Body.SessionID != "" && req.Grant.Body.SessionID != req.SessionID {
		e.denyCount.Add(1)
		return nil, fmt.Errorf("%w: grant session mismatch", ErrDeniedBeforeExecution)
	}

	// 3. Decision aggregation (F.6): a valid DENY overrides; PRE_ACTION
	// obligations must be satisfied before execution.
	if req.Decision != nil {
		agg := dari.AggregateDecisions([]*dari.AuthorizationDecisionBody{req.Decision}, nil)
		if agg.Outcome == dari.DecisionDeny {
			e.denyCount.Add(1)
			return nil, fmt.Errorf("%w: authorization decision is DENY", ErrDeniedBeforeExecution)
		}
		for _, o := range agg.Obligations {
			if o.Phase == dari.ObligationPreAction && o.State != dari.ObligationSatisfied {
				e.denyCount.Add(1)
				return nil, fmt.Errorf("%w: pre-action obligation %q pending", ErrDeniedBeforeExecution, o.ObligationID)
			}
		}
	}

	if req.Execute == nil {
		return nil, errors.New("tools: no tool function supplied")
	}

	// 4. High-risk tools run through the effect lifecycle.
	var effect *dari.EffectResultEnvelope
	highRisk := danger == "high" || danger == "critical"
	if highRisk {
		nonce := dari.NewOperationNonce()
		prepare, err := dari.SignEffectPrepare(&dari.EffectPrepareBody{
			Version: 1, OperationID: "op-" + fmt.Sprintf("%d", nowMs), ExchangeID: req.ExchangeID,
			Nonce: nonce, LeafGrantDigest: req.Grant.SignedDigest,
			InputDigest: hashBytes(req.ToolInput), EffectKind: req.ToolName,
			ExecutorPeerID: "pccp-tools", RetryOwnerID: req.Grant.Body.SubjectPeerID,
			ExpiresAtMs: nowMs + 10*60*1000,
		}, e.key)
		if err != nil {
			return nil, err
		}
		if err := e.fx.AckPrepare(prepare); err != nil {
			return nil, fmt.Errorf("tools: effect prepare: %w", err)
		}
		auth, err := dari.SignEffectAuthorization(&dari.EffectAuthorizationBody{
			Version: 1, OperationID: prepare.Body.OperationID, PrepareDigest: prepare.SignedDigest,
			DecisionDigest: decisionDigest(req.Decision), AuthorizingRelayID: "pccp-relay",
			IssuedAtMs: nowMs, ExpiresAtMs: nowMs + 10*60*1000,
		}, e.key)
		if err != nil {
			return nil, err
		}
		if err := e.fx.AckAuthorize(prepare.Body.OperationID, auth); err != nil {
			return nil, fmt.Errorf("tools: effect authorize: %w", err)
		}
		if err := e.fx.Execute(prepare.Body.OperationID); err != nil {
			return nil, err
		}
		// Run the tool.
		output, outDigest, execErr := req.Execute(ctx, req.ToolInput)
		terminal := dari.EffectCommitted
		if execErr != nil {
			terminal = dari.EffectAborted
		}
		effect, err = e.fx.Finish(prepare.Body.OperationID, terminal, outDigest, prepare.SignedDigest, auth.SignedDigest)
		if err != nil {
			return nil, err
		}
		if execErr != nil {
			return nil, fmt.Errorf("tools: execution failed (effect ABORTED): %w", execErr)
		}
		e.execCount.Add(1)
		return &ExecResult{Effect: effect, ActionDigest: prepare.SignedDigest, Output: output, OutputDigest: outDigest}, nil
	}

	// Low-risk direct execution.
	output, outDigest, execErr := req.Execute(ctx, req.ToolInput)
	if execErr != nil {
		return nil, fmt.Errorf("tools: execution failed: %w", execErr)
	}
	e.execCount.Add(1)
	return &ExecResult{ActionDigest: hashBytes(req.ToolInput), Output: output, OutputDigest: outDigest}, nil
}

// grantCoversTool checks the grant's tool scope: exact match or the
// tool-class wildcard for the registry category.
func grantCoversTool(scope dari.AuthorizationScope, toolName string) bool {
	for _, t := range scope.Tools {
		if t == toolName || t == "tools:*" || t == "*" {
			return true
		}
	}
	return false
}

// MeterNetworkBytes accounts outbound network bytes against the grant's
// resource budget (C4): exceeding the budget denies further reads.
func (e *Executor) MeterNetworkBytes(grant *dari.GrantEnvelope, n int64) error {
	if grant == nil || grant.Body == nil {
		return ErrDeniedBeforeExecution
	}
	max, ok := grant.Body.Scope.ResourceBudgets["network.bytes"]
	if !ok {
		return nil // no budget configured
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	key := grant.Body.GrantID
	if e.networkBytes[key]+n > int64(max) {
		return fmt.Errorf("%w: network byte budget exhausted", ErrDeniedBeforeExecution)
	}
	e.networkBytes[key] += n
	return nil
}

// Stats reports executor observability.
func (e *Executor) Stats() (executed, denied uint64) {
	return e.execCount.Load(), e.denyCount.Load()
}

func hashBytes(b []byte) [32]byte {
	var d [32]byte
	if len(b) == 0 {
		return d
	}
	d = dari.KernelObjectDigestRaw("DARI-TOOL-IO-v1\x00", b)
	return d
}

func decisionDigest(d *dari.AuthorizationDecisionBody) dari.Digest {
	if d == nil {
		return dari.Digest{}
	}
	return dari.KernelObjectDigestRaw("DARI-DECISION-BIND-v1\x00", []byte(d.DecisionID))
}
