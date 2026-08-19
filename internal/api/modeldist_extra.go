package api

// Governed enterprise model distribution campaigns (PAT-1444).
//
// Locked decisions enforced here:
//   - Signed desired state + customer-side outbound pull: no inbound
//     vendor access; assignment never bypasses the customer's approval
//     policy (manual approval is the default).
//   - Entitlement grants discovery/download only — never deployment.
//   - Artifact URLs are short-lived leases bound to (org, digest);
//     expired possession is not entitlement.
//   - Silence is never success: stale targets derive offline_unknown.
//   - Canary promotion uses objective health-gate evidence and fails
//     closed on missing or stale data.
//   - Recall blocks new governed selection immediately and marks
//     campaign targets blocked_recalled.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// Observed target states (PAT-1444).
const (
	mdIneligible        = "ineligible"
	mdEntitled          = "entitled"
	mdAwaitingApproval  = "awaiting_customer_approval"
	mdScheduled         = "scheduled"
	mdDownloading       = "downloading"
	mdVerifying         = "verifying"
	mdStaged            = "staged"
	mdLoading           = "loading"
	mdCanary            = "canary"
	mdActive            = "active"
	mdPaused            = "paused"
	mdFailed            = "failed"
	mdRollbackInProgress = "rollback_in_progress"
	mdRolledBack        = "rolled_back"
	mdBlockedRecalled   = "blocked_recalled"
	mdOfflineUnknown    = "offline_unknown"
)

var mdStateKo = map[string]string{
	mdIneligible:         "비대상 (자격/호환 불가)",
	mdEntitled:           "자격 부여됨",
	mdAwaitingApproval:   "고객 승인 대기",
	mdScheduled:          "예약됨",
	mdDownloading:        "다운로드 중",
	mdVerifying:          "검증 중",
	mdStaged:             "스테이징됨",
	mdLoading:            "로드 중",
	mdCanary:             "카나리",
	mdActive:             "활성",
	mdPaused:             "일시중지",
	mdFailed:             "실패",
	mdRollbackInProgress: "롤백 진행 중",
	mdRolledBack:         "롤백됨",
	mdBlockedRecalled:    "차단 (리콜)",
	mdOfflineUnknown:     "오프라인/알 수 없음",
}

// mdAgentReportTransitions restricts observed-state reporting to the
// legal reconciler flow.
var mdAgentReportTransitions = map[string][]string{
	mdEntitled:         {mdDownloading},
	mdScheduled:        {mdDownloading, mdVerifying},
	mdDownloading:      {mdVerifying, mdFailed},
	mdVerifying:        {mdStaged, mdFailed},
	mdStaged:           {mdLoading, mdCanary, mdFailed},
	mdLoading:          {mdActive, mdFailed},
	mdCanary:           {mdActive, mdFailed, mdRollbackInProgress},
	mdActive:           {mdRollbackInProgress, mdPaused},
	mdRollbackInProgress: {mdRolledBack, mdFailed},
}

func mdCanTransition(from, to string) bool {
	for _, t := range mdAgentReportTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// ---------- entitlements (Patty-controlled) ----------

func (s *Server) handleMDEntitle(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "자격 부여 권한이 필요합니다")
		return
	}
	var req struct {
		OrganizationID string `json:"organization_id"`
		PackageID      string `json:"package_id"`
		Reason         string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil || req.OrganizationID == "" || req.PackageID == "" || strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "organization_id, package_id, 사유가 필요합니다")
		return
	}
	var pkg models.ModelPackage
	if err := s.db.Where("package_id = ?", req.PackageID).First(&pkg).Error; err != nil {
		writeError(w, http.StatusNotFound, "패키지를 찾을 수 없습니다")
		return
	}
	ent := models.ModelPackageEntitlement{
		OrganizationID: req.OrganizationID, PackageID: req.PackageID,
		GrantedBy: getOperatorEmail(r), Reason: req.Reason,
		GrantedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.db.Create(&ent).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: req.OrganizationID, EventType: "cp.model.entitled", ActorType: "admin",
		Action: "entitle_package", ResourceType: "model_package", ResourceID: req.PackageID,
		Details: fmt.Sprintf(`{"reason":"%s"}`, req.Reason), Result: "success",
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, ent)
}

func (s *Server) handleMDEntitleRevoke(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "자격 관리 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct{ Reason string `json:"reason"` }
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "사유가 필요합니다")
		return
	}
	var ent models.ModelPackageEntitlement
	if err := s.db.First(&ent, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "자격을 찾을 수 없습니다")
		return
	}
	s.db.Model(&ent).Updates(map[string]interface{}{"revoked": true, "revoked_at": time.Now().UTC().Format(time.RFC3339)})
	// Revocation mid-rollout: campaign targets stop before activation.
	s.db.Model(&models.ModelCampaignTarget{}).
		Where("organization_id = ? AND observed_state IN ?", ent.OrganizationID,
			[]string{mdEntitled, mdDownloading, mdVerifying, mdScheduled}).
		Updates(map[string]interface{}{"observed_state": mdBlockedRecalled, "reason_code": "entitlement_revoked"})
	s.db.Create(&models.AuditEvent{
		OrganizationID: ent.OrganizationID, EventType: "cp.model.entitlement_revoked", ActorType: "admin",
		Action: "revoke_entitlement", ResourceType: "model_package", ResourceID: ent.PackageID,
		Details: fmt.Sprintf(`{"reason":"%s"}`, req.Reason), Result: "success",
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// handleMDEntitledPackages is the customer-side discovery surface: only
// entitled, non-recalled packages for the caller's organization.
func (s *Server) handleMDEntitledPackages(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var ents []models.ModelPackageEntitlement
	s.db.Where("organization_id = ? AND revoked = ?", orgID, false).Find(&ents)
	ids := make([]string, 0, len(ents))
	for _, e := range ents {
		ids = append(ids, e.PackageID)
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	var pkgs []models.ModelPackage
	s.db.Where("package_id IN ?", ids).Find(&pkgs)
	out := make([]map[string]interface{}, 0, len(pkgs))
	for _, p := range pkgs {
		out = append(out, map[string]interface{}{
			"package_id": p.PackageID, "model_id": p.ModelID, "name_ko": p.NameKo,
			"version": p.Version, "quant_type": p.QuantType,
			"entitlement_class": p.EntitlementClass, "size_hint": p.WeightsMerkleRoot != "",
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------- campaigns ----------

func (s *Server) handleMDCampaignCreate(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "캠페인 관리 권한이 필요합니다")
		return
	}
	var req models.ModelDistributionCampaign
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "캠페인 사유가 필요합니다")
		return
	}
	var pkg models.ModelPackage
	if err := s.db.Where("package_id = ?", req.PackageID).First(&pkg).Error; err != nil {
		writeError(w, http.StatusNotFound, "패키지를 찾을 수 없습니다")
		return
	}
	if req.ManifestDigest == "" {
		req.ManifestDigest = pkg.WeightsMerkleRoot
	}
	if req.TargetsJSON == "" {
		writeError(w, http.StatusBadRequest, "배포 대상이 필요합니다")
		return
	}
	if req.State == "" {
		req.State = "draft"
	}
	if req.MaxConcurrent <= 0 {
		req.MaxConcurrent = 3
	}
	if req.DelegationJSON == "" {
		// Manual approval is the default (PAT-1444).
		req.DelegationJSON = `{"auto":false}`
	}
	req.CreatedBy = getOperatorEmail(r)
	if err := s.db.Create(&req).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mdMaterializeTargets(&req)
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.model.campaign_created", ActorType: "admin",
		Action: "create_model_campaign", ResourceType: "model_campaign", ResourceID: fmt.Sprint(req.ID),
		Details: fmt.Sprintf(`{"package":"%s","reason":"%s"}`, req.PackageID, req.Reason),
		Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, req)
}

// mdMaterializeTargets creates per-target rows with eligibility derived
// from entitlement (PAT-1444 previewable targeting).
func (s *Server) mdMaterializeTargets(c *models.ModelDistributionCampaign) {
	var targets []map[string]interface{}
	if err := json.Unmarshal([]byte(c.TargetsJSON), &targets); err != nil {
		return
	}
	for _, t := range targets {
		orgID, _ := t["organization_id"].(string)
		if orgID == "" {
			continue
		}
		envs, _ := t["environments"].([]interface{})
		var entitled int64
		s.db.Model(&models.ModelPackageEntitlement{}).
			Where("organization_id = ? AND package_id = ? AND revoked = ?", orgID, c.PackageID, false).Count(&entitled)
		envList := make([]string, 0, len(envs))
		for _, e := range envs {
			if es, ok := e.(string); ok {
				envList = append(envList, es)
			}
		}
		if len(envList) == 0 {
			envList = []string{"default"}
		}
		for _, env := range envList {
			state := mdIneligible
			if entitled > 0 {
				state = mdAwaitingApproval
			}
			var existing models.ModelCampaignTarget
			if err := s.db.Where("campaign_id = ? AND organization_id = ? AND environment = ?",
				fmt.Sprint(c.ID), orgID, env).First(&existing).Error; err != nil {
				s.db.Create(&models.ModelCampaignTarget{
					CampaignID: fmt.Sprint(c.ID), OrganizationID: orgID, Environment: env,
					Ring: "canary", DesiredState: "canary", ObservedState: state,
					ReasonCode: map[bool]string{true: "", false: "no_entitlement"}[state == mdAwaitingApproval],
					ApprovalState: "required",
				})
			}
		}
	}
}

// handleMDCampaignPreview shows exact targets and ineligible ones before
// activation.
func (s *Server) handleMDCampaignPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PackageID   string `json:"package_id"`
		TargetsJSON string `json:"targets_json"`
	}
	if err := decodeJSON(r, &req); err != nil || req.PackageID == "" {
		writeError(w, http.StatusBadRequest, "package_id가 필요합니다")
		return
	}
	var targets []map[string]interface{}
	json.Unmarshal([]byte(req.TargetsJSON), &targets)
	eligible, ineligible := []map[string]string{}, []map[string]string{}
	for _, t := range targets {
		orgID, _ := t["organization_id"].(string)
		var entitled int64
		s.db.Model(&models.ModelPackageEntitlement{}).
			Where("organization_id = ? AND package_id = ? AND revoked = ?", orgID, req.PackageID, false).Count(&entitled)
		row := map[string]string{"organization_id": orgID}
		if entitled > 0 {
			eligible = append(eligible, row)
		} else {
			row["reason"] = "no_entitlement"
			ineligible = append(ineligible, row)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"eligible": eligible, "ineligible": ineligible})
}

func (s *Server) handleMDCampaignMutate(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "캠페인 관리 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Action        string `json:"action"` // activate|pause|resume|complete|cancel
		Reason        string `json:"reason"`
		ExpectedEpoch int    `json:"expected_epoch"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "작업과 사유가 필요합니다")
		return
	}
	var c models.ModelDistributionCampaign
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
		// Activating requires every target eligible (or explicitly
		// pruned) — the preview must have been reviewed.
		var ineligible int64
		s.db.Model(&models.ModelCampaignTarget{}).
			Where("campaign_id = ? AND observed_state = ?", fmt.Sprint(c.ID), mdIneligible).Count(&ineligible)
		if ineligible > 0 {
			writeError(w, http.StatusUnprocessableEntity, "비대상 타깃이 있습니다. 대상에서 제외한 뒤 활성화하세요")
			return
		}
		next = "active"
	case "pause":
		next = "paused"
		s.db.Model(&models.ModelCampaignTarget{}).
			Where("campaign_id = ? AND observed_state IN ?", fmt.Sprint(c.ID),
				[]string{mdDownloading, mdVerifying, mdScheduled}).
			Updates(map[string]interface{}{"observed_state": mdPaused, "reason_code": "campaign_paused"})
	case "resume":
		next = "active"
	case "complete":
		next = "completed"
	case "cancel":
		next = "cancelled"
	default:
		writeError(w, http.StatusBadRequest, "알 수 없는 작업입니다")
		return
	}
	res := s.db.Model(&models.ModelDistributionCampaign{}).
		Where("id = ? AND expected_epoch = ?", c.ID, c.ExpectedEpoch).
		Updates(map[string]interface{}{"state": next, "expected_epoch": c.ExpectedEpoch + 1})
	if res.RowsAffected == 0 {
		writeError(w, http.StatusConflict, "동시 변경이 감지되었습니다")
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.model.campaign_" + req.Action, ActorType: "admin",
		Action: req.Action + "_model_campaign", ResourceType: "model_campaign", ResourceID: fmt.Sprint(c.ID),
		Details: fmt.Sprintf(`{"reason":"%s","to":"%s"}`, req.Reason, next), Result: "success",
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": next})
}

// handleMDCampaignRollback restores a verified compatible prior digest —
// a new auditable transition, not a tag change.
func (s *Server) handleMDCampaignRollback(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "캠페인 관리 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Reason        string `json:"reason"`
		RollbackTo    string `json:"rollback_to"` // package id
		ExpectedEpoch int    `json:"expected_epoch"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" || req.RollbackTo == "" {
		writeError(w, http.StatusBadRequest, "롤백 대상과 사유가 필요합니다")
		return
	}
	var to models.ModelPackage
	if err := s.db.Where("package_id = ?", req.RollbackTo).First(&to).Error; err != nil {
		writeError(w, http.StatusBadRequest, "롤백 대상 패키지를 찾을 수 없습니다")
		return
	}
	var c models.ModelDistributionCampaign
	if err := s.db.Where("id = ?", id).First(&c).Error; err != nil {
		writeError(w, http.StatusNotFound, "캠페인을 찾을 수 없습니다")
		return
	}
	if req.ExpectedEpoch != c.ExpectedEpoch {
		writeError(w, http.StatusConflict, "epoch 불일치")
		return
	}
	// Rollback target must be listed as a permitted rollback version.
	if !strings.Contains(c.RollbackVersionsJSON, req.RollbackTo) && c.RollbackVersionsJSON != "" {
		writeError(w, http.StatusUnprocessableEntity, "허용된 롤백 버전이 아닙니다")
		return
	}
	s.db.Model(&models.ModelCampaignTarget{}).
		Where("campaign_id = ? AND observed_state IN ?", fmt.Sprint(c.ID),
			[]string{mdCanary, mdActive, mdLoading, mdStaged}).
		Updates(map[string]interface{}{"observed_state": mdRollbackInProgress, "reason_code": "operator_rollback", "previous_digest": req.RollbackTo})
	s.db.Model(&c).Updates(map[string]interface{}{"expected_epoch": c.ExpectedEpoch + 1})
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.model.campaign_rollback", ActorType: "admin",
		Action: "rollback_model_campaign", ResourceType: "model_campaign", ResourceID: fmt.Sprint(c.ID),
		Details: fmt.Sprintf(`{"to":"%s","reason":"%s"}`, req.RollbackTo, req.Reason), Result: "success",
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "rollback_in_progress", "to": req.RollbackTo})
}

func (s *Server) handleMDCampaignsList(w http.ResponseWriter, r *http.Request) {
	var campaigns []models.ModelDistributionCampaign
	q := s.db
	if v := r.URL.Query().Get("state"); v != "" {
		q = q.Where("state = ?", v)
	}
	q.Order("created_at DESC").Limit(100).Find(&campaigns)
	out := make([]map[string]interface{}, 0, len(campaigns))
	for _, c := range campaigns {
		var targets []models.ModelCampaignTarget
		s.db.Where("campaign_id = ?", fmt.Sprint(c.ID)).Find(&targets)
		dist := map[string]int{}
		for _, t := range targets {
			dist[t.ObservedState]++
		}
		out = append(out, map[string]interface{}{
			"campaign": c, "targets": targets, "distribution": dist,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------- customer-side agent surface ----------

// handleMDRequestLease issues a short-lived artifact transfer lease
// bound to (org, package). Authorization is rechecked every issue.
func (s *Server) handleMDRequestLease(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PackageID string `json:"package_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.PackageID == "" {
		writeError(w, http.StatusBadRequest, "package_id가 필요합니다")
		return
	}
	orgID := getOrgID(r)
	var entitled int64
	s.db.Model(&models.ModelPackageEntitlement{}).
		Where("organization_id = ? AND package_id = ? AND revoked = ?", orgID, req.PackageID, false).Count(&entitled)
	if entitled == 0 {
		writeError(w, http.StatusForbidden, "자격이 없는 패키지입니다")
		return
	}
	b := make([]byte, 18)
	rand.Read(b)
	token := "mdl-" + base64.RawURLEncoding.EncodeToString(b)
	now := time.Now().UTC()
	lease := models.ModelArtifactLease{
		OrganizationID: orgID, PackageID: req.PackageID, Token: token,
		IssuedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(15 * time.Minute).Format(time.RFC3339),
	}
	s.db.Create(&lease)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"lease_token": token, "expires_at": lease.ExpiresAt, "url": "/api/models/distribution/transfer/" + token,
	})
}

// handleMDTransfer serves the artifact through a lease. Expired or
// cross-org leases are rejected — possession is not entitlement.
func (s *Server) handleMDTransfer(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	var lease models.ModelArtifactLease
	if err := s.db.Where("token = ?", token).First(&lease).Error; err != nil {
		writeError(w, http.StatusNotFound, "리스를 찾을 수 없습니다")
		return
	}
	if lease.OrganizationID != getOrgID(r) {
		writeError(w, http.StatusForbidden, "교차 조직 리스 사용이 거부되었습니다")
		return
	}
	exp, err := time.Parse(time.RFC3339, lease.ExpiresAt)
	if err != nil || time.Now().UTC().After(exp) {
		writeError(w, http.StatusForbidden, "만료된 리스입니다")
		return
	}
	var pkg models.ModelPackage
	if err := s.db.Where("package_id = ?", lease.PackageID).First(&pkg).Error; err != nil {
		writeError(w, http.StatusNotFound, "패키지를 찾을 수 없습니다")
		return
	}
	// Manifest-style response: agents verify digests before staging.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"package_id": pkg.PackageID, "manifest_digest": pkg.WeightsMerkleRoot,
		"shards": pkg.WeightsShardsJSON, "container_digest": pkg.ContainerDigest,
		"tokenizer_digest": pkg.TokenizerDigest, "config_digest": pkg.ConfigDigest,
	})
}

// handleMDApprove is the customer administrator's deployment approval —
// required by default; automatic rollout needs an explicit bounded
// delegation on the campaign.
func (s *Server) handleMDApprove(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "배포 승인 권한이 필요합니다")
		return
	}
	var req struct {
		OrganizationID string `json:"organization_id"`
		Environment    string `json:"environment"`
		Approve        bool   `json:"approve"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	campaignID := chi.URLParam(r, "id")
	if campaignID == "" {
		writeError(w, http.StatusBadRequest, "campaign_id가 필요합니다")
		return
	}
	if req.OrganizationID == "" {
		req.OrganizationID = getOrgID(r)
	}
	var t models.ModelCampaignTarget
	if err := s.db.Where("campaign_id = ? AND organization_id = ? AND environment = ?",
		campaignID, req.OrganizationID, req.Environment).First(&t).Error; err != nil {
		writeError(w, http.StatusNotFound, "타깃을 찾을 수 없습니다")
		return
	}
	if t.ObservedState != mdAwaitingApproval {
		writeError(w, http.StatusConflict, "승인 대기 상태가 아닙니다")
		return
	}
	if !req.Approve {
		s.db.Model(&t).Updates(map[string]interface{}{"approval_state": "declined", "reason_code": "customer_declined"})
		writeJSON(w, http.StatusOK, map[string]string{"status": "declined"})
		return
	}
	s.db.Model(&t).Updates(map[string]interface{}{
		"approval_state": "granted", "approved_by": getOperatorEmail(r),
		"observed_state": mdEntitled, "reason_code": "",
	})
	s.db.Create(&models.AuditEvent{
		OrganizationID: req.OrganizationID, EventType: "cp.model.deployment_approved", ActorType: "admin",
		Action: "approve_deployment", ResourceType: "model_campaign_target", ResourceID: fmt.Sprint(t.ID),
		Details: fmt.Sprintf(`{"campaign":"%s","environment":"%s"}`, campaignID, req.Environment),
		Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "granted"})
}

// handleMDAgentReport is the reconciler's observed-state report. Illegal
// transitions are rejected; reports stamp last_contact.
func (s *Server) handleMDAgentReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CampaignID     string `json:"campaign_id"`
		OrganizationID string `json:"organization_id"`
		Environment    string `json:"environment"`
		ObservedState  string `json:"observed_state"`
		ProgressBytes  int64  `json:"progress_bytes"`
		ProgressShards int    `json:"progress_shards"`
		CurrentDigest  string `json:"current_digest"`
		ReasonCode     string `json:"reason_code"`
		AgentReceipt   string `json:"agent_receipt"`
	}
	if err := decodeJSON(r, &req); err != nil || req.CampaignID == "" || req.ObservedState == "" {
		writeError(w, http.StatusBadRequest, "campaign_id와 observed_state가 필요합니다")
		return
	}
	if req.OrganizationID == "" {
		req.OrganizationID = getOrgID(r)
	}
	var t models.ModelCampaignTarget
	if err := s.db.Where("campaign_id = ? AND organization_id = ? AND environment = ?",
		req.CampaignID, req.OrganizationID, req.Environment).First(&t).Error; err != nil {
		writeError(w, http.StatusNotFound, "타깃을 찾을 수 없습니다")
		return
	}
	// Unattended activation requires the campaign's explicit delegation.
	if req.ObservedState == mdActive && t.ObservedState != mdCanary && t.ObservedState != mdLoading {
		var c models.ModelDistributionCampaign
		if err := s.db.Where("id = ?", req.CampaignID).First(&c).Error; err == nil {
			var deleg map[string]interface{}
			json.Unmarshal([]byte(c.DelegationJSON), &deleg)
			if deleg["auto"] != true {
				writeError(w, http.StatusForbidden, "자동 활성화는 명시적 위임이 있는 캠페인에서만 허용됩니다")
				return
			}
		}
	}
	if !mdCanTransition(t.ObservedState, req.ObservedState) && req.ObservedState != mdFailed {
		writeError(w, http.StatusConflict, fmt.Sprintf("불법 전환: %s → %s", t.ObservedState, req.ObservedState))
		return
	}
	s.db.Model(&t).Updates(map[string]interface{}{
		"observed_state": req.ObservedState, "progress_bytes": req.ProgressBytes,
		"progress_shards": req.ProgressShards, "current_digest": req.CurrentDigest,
		"reason_code": req.ReasonCode, "agent_receipt": req.AgentReceipt,
		"last_contact": time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

// handleMDReconcileSweep marks stale targets offline_unknown — silence
// is never success (PAT-1444).
func (s *Server) handleMDReconcileSweep(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "조정 권한이 필요합니다")
		return
	}
	staleBefore := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)
	res := s.db.Model(&models.ModelCampaignTarget{}).
		Where("(last_contact = '' OR last_contact < ?) AND observed_state IN ? AND observed_state != ?",
			staleBefore,
			[]string{mdEntitled, mdScheduled, mdDownloading, mdVerifying, mdStaged, mdLoading, mdCanary, mdActive},
			mdOfflineUnknown).
		Updates(map[string]interface{}{"observed_state": mdOfflineUnknown, "reason_code": "agent_stale"})
	writeJSON(w, http.StatusOK, map[string]interface{}{"marked_stale": res.RowsAffected})
}

// handleMDPromoteGate evaluates canary → stable promotion against the
// objective health gates. Missing or stale evidence fails closed.
func (s *Server) handleMDPromoteGate(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "승격 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var c models.ModelDistributionCampaign
	if err := s.db.Where("id = ?", id).First(&c).Error; err != nil {
		writeError(w, http.StatusNotFound, "캠페인을 찾을 수 없습니다")
		return
	}
	gates := map[string]interface{}{}
	json.Unmarshal([]byte(c.HealthGatesJSON), &gates)
	maxErr, _ := gates["error_rate"].(float64)
	minLoad, _ := gates["load_success"].(float64)
	needObs, _ := gates["observation_minutes"].(float64)
	if len(gates) == 0 {
		maxErr, minLoad, needObs = 0.02, 0.99, 30
	}
	var targets []models.ModelCampaignTarget
	s.db.Where("campaign_id = ? AND observed_state = ?", fmt.Sprint(c.ID), mdCanary).Find(&targets)
	promoted, blocked := 0, 0
	for _, t := range targets {
		var ev models.ModelCampaignHealthEvidence
		err := s.db.Where("campaign_id = ? AND organization_id = ?", fmt.Sprint(c.ID), t.OrganizationID).
			Order("recorded_at DESC").First(&ev).Error
		failClosed := err != nil
		if !failClosed {
			if rec, perr := time.Parse(time.RFC3339, ev.RecordedAt); perr != nil || time.Since(rec) > 2*time.Hour {
				failClosed = true // stale evidence
			} else if ev.ErrorRate > maxErr || ev.LoadSuccess < minLoad || float64(ev.ObservationMinutes) < needObs || !ev.Attested {
				failClosed = true
			}
		}
		if failClosed {
			s.db.Model(&t).Updates(map[string]interface{}{"reason_code": "promotion_gate_failed_closed"})
			blocked++
			continue
		}
		s.db.Model(&t).Updates(map[string]interface{}{"observed_state": mdActive, "reason_code": "promoted_by_health_gate"})
		promoted++
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"promoted": promoted, "blocked": blocked})
}

// handleMDRecall bridges the existing registry recall: campaign targets
// become blocked_recalled immediately.
func (s *Server) handleMDRecall(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "리콜 권한이 필요합니다")
		return
	}
	var req struct {
		PackageID string `json:"package_id"`
		Reason    string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil || req.PackageID == "" || strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "package_id와 사유가 필요합니다")
		return
	}
	res := s.db.Model(&models.ModelCampaignTarget{}).
		Where("campaign_id IN (SELECT id FROM model_distribution_campaigns WHERE package_id = ?) AND observed_state NOT IN ?",
			req.PackageID, []string{mdBlockedRecalled, mdRolledBack}).
		Updates(map[string]interface{}{"observed_state": mdBlockedRecalled, "reason_code": "package_recalled"})
	s.db.Model(&models.ModelPackageEntitlement{}).Where("package_id = ?", req.PackageID).
		Updates(map[string]interface{}{"revoked": true, "revoked_at": time.Now().UTC().Format(time.RFC3339)})
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.model.recalled", ActorType: "admin",
		Action: "recall_package", ResourceType: "model_package", ResourceID: req.PackageID,
		Details: fmt.Sprintf(`{"reason":"%s","targets_blocked":%d}`, req.Reason, res.RowsAffected),
		Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"blocked_targets": res.RowsAffected})
}
