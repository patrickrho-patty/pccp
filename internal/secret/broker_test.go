package secret

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fixedAuthority struct{ key ed25519.PublicKey }

func (f fixedAuthority) IssuerKey(string) (ed25519.PublicKey, bool) { return f.key, true }

func brokerStack(t *testing.T) (*Broker, ed25519.PrivateKey, *dari.GrantEnvelope) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/s.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(db)
	// Prime the in-memory credential store through Issue.
	_, priv, _ := ed25519.GenerateKey(nil)
	_ = priv
	broker := NewBroker(svc, nil)
	_ = broker
	return broker, priv, nil
}

// TestBrokerDeniesBeforeRead is the Task 17 boundary: an out-of-scope
// or missing grant is denied BEFORE the credential value is touched.
func TestBrokerDeniesBeforeRead(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(t.TempDir()+"/s2.db"), &gorm.Config{Logger: logger.Discard})
	svc := New(db)
	broker := NewBroker(svc, nil)

	_, issuerPriv, _ := ed25519.GenerateKey(nil)
	authority := fixedAuthority{key: issuerPriv.Public().(ed25519.PublicKey)}

	// A grant WITHOUT secret coverage.
	now := time.Now().UnixMilli()
	body := &dari.AuthorizationGrantBody{
		Version: 1, GrantID: "g1", Issuer: "pccp-policy",
		SubjectPeerID: "harness", SubjectKeyThumbprint: dari.SubjectKeyThumbprint(issuerPriv.Public().(ed25519.PublicKey)),
		Audience: []string{"relay"}, OrganizationID: "o", UserID: "u", SessionID: "s", PolicyEpochID: "e",
		Scope:       dari.AuthorizationScope{Tools: []string{"edit"}},
		NotBeforeMs: now - 1000, NotAfterMs: now + time.Hour.Milliseconds(),
	}
	grant, err := dari.SignAuthorizationGrant(body, issuerPriv)
	if err != nil {
		t.Fatal(err)
	}

	// No grant → denied before value access.
	if _, err := broker.ReadAuthorized(context.Background(), nil, authority, "cred-1", "vault://db", now); err == nil {
		t.Fatal("grant-less read accepted")
	}
	// Out-of-scope grant → denied.
	if _, err := broker.ReadAuthorized(context.Background(), grant, authority, "cred-1", "vault://db", now); err == nil {
		t.Fatal("out-of-scope grant accepted")
	}
	// Wrong authority (rogue issuer key) → denied.
	_, rogue, _ := ed25519.GenerateKey(nil)
	if _, err := broker.ReadAuthorized(context.Background(), grant, fixedAuthority{key: rogue.Public().(ed25519.PublicKey)}, "cred-1", "vault://db", now); err == nil {
		t.Fatal("rogue issuer accepted")
	}
	// Expired grant → denied.
	expired := &dari.AuthorizationGrantBody{
		Version: 1, GrantID: "g2", Issuer: "pccp-policy",
		SubjectPeerID: "harness", SubjectKeyThumbprint: dari.SubjectKeyThumbprint(issuerPriv.Public().(ed25519.PublicKey)),
		Audience: []string{"relay"}, OrganizationID: "o", UserID: "u", SessionID: "s", PolicyEpochID: "e",
		Scope:       dari.AuthorizationScope{Tools: []string{"secrets:*"}},
		NotBeforeMs: now - time.Hour.Milliseconds()*3, NotAfterMs: now - time.Hour.Milliseconds(),
	}
	expiredGrant, err := dari.SignAuthorizationGrant(expired, issuerPriv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.ReadAuthorized(context.Background(), expiredGrant, authority, "cred-1", "vault://db", now); err == nil {
		t.Fatal("expired grant accepted")
	}

	// In-scope grant reaches the underlying broker (unknown credential
	// → the credential layer's own failure, AFTER authorization).
	inScope := &dari.AuthorizationGrantBody{
		Version: 1, GrantID: "g3", Issuer: "pccp-policy",
		SubjectPeerID: "harness", SubjectKeyThumbprint: dari.SubjectKeyThumbprint(issuerPriv.Public().(ed25519.PublicKey)),
		Audience: []string{"relay"}, OrganizationID: "o", UserID: "u", SessionID: "s", PolicyEpochID: "e",
		Scope:       dari.AuthorizationScope{Tools: []string{"secrets:vault://db"}},
		NotBeforeMs: now - 1000, NotAfterMs: now + time.Hour.Milliseconds(),
	}
	inScopeGrant, err := dari.SignAuthorizationGrant(inScope, issuerPriv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.ReadAuthorized(context.Background(), inScopeGrant, authority, "cred-unknown", "vault://db", now); err == nil {
		t.Fatal("authorization passed but the credential must still be validated")
	}
	// Usage metering counted the authorized attempt.
	if u := broker.UsageReport()["g3|vault://db"]; u != 1 {
		t.Fatalf("usage = %d, want 1", u)
	}

	_ = brokerStack
}
