package api

// PAT-1443 GPU queue / inference QoS operations analytics — API adapter.
// Internal SRE/engineering surface only (dedicated least-privilege ops role,
// audited access/export). Returns measured + estimated values with explicit
// confidence and last-updated time; never user/session/request identity.

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

func (s *Server) handleQoSIngest(w http.ResponseWriter, r *http.Request) {
	// Ingest called by scheduler/service producers with an opaque scope; the
	// event never carries user/request identity.
	var ev models.QoSRequestEvent
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if ev.Deployment == "" {
		writeError(w, http.StatusBadRequest, "deployment required")
		return
	}
	s.qosSV.RecordLifecycle(ev)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// handleQoSQueueSnapshot is the current saturation snapshot: active/max,
// queued, oldest age, arrival/dispatch/completion + projected wait range.
func (s *Server) handleQoSQueueSnapshot(w http.ResponseWriter, r *http.Request) {
	deployment := r.URL.Query().Get("deployment")
	if deployment == "" {
		deployment = "public"
	}
	model := r.URL.Query().Get("model")
	region := r.URL.Query().Get("region")
	cls := r.URL.Query().Get("traffic_class")
	since := time.Now().Add(-2 * time.Hour)
	stats, err := s.qosSV.QueueWaitStats(deployment, since, model, region, cls)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	f, ferr := s.qosSV.Forecast(deployment, model, region, "", cls, 10)
	// Active concurrency: count non-terminal (ingress w/o terminal) in window.
	var active int64
	s.db.Model(&models.QoSRequestEvent{}).
		Where("deployment = ? AND occurred_at >= ? AND terminal = ?", deployment, since, false).
		Distinct("trace_key").Count(&active)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"deployment":       deployment,
		"active":           active,
		"max_concurrency":  50, // configured max; producer overrides in production
		"queued":           stats.Arrivals - stats.Completions - stats.Canceled - stats.Expired,
		"queued_tokens":    0,
		"wait_percentiles": map[string]float64{"p50": stats.QueueWaitP50, "p90": stats.QueueWaitP90, "p95": stats.QueueWaitP95, "p99": stats.QueueWaitP99},
		"service_rate":     stats.Completions,
		"measured":         map[string]interface{}{"window_hours": 2, "sample_n": stats.WaitSampleN, "updated_at": time.Now().UTC().Format(time.RFC3339)},
		"forecast":         forecastOrNil(f, ferr),
	})
}

func forecastOrNil(f *models.QoSForecast, err error) interface{} {
	if err != nil || f == nil {
		return nil
	}
	return f
}

// handleQoSOutcomes returns lifecycle outcome counts for the queue-outcome panel.
func (s *Server) handleQoSOutcomes(w http.ResponseWriter, r *http.Request) {
	deployment := r.URL.Query().Get("deployment")
	if deployment == "" {
		deployment = "public"
	}
	since := time.Now().Add(-24 * time.Hour)
	rows := s.countLifecycles(deployment, since)
	writeJSON(w, http.StatusOK, map[string]interface{}{"outcomes": rows, "window_hours": 24})
}

func (s *Server) countLifecycles(deployment string, since time.Time) map[string]int64 {
	out := map[string]int64{}
	var counts []struct {
		Lifecycle string
		Cnt       int64
	}
	s.db.Model(&models.QoSRequestEvent{}).
		Select("lifecycle, count(*) as cnt").
		Where("deployment = ? AND occurred_at >= ?", deployment, since).
		Group("lifecycle").Scan(&counts)
	for _, c := range counts {
		out[c.Lifecycle] = c.Cnt
	}
	return out
}

// handleQoSTimeline returns recent anonymized lifecycle boundaries for a
// filtered drill-down (never any identity field).
func (s *Server) handleQoSTimeline(w http.ResponseWriter, r *http.Request) {
	deployment := r.URL.Query().Get("deployment")
	if deployment == "" {
		deployment = "public"
	}
	q := s.db.Where("deployment = ?", deployment).Order("occurred_at DESC").Limit(100)
	if m := r.URL.Query().Get("model"); m != "" {
		q = q.Where("model = ?", m)
	}
	if rc := r.URL.Query().Get("region"); rc != "" {
		q = q.Where("region = ?", rc)
	}
	if lc := r.URL.Query().Get("lifecycle"); lc != "" {
		q = q.Where("lifecycle = ?", lc)
	}
	var events []models.QoSRequestEvent
	q.Find(&events)
	writeJSON(w, http.StatusOK, events)
}

// handleQoSForecast triggers or reads a forecast for a stratum.
func (s *Server) handleQoSForecast(w http.ResponseWriter, r *http.Request) {
	deployment := r.URL.Query().Get("deployment")
	if deployment == "" {
		deployment = "public"
	}
	f, err := s.qosSV.Forecast(deployment,
		r.URL.Query().Get("model"), r.URL.Query().Get("region"),
		r.URL.Query().Get("worker_pool"), r.URL.Query().Get("traffic_class"), 10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, f)
}
