package identity

import (
	"errors"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func hierarchyDB(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/h.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Organization{}, &models.ServiceSigningKey{}); err != nil {
		t.Fatal(err)
	}
	svc, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func org(t *testing.T, db *gorm.DB, name string, parent string) string {
	t.Helper()
	o := &models.Organization{Name: name, Slug: name, ParentOrgID: parent, Status: "active"}
	if err := db.Create(o).Error; err != nil {
		t.Fatal(err)
	}
	return o.ID
}

// TestDelegatedAdminHierarchy covers Task 16: ancestor admins manage
// descendants; descendants never administer ancestors/siblings;
// cycles fail closed.
func TestDelegatedAdminHierarchy(t *testing.T) {
	svc := hierarchyDB(t)
	db := svc.DB()

	root := org(t, db, "root", "")
	child := org(t, db, "child", root)
	grand := org(t, db, "grand", child)
	sibling := org(t, db, "sibling", root)

	// Ancestor authority: root → child → grand.
	if ok, err := svc.IsAncestor(root, root); err != nil || !ok {
		t.Fatalf("self: %v %v", ok, err)
	}
	if ok, err := svc.IsAncestor(root, grand); err != nil || !ok {
		t.Fatalf("root→grand: %v %v", ok, err)
	}
	// Descendant may NOT administer the ancestor.
	if ok, err := svc.IsAncestor(grand, root); err != nil || ok {
		t.Fatalf("grand→root must be denied: %v %v", ok, err)
	}
	// Siblings are separate authorities.
	if ok, _ := svc.IsAncestor(child, sibling); ok {
		t.Fatal("sibling administration allowed")
	}
	// AuthorizeAdmin wraps the check with an action context.
	if err := svc.AuthorizeAdmin(root, grand, "manage_users"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AuthorizeAdmin(grand, root, "manage_users"); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("expected not-authorized, got %v", err)
	}
	// Descendants enumeration includes the root.
	ds, err := svc.Descendants(root)
	if err != nil || len(ds) != 4 {
		t.Fatalf("descendants = %v err=%v", ds, err)
	}
	// Cycle fails closed.
	if err := db.Model(&models.Organization{}).Where("id = ?", root).
		Update("parent_org_id", grand).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IsAncestor(root, grand); !errors.Is(err, ErrHierarchyCycle) {
		t.Fatalf("expected cycle error, got %v", err)
	}
}
