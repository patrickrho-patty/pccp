package scheduler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFleetSnapshot(t *testing.T) {
	obs := NewObservability(nil)
	obs.registry = NewRegistry(2*timeSecond(), 4*timeSecond())
	obs.registry.Register(mkWorkerCard("w1", "model-a"), nil, timeNow())

	snap := obs.FleetSnapshot()
	if snap.Workers != 1 {
		t.Fatalf("workers = %d, want 1", snap.Workers)
	}
}

func TestRoutingExplainability(t *testing.T) {
	obs := NewObservability(nil)
	rs := NewReceiptStore(8)
	rs.Add(RoutingReceipt{
		Decision:    RouteDecision{WorkerID: "w1", Cost: 42.5, OverlapTokens: 300, Reason: "lowest-cost"},
		Model:       "model-a",
		Namespace:   "tenant-a",
		InputTokens: 1000,
	})
	obs.receipts = rs
	expl := obs.ExplainRouting("tenant-a", "model-a")
	if len(expl) != 1 {
		t.Fatalf("explanations = %d, want 1", len(expl))
	}
	if expl[0].WorkerID != "w1" || expl[0].OverlapTokens != 300 {
		t.Fatalf("explanation = %+v", expl[0])
	}
}

func TestObservabilityHTTPViews(t *testing.T) {
	obs := NewObservability(nil)
	mux := obs.AdminViews("")
	for _, path := range []string{
		"/api/v1/fleet", "/api/v1/queue", "/api/v1/cache",
		"/api/v1/perf", "/api/v1/routing", "/api/v1/scaling",
	} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%s content-type = %s", path, w.Header().Get("Content-Type"))
		}
		var v map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatalf("%s body not JSON: %v", path, err)
		}
	}
}

// --- helpers ---

func mkWorkerCard(id, model string) WorkerCard {
	return WorkerCard{WorkerID: id, ModelName: model, CardVersion: 2, DariAddr: "127.0.0.1:1", Status: "active"}
}

func timeSecond() time.Duration { return time.Second }

func timeNow() time.Time { return time.Now() }
