package events

import (
	"path/filepath"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupDB(t *testing.T) *gorm.DB {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(append(models.AllModels(), &identity.AdminCredentials{})...)
	return db
}

func TestEventEmitAndQuery(t *testing.T) {
	db := setupDB(t)
	svc, err := New(db)
	if err != nil {
		t.Fatal(err)
	}

	// Emit events
	env1, err := svc.Emit(EmitRequest{
		EventType:      TypeSessionLifecycle,
		OrganizationID: "org-1",
		UserID:         "user-1",
		ActorType:      "user",
		SessionID:      "ses-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if env1.EventDigest == "" {
		t.Fatal("expected non-empty digest")
	}
	if env1.EventID == "" {
		t.Fatal("expected non-empty event ID")
	}

	env2, err := svc.Emit(EmitRequest{
		EventType:      TypePromptExchange,
		OrganizationID: "org-1",
		UserID:         "user-1",
		ActorType:      "user",
		SessionID:      "ses-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Chained: env2 should reference env1's digest
	if env2.PrevEventDigest == "" {
		t.Fatal("expected non-empty prev digest (chained)")
	}

	// Query events
	events, err := svc.Query(QueryFilter{
		OrganizationID: "org-1",
		Limit:          10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	// Query by session
	events, _ = svc.Query(QueryFilter{
		OrganizationID: "org-1",
		SessionID:      "ses-1",
	})
	if len(events) != 2 {
		t.Fatalf("expected 2 events for session, got %d", len(events))
	}
}

func TestEventSignature(t *testing.T) {
	db := setupDB(t)
	svc, _ := New(db)
	env, _ := svc.Emit(EmitRequest{
		EventType:      TypeAdminAction,
		OrganizationID: "org-1",
		ActorType:      "admin",
	})
	if env.Signature == "" {
		t.Fatal("expected non-empty signature")
	}
}
