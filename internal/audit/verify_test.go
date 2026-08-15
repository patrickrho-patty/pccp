package audit

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	// busy_timeout + WAL: concurrent writers wait instead of failing with
	// SQLITE_BUSY. Without this, a failed insert burns an allocator
	// sequence and the retry (a new event) leaves a gap that breaks the
	// chain — the suite's historical flake.
	g, err := gorm.Open(sqlite.Open(t.TempDir()+"/a.db?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	g.AutoMigrate(&models.AuditEvent{})
	return g
}

func TestChainVerifiesAndDetectsTampering(t *testing.T) {
	db := testDB(t)
	// Unique orgs per run: the chain allocator caches per-org high-water
	// state process-wide; fixed names would leak previous runs' chains
	// into a fresh DB under -count=N.
	o1 := fmt.Sprintf("o1-%d", time.Now().UnixNano())
	o2 := fmt.Sprintf("o2-%d", time.Now().UnixNano())
	for i := 0; i < 5; i++ {
		if err := db.Create(&models.AuditEvent{OrganizationID: o1, EventType: "test", Action: "a"}).Error; err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	rep, err := VerifyChain(db, o1)
	if err != nil || !rep.Verified || rep.Events != 5 {
		t.Fatalf("clean chain must verify: %+v err=%v", rep, err)
	}

	// Tamper with a row's content after write.
	var victim models.AuditEvent
	if err := db.Where("organization_id = ?", o1).Order("chain_seq ASC").First(&victim).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.AuditEvent{}).Where("id = ?", victim.ID).Update("details", "rewritten").Error; err != nil {
		t.Fatal(err)
	}
	rep, _ = VerifyChain(db, o1)
	if rep.Verified {
		t.Fatal("tampered content must break the chain")
	}
	if rep.FirstBreakID != victim.ID {
		t.Fatalf("break must name the tampered row %s, got %s", victim.ID, rep.FirstBreakID)
	}

	// Deleted (removed) row breaks linkage.
	db2 := testDB(t)
	for i := 0; i < 3; i++ {
		db2.Create(&models.AuditEvent{OrganizationID: o2, EventType: "t", Action: "a"})
	}
	var first models.AuditEvent
	db2.Where("organization_id = ?", o2).Order("chain_seq ASC").First(&first)
	db2.Delete(&first)
	rep, _ = VerifyChain(db2, o2)
	if rep.Verified {
		t.Fatal("deleted row must break the chain")
	}
}

// TestChainAllocatesSequentiallyUnderConcurrency guards the ChainSeq
// allocation race: concurrent audit creates must produce a contiguous,
// correctly linked chain that verifies.
func TestChainAllocatesSequentiallyUnderConcurrency(t *testing.T) {
	db := testDB(t)
	// Unique org per run: the chain allocator caches per-org high-water
	// state process-wide, so a reused org name would leak the previous
	// run's digest chain into a fresh DB (the -count=N flake).
	org := fmt.Sprintf("race-%d", time.Now().UnixNano())
	var wg sync.WaitGroup
	// 8-way concurrency: enough to exercise the allocator race, gentle
	// enough for sqlite under full-suite load.
	sem := make(chan struct{}, 8)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				if err := db.Create(&models.AuditEvent{OrganizationID: org, EventType: "t", Action: "a"}).Error; err == nil {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		}()
	}
	wg.Wait()
	rep, err := VerifyChain(db, org)
	if err != nil || !rep.Verified {
		t.Fatalf("concurrent chain must verify: %+v err=%v", rep, err)
	}
	if rep.Events != 32 {
		t.Fatalf("events = %d, want 32", rep.Events)
	}
}
