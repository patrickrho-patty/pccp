package relay

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// governance_view.go gathers the org's governance state from the
// control-plane services into the GovernanceStateView the snapshot
// push serializes. This is the production wiring for harness plans
// C3/C4 (tool/MCP approval), D1/D3/D4/D5/D6 (workflow gates), and
// E4 (sandbox baseline): the connector's governed.State is fed from
// THIS view via the GOVERNANCE_STATE push at session setup.

// koreanFreeze mirrors the korean service's ChangeFreeze JSON.
type koreanFreeze struct {
	FreezeReason   string   `json:"freeze_reason"`
	FreezeReasonKo string   `json:"freeze_reason_ko"`
	AffectedRepos  []string `json:"affected_repos"`
	AllowedActions []string `json:"allowed_actions"`
}

// koreanRecall mirrors the korean service's ModelRecall JSON.
type koreanRecall struct {
	ModelPackageID string `json:"model_package_id"`
	Reason         string `json:"reason"`
}

// koreanForcedVersion mirrors the ForcedVersion JSON.
type koreanForcedVersion struct {
	MinVersion  string `json:"min_version"`
	ReleaseRing string `json:"release_ring"`
}

// GatherGovernanceState builds the view from audit-trail governance
// state (freezes, recalls, forced version) plus the tool registry
// and sandbox definitions.
func (s *Service) GatherGovernanceState(orgID, repoID, modelID string) GovernanceStateView {
	view := GovernanceStateView{OrgID: orgID, RepoID: repoID, ModelID: modelID}

	// Freeze: the most recent started/ended event decides.
	var events []models.AuditEvent
	s.db.Where("organization_id = ? AND event_type IN (?, ?)", orgID,
		"cp.korean.change_freeze_started", "cp.korean.change_freeze_ended").
		Order("occurred_at DESC").Limit(2).Find(&events)
	for _, e := range events {
		if e.EventType == "cp.korean.change_freeze_started" {
			var f koreanFreeze
			_ = json.Unmarshal([]byte(e.Details), &f)
			view.Freeze = &GovernanceFreezeView{
				Reason: f.FreezeReason, ReasonKo: f.FreezeReasonKo,
				AffectedRepos: f.AffectedRepos, AllowedActions: f.AllowedActions,
				NotAfterMs: time.Now().Add(24 * time.Hour).UnixMilli(), // audit carries no end; renew per session
			}
			break
		}
		if e.EventType == "cp.korean.change_freeze_ended" {
			break
		}
	}

	// Recalls: the latest per model.
	var recallEvents []models.AuditEvent
	s.db.Where("organization_id = ? AND event_type = ?", orgID, "cp.korean.emergency_model_recall").
		Order("occurred_at DESC").Limit(50).Find(&recallEvents)
	seen := map[string]bool{}
	for _, e := range recallEvents {
		var r koreanRecall
		_ = json.Unmarshal([]byte(e.Details), &r)
		if r.ModelPackageID == "" || seen[r.ModelPackageID] {
			continue
		}
		seen[r.ModelPackageID] = true
		view.Recalls = append(view.Recalls, GovernanceRecallView{Model: r.ModelPackageID, Reason: r.Reason})
	}

	// Forced harness version: the latest event wins.
	var versionEvents []models.AuditEvent
	s.db.Where("organization_id = ? AND event_type = ?", orgID, "cp.korean.forced_harness_version").
		Order("occurred_at DESC").Limit(1).Find(&versionEvents)
	for _, e := range versionEvents {
		var v koreanForcedVersion
		_ = json.Unmarshal([]byte(e.Details), &v)
		if v.MinVersion != "" {
			view.VersionReq = &GovernanceVersionView{MinVersion: v.MinVersion, Ring: v.ReleaseRing}
		}
	}

	// Tool registry: every registered tool with its live status.
	var tools []models.Tool
	s.db.Where("organization_id = ?", orgID).Find(&tools)
	for _, t := range tools {
		status := "APPROVED"
		if t.RequiresApproval {
			status = "REQUIRE_REVIEW"
		}
		if t.Status != "active" {
			status = "BLOCKED"
		}
		view.Tools = append(view.Tools, GovernanceToolView{ToolID: t.Name, Status: status})
	}

	// Sandbox definitions: the control-plane sandbox service is
	// definition-only today (audit-fixed); policies persist nowhere the
	// relay can query, so the snapshot carries no sandbox rows until
	// they do. The wire field remains for that push.
	return view
}

// ForcedHarnessVersion returns the org's active minimum harness
// version from the audit trail (empty when none is forced).
func (s *Service) ForcedHarnessVersion(orgID string) string {
	var events []models.AuditEvent
	s.db.Where("organization_id = ? AND event_type = ?", orgID, "cp.korean.forced_harness_version").
		Order("occurred_at DESC").Limit(1).Find(&events)
	for _, e := range events {
		var v koreanForcedVersion
		_ = json.Unmarshal([]byte(e.Details), &v)
		return v.MinVersion
	}
	return ""
}

// versionBelow reports whether v sorts below minV as a dotted numeric
// version (leading v is tolerated; non-numeric segments compare
// lexicographically). Unparseable inputs never block.
func versionBelow(v, minV string) bool {
	norm := func(s string) []string {
		s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
		return strings.Split(s, ".")
	}
	a, b := norm(v), norm(minV)
	for i := 0; i < len(a) && i < len(b); i++ {
		ai, aerr := strconv.Atoi(a[i])
		bi, berr := strconv.Atoi(b[i])
		if aerr != nil || berr != nil {
			if aerr != berr {
				// Mixed numeric/non-numeric: the non-numeric side
				// (dev builds) sorts below a numeric floor.
				return aerr != nil
			}
			if a[i] != b[i] {
				return a[i] < b[i]
			}
			continue
		}
		if ai != bi {
			return ai < bi
		}
	}
	return false
}
