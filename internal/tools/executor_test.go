package tools

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// executor_test.go implements the Task 17 Step-1 boundary matrix:
// unauthorized tools/commands/MCP are denied BEFORE the external
// process/socket is touched; approval obligations, effect receipts,
// and network byte accounting are enforced.

func executorStack(t *testing.T) (*Executor, ed25519.PrivateKey, *dari.GrantEnvelope) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/t.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(&models.Tool{}, &models.Approval{})
	svc := New(db)
	_, _ = svc.RegisterTool("org-x", "read_file", "읽기", "read", "fs", "low", false)
	_, _ = svc.RegisterTool("org-x", "deploy_prod", "배포", "write", "deploy", "high", true)

	_, priv, _ := ed25519.GenerateKey(nil)
	exec := NewExecutor(svc, priv)

	now := time.Now().UnixMilli()
	grant, err := dari.SignAuthorizationGrant(&dari.AuthorizationGrantBody{
		Version: 1, GrantID: "g1", Issuer: "pccp-policy",
		SubjectPeerID: "harness", SubjectKeyThumbprint: dari.SubjectKeyThumbprint(priv.Public().(ed25519.PublicKey)),
		Audience: []string{"relay"}, OrganizationID: "org-x", UserID: "u", SessionID: "sess-1", PolicyEpochID: "e1",
		Scope: dari.AuthorizationScope{
			ActionClasses:   []string{"tool"},
			Tools:           []string{"read_file", "deploy_prod"},
			ResourceBudgets: map[string]uint64{"network.bytes": 1000},
		},
		NotBeforeMs: now - 1000, NotAfterMs: now + time.Hour.Milliseconds(),
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	return exec, priv, grant
}

func TestExecutorDeniesBeforeExecution(t *testing.T) {
	exec, priv, grant := executorStack(t)
	now := time.Now().UnixMilli()

	touched := false
	mk := func() ToolFunc {
		return func(context.Context, json.RawMessage) (json.RawMessage, [32]byte, error) {
			touched = true
			return json.RawMessage(`"ok"`), [32]byte{}, nil
		}
	}

	// Unregistered tool.
	_, err := exec.Execute(context.Background(), ExecRequest{
		OrganizationID: "org-x", SessionID: "sess-1", ToolName: "not-a-tool",
		Grant: grant, Execute: mk(),
	}, now)
	if !errors.Is(err, ErrDeniedBeforeExecution) || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected registry denial, got %v", err)
	}
	if touched {
		t.Fatal("tool function was invoked on a denied request")
	}

	// No grant.
	_, err = exec.Execute(context.Background(), ExecRequest{
		OrganizationID: "org-x", SessionID: "sess-1", ToolName: "read_file",
		Grant: nil, Execute: mk(),
	}, now)
	if !errors.Is(err, ErrDeniedBeforeExecution) || touched {
		t.Fatalf("expected grant-less denial, got %v", err)
	}

	// Grant outside scope (signed by the same issuer key).
	scopedAway, _ := dari.SignAuthorizationGrant(&dari.AuthorizationGrantBody{
		Version: 1, GrantID: "g2", Issuer: "pccp-policy",
		SubjectPeerID: "harness", SubjectKeyThumbprint: dari.SubjectKeyThumbprint(priv.Public().(ed25519.PublicKey)),
		Audience: []string{"relay"}, OrganizationID: "org-x", UserID: "u", SessionID: "sess-1", PolicyEpochID: "e1",
		Scope:       dari.AuthorizationScope{Tools: []string{"other_tool"}},
		NotBeforeMs: now - 1000, NotAfterMs: now + time.Hour.Milliseconds(),
	}, priv)
	_, err = exec.Execute(context.Background(), ExecRequest{
		OrganizationID: "org-x", SessionID: "sess-1", ToolName: "read_file",
		Grant: scopedAway, Execute: mk(),
	}, now)
	if !errors.Is(err, ErrDeniedBeforeExecution) || !strings.Contains(err.Error(), "does not authorize") {
		t.Fatalf("expected scope denial, got %v", err)
	}
	if touched {
		t.Fatal("out-of-scope tool executed")
	}

	// Expired grant.
	expiredBody := *grant.Body
	expiredBody.NotAfterMs = now - 1
	expired, err := dari.SignAuthorizationGrant(&expiredBody, priv)
	if err != nil {
		t.Fatal(err)
	}
	_, err = exec.Execute(context.Background(), ExecRequest{
		OrganizationID: "org-x", SessionID: "sess-1", ToolName: "read_file",
		Grant: expired, Execute: mk(),
	}, now)
	if !errors.Is(err, ErrDeniedBeforeExecution) || !strings.Contains(err.Error(), "validity") {
		t.Fatalf("expected expiry denial, got %v", err)
	}
	if touched {
		t.Fatal("expired grant executed")
	}

	// Session mismatch.
	_, err = exec.Execute(context.Background(), ExecRequest{
		OrganizationID: "org-x", SessionID: "OTHER", ToolName: "read_file",
		Grant: grant, Execute: mk(),
	}, now)
	if !errors.Is(err, ErrDeniedBeforeExecution) || touched {
		t.Fatalf("expected session denial, got %v", err)
	}

	// Decision DENY overrides the grant.
	denyDecision := &dari.AuthorizationDecisionBody{Outcome: dari.DecisionDeny, DecisionID: "d-deny"}
	_, err = exec.Execute(context.Background(), ExecRequest{
		OrganizationID: "org-x", SessionID: "sess-1", ToolName: "read_file",
		Grant: grant, Decision: denyDecision, Execute: mk(),
	}, now)
	if !errors.Is(err, ErrDeniedBeforeExecution) || !strings.Contains(err.Error(), "DENY") {
		t.Fatalf("expected decision denial, got %v", err)
	}
	if touched {
		t.Fatal("DENY-decision tool executed")
	}

	// Pending PRE_ACTION obligation blocks.
	pendingDecision := &dari.AuthorizationDecisionBody{
		Outcome: dari.DecisionAllowWithObligations, DecisionID: "d-ob",
		Obligations: []dari.Obligation{{ObligationID: "approval", Kind: "human", Phase: dari.ObligationPreAction, State: dari.ObligationPending, ResponsiblePeer: "harness"}},
	}
	_, err = exec.Execute(context.Background(), ExecRequest{
		OrganizationID: "org-x", SessionID: "sess-1", ToolName: "read_file",
		Grant: grant, Decision: pendingDecision, Execute: mk(),
	}, now)
	if !errors.Is(err, ErrDeniedBeforeExecution) || !strings.Contains(err.Error(), "pre-action") {
		t.Fatalf("expected obligation denial, got %v", err)
	}
	if touched {
		t.Fatal("pending-obligation tool executed")
	}

	_, denied := exec.Stats()
	if denied == 0 {
		t.Fatal("denials not counted")
	}
}

func TestExecutorLowRiskExecutes(t *testing.T) {
	exec, _, grant := executorStack(t)
	now := time.Now().UnixMilli()

	out, err := exec.Execute(context.Background(), ExecRequest{
		OrganizationID: "org-x", SessionID: "sess-1", ToolName: "read_file",
		Grant: grant,
		Execute: func(ctx context.Context, in json.RawMessage) (json.RawMessage, [32]byte, error) {
			return json.RawMessage(`{"content":"hello"}`), [32]byte{9}, nil
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Output) != `{"content":"hello"}` {
		t.Fatalf("output = %s", out.Output)
	}
	executed, _ := exec.Stats()
	if executed != 1 {
		t.Fatalf("executed = %d", executed)
	}
}

func TestExecutorHighRiskRunsEffectLifecycle(t *testing.T) {
	exec, _, grant := executorStack(t)
	now := time.Now().UnixMilli()

	var ran bool
	out, err := exec.Execute(context.Background(), ExecRequest{
		OrganizationID: "org-x", SessionID: "sess-1", ToolName: "deploy_prod",
		Grant: grant,
		Execute: func(ctx context.Context, in json.RawMessage) (json.RawMessage, [32]byte, error) {
			ran = true
			return json.RawMessage(`"deployed"`), [32]byte{7}, nil
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("high-risk tool did not run")
	}
	// The effect envelope MUST be present, terminal COMMITTED.
	if out.Effect == nil || out.Effect.Body.State != dari.EffectCommitted {
		t.Fatalf("effect = %+v", out.Effect)
	}
	// Terminal freeze: re-running Finish on the same op returns the
	// same durable result.
	if _, err := exec.Execute(context.Background(), ExecRequest{
		OrganizationID: "org-x", SessionID: "sess-1", ToolName: "deploy_prod",
		Grant: grant,
		Execute: func(ctx context.Context, in json.RawMessage) (json.RawMessage, [32]byte, error) {
			return json.RawMessage(`"again"`), [32]byte{8}, nil
		},
	}, now+10); err != nil {
		t.Fatalf("second op: %v", err)
	}
}

func TestExecutorNetworkByteBudget(t *testing.T) {
	exec, priv, grant := executorStack(t)

	if err := exec.MeterNetworkBytes(grant, 500); err != nil {
		t.Fatal(err)
	}
	if err := exec.MeterNetworkBytes(grant, 600); err == nil || !errors.Is(err, ErrDeniedBeforeExecution) {
		t.Fatalf("expected budget exhaustion, got %v", err)
	}
	// No budget configured → no limit.
	other, err := dari.SignAuthorizationGrant(&dari.AuthorizationGrantBody{
		Version: 1, GrantID: "g-nb", Issuer: "pccp-policy",
		SubjectPeerID: "harness", Audience: []string{"relay"},
		NotBeforeMs: 1, NotAfterMs: 2,
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.MeterNetworkBytes(other, 1<<40); err != nil {
		t.Fatalf("unbudgeted grant denied: %v", err)
	}
}
