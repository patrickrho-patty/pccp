package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// TestSIEMCursorDeliversAllEvents (regression): the forwarder cursor must
// advance over the monotonic ChainSeq, not the random UUID id. With UUID
// ordering, events sorting before the cursor were silently skipped. This
// test forwards a mixed-ID batch and requires every event to arrive.
func TestSIEMCursorDeliversAllEvents(t *testing.T) {
	srv, db := pages1619TestServer(t)
	org := models.Organization{Name: "o", Slug: "osiem", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}

	// Seed 30 events with monotonic chain_seq (as the emitter does).
	for i := 1; i <= 30; i++ {
		ev := models.AuditEvent{
			Base:           models.Base{ID: models.GenerateID("ae")},
			OrganizationID: org.ID, EventType: "cp.test.event", Action: "test",
			ChainSeq: int64(i),
		}
		if err := db.Create(&ev).Error; err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	var received int
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Events []models.AuditEvent `json:"events"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		received += len(payload.Events)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer sink.Close()

	db.Create(&models.OrgSetting{
		Base: models.Base{ID: models.GenerateID("os")}, OrganizationID: org.ID,
		Key: "audit.siem_webhook", Value: sink.URL,
	})

	total := 0
	for i := 0; i < 10; i++ { // batches of 100 > 30 → first call suffices; loop proves idempotent cursor
		total += srv.forwardAuditToSIEM(org.ID)
	}
	mu.Lock()
	got := received
	mu.Unlock()
	if total != 30 || got != 30 {
		t.Fatalf("SIEM delivered %d (batches %d), want all 30 — cursor skips events", got, total)
	}
}
