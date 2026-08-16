package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/compliance"
)

// compliance_extra.go: web/08 — certification meta (B1), evidence vault
// (C1), remediation tracking (C2), assessment history + continuous
// re-assessment (C3), audit-ready export (C5).

// handleComplianceMeta returns the certification catalog with level and
// scope options (B1) so the admin can pick a real target.
func (s *Server) handleComplianceMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, compliance.CertMetaList())
}

// handleComplianceAssessWithTarget assesses a specific scope/level
// target and persists the snapshot.
func (s *Server) handleComplianceAssessWithTarget(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		Certification string `json:"certification"`
		Scope         string `json:"scope"`
		Level         string `json:"level"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	assessment, err := s.ext().Compliance.AssessWithTarget(orgID,
		compliance.CertificationType(req.Certification), req.Scope, req.Level)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, assessment)
}

// handleComplianceHistory lists the persisted assessment snapshots (C3).
func (s *Server) handleComplianceHistory(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	records, err := s.ext().Compliance.RecentAssessments(orgID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, records)
}

// handleComplianceEvidence CRUDs evidence items (C1).
func (s *Server) handleComplianceEvidence(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	cert := r.URL.Query().Get("certification")
	switch r.Method {
	case http.MethodGet:
		controlID := r.URL.Query().Get("control")
		items, err := s.ext().Compliance.ListEvidence(orgID, cert, controlID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req struct {
			Certification string `json:"certification"`
			ControlID     string `json:"control_id"`
			Title         string `json:"title"`
			Description   string `json:"description"`
			Source        string `json:"source"`
			Reference     string `json:"reference"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Certification == "" || req.ControlID == "" {
			writeError(w, http.StatusBadRequest, "certification + control_id required")
			return
		}
		if req.Source == "" {
			req.Source = "manual"
		}
		item, err := s.ext().Compliance.AddEvidence(orgID, req.Certification, req.ControlID,
			req.Title, req.Description, req.Source, req.Reference)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, item)
	case http.MethodDelete:
		id := chi.URLParam(r, "id")
		if err := s.ext().Compliance.DeleteEvidence(orgID, id); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleComplianceRemediations CRUDs gap tasks (C2) + bulk conversion.
func (s *Server) handleComplianceRemediations(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	switch r.Method {
	case http.MethodGet:
		cert := r.URL.Query().Get("certification")
		status := r.URL.Query().Get("status")
		tasks, err := s.ext().Compliance.ListRemediations(orgID, cert, status)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, tasks)
	case http.MethodPost:
		var req struct {
			Certification string `json:"certification"`
			ControlID     string `json:"control_id"`
			Owner         string `json:"owner"`
			DueDate       string `json:"due_date"`
			SLA           string `json:"sla"`
			Notes         string `json:"notes"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Certification == "" || req.ControlID == "" {
			writeError(w, http.StatusBadRequest, "certification + control_id required")
			return
		}
		task, err := s.ext().Compliance.AddRemediation(orgID, req.Certification, req.ControlID,
			req.Owner, req.DueDate, req.SLA, req.Notes)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, task)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleComplianceRemediationItem updates one gap task.
func (s *Server) handleComplianceRemediationItem(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	var req struct {
		Status  string `json:"status"`
		Owner   string `json:"owner"`
		DueDate string `json:"due_date"`
		Notes   string `json:"notes"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	task, err := s.ext().Compliance.UpdateRemediation(orgID, id, req.Status, req.Owner, req.DueDate, req.Notes)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// handleComplianceBulkRemediate converts all open gaps into tasks.
func (s *Server) handleComplianceBulkRemediate(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		Certification string `json:"certification"`
		Owner         string `json:"owner"`
		SLA           string `json:"sla"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Certification == "" {
		writeError(w, http.StatusBadRequest, "certification required")
		return
	}
	created, err := s.ext().Compliance.BulkRemediate(orgID, req.Certification, req.Owner, req.SLA)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"created": created})
}

// handleComplianceExport produces the audit-ready matrix (C5). JSON by
// default; ?format=csv returns a CSV matrix (control, status, evidence,
// remediation state).
func (s *Server) handleComplianceExport(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	cert := r.URL.Query().Get("certification")
	if cert == "" {
		writeError(w, http.StatusBadRequest, "certification required")
		return
	}
	assessment, err := s.ext().Compliance.AssessWithTarget(orgID, compliance.CertificationType(cert), "", "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	evidence, _ := s.ext().Compliance.ListEvidence(orgID, cert, "")
	remediations, _ := s.ext().Compliance.ListRemediations(orgID, cert, "")
	remedByControl := map[string]string{}
	for _, t := range remediations {
		remedByControl[t.ControlID] = t.Status
	}
	evByControl := map[string]int{}
	for _, e := range evidence {
		evByControl[e.ControlID]++
	}

	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-compliance-matrix.csv", cert))
		cw := csv.NewWriter(w)
		cw.Write([]string{"control_id", "control_name_ko", "status", "evidence_count", "remediation", "assessed_at"})
		for _, result := range assessment.ControlResults {
			cw.Write([]string{
				result.ControlID, result.GapDescKo, result.Status,
				strconv.Itoa(evByControl[result.ControlID]),
				remedByControl[result.ControlID],
				assessment.AssessedAt,
			})
		}
		cw.Flush()
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"certification":   cert,
		"assessed_at":     assessment.AssessedAt,
		"overall":         assessment.OverallStatus,
		"open_gaps":       assessment.OpenGaps,
		"self_assessment": true, // honest disclaimer — certification is the customer's process
		"controls":        assessment.ControlResults,
		"evidence":        evidence,
		"remediations":    remediations,
		"generated_at":    time.Now().Format(time.RFC3339),
		"signature":       "", // export integrity: signed by the audit chain on request
	})
}

var _ = json.Marshal
