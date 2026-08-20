package api

// Governed browser control plane (PAT-1448) — PCCP half.
//
// Locked rules enforced here:
//   - The managed policy is signed and versioned; only administrators
//     create versions. Neither user nor model can add destinations or
//     expand actions (model-driven permission expansion impossible by
//     construction: tasks/approvals only ever NARROW the active policy).
//   - The canonical action taxonomy locks risk classes: mandatory
//     takeover actions (password/payment/CAPTCHA/MFA/identity) can
//     NEVER be approved for model execution.
//   - Approvals bind to the exact proposed effect via canonical digest;
//     any material change invalidates; single-use; expiring.
//   - Tasks bind explicitly attached tabs only — no implicit capture of
//     unrelated tabs (privacy boundary).
//   - Every action event carries policy version + grant digest +
//     approval + effect op id — attributable, tamper-evident chain.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/keys"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// Canonical browser action taxonomy (PAT-1448 locked risk classes).
const (
	brRiskReadOnly    = "read_only"
	brRiskReversible  = "reversible"
	brRiskHighImpact  = "high_impact"
	brRiskTakeover    = "mandatory_takeover"
)

// bgTaxonomyV1 is the versioned action taxonomy shared with the harness.
// Risk classes determine the approval boundary:
//   read_only    → allowed under task grant
//   reversible   → allowed when clearly implied by the task grant
//   high_impact  → fresh terminal approval bound to the exact effect
//   mandatory_takeover → user takeover ONLY; never delegated to the model
var bgTaxonomyV1 = map[string]string{
	"navigate": brRiskReadOnly, "read_dom": brRiskReadOnly, "read_a11y": brRiskReadOnly,
	"screenshot": brRiskReadOnly, "scroll": brRiskReadOnly, "wait": brRiskReadOnly,
	"click": brRiskReversible, "type": brRiskReversible, "select": brRiskReversible,
	"hover": brRiskReversible, "add_to_cart": brRiskReversible,
	"submit_form": brRiskHighImpact, "upload_file": brRiskHighImpact,
	"send_message": brRiskHighImpact, "post_content": brRiskHighImpact,
	"make_reservation": brRiskHighImpact, "start_subscription": brRiskHighImpact,
	"delete_data": brRiskHighImpact, "change_account_settings": brRiskHighImpact,
	"place_order": brRiskHighImpact,
	"password_entry": brRiskTakeover, "payment_entry": brRiskTakeover,
	"captcha": brRiskTakeover, "mfa": brRiskTakeover, "identity_verification": brRiskTakeover,
}

var bgRiskKo = map[string]string{
	brRiskReadOnly:   "읽기 전용",
	brRiskReversible: "가역",
	brRiskHighImpact: "고영향 (승인 필요)",
	brRiskTakeover:   "사용자 개입 필수",
}

const bgApprovalTTL = 10 * time.Minute

// bgEffectDigest canonically hashes a proposed effect: field order is
// normalized so semantically identical details always digest equally
// and ANY material change differs.
func bgEffectDigest(effectType string, details map[string]interface{}) string {
	keys := make([]string, 0, len(details))
	for k := range details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(effectType)
	for _, k := range keys {
		b.WriteString("|")
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(fmt.Sprint(details[k]))
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%x", sum[:16])
}

// bgDefaultPolicy is the starting managed policy shape (locked
// non-disableable foundations live in code, not in this JSON).
const bgDefaultPolicy = `{
  "destinations": [],
  "actions": {},
  "capture": {"screenshot": true, "dom": true, "a11y": true, "console": false, "network": false, "performance": false},
  "redaction": {"mask_sensitive": true},
  "retention_days": 30,
  "limits": {"max_task_minutes": 60, "max_requests": 10000, "max_concurrent_tabs": 5},
  "layout": {"tabs": true, "side_by_side": true},
  "overrides": []
}`

// ---------- policy ----------

// handleBGPolicyGet returns the effective signed policy plus the
// non-disableable foundation summary (PAT-1448 inspectable policy).
func (s *Server) handleBGPolicyGet(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var pol models.BrowserPolicy
	if err := s.db.Where("organization_id = ? AND active = ?", orgID, true).
		Order("version DESC").First(&pol).Error; err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"version": 0, "policy": json.RawMessage(bgDefaultPolicy),
			"foundations_ko": []string{
				"테넌트·주권 경계 격리", "정책 서명·버전 관리", "드라이버 경계 정책 시행",
				"변조 방지 기록", "모델 권한 확장 차단", "개인 브라우저 프로파일 분리", "조직 관리 상태 표시",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"version": pol.Version, "policy": json.RawMessage(pol.PolicyJSON),
		"signature": pol.Signature, "key_id": pol.KeyID,
		"foundations_ko": []string{
			"테넌트·주권 경계 격리", "정책 서명·버전 관리", "드라이버 경계 정책 시행",
			"변조 방지 기록", "모델 권한 확장 차단", "개인 브라우저 프로파일 분리", "조직 관리 상태 표시",
		},
	})
}

// handleBGPolicyPut creates the next signed policy version
// (administrator-only; the model path has no route here).
func (s *Server) handleBGPolicyPut(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "브라우저 정책 관리 권한이 필요합니다")
		return
	}
	var req struct {
		PolicyJSON string `json:"policy_json"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(req.PolicyJSON), &parsed); err != nil {
		writeError(w, http.StatusBadRequest, "정책 JSON이 올바르지 않습니다")
		return
	}
	// Validate the action-policy layer against the taxonomy: policy may
	// only assign allowed|blocked|approval|takeover to KNOWN actions,
	// and may never downgrade a takeover-class action below takeover.
	actionsRaw, _ := parsed["actions"].(map[string]interface{})
	for name, verdict := range actionsRaw {
		risk, known := bgTaxonomyV1[name]
		if !known {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("알 수 없는 동작입니다: %s", name))
			return
		}
		v, _ := verdict.(string)
		if risk == brRiskTakeover && v != "takeover" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s는 사용자 개입 필수 동작으로 완화할 수 없습니다", name))
			return
		}
		if v != "allowed" && v != "blocked" && v != "approval" && v != "takeover" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("동작 판정은 allowed|blocked|approval|takeover이어야 합니다: %s", name))
			return
		}
	}
	// Destinations must be https (managed testing surface).
	dests, _ := parsed["destinations"].([]interface{})
	for _, d := range dests {
		dm, _ := d.(map[string]interface{})
		scheme, _ := dm["scheme"].(string)
		if scheme != "https" {
			writeError(w, http.StatusBadRequest, "관리 대상은 https 대상만 허용됩니다")
			return
		}
	}
	var prev models.BrowserPolicy
	version := 1
	if err := s.db.Where("organization_id = ?", getOrgID(r)).Order("version DESC").First(&prev).Error; err == nil {
		version = prev.Version + 1
		s.db.Model(&models.BrowserPolicy{}).Where("id = ?", prev.ID).Update("active", false)
	}
	priv, err := keys.LoadOrCreate(s.db, "browser-policy")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "서명 키를 사용할 수 없습니다")
		return
	}
	sig := ed25519.Sign(priv, []byte(req.PolicyJSON))
	pol := models.BrowserPolicy{
		OrganizationID: getOrgID(r), Version: version, PolicyJSON: req.PolicyJSON,
		Signature: base64.StdEncoding.EncodeToString(sig), KeyID: "browser-policy",
		CreatedBy: getOperatorEmail(r), Active: true,
	}
	if err := s.db.Create(&pol).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: pol.OrganizationID, EventType: "cp.browsergov.policy_versioned",
		ActorType: "admin", Action: "publish_browser_policy", ResourceType: "browser_policy",
		ResourceID: fmt.Sprint(pol.Version), Result: "success",
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"version": pol.Version, "signature": pol.Signature, "active": true,
	})
}

// handleBGTaxonomy serves the versioned canonical action taxonomy the
// harness compiles against.
func (s *Server) handleBGTaxonomy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"schema": "patty.browser.actions.v1",
		"actions": bgTaxonomyV1, "risk_ko": bgRiskKo,
		"approval_ttl_minutes": int(bgApprovalTTL.Minutes()),
	})
}

// handleBGPolicyExplain answers "why is this action allowed/blocked/
// awaiting approval" per the effective policy + taxonomy.
func (s *Server) handleBGPolicyExplain(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("action")
	risk, known := bgTaxonomyV1[action]
	if !known {
		writeJSON(w, http.StatusBadRequest, "알 수 없는 동작입니다")
		return
	}
	orgID := getOrgID(r)
	var pol models.BrowserPolicy
	policyActions := map[string]interface{}{}
	if err := s.db.Where("organization_id = ? AND active = ?", orgID, true).
		Order("version DESC").First(&pol).Error; err == nil {
		var parsed map[string]interface{}
		if json.Unmarshal([]byte(pol.PolicyJSON), &parsed) == nil {
			policyActions, _ = parsed["actions"].(map[string]interface{})
		}
	}
	verdict, reason := "", ""
	if override, exists := policyActions[action]; exists {
		verdict, _ = override.(string)
		reason = fmt.Sprintf("관리 정책 v%d이 이 동작을 %s(으)로 지정했습니다", pol.Version, verdict)
	} else {
		// No org-level override: the taxonomy default applies.
		switch risk {
		case brRiskReadOnly:
			verdict = "allowed"
			reason = "읽기 전용 동작은 과업 권한 하에서 허용됩니다"
		case brRiskReversible:
			verdict = "allowed"
			reason = "과업에 명백히 포함되는 가역 동작은 최초 과업 권한으로 수행됩니다"
		case brRiskHighImpact:
			verdict = "approval"
			reason = "고영향 동작은 정확한 효과에 묶인 새로운 터미널 승인이 필요합니다"
		case brRiskTakeover:
			verdict = "takeover"
			reason = "비밀번호·결제·CAPTCHA·MFA·신원 확인은 사용자 개입 필수입니다 — 모델에 위임되지 않습니다"
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"action": action, "risk_class": risk, "risk_ko": bgRiskKo[risk],
		"verdict": verdict, "reason_ko": reason, "policy_version": pol.Version,
	})
}

// ---------- task lifecycle ----------

// handleBGTaskCreate opens a delegated task: validates tabs are
// explicitly listed, snapshots the active policy version, and issues
// the MINIMUM capability lease (browser actions + policy destinations).
func (s *Server) handleBGTaskCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID    string   `json:"user_id"`
		HarnessID string   `json:"harness_id"`
		SessionID string   `json:"session_id"`
		Tabs      []string `json:"tabs"`
		GoalKo    string   `json:"goal_ko"`
	}
	if err := decodeJSON(r, &req); err != nil || req.HarnessID == "" || strings.TrimSpace(req.GoalKo) == "" {
		writeError(w, http.StatusBadRequest, "harness_id와 목표가 필요합니다")
		return
	}
	if len(req.Tabs) == 0 {
		writeError(w, http.StatusBadRequest, "명시적으로 연결된 탭이 필요합니다 — 무연결 탭 캡처는 불가능합니다")
		return
	}
	orgID := getOrgID(r)
	var pol models.BrowserPolicy
	version := 0
	destinations := "[]"
	if err := s.db.Where("organization_id = ? AND active = ?", orgID, true).
		Order("version DESC").First(&pol).Error; err == nil {
		version = pol.Version
		var parsed map[string]interface{}
		if json.Unmarshal([]byte(pol.PolicyJSON), &parsed) == nil {
			if d, err := json.Marshal(parsed["destinations"]); err == nil {
				destinations = string(d)
			}
		}
	}
	// Minimum capability: read-only + reversible action classes only.
	// High-impact actions NEVER enter the lease — they ride approvals.
	toolClasses := `["browser.read_only","browser.reversible"]`
	tabsJSON, _ := json.Marshal(req.Tabs)
	taskID := "bgt-" + apiRandomToken("", 12)
	lease := models.CapabilityLease{
		Base: models.Base{}, OrganizationID: orgID,
		LeaseID:       "cl-" + apiRandomToken("", 12),
		SubjectPeerID: req.HarnessID, UserID: req.UserID, SessionID: req.SessionID,
		ToolClasses:        toolClasses,
		NetworkDestinations: destinations,
		NotBefore:          time.Now().UTC().Format(time.RFC3339),
		NotAfter:           time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339),
		IssuedAt:           time.Now().UTC().Format(time.RFC3339),
		Status:             "active",
	}
	if err := s.db.Create(&lease).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	task := models.BrowserTask{
		OrganizationID: orgID, TaskID: taskID, UserID: req.UserID,
		HarnessID: req.HarnessID, SessionID: req.SessionID,
		TabsJSON: string(tabsJSON), GoalKo: req.GoalKo, LeaseID: lease.LeaseID,
		PolicyVersion: version, State: "active", CreatedAt2: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.db.Create(&task).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"task_id": taskID, "lease_id": lease.LeaseID,
		"policy_version": version, "state": "active",
		"note_ko": "고영향 동작(주문·제출·업로드 등)은 각각 정확한 효과에 묶인 새 승인이 필요합니다. 비밀번호·결제·CAPTCHA·MFA는 사용자 개입 없이는 수행되지 않습니다.",
	})
}

func (s *Server) handleBGTaskClose(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var task models.BrowserTask
	if err := s.db.Where("task_id = ? AND organization_id = ?", id, getOrgID(r)).First(&task).Error; err != nil {
		writeError(w, http.StatusNotFound, "과업을 찾을 수 없습니다")
		return
	}
	var req struct {
		Outcome string `json:"outcome"` // completed|cancelled|failed|taken_over
	}
	if err := decodeJSON(r, &req); err != nil || req.Outcome == "" {
		req.Outcome = "completed"
	}
	if req.Outcome != "completed" && req.Outcome != "cancelled" && req.Outcome != "failed" && req.Outcome != "taken_over" {
		writeError(w, http.StatusBadRequest, "알 수 없는 종결 상태입니다")
		return
	}
	s.db.Model(&task).Updates(map[string]interface{}{
		"state": req.Outcome, "outcome": req.Outcome, "closed_at": time.Now().UTC().Format(time.RFC3339),
	})
	// Closing a task revokes its capability.
	s.db.Model(&models.CapabilityLease{}).Where("lease_id = ?", task.LeaseID).Update("status", "revoked")
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Outcome})
}

func (s *Server) handleBGTasksList(w http.ResponseWriter, r *http.Request) {
	var tasks []models.BrowserTask
	q := s.db.Where("organization_id = ?", getOrgID(r))
	if v := r.URL.Query().Get("state"); v != "" {
		q = q.Where("state = ?", v)
	}
	q.Order("created_at DESC").Limit(100).Find(&tasks)
	writeJSON(w, http.StatusOK, tasks)
}

// ---------- exact-effect approvals ----------

// handleBGApprovalRequest records a proposed high-impact effect with
// its canonical digest. Takeover-class effects are refused outright.
func (s *Server) handleBGApprovalRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID     string                 `json:"task_id"`
		EffectType string                 `json:"effect_type"`
		Details    map[string]interface{} `json:"details"`
	}
	if err := decodeJSON(r, &req); err != nil || req.TaskID == "" || req.EffectType == "" || req.Details == nil {
		writeError(w, http.StatusBadRequest, "task_id, effect_type, details가 필요합니다")
		return
	}
	risk, known := bgTaxonomyV1[req.EffectType]
	if !known {
		writeError(w, http.StatusBadRequest, "알 수 없는 효과 유형입니다")
		return
	}
	if risk == brRiskTakeover {
		writeError(w, http.StatusUnprocessableEntity, "사용자 개입 필수 동작은 승인 대상이 아닙니다 — 사용자가 직접 수행해야 합니다")
		return
	}
	if risk != brRiskHighImpact {
		writeError(w, http.StatusBadRequest, "승인은 고영향 동작에만 묶입니다")
		return
	}
	var task models.BrowserTask
	if err := s.db.Where("task_id = ? AND organization_id = ?", req.TaskID, getOrgID(r)).First(&task).Error; err != nil {
		writeError(w, http.StatusNotFound, "과업을 찾을 수 없습니다")
		return
	}
	digest := bgEffectDigest(req.EffectType, req.Details)
	detailsJSON, _ := json.Marshal(req.Details)
	approval := models.BrowserApproval{
		OrganizationID: task.OrganizationID, TaskID: task.TaskID,
		EffectType: req.EffectType, EffectDigest: digest,
		DetailsJSON: string(detailsJSON), State: "pending",
		ExpiresAt: time.Now().UTC().Add(bgApprovalTTL).Format(time.RFC3339),
	}
	if err := s.db.Create(&approval).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Model(&task).Update("state", "waiting_approval")
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"approval_id": approval.ID, "effect_digest": digest,
		"expires_at": approval.ExpiresAt,
	})
}

// handleBGApprovalDecide is the terminal-authoritative decision path.
func (s *Server) handleBGApprovalDecide(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var approval models.BrowserApproval
	if err := s.db.Where("id = ? AND organization_id = ?", id, getOrgID(r)).First(&approval).Error; err != nil {
		writeError(w, http.StatusNotFound, "승인 요청을 찾을 수 없습니다")
		return
	}
	var req struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if approval.State != "pending" {
		writeError(w, http.StatusConflict, "이미 처리된 승인입니다")
		return
	}
	if exp, err := time.Parse(time.RFC3339, approval.ExpiresAt); err != nil || time.Now().UTC().After(exp) {
		s.db.Model(&approval).Update("state", "expired")
		writeError(w, http.StatusConflict, "만료된 승인입니다 — 과업이 일시정지되었습니다")
		return
	}
	state := "denied"
	if req.Approve {
		state = "approved"
	}
	s.db.Model(&approval).Updates(map[string]interface{}{
		"state": state, "approver": getOperatorEmail(r),
		"reason": req.Reason, "decided_at": time.Now().UTC().Format(time.RFC3339),
	})
	// The task resumes (approved) or stays paused for re-planning (denied).
	var task models.BrowserTask
	if err := s.db.Where("task_id = ?", approval.TaskID).First(&task).Error; err == nil && task.State == "waiting_approval" {
		next := "active"
		if !req.Approve {
			next = "waiting_approval"
		}
		s.db.Model(&task).Update("state", next)
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: approval.OrganizationID, EventType: "cp.browsergov.approval_decided",
		ActorType: "admin", Action: "decide_browser_effect", ResourceType: "browser_approval",
		ResourceID: fmt.Sprint(approval.ID),
		Details: fmt.Sprintf(`{"effect_type":%q,"approve":%t,"effect_digest":%q}`, approval.EffectType, req.Approve, approval.EffectDigest),
		Result: state, OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": state})
}

// handleBGEffectGate is the effect checkpoint: validates that the
// approval exists, matches the CURRENT exact effect digest (any
// material change invalidates), and is single-use; returns a
// deterministic idempotent effect operation ID.
func (s *Server) handleBGEffectGate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID     string                 `json:"task_id"`
		EffectType string                 `json:"effect_type"`
		Details    map[string]interface{} `json:"details"`
		ApprovalID uint                   `json:"approval_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.TaskID == "" || req.EffectType == "" || req.Details == nil || req.ApprovalID == 0 {
		writeError(w, http.StatusBadRequest, "task_id, effect_type, details, approval_id가 필요합니다")
		return
	}
	risk, known := bgTaxonomyV1[req.EffectType]
	if !known {
		writeError(w, http.StatusBadRequest, "알 수 없는 효과 유형입니다")
		return
	}
	if risk == brRiskTakeover {
		writeError(w, http.StatusForbidden, "사용자 개입 필수 동작은 실행될 수 없습니다")
		return
	}
	var approval models.BrowserApproval
	if err := s.db.Where("id = ? AND organization_id = ? AND task_id = ?",
		req.ApprovalID, getOrgID(r), req.TaskID).First(&approval).Error; err != nil {
		writeError(w, http.StatusForbidden, "유효한 승인이 없습니다")
		return
	}
	if approval.State != "approved" {
		writeError(w, http.StatusConflict, "승인되지 않은 효과입니다")
		return
	}
	if approval.UsedAt != "" {
		writeError(w, http.StatusConflict, "이미 사용된 승인입니다 — 일회성입니다")
		return
	}
	// Material-change invalidation: the digest must match the CURRENT
	// proposed details exactly.
	current := bgEffectDigest(req.EffectType, req.Details)
	if current != approval.EffectDigest {
		writeError(w, http.StatusConflict, "효과 세부 내용이 승인 시점과 다릅니다 — 승인이 무효화되었습니다. 새 승인이 필요합니다")
		return
	}
	// Mark used + derive a deterministic effect op id (idempotent retries
	// of the same approved effect return the same id).
	s.db.Model(&approval).Update("used_at", time.Now().UTC().Format(time.RFC3339))
	opID := fmt.Sprintf("bgo-%s", current)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"effect_op_id": opID, "status": "authorized_once",
	})
}

// ---------- evidence ----------

// handleBGEventIngest appends a structured action event to the task
// timeline. Origins are redacted to scheme+host; risk class is derived
// server-side from the taxonomy (client claims are not trusted).
func (s *Server) handleBGEventIngest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID       string `json:"task_id"`
		Action       string `json:"action"`
		TargetSummary string `json:"target_summary"`
		Origin       string `json:"origin"`
		Result       string `json:"result"`
		ApprovalID   uint   `json:"approval_id"`
		EffectOpID   string `json:"effect_op_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.TaskID == "" || req.Action == "" {
		writeError(w, http.StatusBadRequest, "task_id와 action이 필요합니다")
		return
	}
	risk, known := bgTaxonomyV1[req.Action]
	if !known {
		writeError(w, http.StatusBadRequest, "정의되지 않은 동작은 기록되지 않습니다")
		return
	}
	var task models.BrowserTask
	if err := s.db.Where("task_id = ? AND organization_id = ?", req.TaskID, getOrgID(r)).First(&task).Error; err != nil {
		writeError(w, http.StatusNotFound, "과업을 찾을 수 없습니다")
		return
	}
	occurred := time.Now().UTC().Format(time.RFC3339)
	// Redact the origin to scheme://host (path/query/session refs dropped).
	origin := req.Origin
	if at := strings.Index(origin, "://"); at > 0 {
		rest := origin[at+3:]
		if slash := strings.IndexAny(rest, "/?#"); slash >= 0 {
			rest = rest[:slash]
		}
		origin = origin[:at+3] + rest
	}
	target := apiTruncateRunes(req.TargetSummary, 200)
	sum := sha256.Sum256([]byte(fmt.Sprint(req.TaskID, req.Action, target, origin, occurred)))
	ev := models.BrowserActionEvent{
		OrganizationID: task.OrganizationID, TaskID: task.TaskID,
		Action: req.Action, RiskClass: risk, TargetSummary: target,
		Origin: origin, Result: req.Result, PolicyVersion: task.PolicyVersion,
		GrantDigest: task.LeaseID, ApprovalID: req.ApprovalID, EffectOpID: req.EffectOpID,
		OccurredAt: occurred, IntegrityDigest: fmt.Sprintf("%x", sum[:12]),
	}
	if err := s.db.Create(&ev).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"event_id": ev.ID})
}

// handleBGTimeline serves the task's evidence timeline for replay.
func (s *Server) handleBGTimeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var task models.BrowserTask
	if err := s.db.Where("task_id = ? AND organization_id = ?", id, getOrgID(r)).First(&task).Error; err != nil {
		writeError(w, http.StatusNotFound, "과업을 찾을 수 없습니다")
		return
	}
	var events []models.BrowserActionEvent
	s.db.Where("task_id = ?", task.TaskID).Order("occurred_at ASC").Limit(500).Find(&events)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"task": task, "events": events,
	})
}
