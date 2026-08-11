package test

import (
	"testing"

	"github.com/patrickrho-patty/pccp/internal/billing"
	"github.com/patrickrho-patty/pccp/internal/command"
	"github.com/patrickrho-patty/pccp/internal/mcp"
	"github.com/patrickrho-patty/pccp/internal/network"
	"github.com/patrickrho-patty/pccp/internal/secret"
)

func TestMCPGovernance(t *testing.T) {
	db := setupTestDB(t)
	svc := mcp.New(db)

	server, err := svc.RegisterServer(mcp.MCPServer{
		OrganizationID: "org-1",
		Name:           "filesystem-mcp",
		ServerID:       "fs-mcp",
		Version:        "1.0",
		Publisher:      "patty",
		RiskLevel:      "medium",
		Status:         "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.ID == "" {
		t.Fatal("expected server ID")
	}

	decision, err := svc.EvaluateConnection(mcp.MCPConnectionRequest{
		OrganizationID: "org-1",
		SessionID:      "ses-1",
		HarnessID:      "hrn-1",
		ServerID:       "fs-mcp",
		RequestedOps:   []string{"read", "write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed {
		t.Fatalf("expected allowed, got: %s", decision.Reason)
	}
}

func TestMCPDenyList(t *testing.T) {
	db := setupTestDB(t)
	svc := mcp.New(db)
	svc.SetPolicy("org-1", mcp.MCPPolicy{
		OrganizationID: "org-1",
		DenyList:       []string{"evil-mcp"},
	})

	decision, _ := svc.EvaluateConnection(mcp.MCPConnectionRequest{
		OrganizationID: "org-1",
		ServerID:       "evil-mcp",
	})
	if decision.Allowed {
		t.Fatal("deny-listed server should be rejected")
	}
}

func TestNetworkBroker(t *testing.T) {
	db := setupTestDB(t)
	svc := network.New(db)

	decision, _ := svc.EvaluateConnection(network.ConnectionRequest{
		OrganizationID: "org-1",
		Destination:    "169.254.169.254:80",
		Protocol:       "http",
	})
	if decision.Allowed {
		t.Fatal("cloud metadata should be blocked")
	}

	decision, _ = svc.EvaluateConnection(network.ConnectionRequest{
		OrganizationID: "org-1",
		Destination:    "registry.npmjs.org:443",
		Protocol:       "https",
	})
	if !decision.Allowed {
		t.Fatalf("npm registry should be allowed: %s", decision.Reason)
	}

	decision, _ = svc.EvaluateConnection(network.ConnectionRequest{
		OrganizationID: "org-1",
		Destination:    "random-server.example.com:8080",
		Protocol:       "tcp",
	})
	if decision.Allowed {
		t.Fatal("unknown destination should be denied")
	}
}

func TestNetworkGrant(t *testing.T) {
	db := setupTestDB(t)
	svc := network.New(db)
	grant, err := svc.Grant(network.NetworkGrant{
		OrganizationID: "org-1",
		DNSPattern:      "*.internal.example.com",
		Protocol:        "https",
		Duration:        "1h",
		Purpose:         "internal API",
	})
	if err != nil {
		t.Fatal(err)
	}
	if grant.ID == "" {
		t.Fatal("expected grant ID")
	}
}

func TestSecretBroker(t *testing.T) {
	db := setupTestDB(t)
	svc := secret.New(db)

	cred, err := svc.Issue(secret.IssueRequest{
		OrganizationID: "org-1",
		SessionID:      "ses-1",
		SecretRef:      "vault://db/readonly",
		TargetService:  "postgres",
		Operation:      "migrate",
		Scopes:         []string{"db:migrate"},
		ValiditySecs:   300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cred.CredentialValue == "" {
		t.Fatal("expected credential value")
	}

	validated, err := svc.Validate(cred.ID, "ses-1")
	if err != nil {
		t.Fatal(err)
	}

	value, err := svc.GetCredentialValue(cred.ID)
	if err != nil {
		t.Fatal(err)
	}
	if value == "" {
		t.Fatal("expected value")
	}

	svc.Revoke("org-1", cred.ID, "test")
	_, err = svc.GetCredentialValue(cred.ID)
	if err == nil {
		t.Fatal("revoked should fail")
	}

	_ = validated
}

func TestSecretSessionExpiry(t *testing.T) {
	db := setupTestDB(t)
	svc := secret.New(db)
	cred, _ := svc.Issue(secret.IssueRequest{
		OrganizationID: "org-1",
		SessionID:      "ses-expire",
		SecretRef:      "vault://test",
	})
	svc.ExpireAllForSession("org-1", "ses-expire")
	_, err := svc.Validate(cred.ID, "ses-expire")
	if err == nil {
		t.Fatal("expired should fail")
	}
}

func TestBillingEntitlement(t *testing.T) {
	db := setupTestDB(t)
	svc, _ := billing.New(db)

	ent, err := svc.SetEntitlement(billing.Entitlement{
		OrganizationID:      "org-1",
		Plan:                "enterprise",
		AllowedModelFamilies: []string{"coder", "chat"},
		TokenQuotaPerDay:    1000000,
		MaxHarnesses:        10,
		ConcurrentSessions:  20,
		Status:              "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ent.CPSignature == "" {
		t.Fatal("expected signed entitlement")
	}
	if !svc.CheckModelAllowed("org-1", "coder") {
		t.Fatal("coder should be allowed")
	}
	if svc.CheckModelAllowed("org-1", "vision") {
		t.Fatal("vision should not be allowed")
	}
}

func TestCommandAuthorization(t *testing.T) {
	svc := command.New()

	decision := svc.Evaluate(command.CommandRequest{
		OrganizationID: "org-1",
		Executable:     "pytest",
		Arguments:      []string{"-v"},
	})
	if !decision.Allowed {
		t.Fatalf("pytest should be allowed: %s", decision.Reason)
	}

	decision = svc.Evaluate(command.CommandRequest{
		OrganizationID: "org-1",
		Executable:     "sudo",
		Arguments:      []string{"rm", "-rf", "/"},
	})
	if decision.Allowed {
		t.Fatal("sudo should be denied")
	}

	decision = svc.Evaluate(command.CommandRequest{
		OrganizationID: "org-1",
		Executable:     "git",
		Arguments:      []string{"push", "origin", "main"},
	})
	if decision.Allowed || !decision.RequiresApproval {
		t.Fatal("git push should require approval")
	}

	decision = svc.Evaluate(command.CommandRequest{
		OrganizationID: "org-1",
		Executable:     "curl",
		Arguments:      []string{"http://169.254.169.254/"},
	})
	if decision.Allowed {
		t.Fatal("curl to metadata should be denied")
	}
}
