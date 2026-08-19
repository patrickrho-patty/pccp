package api

// Governed broadcast audience resolution and send (PAT-1510): an admin
// broadcast requires an explicit audience scope, freezes the resolved
// audience as a snapshot for reproducible reporting, and enforces
// confirmation policy for critical/emergency and empty audiences.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// audienceExclusion is a recipient ruled out of a broadcast with the
// reason (suspended/offboarded accounts never receive broadcasts).
type audienceExclusion struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	NameKo string `json:"name_ko"`
	Email  string `json:"email"`
	Reason string `json:"reason"` // suspended, offboarded
}

// broadcastAudience is the resolved recipient set for a scope.
type broadcastAudience struct {
	Eligible []models.User
	Excluded []audienceExclusion
}

// broadcastAudienceSnapshot is frozen onto the broadcast at send time.
type broadcastAudienceSnapshot struct {
	EligibleIDs []string            `json:"eligible_ids"`
	Excluded    []audienceExclusion `json:"excluded,omitempty"`
	ResolvedAt  string              `json:"resolved_at"`
}

// resolveBroadcastAudience maps a target scope to concrete org users.
// Suspended/offboarded users are split out as exclusions, and duplicate
// IDs (e.g. overlapping rosters) collapse to one recipient.
func resolveBroadcastAudience(db *gorm.DB, orgID, targetType, targetID string) (*broadcastAudience, error) {
	var users []models.User
	switch targetType {
	case "all", "org":
		db.Where("organization_id = ?", orgID).Find(&users)
	case "project":
		var memberIDs []string
		db.Model(&models.ProjectMember{}).
			Where("organization_id = ? AND project_id = ?", orgID, targetID).
			Pluck("user_id", &memberIDs)
		if len(memberIDs) > 0 {
			db.Where("organization_id = ? AND id IN ?", orgID, memberIDs).Find(&users)
		}
	case "user":
		if targetID != "" {
			db.Where("organization_id = ? AND id = ?", orgID, targetID).Find(&users)
		}
	default:
		return nil, fmt.Errorf("unsupported target_type: %s", targetType)
	}
	aud := &broadcastAudience{}
	seen := map[string]bool{}
	for _, u := range users {
		if seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		if u.Status == "suspended" || u.Status == "offboarded" {
			aud.Excluded = append(aud.Excluded, audienceExclusion{
				UserID: u.ID, Name: u.Name, NameKo: u.NameKo, Email: u.Email, Reason: u.Status,
			})
			continue
		}
		aud.Eligible = append(aud.Eligible, u)
	}
	return aud, nil
}

// handleSendBroadcastGoverned sends a broadcast only against an explicit,
// authorized audience scope. The resolved audience is frozen onto the
// record, sends are idempotent per client_token, and critical/emergency
// severities require a confirmation reason that lands in the audit log.
func (s *Server) handleSendBroadcastGoverned(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Severity      string `json:"severity"`
		Title         string `json:"title"`
		TitleKo       string `json:"title_ko"`
		Body          string `json:"body"`
		BodyKo        string `json:"body_ko"`
		TargetType    string `json:"target_type"` // required: org, project, user
		TargetID      string `json:"target_id"`
		RequiresAck   bool   `json:"requires_ack"`
		ExpiresAt     string `json:"expires_at"`
		ConfirmReason string `json:"confirm_reason"`
		AllowEmpty    bool   `json:"allow_empty"`
		ClientToken   string `json:"client_token"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title required")
		return
	}
	if req.TargetType == "" {
		writeError(w, http.StatusBadRequest, "explicit target_type required")
		return
	}
	if req.TargetType != "org" && req.TargetType != "all" && req.TargetID == "" {
		writeError(w, http.StatusBadRequest, "target_id required for target_type "+req.TargetType)
		return
	}
	if req.Severity == "" {
		req.Severity = "info"
	}
	if (req.Severity == "critical" || req.Severity == "emergency") && req.ConfirmReason == "" {
		writeError(w, http.StatusBadRequest, "confirm_reason required for critical/emergency broadcasts")
		return
	}
	if req.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, req.ExpiresAt); err != nil {
			writeError(w, http.StatusBadRequest, "expires_at must be RFC3339")
			return
		}
	}
	orgID := getOrgID(r)
	// Idempotency: a retried send with the same client token returns the
	// already-created broadcast instead of duplicating it.
	if req.ClientToken != "" {
		var existing models.Broadcast
		if err := s.db.Where("organization_id = ? AND client_token = ?", orgID, req.ClientToken).
			First(&existing).Error; err == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"broadcast": existing, "duplicate": true})
			return
		}
	}
	aud, err := resolveBroadcastAudience(s.db, orgID, req.TargetType, req.TargetID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(aud.Eligible) == 0 && !req.AllowEmpty {
		writeError(w, http.StatusBadRequest, "audience is empty — pass allow_empty to confirm sending to zero recipients")
		return
	}
	eligibleIDs := make([]string, 0, len(aud.Eligible))
	for _, u := range aud.Eligible {
		eligibleIDs = append(eligibleIDs, u.ID)
	}
	snapshot, _ := json.Marshal(broadcastAudienceSnapshot{
		EligibleIDs: eligibleIDs,
		Excluded:    aud.Excluded,
		ResolvedAt:  time.Now().Format(time.RFC3339),
	})
	bc := models.Broadcast{
		Severity:     req.Severity,
		Title:        req.Title,
		TitleKo:      req.TitleKo,
		Body:         req.Body,
		BodyKo:       req.BodyKo,
		TargetType:   req.TargetType,
		TargetID:     req.TargetID,
		RequiresAck:  req.RequiresAck,
		Dismissable:  req.Severity != "emergency",
		ExpiresAt:    req.ExpiresAt,
		Status:       "active",
		SentBy:       getOperatorEmail(r),
		AudienceJSON: string(snapshot),
		ClientToken:  req.ClientToken,
	}
	bc.OrganizationID = orgID
	if err := s.db.Create(&bc).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Persist first, then attempt live delivery; the audit event records
	// the frozen audience size, the confirmation reason, and how many
	// live sessions actually received it.
	delivered := deliverBroadcastToRelay(orgID, req.Severity, req.Body)
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.comms.broadcast_sent",
		ActorType:      "admin",
		Action:         "send_broadcast",
		ResourceType:   "broadcast",
		ResourceID:     bc.ID,
		Details: fmt.Sprintf("severity: %s, title: %s, target: %s/%s, eligible: %d, excluded: %d, live deliveries: %d, reason: %s",
			req.Severity, req.Title, req.TargetType, req.TargetID, len(aud.Eligible), len(aud.Excluded), delivered, req.ConfirmReason),
		Result:     "success",
		OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"broadcast": bc,
		"audience": map[string]interface{}{
			"eligible": len(aud.Eligible),
			"excluded": len(aud.Excluded),
		},
	})
}
