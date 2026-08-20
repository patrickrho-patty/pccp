package tools

import (
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// PAT-1403: the seeded governed tool catalog must include the HWP/HWPX
// internal document tools with read-only/low classification so allowlist,
// approval, and audit govern them like any other read tool.
func TestSeedIncludesDocumentTools(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/tools-seed.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Tool{}); err != nil {
		t.Fatal(err)
	}
	svc := New(db)
	if err := svc.SeedDefaultTools("org-1403"); err != nil {
		t.Fatal(err)
	}
	tools, err := svc.ListTools("org-1403")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]models.Tool{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}
	for _, name := range []string{"doc.read_hwp", "doc.read_hwpx"} {
		tl, ok := byName[name]
		if !ok {
			t.Fatalf("%s missing from seeded catalog", name)
		}
		// Document tools are read-only, low-danger, document-class governed
		// tools. (The Tool model's RequiresApproval gorm default:true applies
		// uniformly to every seeded tool, including pre-existing read tools,
		// so we assert the classification contract that is distinct to them.)
		if tl.Category != "read" || tl.ToolClass != "document" || tl.DangerLevel != "low" {
			t.Fatalf("%s misclassified: %+v", name, tl)
		}
		if tl.NameKo == "" {
			t.Fatalf("%s missing Korean label", name)
		}
	}
}
