package api

// PAT-1456 managed skill governance — admin + harness-report surface for the
// skillpolicy resolver. The resolver stays pure; this file is the API adapter:
// it loads persisted assignments, ingests per-harness skill inventory,
// aggregates an admin view (effective state, drift, affected counts), upserts/
// deletes scoped assignments, and issues signed skill-policy epochs delivered
// to affected harnesses over the relay directive carrier.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/skillpolicy"
)

// skillAssignmentFromRow converts a persisted row back to the resolver shape.
func skillAssignmentFromRow(row models.SkillPolicyAssignment) skillpolicy.Assignment {
	return skillpolicy.Assignment{
		SkillIdentity: row.SkillIdentity,
		Digest:        row.Digest,
		Scope:         skillpolicy.Scope(row.Scope),
		State:         skillpolicy.State(row.State),
	}
}

// listOrgSkillAssignments loads all active assignments for an org.
func (s *Server) listOrgSkillAssignments(orgID string) ([]skillpolicy.Assignment, error) {
	var rows []models.SkillPolicyAssignment
	if err := s.db.Where("organization_id = ?", orgID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]skillpolicy.Assignment, 0, len(rows))
	for _, r := range rows {
		if r.State == "deleted" {
			continue
		}
		out = append(out, skillAssignmentFromRow(r))
	}
	return out, nil
}

// skillInventoryRow is one skill identity aggregated across the org's harness
// reports for the admin list.
type skillInventoryRow struct {
	SkillIdentity string `json:"skill_identity"`
	DisplayName   string `json:"display_name"`
	ExecutionMode string `json:"execution_mode"`
	Source        string `json:"source"`
	PluginPackage string `json:"plugin_package,omitempty"`
	PackageDigest string `json:"package_digest,omitempty"`
	ContentDigest string `json:"content_digest,omitempty"`
	// Effective state summaries across harnesses.
	Required int `json:"required"`
	Optional int `json:"optional"`
	Blocked  int `json:"blocked"`
	Unknown  int `json:"unknown"`
	// Drift / compliance aggregation.
	Installed   int    `json:"installed"`
	Missing     int    `json:"missing"`
	Unverified  int    `json:"unverified"`
	Drifted     int    `json:"drifted"`
	Shadowed    int    `json:"shadowed"`
	Affected    int    `json:"affected"`
	Description string `json:"description,omitempty"`
}

// handleAdminSkillInventory returns the aggregated admin skill inventory with
// effective states resolved per harness and rolled up per skill identity.
func (s *Server) handleAdminSkillInventory(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	assignments, err := s.listOrgSkillAssignments(orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "skill policy: "+err.Error())
		return
	}
	enforcement := s.orgSkillEnforcement(orgID)
	// Per-harness reports (latest per identity per harness).
	var reports []models.HarnessSkillReport
	if err := s.db.Where("organization_id = ?", orgID).Find(&reports).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "skill policy: "+err.Error())
		return
	}
	// Group reports by harness, latest per skill identity.
	byHarness := map[string]map[string]models.HarnessSkillReport{}
	for _, rep := range reports {
		if byHarness[rep.HarnessID] == nil {
			byHarness[rep.HarnessID] = map[string]models.HarnessSkillReport{}
		}
		key := rep.SkillIdentity
		if prev, ok := byHarness[rep.HarnessID][key]; ok && prev.CreatedAt.After(rep.CreatedAt) {
			continue
		}
		byHarness[rep.HarnessID][key] = rep
	}

	// Resolve per harness, then roll up per skill identity.
	rollup := map[string]*skillInventoryRow{}
	order := []string{}
	for _, perHarness := range byHarness {
		reported := make([]skillpolicy.ReportedSkill, 0, len(perHarness))
		for id, rep := range perHarness {
			reported = append(reported, skillpolicy.ReportedSkill{Identity: id, Digest: rep.ContentDigest, Enabled: rep.Enabled})
		}
		results := skillpolicy.Resolve(reported, assignments, skillpolicy.ResolveOptions{EnforcementEnabled: enforcement})
		for _, res := range results {
			row := rollup[res.SkillIdentity]
			if row == nil {
				row = &skillInventoryRow{SkillIdentity: res.SkillIdentity}
				rollup[res.SkillIdentity] = row
				order = append(order, res.SkillIdentity)
			}
			rep := perHarness[res.SkillIdentity]
			if rep.DisplayName != "" {
				row.DisplayName = rep.DisplayName
				row.Description = rep.Description
				row.ExecutionMode = rep.ExecutionMode
				row.Source = rep.Source
				row.PluginPackage = rep.PluginPackage
				row.PackageDigest = rep.PackageDigest
				row.ContentDigest = rep.ContentDigest
			}
			row.Installed++
			row.Affected++
			switch res.State {
			case skillpolicy.Required:
				row.Required++
			case skillpolicy.Optional:
				row.Optional++
			case skillpolicy.Blocked:
				row.Blocked++
			}
			if res.Unknown {
				row.Unknown++
			}
			if res.State != skillpolicy.Blocked && !res.Approved {
				row.Unverified++
			}
			if rep.Shadowed {
				row.Shadowed++
			}
			if res.State == skillpolicy.Required && (!rep.Enabled || res.Unknown) {
				row.Drifted++
			}
			if res.State == skillpolicy.Required && res.Unknown {
				row.Missing++
			}
		}
	}

	// Skills that have a policy assignment but zero harness reports yet are
	// shown as provisioned-but-not-seen so admins can act before first contact.
	for _, a := range assignments {
		if _, ok := rollup[a.SkillIdentity]; ok {
			continue
		}
		row := &skillInventoryRow{SkillIdentity: a.SkillIdentity}
		if a.State == skillpolicy.Required {
			row.Missing = 1
		}
		rollup[a.SkillIdentity] = row
		order = append(order, a.SkillIdentity)
	}

	sort.Strings(order)
	rows := make([]*skillInventoryRow, 0, len(order))
	for _, id := range order {
		if row, ok := rollup[id]; ok {
			rows = append(rows, row)
		}
	}

	// Filters: state, integrity, drift, query.
	fs := r.URL.Query().Get("state")
	ff := r.URL.Query().Get("integrity")
	fd := r.URL.Query().Get("drift")
	fq := strings.ToLower(r.URL.Query().Get("q"))
	var out []*skillInventoryRow
	for _, row := range rows {
		if fs != "" && fs != "any" {
			count := 0
			switch fs {
			case "required":
				count = row.Required
			case "optional":
				count = row.Optional
			case "blocked":
				count = row.Blocked
			case "unknown":
				count = row.Unknown
			case "unverified":
				count = row.Unverified
			}
			if count == 0 {
				continue
			}
		}
		if ff == "unverified" && row.Unverified == 0 {
			continue
		}
		if fd == "drifted" && row.Drifted == 0 && row.Missing == 0 {
			continue
		}
		if fq != "" && !strings.Contains(strings.ToLower(row.SkillIdentity), fq) &&
			!strings.Contains(strings.ToLower(row.DisplayName), fq) &&
			!strings.Contains(strings.ToLower(row.PluginPackage), fq) {
			continue
		}
		out = append(out, row)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":           out,
		"total":           len(out),
		"enforcement":     enforcement,
		"has_assignments": len(assignments) > 0,
	})
}

// orgSkillEnforcement reads the org setting flag. Default = fail closed true.
func (s *Server) orgSkillEnforcement(orgID string) bool {
	var setting models.OrgSetting
	if err := s.db.Where("organization_id = ? AND key = ?", orgID, "skill_policy_enforcement").First(&setting).Error; err != nil {
		return true
	}
	return setting.Value == "true"
}

// skillAssignmentRequest is the admin upsert payload.
type skillAssignmentRequest struct {
	SkillIdentity string `json:"skill_identity"`
	Digest        string `json:"digest,omitempty"`
	Scope         string `json:"scope"`
	ScopeID       string `json:"scope_id,omitempty"`
	State         string `json:"state"`
	Reason        string `json:"reason,omitempty"`
}

func (req *skillAssignmentRequest) validate() error {
	if strings.TrimSpace(req.SkillIdentity) == "" {
		return fmt.Errorf("skill_identity required")
	}
	if req.Scope != "org" && req.Scope != "team" && req.Scope != "fleet" && req.Scope != "user" {
		return fmt.Errorf("scope must be org|team|fleet|user")
	}
	if req.Scope != "org" && strings.TrimSpace(req.ScopeID) == "" {
		return fmt.Errorf("scope_id required for %s scope", req.Scope)
	}
	switch req.State {
	case "required", "optional", "blocked":
	default:
		return fmt.Errorf("state must be required|optional|blocked")
	}
	return nil
}

// handleAdminSkillAssignmentUpsert creates or updates a scoped assignment.
// Changing an assignment invalidates previously-issued epochs (monotonic:
// the next epoch supersedes them).
func (s *Server) handleAdminSkillAssignmentUpsert(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	var req skillAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var existing models.SkillPolicyAssignment
	q := s.db.Where("organization_id = ? AND skill_identity = ? AND scope = ?", orgID, req.SkillIdentity, req.Scope)
	if req.ScopeID != "" {
		q = q.Where("scope_id = ?", req.ScopeID)
	} else {
		q = q.Where("scope_id = '' OR scope_id IS NULL")
	}
	err := q.First(&existing).Error
	actor := getActorID(r)
	if err != nil { // new
		existing = models.SkillPolicyAssignment{
			OrganizationID: orgID,
			Scope:          req.Scope,
			ScopeID:        req.ScopeID,
			SkillIdentity:  req.SkillIdentity,
			Digest:         strings.TrimSpace(req.Digest),
			State:          req.State,
			Reason:         req.Reason,
			CreatedBy:      actor,
		}
		if e := s.db.Create(&existing).Error; e != nil {
			writeError(w, http.StatusInternalServerError, "skill policy: "+e.Error())
			return
		}
	} else {
		updates := map[string]interface{}{
			"state":    req.State,
			"reason":   req.Reason,
			"digest":   strings.TrimSpace(req.Digest),
			"epoch_id": "",
		}
		if e := s.db.Model(&existing).Updates(updates).Error; e != nil {
			writeError(w, http.StatusInternalServerError, "skill policy: "+e.Error())
			return
		}
	}
	s.governanceAudit(orgID, r, "cp.skill_policy.set", "skill_policy.set", "skill", req.SkillIdentity, req.State,
		map[string]interface{}{"scope": req.Scope, "scope_id": req.ScopeID, "digest": req.Digest, "reason": req.Reason})
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "target": req.SkillIdentity})
}

// handleAdminSkillAssignmentDelete soft-deletes an assignment.
func (s *Server) handleAdminSkillAssignmentDelete(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	if orgID == "" || id == "" {
		writeError(w, http.StatusBadRequest, "organization context and assignment id required")
		return
	}

	if !requireGovernanceAdmin(w, r) {
		return
	}
	if err := s.db.Model(&models.SkillPolicyAssignment{}).Where("organization_id = ? AND id = ?", orgID, id).
		Update("state", "deleted").Error; err != nil {
		writeError(w, http.StatusInternalServerError, "skill policy: "+err.Error())
		return
	}
	var row models.SkillPolicyAssignment
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, id).First(&row).Error; err == nil {
		s.governanceAudit(orgID, r, "cp.skill_policy.delete", "skill_policy.delete", "skill", row.SkillIdentity, "deleted",
			map[string]interface{}{"assignment_id": id})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// handleAdminSkillEpochDeliver signs the current effective policy and pushes a
// directive to every harness that reported a skill (so each re-evaluates and
// rebuilds its controller's effective skill set for the next request).
func (s *Server) handleAdminSkillEpochDeliver(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	assignments, err := s.listOrgSkillAssignments(orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "skill policy: "+err.Error())
		return
	}
	payload := map[string]interface{}{
		"organization_id": orgID,
		"assignments":     assignments,
		"enforcement":     s.orgSkillEnforcement(orgID),
		"issued_at":       time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)
	digest, sig, err := signPolicyPayload(s.db, body)
	if err != nil {
		writePolicyEpochError(w, "skill policy: sign", err)
		return
	}
	enforcement := s.orgSkillEnforcement(orgID)
	epoch := &models.SkillPolicyEpoch{
		OrganizationID:     orgID,
		EpochID:            dari.GenerateID("skpe"),
		EpochNumber:        s.nextSkillEpochNumber(orgID),
		AssignmentsJSON:    string(body),
		Digest:             digest,
		SignatureHex:       sig,
		EnforcementEnabled: enforcement,
		CreatedBy:          getActorID(r),
		Status:             "active",
		EffectiveAt:        time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.db.Create(epoch).Error; err != nil {
		writePolicyEpochError(w, "skill policy", err)
		return
	}
	// Supersede older active epochs.
	s.db.Model(&models.SkillPolicyEpoch{}).Where("organization_id = ? AND status = 'active' AND id != ?", orgID, epoch.ID).
		Updates(map[string]interface{}{"status": "superseded", "superseded_by": epoch.EpochID})

	// Push to affected harnesses (any that reported skills or have bindings).
	var harnessIDs []string
	s.db.Model(&models.HarnessSkillReport{}).Where("organization_id = ?", orgID).Distinct().Pluck("harness_id", &harnessIDs)
	targets := harnessIDs
	if len(targets) == 0 {
		// Fall back to all active harnesses so Required-missing stays governed
		// even when inventory has not been reported yet.
		s.db.Model(&models.Harness{}).Where("organization_id = ? AND status IN ?", orgID, []string{"active", "enrolled"}).Pluck("harness_id", &targets)
	}
	delivered := 0
	for _, hid := range targets {
		if err := s.pushRelayDirective("skill_policy", orgID, hid, "skill-policy epoch "+epoch.EpochID, map[string]interface{}{
			"epoch_id": epoch.EpochID, "epoch_number": epoch.EpochNumber,
			"digest": digest, "signature_hex": sig, "enforcement": epoch.EnforcementEnabled,
		}); err == nil {
			delivered++
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "epoch_id": epoch.EpochID, "epoch_number": epoch.EpochNumber,
		"digest": digest, "targets": len(targets), "delivered": delivered,
	})
}

func (s *Server) nextSkillEpochNumber(orgID string) uint64 {
	var max uint64
	s.db.Model(&models.SkillPolicyEpoch{}).Where("organization_id = ?", orgID).Select("COALESCE(MAX(epoch_number),0)").Scan(&max)
	return max + 1
}

// skillReportRequest is one harness-reported skill inventory item.
type skillReportRequest struct {
	HarnessID      string `json:"harness_id"`
	SessionID      string `json:"session_id,omitempty"`
	SkillIdentity  string `json:"skill_identity"`
	DisplayName    string `json:"display_name,omitempty"`
	Description    string `json:"description,omitempty"`
	ExecutionMode  string `json:"execution_mode,omitempty"`
	Source         string `json:"source,omitempty"`
	PluginPackage  string `json:"plugin_package,omitempty"`
	PluginVersion  string `json:"plugin_version,omitempty"`
	PackageDigest  string `json:"package_digest,omitempty"`
	ContentDigest  string `json:"content_digest"`
	RequestedModel string `json:"requested_model,omitempty"`
	RequestedTools string `json:"requested_tools,omitempty"`
	ReadOnly       bool   `json:"read_only,omitempty"`
	Enabled        bool   `json:"enabled"`
	Shadowed       bool   `json:"shadowed,omitempty"`
	Duplicate      bool   `json:"duplicate,omitempty"`
	SignatureState string `json:"signature_state,omitempty"`
}

// handleHarnessSkillReport ingests a harness's skill inventory (idempotent,
// delta-friendly): each row replaces the previous report for that harness+skill.
func (s *Server) handleHarnessSkillReport(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	var req struct {
		HarnessID string               `json:"harness_id"`
		SessionID string               `json:"session_id,omitempty"`
		Skills    []skillReportRequest `json:"skills"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.HarnessID == "" || len(req.Skills) == 0 {
		writeError(w, http.StatusBadRequest, "harness_id and skills[] required")
		return
	}
	// Idempotent replacement: delete prior report rows for this harness and
	// insert the fresh snapshot (delta-capable for large fleets).
	s.db.Where("organization_id = ? AND harness_id = ?", orgID, req.HarnessID).Delete(&models.HarnessSkillReport{})
	rows := make([]models.HarnessSkillReport, 0, len(req.Skills))
	for _, sk := range req.Skills {
		if strings.TrimSpace(sk.SkillIdentity) == "" {
			continue
		}
		rows = append(rows, models.HarnessSkillReport{
			OrganizationID: orgID,
			HarnessID:      req.HarnessID,
			SessionID:      req.SessionID,
			SkillIdentity:  sk.SkillIdentity,
			DisplayName:    sk.DisplayName,
			Description:    sk.Description,
			ExecutionMode:  sk.ExecutionMode,
			Source:         sk.Source,
			PluginPackage:  sk.PluginPackage,
			PluginVersion:  sk.PluginVersion,
			PackageDigest:  sk.PackageDigest,
			ContentDigest:  sk.ContentDigest,
			RequestedModel: sk.RequestedModel,
			RequestedTools: sk.RequestedTools,
			ReadOnly:       sk.ReadOnly,
			Enabled:        sk.Enabled,
			Shadowed:       sk.Shadowed,
			Duplicate:      sk.Duplicate,
			SignatureState: sk.SignatureState,
		})
	}
	if len(rows) > 0 {
		if err := s.db.Create(&rows).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "skill policy: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "recorded": len(rows)})
}

// actorID returns the authenticated user id from the request context.
// (Canonical helper lives in policy_handlers.go as getActorID.)

// mustJSON marshals v to a JSON string, returning "{}" on failure.
func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}
