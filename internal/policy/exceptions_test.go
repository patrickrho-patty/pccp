package policy

import (
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newExceptionTestDB returns a fresh in-memory DB with the PolicyException
// (and supporting Epoch, ServiceSigningKey, AuditEvent) schema migrated.
// Each test gets its own DB to avoid shared-cache contamination.
func newExceptionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.PolicyException{}, &models.PolicyEpoch{}, &models.ServiceSigningKey{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newExceptionService(t *testing.T) *Service {
	t.Helper()
	s, err := New(newExceptionTestDB(t))
	if err != nil {
		t.Fatalf("policy.New: %v", err)
	}
	return s
}

func TestExceptionCreateRequiresTimeBoundedEvidence(t *testing.T) {
	s := newExceptionService(t)
	orgID := "org_test"
	_, err := s.CreateException(orgID, ExceptionInput{
		Scope: "project", ScopeID: "p1", ScopeName: "Payment",
		Reason: "incident", RequestedBy: "alice", RuleIDs: []string{"r1"},
		// Missing ExpiresAt + JustificationKo
	})
	if err == nil {
		t.Fatalf("expected error for missing expires_at and justification")
	}
	_, err = s.CreateException(orgID, ExceptionInput{
		Scope: "project", ScopeID: "p1", ScopeName: "Payment",
		Reason: "incident", RequestedBy: "alice", RuleIDs: []string{"r1"},
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		// Missing JustificationKo
	})
	if err == nil {
		t.Fatalf("expected error for missing justification_ko")
	}
}

func TestExceptionDecideRequiresReason(t *testing.T) {
	s := newExceptionService(t)
	orgID := "org_test"
	ex, err := s.CreateException(orgID, ExceptionInput{
		Scope: "project", ScopeID: "p1", ScopeName: "Payment",
		Reason: "incident", RequestedBy: "alice", RuleIDs: []string{"r1"},
		JustificationKo: "blocking release", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = s.DecideException(orgID, ex.ID, ExceptionDecision{
		Approve: true, DecidedBy: "bob", DecidedByRole: "admin",
	})
	if err == nil {
		t.Fatalf("expected error: decision reason required")
	}
}

func TestExceptionMultiPartyApproval(t *testing.T) {
	s := newExceptionService(t)
	orgID := "org_test"
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	ex, err := s.CreateException(orgID, ExceptionInput{
		Scope: "project", ScopeID: "p1", ScopeName: "Payment",
		Reason: "incident", RequestedBy: "alice", RuleIDs: []string{"r1"},
		JustificationKo: "blocking release", ExpiresAt: future,
		RequiredApproverRoles: []string{"security_admin", "owner"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// First approval — partial, status stays pending
	got, err := s.DecideException(orgID, ex.ID, ExceptionDecision{
		Approve: true, DecidedBy: "sec", DecidedByRole: "security_admin",
		Reason: "ok", PublishNewEpoch: true,
	})
	if err != nil {
		t.Fatalf("first approval: %v", err)
	}
	if got.Status != "pending" {
		t.Fatalf("expected partial status=pending, got %s", got.Status)
	}
	// Duplicate vote from same role is rejected
	_, err = s.DecideException(orgID, ex.ID, ExceptionDecision{
		Approve: true, DecidedBy: "sec2", DecidedByRole: "security_admin",
		Reason: "ok", PublishNewEpoch: true,
	})
	if err == nil {
		t.Fatalf("expected duplicate-role rejection")
	}
	// Second required role approves — exception becomes approved + epoch published
	got, err = s.DecideException(orgID, ex.ID, ExceptionDecision{
		Approve: true, DecidedBy: "boss", DecidedByRole: "owner",
		Reason: "ok", PublishNewEpoch: true,
	})
	if err != nil {
		t.Fatalf("second approval: %v", err)
	}
	if got.Status != "approved" {
		t.Fatalf("expected status=approved, got %s", got.Status)
	}
	if got.PublishedEpochID == "" {
		t.Fatalf("expected published_epoch_id")
	}
}

func TestExceptionDenialShortCircuits(t *testing.T) {
	s := newExceptionService(t)
	orgID := "org_test"
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	ex, err := s.CreateException(orgID, ExceptionInput{
		Scope: "project", ScopeID: "p1", ScopeName: "Payment",
		Reason: "incident", RequestedBy: "alice", RuleIDs: []string{"r1"},
		JustificationKo: "blocking release", ExpiresAt: future,
		RequiredApproverRoles: []string{"security_admin", "owner"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.DecideException(orgID, ex.ID, ExceptionDecision{
		Approve: false, DecidedBy: "sec", DecidedByRole: "security_admin",
		Reason: "too risky",
	})
	if err != nil {
		t.Fatalf("denial: %v", err)
	}
	if got.Status != "denied" {
		t.Fatalf("expected denied, got %s", got.Status)
	}
}

func TestExceptionSweepExpired(t *testing.T) {
	s := newExceptionService(t)
	orgID := "org_test"
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	ex, err := s.CreateException(orgID, ExceptionInput{
		Scope: "project", ScopeID: "p1", ScopeName: "Payment",
		Reason: "will-expire", RequestedBy: "alice", RuleIDs: []string{"r1"},
		JustificationKo: "test", ExpiresAt: future,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.DecideException(orgID, ex.ID, ExceptionDecision{
		Approve: true, DecidedBy: "boss", DecidedByRole: "owner",
		Reason: "ok",
	}); err != nil {
		t.Fatalf("decide: %v", err)
	}
	// Now move ExpiresAt into the past and confirm ListExceptions flips
	// the row's status to "expired" on read.
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	s.db.Model(&models.PolicyException{}).Where("id = ?", ex.ID).Update("expires_at", past)
	list, err := s.ListExceptions(orgID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Status != "expired" {
		t.Fatalf("expected sweep to flip status=expired, got %+v", list)
	}
}

func TestExceptionRankedQueue(t *testing.T) {
	s := newExceptionService(t)
	orgID := "org_test"
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	low, _ := s.CreateException(orgID, ExceptionInput{
		Scope: "project", ScopeID: "p1", ScopeName: "P",
		Reason: "low", RequestedBy: "alice", RuleIDs: []string{"r1"},
		JustificationKo: "j", ExpiresAt: future, SeverityLabel: "low",
	})
	high, _ := s.CreateException(orgID, ExceptionInput{
		Scope: "project", ScopeID: "p2", ScopeName: "P",
		Reason: "high", RequestedBy: "alice", RuleIDs: []string{"r2"},
		JustificationKo: "j", ExpiresAt: future, SeverityLabel: "high",
	})
	ranked, err := s.ListExceptionsRanked(orgID)
	if err != nil {
		t.Fatalf("ranked: %v", err)
	}
	if len(ranked) != 2 || ranked[0].ID != high.ID || ranked[1].ID != low.ID {
		t.Fatalf("ranked order wrong: high(%s) low(%s); got %s, %s", high.ID, low.ID, ranked[0].ID, ranked[1].ID)
	}
}