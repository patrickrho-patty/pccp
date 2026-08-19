package gitscm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// browser_test.go covers the sync-state reconciliation added in PAT-1493:
// attempt/head/error persistence, the in-progress idempotence guard, and
// clone-cache restoration after a simulated server restart.

func browserTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/g.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Repository{}); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}

func TestSyncRepositoryPersistsAttemptHeadAndClearsError(t *testing.T) {
	svc, db := browserTestService(t)
	repo := models.Repository{
		Name:          "r",
		CloneURL:      makeRepo(t),
		DefaultBranch: "main",
		SyncStatus:    "failed",
		LastSyncError: "previous failure",
	}
	db.Create(&repo)

	head, err := svc.SyncRepository(context.Background(), &repo)
	if err != nil {
		t.Fatal(err)
	}
	if head == "" {
		t.Fatal("expected head SHA")
	}
	var got models.Repository
	db.First(&got, "id = ?", repo.ID)
	if got.SyncStatus != "synced" {
		t.Fatalf("sync_status = %q", got.SyncStatus)
	}
	if got.LastSyncAt == "" || got.LastSyncAttemptAt == "" {
		t.Fatalf("timestamps not persisted: %+v", got)
	}
	if got.LastSyncHead != head {
		t.Fatalf("last_sync_head = %q, want %q", got.LastSyncHead, head)
	}
	if got.LastSyncError != "" {
		t.Fatalf("last_sync_error not cleared: %q", got.LastSyncError)
	}
	if got.LastCommitAt == "" {
		t.Fatal("last_commit_at not persisted")
	}
}

func TestSyncRepositoryFailureRecordsAttemptAndError(t *testing.T) {
	svc, db := browserTestService(t)
	repo := models.Repository{
		Name:          "r",
		CloneURL:      t.TempDir() + "/does-not-exist",
		DefaultBranch: "main",
	}
	db.Create(&repo)

	if _, err := svc.SyncRepository(context.Background(), &repo); err == nil {
		t.Fatal("expected clone failure")
	}
	var got models.Repository
	db.First(&got, "id = ?", repo.ID)
	if got.SyncStatus != "failed" {
		t.Fatalf("sync_status = %q", got.SyncStatus)
	}
	if got.LastSyncAttemptAt == "" {
		t.Fatal("last_sync_attempt_at not persisted on failure")
	}
	if !strings.Contains(got.LastSyncError, "gitscm: clone") {
		t.Fatalf("last_sync_error = %q", got.LastSyncError)
	}
}

func TestSyncRepositoryRejectsConcurrentSync(t *testing.T) {
	svc, db := browserTestService(t)
	repo := models.Repository{
		Name:          "r",
		CloneURL:      makeRepo(t),
		DefaultBranch: "main",
		SyncStatus:    "syncing",
		// A live attempt: the atomic claim must refuse a second sync.
		LastSyncAttemptAt: time.Now().Format(time.RFC3339),
	}
	db.Create(&repo)

	_, err := svc.SyncRepository(context.Background(), &repo)
	if err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("err = %v", err)
	}
	var got models.Repository
	db.First(&got, "id = ?", repo.ID)
	if got.SyncStatus != "syncing" {
		t.Fatalf("sync_status mutated to %q", got.SyncStatus)
	}
}

func TestSyncRepositoryReclaimsStaleSyncing(t *testing.T) {
	svc, db := browserTestService(t)
	cases := map[string]models.Repository{
		// Crash mid-clone: attempt recorded long ago, never resolved.
		"old-attempt": {
			Name:              "r-old",
			CloneURL:          makeRepo(t),
			DefaultBranch:     "main",
			SyncStatus:        "syncing",
			LastSyncAttemptAt: time.Now().Add(-time.Hour).Format(time.RFC3339),
		},
		// Legacy row: syncing with no recorded attempt at all.
		"no-attempt": {
			Name:          "r-none",
			CloneURL:      makeRepo(t),
			DefaultBranch: "main",
			SyncStatus:    "syncing",
		},
	}
	for name, repo := range cases {
		t.Run(name, func(t *testing.T) {
			db.Create(&repo)
			head, err := svc.SyncRepository(context.Background(), &repo)
			if err != nil {
				t.Fatalf("stale syncing row must be reclaimable: %v", err)
			}
			if head == "" {
				t.Fatal("expected head SHA")
			}
			var got models.Repository
			db.First(&got, "id = ?", repo.ID)
			if got.SyncStatus != "synced" {
				t.Fatalf("sync_status = %q", got.SyncStatus)
			}
		})
	}
}

func TestSyncHeartbeatKeepsAttemptFresh(t *testing.T) {
	svc, db := browserTestService(t)
	// A running sync whose claim is 10 minutes old: inside the stale
	// window, so without the heartbeat a second sync could hijack it.
	repo := models.Repository{
		Name:              "r",
		CloneURL:          makeRepo(t),
		DefaultBranch:     "main",
		SyncStatus:        "syncing",
		LastSyncAttemptAt: time.Now().Add(-10 * time.Minute).Format(time.RFC3339),
	}
	db.Create(&repo)

	done := make(chan struct{})
	go svc.heartbeatSyncAttempt(repo.ID, 20*time.Millisecond, done)
	defer close(done)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var got models.Repository
		db.First(&got, "id = ?", repo.ID)
		if got.LastSyncAttemptAt > repo.LastSyncAttemptAt {
			return // heartbeat refreshed the attempt timestamp
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("heartbeat did not refresh last_sync_attempt_at")
}

func TestListTreeRestoresCloneAfterCacheLoss(t *testing.T) {
	svc, db := browserTestService(t)
	repo := models.Repository{
		Name:          "r",
		CloneURL:      makeRepo(t),
		DefaultBranch: "main",
	}
	db.Create(&repo)
	if _, err := svc.SyncRepository(context.Background(), &repo); err != nil {
		t.Fatal(err)
	}
	// Simulate a server restart: the in-memory cache is empty but the
	// clone still exists under the OS temp dir.
	clones.mu.Lock()
	delete(clones.dirs, repo.ID)
	clones.mu.Unlock()

	entries, err := svc.ListTree(repo.ID, "")
	if err != nil {
		t.Fatalf("ListTree after cache loss: %v", err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	if !names["a.txt"] || !names["b.txt"] {
		t.Fatalf("entries = %v", names)
	}
	content, err := svc.ReadFile(repo.ID, "a.txt")
	if err != nil {
		t.Fatalf("ReadFile after cache loss: %v", err)
	}
	if strings.TrimSpace(string(content)) != "one" {
		t.Fatalf("content = %q", content)
	}
}

func TestListTreeNeverSyncedStillErrs(t *testing.T) {
	svc, _ := browserTestService(t)
	if _, err := svc.ListTree("repo-never-synced", ""); err != ErrNotSynced {
		t.Fatalf("err = %v", err)
	}
}
