package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sandbox"
)

// Sandbox detail + recovery handlers (PAT-1513). Registered additively in
// services.go so server.go's sandbox block stays untouched.

// handleGetSandboxDetail returns the full vertical for the sandbox detail
// page: the durable record, owning session/user/harness, timestamps
// derived from lifecycle audit events, snapshot history, audit evidence,
// and the actions the lifecycle state machine admits right now.
func (s *Server) handleGetSandboxDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)

	var rec models.SandboxRecord
	// Org scoping is the tenant boundary: a sandbox outside the caller's
	// org is indistinguishable from a missing one.
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, id).First(&rec).Error; err != nil {
		writeError(w, http.StatusNotFound, "샌드박스를 찾을 수 없습니다")
		return
	}

	result := map[string]interface{}{
		"sandbox": sandbox.Sandbox{
			DBID: rec.ID, ID: rec.ID,
			OrganizationID: rec.OrganizationID, SessionID: rec.SessionID, RepositoryID: rec.RepositoryID, UserID: rec.UserID,
			Mode: sandbox.RuntimeMode(rec.Mode), BaseImage: rec.BaseImage, ImageDigest: rec.ImageDigest,
			CPULimit: rec.CPULimit, MemoryLimitMB: rec.MemoryLimitMB, NetworkPolicy: rec.NetworkPolicy,
			Status: rec.Status, RuntimeProvider: rec.RuntimeProvider, ResourceLimits: rec.ResourceLimitsJSON,
		},
		"created_at":    rec.CreatedAt.Format(time.RFC3339),
		"updated_at":    rec.UpdatedAt.Format(time.RFC3339),
		"valid_actions": sandbox.ValidActions(rec.Status),
	}

	if rec.SessionID != "" {
		var session models.Session
		if s.db.Where("organization_id = ? AND session_id = ?", orgID, rec.SessionID).First(&session).Error == nil {
			result["session"] = session
		}
	}
	if rec.UserID != "" {
		var user models.User
		if s.db.Where("organization_id = ? AND id = ?", orgID, rec.UserID).First(&user).Error == nil {
			result["user"] = user
		}
	}

	// Lifecycle evidence: every create/running/defined/destroy/snapshot
	// transition is an audit event on this resource.
	var auditEvents []models.AuditEvent
	s.db.Where("organization_id = ? AND resource_type = ? AND resource_id = ?", orgID, "sandbox", id).
		Order("occurred_at DESC").Limit(50).Find(&auditEvents)
	result["audit_events"] = auditEvents

	// Snapshot history and lifecycle timestamps ride on those events — the
	// durable record only carries current status, so started/destroyed
	// times and snapshot IDs are recovered from the audit trail.
	snapshots := []map[string]string{}
	for _, ev := range auditEvents {
		switch ev.Action {
		case "forensic_snapshot":
			entry := map[string]string{"occurred_at": ev.OccurredAt}
			var details struct {
				SnapshotID string `json:"snapshot_id"`
			}
			if json.Unmarshal([]byte(ev.Details), &details) == nil {
				entry["snapshot_id"] = details.SnapshotID
			}
			snapshots = append(snapshots, entry)
		case "sandbox_running":
			if result["started_at"] == nil {
				result["started_at"] = ev.OccurredAt
			}
		case "sandbox_destroy":
			if result["destroyed_at"] == nil {
				result["destroyed_at"] = ev.OccurredAt
				var details struct {
					DestroyEvidence string `json:"destroy_evidence"`
				}
				if json.Unmarshal([]byte(ev.Details), &details) == nil {
					result["destroy_evidence"] = details.DestroyEvidence
				}
			}
		}
	}
	result["snapshots"] = snapshots

	writeJSON(w, http.StatusOK, result)
}

// handleRetrySandbox re-attempts provisioning for a defined/failed
// sandbox — the explicit recovery path for runtime-disconnected states.
func (s *Server) handleRetrySandbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)

	var rec models.SandboxRecord
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, id).First(&rec).Error; err != nil {
		writeError(w, http.StatusNotFound, "샌드박스를 찾을 수 없습니다")
		return
	}

	sb, err := s.sandbox.RetryProvision(id)
	if err != nil {
		var inv *sandbox.InvalidTransitionError
		if errors.As(err, &inv) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.sandbox.provision_retried",
		ActorType:      "admin",
		Action:         "retry_sandbox_provision",
		ResourceType:   "sandbox",
		ResourceID:     id,
		Details:        "provision retry requested; outcome status=" + sb.Status,
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, sb)
}
