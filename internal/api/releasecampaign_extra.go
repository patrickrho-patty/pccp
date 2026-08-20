package api

// Governed harness update campaigns (PAT-1449).
//
// Locked rules enforced here:
//   - Self-reported version strings are insufficient: identity is the
//     (release catalog entry, artifact digest) pair; unknown/mismatched
//     reports get explicit states, not "old version".
//   - Target and minimum are separate; a canary/preview artifact never
//     satisfies a stable minimum (internal/version).
//   - Ring percentage membership is deterministic per (cohort seed,
//     harness) so harnesses don't jump cohorts across heartbeats.
//   - Per-harness states are derived — not editable strings.
//   - Managed exceptions are scoped/expiring/approved and can never
//     bypass the hosted floor, revoked artifacts, or unknown identity.
//   - Operator mutations require a reason, CAS on expected_epoch, and
//     audit records.

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/version"
)

// Harness version states (PAT-1449).
const (
	hvSupported         = "supported"
	hvUpdateAvailable   = "update_available"
	hvUpdateRequiredGrace = "update_required_grace"
	hvRestricted        = "restricted"
	hvRevoked           = "revoked"
	hvUpdating          = "updating"
	hvVerifying         = "verifying"
	hvRollbackRequired  = "rollback_required"
	hvRepairRequired    = "repair_required"
	hvUnknownTampered   = "unknown_or_tampered"
)

var hvStateKo = map[string]string{
	hvSupported:           "정상 지원",
	hvUpdateAvailable:     "업데이트 가능",
	hvUpdateRequiredGrace: "업데이트 필요 (유예 중)",
	hvRestricted:          "제한 모드",
	hvRevoked:             "폐기된 릴리스",
	hvUpdating:            "설치 진행 중",
	hvVerifying:           "설치 검증 중",
	hvRollbackRequired:    "롤백 필요",
	hvRepairRequired:      "복구 필요",
	hvUnknownTampered:     "검증 불가 (변조 의심)",
}

// campaignEpochBump performs the shared compare-and-set epoch advance
// used by harness-release and model-distribution campaign mutations —
// concurrent operator actions cannot both land. The extra updates ride
// the same conditional statement.
func campaignEpochBump(db *gorm.DB, model interface{}, id uint, fromEpoch int, extra map[string]interface{}) bool {
	updates := map[string]interface{}{"expected_epoch": fromEpoch + 1}
	for k, v := range extra {
		updates[k] = v
	}
	res := db.Model(model).
		Where("id = ? AND expected_epoch = ?", id, fromEpoch).
		Updates(updates)
	return res.RowsAffected > 0
}

// hvCohortMember deterministically decides whether a harness falls inside
// a campaign's rollout percentage. Same (seed, harness) → same answer.
func hvCohortMember(seed, harnessID string, percentage int) bool {
	if percentage >= 100 {
		return true
	}
	if percentage <= 0 {
		return false
	}
	h := fnv.New32a()
	h.Write([]byte(seed + ":" + harnessID))
	return int(h.Sum32()%100) < percentage
}

// hvDeriveState computes one harness's effective state from the release
// catalog, active campaigns, the org floor, and live exceptions.
func hvDeriveState(rep *models.HarnessHeartbeatReport, releases []models.HarnessRelease,
	campaigns []models.HarnessUpdateCampaign, floor string, exceptions []models.HarnessVersionException, now time.Time) (string, string) {

	// Identity validation first: the report must resolve to a known,
	// non-dev catalog entry whose digest matches.
	if rep == nil {
		return hvUnknownTampered, "no_attestation"
	}
	repVer, err := version.Parse(rep.Version)
	if err != nil {
		return hvUnknownTampered, "malformed_version"
	}
	var matched *models.HarnessRelease
	for i := range releases {
		if releases[i].ReleaseID == rep.ReleaseID {
			matched = &releases[i]
			break
		}
	}
	if matched == nil {
		return hvUnknownTampered, "unknown_release"
	}
	// Identity = catalog entry AND digest AND build-profile match. A
	// missing attestation field is a non-match, never a pass.
	if matched.ArtifactDigest != "" && rep.ExecutableDigest == "" {
		return hvUnknownTampered, "no_attestation"
	}
	if matched.ArtifactDigest != "" && rep.ExecutableDigest != "" && matched.ArtifactDigest != rep.ExecutableDigest {
		return hvUnknownTampered, "digest_mismatch"
	}
	if matched.BuildProfile != "" && rep.BuildProfile == "" {
		return hvUnknownTampered, "no_attestation"
	}
	if matched.BuildProfile != "" && rep.BuildProfile != "" && matched.BuildProfile != rep.BuildProfile {
		return hvUnknownTampered, "profile_mismatch"
	}
	if matched.Revoked {
		return hvRevoked, "release_revoked"
	}

	// Effective minimum: strongest of org floor and campaign minimums.
	mins := []string{}
	if floor != "" {
		mins = append(mins, floor)
	}
	for _, c := range campaigns {
		if c.State != "active" {
			continue
		}
		if c.MinVersion != "" && hvCampaignAppliesTo(&c, rep, now) {
			mins = append(mins, c.MinVersion)
		}
	}
	effectiveMin := ""
	for _, m := range mins {
		mv, err := version.Parse(m)
		if err != nil {
			continue
		}
		if effectiveMin == "" {
			effectiveMin = m
			continue
		}
		ev, _ := version.Parse(effectiveMin)
		if mv.Compare(ev) > 0 {
			effectiveMin = m
		}
	}

	if effectiveMin != "" {
		minV, _ := version.Parse(effectiveMin)
		if !repVer.SatisfiesMinimum(minV) {
			// An active, unexpired exception can DEFER an ordinary
			// customer-controlled deadline — it never grants full support
			// and never bypasses revoked/unknown states (handled above).
			if hvActiveException(exceptions, rep.HarnessID, now) {
				return hvUpdateRequiredGrace, "exception_deferred"
			}
			// Below the floor: grace until the campaign deadline, then
			// restricted.
			for _, c := range campaigns {
				if c.State != "active" || c.MinVersion == "" || !hvCampaignAppliesTo(&c, rep, now) {
					continue
				}
				cm, _ := version.Parse(c.MinVersion)
				if cm.String() == minV.String() && c.Deadline != "" {
					dl, err := time.Parse(time.RFC3339, c.Deadline)
					if err == nil && now.Before(dl) {
						return hvUpdateRequiredGrace, "deadline_" + c.Deadline
					}
				}
			}
			return hvRestricted, "below_minimum_" + effectiveMin
		}
	}

	// Above the floor: is an optional target offered to this harness?
	for _, c := range campaigns {
		if c.State != "active" || !hvCampaignAppliesTo(&c, rep, now) {
			continue
		}
		tv, err := version.Parse(c.TargetVersion)
		if err != nil {
			continue
		}
		if repVer.Compare(tv) < 0 {
			return hvUpdateAvailable, "target_" + c.TargetVersion
		}
	}
	return hvSupported, ""
}

// hvCampaignAppliesTo checks ring/percentage membership and start time.
func hvCampaignAppliesTo(c *models.HarnessUpdateCampaign, rep *models.HarnessHeartbeatReport, now time.Time) bool {
	if c.StartTime != "" {
		st, err := time.Parse(time.RFC3339, c.StartTime)
		if err == nil && now.Before(st) {
			return false
		}
	}
	if c.Ring == "canary" || c.Percentage < 100 {
		if !hvCohortMember(c.CohortSeed, rep.HarnessID, c.Percentage) {
			return false
		}
	}
	return true
}

func hvActiveException(exceptions []models.HarnessVersionException, harnessID string, now time.Time) bool {
	for _, ex := range exceptions {
		if ex.Revoked {
			continue
		}
		if ex.ExpiresAt == "" {
			continue // no expiry = invalid
		}
		exp, err := time.Parse(time.RFC3339, ex.ExpiresAt)
		if err != nil || now.After(exp) {
			continue
		}
		var covered []string
		if err := json.Unmarshal([]byte(ex.HarnessIDsJSON), &covered); err == nil {
			for _, h := range covered {
				if h == harnessID {
					return true
				}
			}
		}
	}
	return false
}

// ---------- release catalog ----------

func (s *Server) handleHVReleaseRegister(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "릴리스 카탈로그 관리 권한이 필요합니다")
		return
	}
	var req models.HarnessRelease
	if err := decodeJSON(r, &req); err != nil || req.ReleaseID == "" || req.Version == "" {
		writeError(w, http.StatusBadRequest, "release_id와 version이 필요합니다")
		return
	}
	if _, err := version.Parse(req.Version); err != nil {
		writeError(w, http.StatusBadRequest, "버전이 정규 형식이 아닙니다: "+err.Error())
		return
	}
	switch req.BuildProfile {
	case "public", "enterprise", "sovereign":
	default:
		writeError(w, http.StatusBadRequest, "build_profile은 public|enterprise|sovereign이어야 합니다")
		return
	}
	var dup int64
	s.db.Model(&models.HarnessRelease{}).Where("release_id = ?", req.ReleaseID).Count(&dup)
	if dup > 0 {
		writeError(w, http.StatusConflict, "이미 등록된 릴리스 ID입니다")
		return
	}
	req.PublishedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.db.Create(&req).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.release.registered", ActorType: "admin",
		Action: "register_release", ResourceType: "harness_release", ResourceID: req.ReleaseID,
		Details: fmt.Sprintf(`{"version":"%s","profile":"%s","digest":"%s"}`, req.Version, req.BuildProfile, req.ArtifactDigest),
		Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, req)
}

// handleHVReleaseRevoke immediately prohibits a compromised release.
func (s *Server) handleHVReleaseRevoke(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "릴리스 카탈로그 관리 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "폐기 사유가 필요합니다")
		return
	}
	var rel models.HarnessRelease
	if err := s.db.Where("id = ? OR release_id = ?", id, id).First(&rel).Error; err != nil {
		writeError(w, http.StatusNotFound, "릴리스를 찾을 수 없습니다")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	s.db.Model(&rel).Updates(map[string]interface{}{"revoked": true, "revoked_at": now, "revoked_reason": req.Reason})
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.release.revoked", ActorType: "admin",
		Action: "revoke_release", ResourceType: "harness_release", ResourceID: rel.ReleaseID,
		Details: fmt.Sprintf(`{"reason":%q}`, req.Reason), Result: "success", OccurredAt: now,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked", "release_id": rel.ReleaseID})
}

func (s *Server) handleHVReleasesList(w http.ResponseWriter, r *http.Request) {
	var releases []models.HarnessRelease
	q := s.db
	if v := r.URL.Query().Get("profile"); v != "" {
		q = q.Where("build_profile = ?", v)
	}
	q.Order("created_at DESC").Limit(200).Find(&releases)
	writeJSON(w, http.StatusOK, releases)
}

// ---------- campaigns ----------

func (s *Server) handleHVCampaignCreate(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "캠페인 관리 권한이 필요합니다")
		return
	}
	var req models.HarnessUpdateCampaign
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "캠페인 사유가 필요합니다")
		return
	}
	tv, err1 := version.Parse(req.TargetVersion)
	if err1 != nil {
		writeError(w, http.StatusBadRequest, "대상 버전이 정규 형식이 아닙니다")
		return
	}
	if req.MinVersion != "" {
		mv, err := version.Parse(req.MinVersion)
		if err != nil {
			writeError(w, http.StatusBadRequest, "최소 버전이 정규 형식이 아닙니다")
			return
		}
		if !mv.IsStable() {
			writeError(w, http.StatusBadRequest, "최소 버전은 안정 릴리스여야 합니다 (사전 버전 불가)")
			return
		}
		if mv.Compare(tv) > 0 {
			writeError(w, http.StatusBadRequest, "최소 버전이 대상 버전보다 높을 수 없습니다")
			return
		}
	}
	switch req.Ring {
	case "canary", "beta", "stable":
	default:
		req.Ring = "stable"
	}
	if req.Percentage < 0 || req.Percentage > 100 {
		writeError(w, http.StatusBadRequest, "percentage는 0-100이어야 합니다")
		return
	}
	if req.Severity != "emergency" {
		req.Severity = "ordinary"
	}
	if req.CohortSeed == "" {
		req.CohortSeed = fmt.Sprintf("seed-%d", time.Now().UnixNano())
	}
	if req.State == "" {
		req.State = "draft"
	}
	req.CreatedBy = getOperatorEmail(r)
	if err := s.db.Create(&req).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.campaign.created", ActorType: "admin",
		Action: "create_campaign", ResourceType: "harness_campaign", ResourceID: fmt.Sprint(req.ID),
		Details: fmt.Sprintf(`{"target":"%s","min":"%s","ring":"%s","pct":%d,"reason":%q}`,
			req.TargetVersion, req.MinVersion, req.Ring, req.Percentage, req.Reason),
		Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, req)
}

// handleHVCampaignMutate pauses/resumes/cancels/rolls back with reason +
// epoch CAS (PAT-1449 operator controls).
func (s *Server) handleHVCampaignMutate(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "캠페인 관리 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Action         string `json:"action"` // activate|pause|resume|cancel|rollback
		Reason         string `json:"reason"`
		ExpectedEpoch  int    `json:"expected_epoch"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "작업과 사유가 필요합니다")
		return
	}
	var c models.HarnessUpdateCampaign
	if err := s.db.Where("id = ?", id).First(&c).Error; err != nil {
		writeError(w, http.StatusNotFound, "캠페인을 찾을 수 없습니다")
		return
	}
	if req.ExpectedEpoch != c.ExpectedEpoch {
		writeError(w, http.StatusConflict, "캠페인이 이미 변경되었습니다 (epoch 불일치)")
		return
	}
	next := ""
	switch req.Action {
	case "activate":
		next = "active"
	case "pause":
		if c.State != "active" {
			writeError(w, http.StatusUnprocessableEntity, "활성 캠페인만 일시중지할 수 있습니다")
			return
		}
		next = "paused"
	case "resume":
		if c.State != "paused" {
			writeError(w, http.StatusUnprocessableEntity, "일시중지된 캠페인만 재개할 수 있습니다")
			return
		}
		next = "active"
	case "cancel":
		next = "cancelled"
	case "rollback":
		next = "rolled_back"
	default:
		writeError(w, http.StatusBadRequest, "알 수 없는 작업입니다")
		return
	}
	if !campaignEpochBump(s.db, &models.HarnessUpdateCampaign{}, c.ID, c.ExpectedEpoch, map[string]interface{}{"state": next}) {
		writeError(w, http.StatusConflict, "동시 변경이 감지되었습니다")
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.campaign." + req.Action, ActorType: "admin",
		Action: req.Action + "_campaign", ResourceType: "harness_campaign", ResourceID: fmt.Sprint(c.ID),
		Details: fmt.Sprintf(`{"reason":%q,"from":"%s","to":"%s"}`, req.Reason, c.State, next),
		Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": next})
}

func (s *Server) handleHVCampaignsList(w http.ResponseWriter, r *http.Request) {
	var campaigns []models.HarnessUpdateCampaign
	q := s.db
	if v := r.URL.Query().Get("state"); v != "" {
		q = q.Where("state = ?", v)
	}
	q.Order("created_at DESC").Limit(100).Find(&campaigns)
	writeJSON(w, http.StatusOK, campaigns)
}

// handleHVCampaignPreview computes affected/ineligible/unknown counts
// BEFORE launch or floor advancement (PAT-1449). Admin-gated and
// org-scoped.
func (s *Server) handleHVCampaignPreview(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "미리보기 권한이 필요합니다")
		return
	}
	var req struct {
		MinVersion string `json:"min_version"`
		TargetVersion string `json:"target_version"`
		Percentage  int    `json:"percentage"`
		CohortSeed  string `json:"cohort_seed"`
		Deadline    string `json:"deadline"`
	}
	if err := decodeJSON(r, &req); err != nil || req.MinVersion == "" {
		writeError(w, http.StatusBadRequest, "min_version가 필요합니다")
		return
	}
	minV, err := version.Parse(req.MinVersion)
	if err != nil {
		writeError(w, http.StatusBadRequest, "최소 버전이 정규 형식이 아닙니다")
		return
	}
	var reports []models.HarnessHeartbeatReport
	s.db.Where("organization_id = ?", getOrgID(r)).Find(&reports)
	latest := hvLatestReports(reports)
	var releases []models.HarnessRelease
	s.db.Find(&releases)
	counts := map[string]int{"affected": 0, "already_compliant": 0, "excluded_by_cohort": 0, "ineligible_unknown": 0, "revoked_or_tampered": 0}
	now := time.Now().UTC()
	for _, rep := range latest {
		state, _ := hvDeriveState(rep, releases, nil, req.MinVersion, nil, now)
		switch state {
		case hvRestricted, hvUpdateRequiredGrace:
			counts["affected"]++
		case hvSupported, hvUpdateAvailable:
			// In-cohort check for the prospective campaign.
			if req.Percentage > 0 && req.Percentage < 100 && !hvCohortMember(req.CohortSeed, rep.HarnessID, req.Percentage) {
				counts["excluded_by_cohort"]++
			} else {
				counts["already_compliant"]++
			}
		case hvRevoked:
			counts["revoked_or_tampered"]++
		default:
			counts["ineligible_unknown"]++
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"min_version": minV.String(), "counts": counts})
}

// ---------- heartbeat attestation + fleet states ----------

// handleHVHeartbeatReport ingests a verifiable build identity report and
// derives the harness's effective state against catalog + campaigns +
// floor + exceptions. Organization identity always comes from the
// authenticated session, never the request body.
func (s *Server) handleHVHeartbeatReport(w http.ResponseWriter, r *http.Request) {
	var req models.HarnessHeartbeatReport
	if err := decodeJSON(r, &req); err != nil || req.HarnessID == "" || req.Version == "" {
		writeError(w, http.StatusBadRequest, "harness_id와 version이 필요합니다")
		return
	}
	req.ReportedAt = time.Now().UTC().Format(time.RFC3339)
	req.OrganizationID = getOrgID(r)
	if err := s.db.Create(&req).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	state, reason := s.hvComputeState(&req, time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"state": state, "state_ko": hvStateKo[state], "reason": reason,
		"version": req.Version, "release_id": req.ReleaseID,
	})
}

func (s *Server) hvComputeState(rep *models.HarnessHeartbeatReport, now time.Time) (string, string) {
	var releases []models.HarnessRelease
	s.db.Where("revoked = ?", false).Find(&releases)
	var revoked []models.HarnessRelease
	s.db.Where("revoked = ?", true).Find(&revoked)
	var campaigns []models.HarnessUpdateCampaign
	s.db.Where("state = ?", "active").Find(&campaigns)
	var exceptions []models.HarnessVersionException
	s.db.Where("organization_id = ? AND revoked = ?", rep.OrganizationID, false).Find(&exceptions)
	floor := ""
	var setting models.OrgSetting
	if err := s.db.Where("organization_id = ? AND key = ?", rep.OrganizationID, "forced_harness_version").First(&setting).Error; err == nil {
		floor = setting.Value
		if _, err := version.Parse(floor); err != nil {
			floor = ""
		}
	}
	return hvDeriveState(rep, append(releases, revoked...), campaigns, floor, exceptions, now)
}

// hvLatestReports folds reports to the latest per harness (shared by
// preview + fleet states).
func hvLatestReports(reports []models.HarnessHeartbeatReport) map[string]*models.HarnessHeartbeatReport {
	latest := map[string]*models.HarnessHeartbeatReport{}
	for i := range reports {
		rep := &reports[i]
		if cur, ok := latest[rep.HarnessID]; !ok || rep.ReportedAt > cur.ReportedAt {
			latest[rep.HarnessID] = rep
		}
	}
	return latest
}

// handleHVFleetVersionStates lists per-harness derived states for the
// fleet inventory view (PAT-1449 fleet UI).
func (s *Server) handleHVFleetVersionStates(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var reports []models.HarnessHeartbeatReport
	s.db.Where("organization_id = ?", orgID).Find(&reports)
	latest := hvLatestReports(reports)
	now := time.Now().UTC()
	out := make([]map[string]interface{}, 0, len(latest))
	dist := map[string]int{}
	for _, rep := range latest {
		state, reason := s.hvComputeState(rep, now)
		dist[state]++
		out = append(out, map[string]interface{}{
			"harness_id": rep.HarnessID, "version": rep.Version, "release_id": rep.ReleaseID,
			"os": rep.OS, "arch": rep.Arch, "packaging": rep.Packaging,
			"installation_owner": rep.InstallationOwner, "policy_epoch": rep.PolicyEpoch,
			"reported_at": rep.ReportedAt, "state": state, "state_ko": hvStateKo[state], "reason": reason,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"harnesses": out, "distribution": dist})
}

// ---------- exceptions ----------

func (s *Server) handleHVExceptionCreate(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "예외 관리 권한이 필요합니다")
		return
	}
	var req struct {
		OrganizationID string `json:"organization_id"`
		HarnessIDs     []string `json:"harness_ids"`
		CurrentVersion string `json:"current_version"`
		TargetVersion  string `json:"target_version"`
		Reason         string `json:"reason"`
		Owner          string `json:"owner"`
		ApprovedBy     string `json:"approved_by"`
		CompensatingControls string `json:"compensating_controls"`
		ExpiresAt      string `json:"expires_at"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ex := models.HarnessVersionException{
		OrganizationID: req.OrganizationID, CurrentVersion: req.CurrentVersion,
		TargetVersion: req.TargetVersion, Reason: req.Reason, Owner: req.Owner,
		ApprovedBy: req.ApprovedBy, CompensatingControls: req.CompensatingControls,
		ExpiresAt: req.ExpiresAt,
	}
	if ex.OrganizationID == "" {
		ex.OrganizationID = getOrgID(r)
	}
	if len(req.HarnessIDs) == 0 {
		writeError(w, http.StatusBadRequest, "대상 하네스 목록이 필요합니다")
		return
	}
	raw, _ := json.Marshal(req.HarnessIDs)
	ex.HarnessIDsJSON = string(raw)
	if strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.Owner) == "" || strings.TrimSpace(req.ApprovedBy) == "" {
		writeError(w, http.StatusBadRequest, "사유, 소유자, 승인자가 모두 필요합니다")
		return
	}
	if strings.TrimSpace(ex.CompensatingControls) == "" {
		writeError(w, http.StatusBadRequest, "보완 통제가 필요합니다")
		return
	}
	if ex.ExpiresAt == "" {
		writeError(w, http.StatusBadRequest, "만료 시각이 필요합니다 (무기한 예외는 불가)")
		return
	}
	exp, err := time.Parse(time.RFC3339, ex.ExpiresAt)
	if err != nil || !exp.After(time.Now()) || exp.After(time.Now().Add(90*24*time.Hour)) {
		writeError(w, http.StatusBadRequest, "만료 시각은 현재 이후 90일 이내여야 합니다")
		return
	}
	// Exceptions can only defer an ordinary customer-controlled deadline:
	// they can never name a revoked/unknown release as acceptable, and the
	// target must parse canonically.
	if ex.TargetVersion != "" {
		tv, err := version.Parse(ex.TargetVersion)
		if err != nil || !tv.IsStable() {
			writeError(w, http.StatusBadRequest, "예외 대상 버전은 안정 릴리스여야 합니다")
			return
		}
	}
	ex.StartedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.db.Create(&ex).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: ex.OrganizationID, EventType: "cp.version_exception.created", ActorType: "admin",
		Action: "create_version_exception", ResourceType: "harness_version_exception", ResourceID: fmt.Sprint(ex.ID),
		Details: fmt.Sprintf(`{"harnesses":%s,"expires_at":"%s","reason":%q}`, ex.HarnessIDsJSON, ex.ExpiresAt, ex.Reason),
		Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, ex)
}

func (s *Server) handleHVExceptionRevoke(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "예외 관리 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct{ Reason string `json:"reason"` }
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "철회 사유가 필요합니다")
		return
	}
	res := s.db.Model(&models.HarnessVersionException{}).
		Where("id = ? AND organization_id = ?", id, getOrgID(r)).
		Updates(map[string]interface{}{"revoked": true, "revoked_reason": req.Reason})
	if res.RowsAffected == 0 {
		writeError(w, http.StatusNotFound, "예외를 찾을 수 없습니다")
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.version_exception.revoked", ActorType: "admin",
		Action: "revoke_version_exception", ResourceType: "harness_version_exception", ResourceID: id,
		Details: fmt.Sprintf(`{"reason":%q}`, req.Reason), Result: "success",
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) handleHVExceptionsList(w http.ResponseWriter, r *http.Request) {
	var exceptions []models.HarnessVersionException
	s.db.Where("organization_id = ?", getOrgID(r)).Order("created_at DESC").Find(&exceptions)
	writeJSON(w, http.StatusOK, exceptions)
}
