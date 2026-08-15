package policy

import (
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"testing"
)

func TestPolicyDigestIsContentAddressed(t *testing.T) {
	g, err := gorm.Open(sqlite.Open(t.TempDir()+"/t.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	g.AutoMigrate(&models.PolicyRule{})
	s := &Service{db: g}
	d1 := s.computePolicyDigest("org-1", "models")
	d2 := s.computePolicyDigest("org-1", "models")
	if d1 != d2 {
		t.Fatalf("digest must be stable for identical rule sets: %s vs %s", d1, d2)
	}
	g.Create(&models.PolicyRule{OrganizationID: "org-1", Domain: "models", Name: "r1", Enabled: true, ConfigJSON: `{"a":1}`})
	d3 := s.computePolicyDigest("org-1", "models")
	if d3 == d1 {
		t.Fatal("digest must change when a rule is added")
	}
	// disabled rules do not count
	g.Model(&models.PolicyRule{}).Where("name = ?", "r1").Update("enabled", false)
	if s.computePolicyDigest("org-1", "models") != d1 {
		t.Fatal("disabling the only rule must return the empty-set digest")
	}
}
