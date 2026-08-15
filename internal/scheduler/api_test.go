package scheduler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func apiTestScheduler(t *testing.T) *Scheduler {
	t.Helper()
	fx := newWorkerFixture(t)
	s := NewScheduler(fx.trust, nil, 30*time.Second, 60*time.Second, testEvidenceKey(t))
	now := time.Now()
	s.Registry.Register(fx.card, fx.subjectPub, now)
	return s
}

func TestHealthzHandler(t *testing.T) {
	handler := NewHTTPHandler(apiTestScheduler(t), "")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status %d", rec.Code)
	}
}

func TestWorkersHandlerRequiresToken(t *testing.T) {
	handler := NewHTTPHandler(apiTestScheduler(t), "secret-token")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
}

func TestWorkersHandlerListsWorkers(t *testing.T) {
	handler := NewHTTPHandler(apiTestScheduler(t), "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var workers []WorkerEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &workers); err != nil {
		t.Fatalf("decode workers: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("workers %d, want 1", len(workers))
	}
	if workers[0].Card.WorkerID != "wkr-test-001" {
		t.Fatalf("worker %q", workers[0].Card.WorkerID)
	}
}

func TestWorkerDetailHandlerIncludesEvidence(t *testing.T) {
	s := apiTestScheduler(t)
	s.Evidence.Emit(EventWorkerQuarantine, "wkr-test-001", "below reachability")
	handler := NewHTTPHandler(s, "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/wkr-test-001", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	var detail struct {
		Entry  WorkerEntry     `json:"entry"`
		Events []EvidenceEvent `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Entry.Card.WorkerID != "wkr-test-001" {
		t.Fatalf("entry worker %q", detail.Entry.Card.WorkerID)
	}
	if len(detail.Events) != 1 || detail.Events[0].EventType != EventWorkerQuarantine {
		t.Fatalf("events %+v", detail.Events)
	}
}

func TestWorkerDetailHandlerUnknownWorker(t *testing.T) {
	handler := NewHTTPHandler(apiTestScheduler(t), "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/unknown", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestSweepEmitsEvictEvidence(t *testing.T) {
	fx := newWorkerFixture(t)
	s := NewScheduler(fx.trust, nil, 30*time.Second, 60*time.Second, testEvidenceKey(t))
	now := time.Now()
	s.Registry.Register(fx.card, fx.subjectPub, now)

	evicted := s.Sweep(now.Add(100 * time.Second))
	if len(evicted) != 1 {
		t.Fatalf("evicted %v, want 1", evicted)
	}
	events := s.Evidence.Events()
	if len(events) != 1 || events[0].EventType != EventWorkerEvict {
		t.Fatalf("events %+v, want single worker.evict", events)
	}
}
