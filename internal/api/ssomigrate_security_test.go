package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSSOMigrationLinkListingRequiresGovernanceAdminAndSurfacesDBFailure(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(t.TempDir()+"/links.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.SSOIdentityLink{}); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database}

	memberReq := httptest.NewRequest(http.MethodGet, "/api/sso-migrate/links", nil)
	memberReq = memberReq.WithContext(ctxWithClaims(memberReq.Context(), &identity.Claims{OrganizationID: "org-1", Role: "member"}))
	memberRec := httptest.NewRecorder()
	server.handleSSOMigrateLinks(memberRec, memberReq)
	if memberRec.Code != http.StatusForbidden {
		t.Fatalf("member link listing = %d, want 403", memberRec.Code)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	adminReq := httptest.NewRequest(http.MethodGet, "/api/sso-migrate/links", nil)
	adminReq = adminReq.WithContext(ctxWithClaims(adminReq.Context(), &identity.Claims{OrganizationID: "org-1", Role: "security_admin"}))
	adminRec := httptest.NewRecorder()
	server.handleSSOMigrateLinks(adminRec, adminReq)
	if adminRec.Code != http.StatusInternalServerError {
		t.Fatalf("failed DB link listing = %d, want 500: %s", adminRec.Code, adminRec.Body.String())
	}
}
