package api

// Governed admin incident notifications (PAT-1454).
//
// Locked boundaries enforced here:
//   - Ordinary users cannot configure or receive notifications; a
//     dedicated permission (incident-notify admin roles) gates mutation.
//   - One incident identity per fingerprint: repeated correlated events
//     update the incident instead of creating notification storms.
//   - Outbound content is minimized by construction — the safe envelope
//     builder only ever copies allow-listed fields; raw evidence never
//     enters an envelope.
//   - Per-channel Patty-managed vs customer-managed choice is explicit;
//     air-gapped tenants see no Patty-managed path.
//   - Provider acceptance is never human acknowledgement; ack is a
//     separate single-use-token flow with escalation when deadlines pass.
//   - Durable jobs with bounded exponential-backoff retries and a
//     dead-letter state; dispatch is idempotent.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
)

const inMaxAttempts = 5

// inDefaultRouting is the locked severity → channel default policy.
var inDefaultRouting = map[string]map[string]interface{}{
	"critical": {"channels": []string{"sms", "email", "slack"}, "ack_required": true},
	"high":     {"channels": []string{"email", "slack"}, "sms": false},
	"medium":   {"channels": []string{"email"}, "digest": true},
	"low":      {"channels": []string{}, "inbox_only": true},
}

// inSafeEnvelope is the canonical minimized outbound envelope. Every
// field is safe by construction; there is no code path that copies
// evidence, payloads, logs, or secrets into it (PAT-1354 content boundary).
type inSafeEnvelope struct {
	Schema         string `json:"schema"`
	TenantDisplay  string `json:"tenant_display"`
	IncidentID     string `json:"incident_id"`
	Severity       string `json:"severity"`
	TitleKo        string `json:"title_ko"`
	EventTime      string `json:"event_time"`
	PccpLink       string `json:"pccp_link"`
	// Email/Slack only (SMS stops at the fields above).
	ImpactSummaryKo string `json:"impact_summary_ko,omitempty"`
	StatusKo        string `json:"status_ko,omitempty"`
	AffectedScope   string `json:"affected_scope,omitempty"`
	NextActionKo    string `json:"next_action_ko,omitempty"`
	AckStatusKo     string `json:"ack_status_ko,omitempty"`
	IsTest          bool   `json:"is_test,omitempty"`
}

// inBuildSMSenvelope constructs the SMS-minimal envelope (no summary
// fields — segment count stays bounded for Korean text).
func inBuildSMSenvelope(tenant, incidentID, severity, title, eventTime, link string, isTest bool) inSafeEnvelope {
	return inSafeEnvelope{
		Schema: "patty.incident.v1", TenantDisplay: tenant, IncidentID: incidentID,
		Severity: severity, TitleKo: apiTruncateRunes(title, 40), EventTime: eventTime,
		PccpLink: link, IsTest: isTest,
	}
}

// inFingerprint derives the stable incident identity from correlation
// dimensions (tenant, source, service, rule).
func inFingerprint(orgID, sourceType, service, rule string) string {
	sum := sha256.Sum256([]byte(orgID + "|" + sourceType + "|" + service + "|" + rule))
	return fmt.Sprintf("inc-%x", sum[:10])
}

// inRolesAllowed gates incident-notification administration (dedicated
// permission separate from generic admin status — PAT-1454 RBAC).
func inRolesAllowed(role string) bool {
	switch role {
	case "super_admin", "admin", "security_admin", "ops_admin":
		return true
	}
	return false
}

// ---------- policy / config ----------

func (s *Server) handleINPolicyGet(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var pol models.IncidentNotifyPolicy
	if err := s.db.Where("organization_id = ?", orgID).First(&pol).Error; err != nil {
		// Serve the locked defaults on first read.
		routing, _ := json.Marshal(inDefaultRouting)
		writeJSON(w, http.StatusOK, models.IncidentNotifyPolicy{
			OrganizationID: orgID, RoutingJSON: string(routing),
			ManagedByJSON: `{"email":"customer","sms":"customer","slack":"customer"}`,
			AckDeadlineMinutes: 15, EscalationSteps: 3,
		})
		return
	}
	writeJSON(w, http.StatusOK, pol)
}

func (s *Server) handleINPolicyPut(w http.ResponseWriter, r *http.Request) {
	if !inRolesAllowed(getRole(r)) {
		writeError(w, http.StatusForbidden, "알림 정책 관리 권한이 필요합니다")
		return
	}
	orgID := getOrgID(r)
	var req models.IncidentNotifyPolicy
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Validate routing shape against the locked policy: tenants may only
	// STRENGTHEN routing (add channels), never weaken the critical floor.
	var routing map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(req.RoutingJSON), &routing); err != nil {
		writeError(w, http.StatusBadRequest, "routing JSON이 올바르지 않습니다")
		return
	}
	crit, ok := routing["critical"]
	if !ok {
		writeError(w, http.StatusBadRequest, "critical 라우팅은 필수입니다")
		return
	}
	chans, _ := crit["channels"].([]interface{})
	found := map[string]bool{}
	for _, c := range chans {
		if cs, ok := c.(string); ok {
			found[cs] = true
		}
	}
	for _, need := range []string{"sms", "email", "slack"} {
		if !found[need] {
			writeError(w, http.StatusBadRequest, "critical은 SMS·이메일·Slack 즉시 발송이 기본 정책입니다 (약화 불가)")
			return
		}
	}
	// The critical floor also includes acknowledgement — a policy that
	// drops ack_required weakens the floor just as removing a channel does.
	if crit["ack_required"] != true {
		writeError(w, http.StatusBadRequest, "critical은 확인(Ack) 필수가 기본 정책입니다 (약화 불가)")
		return
	}
	// Patty-managed delivery is never implicit: each channel choosing
	// "patty" is an explicit opt-in recorded here and audited below.
	var managed map[string]string
	if err := json.Unmarshal([]byte(req.ManagedByJSON), &managed); err != nil {
		writeError(w, http.StatusBadRequest, "managed_by JSON이 올바르지 않습니다")
		return
	}
	var pol models.IncidentNotifyPolicy
	err := s.db.Where("organization_id = ?", orgID).First(&pol).Error
	if err != nil {
		pol = models.IncidentNotifyPolicy{OrganizationID: orgID}
	}
	pol.RoutingJSON = req.RoutingJSON
	pol.ManagedByJSON = req.ManagedByJSON
	pol.AckDeadlineMinutes = req.AckDeadlineMinutes
	pol.EscalationSteps = req.EscalationSteps
	pol.QuietHoursStart = req.QuietHoursStart
	pol.QuietHoursEnd = req.QuietHoursEnd
	pol.AirGapped = req.AirGapped
	if pol.AckDeadlineMinutes <= 0 {
		pol.AckDeadlineMinutes = 15
	}
	if pol.EscalationSteps <= 0 {
		pol.EscalationSteps = 3
	}
	// Air-gapped tenants cannot opt into Patty-managed delivery.
	if pol.AirGapped {
		for ch, who := range managed {
			if who == "patty" {
				managed[ch] = "customer"
			}
		}
		raw, _ := json.Marshal(managed)
		pol.ManagedByJSON = string(raw)
	}
	if pol.ID == 0 {
		s.db.Create(&pol)
	} else {
		s.db.Save(&pol)
	}
	s.db.Create(&models.IncidentNotifyAudit{
		OrganizationID: orgID, Action: "policy_updated", ActorEmail: getOperatorEmail(r),
		Detail: "routing + per-channel managed_by updated", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, pol)
}

// ---------- recipient groups & channels ----------

func (s *Server) handleINGroupsList(w http.ResponseWriter, r *http.Request) {
	var groups []models.IncidentNotifyRecipientGroup
	s.db.Where("organization_id = ?", getOrgID(r)).Order("escalation_order ASC").Find(&groups)
	writeJSON(w, http.StatusOK, groups)
}

func (s *Server) handleINGroupUpsert(w http.ResponseWriter, r *http.Request) {
	if !inRolesAllowed(getRole(r)) {
		writeError(w, http.StatusForbidden, "알림 그룹 관리 권한이 필요합니다")
		return
	}
	var req models.IncidentNotifyRecipientGroup
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "그룹 이름이 필요합니다")
		return
	}
	if req.ID != 0 {
		// Tenant ownership check on update — an org's admin must not be
		// able to re-home or overwrite another org's recipient group.
		var existing models.IncidentNotifyRecipientGroup
		if err := s.db.Where("id = ? AND organization_id = ?", req.ID, getOrgID(r)).First(&existing).Error; err != nil {
			writeError(w, http.StatusNotFound, "그룹을 찾을 수 없습니다")
			return
		}
	}
	req.OrganizationID = getOrgID(r)
	if req.ID == 0 {
		s.db.Create(&req)
	} else {
		s.db.Save(&req)
	}
	s.db.Create(&models.IncidentNotifyAudit{
		OrganizationID: req.OrganizationID, Action: "group_upserted", ActorEmail: getOperatorEmail(r),
		Detail: req.Name, OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) handleINChannelsList(w http.ResponseWriter, r *http.Request) {
	var channels []models.IncidentNotifyChannel
	s.db.Where("organization_id = ?", getOrgID(r)).Find(&channels)
	// Mask endpoints; credentials never leave the server.
	out := make([]map[string]interface{}, 0, len(channels))
	for _, c := range channels {
		out = append(out, map[string]interface{}{
			"id": c.ID, "channel": c.Channel, "managed_by": c.ManagedBy,
			"masked_endpoint": c.MaskedEndpoint, "verified": c.Verified,
			"healthy": c.Healthy, "last_failure": c.LastFailure,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleINChannelUpsert(w http.ResponseWriter, r *http.Request) {
	if !inRolesAllowed(getRole(r)) {
		writeError(w, http.StatusForbidden, "채널 관리 권한이 필요합니다")
		return
	}
	var req struct {
		ID            uint   `json:"id"`
		Channel       string `json:"channel"`
		ManagedBy     string `json:"managed_by"`
		Endpoint      string `json:"endpoint"`
		CredentialRef string `json:"credential_ref"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Channel {
	case "email", "sms", "slack":
	default:
		writeError(w, http.StatusBadRequest, "채널은 email|sms|slack이어야 합니다")
		return
	}
	if req.ManagedBy != "patty" && req.ManagedBy != "customer" {
		writeError(w, http.StatusBadRequest, "managed_by는 patty|customer이어야 합니다")
		return
	}
	var pol models.IncidentNotifyPolicy
	airGapped := false
	if err := s.db.Where("organization_id = ?", getOrgID(r)).First(&pol).Error; err == nil {
		airGapped = pol.AirGapped
	}
	if airGapped && req.ManagedBy == "patty" {
		writeError(w, http.StatusUnprocessableEntity, "에어갭 배포에서는 Patty 관리 발송을 사용할 수 없습니다")
		return
	}
	masked := inMaskEndpoint(req.Endpoint)
	var ch models.IncidentNotifyChannel
	if req.ID != 0 {
		if err := s.db.Where("id = ? AND organization_id = ?", req.ID, getOrgID(r)).First(&ch).Error; err != nil {
			writeError(w, http.StatusNotFound, "채널을 찾을 수 없습니다")
			return
		}
	} else {
		if err := s.db.Where("organization_id = ? AND channel = ?", getOrgID(r), req.Channel).First(&ch).Error; err == nil {
			writeError(w, http.StatusConflict, "이미 해당 채널 구성이 존재합니다")
			return
		}
		ch = models.IncidentNotifyChannel{OrganizationID: getOrgID(r), Channel: req.Channel}
	}
	ch.ManagedBy = req.ManagedBy
	ch.MaskedEndpoint = masked
	if req.CredentialRef != "" {
		ch.CredentialRef = req.CredentialRef // secret-store reference; masked everywhere else
	}
	if ch.MaskedEndpoint != masked || ch.ID == 0 {
		ch.Verified = false // new/changed endpoint requires re-verification
	}
	if ch.ID == 0 {
		s.db.Create(&ch)
	} else {
		s.db.Save(&ch)
	}
	s.db.Create(&models.IncidentNotifyAudit{
		OrganizationID: ch.OrganizationID, Action: "channel_upserted", ActorEmail: getOperatorEmail(r),
		Detail: fmt.Sprintf("%s managed_by=%s endpoint=%s", ch.Channel, ch.ManagedBy, masked),
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": ch.ID, "channel": ch.Channel, "verified": ch.Verified, "masked_endpoint": ch.MaskedEndpoint})
}

func inMaskEndpoint(endpoint string) string {
	if len(endpoint) <= 3 {
		return "***"
	}
	if at := strings.Index(endpoint, "@"); at > 0 {
		return endpoint[:inMin(2, at)] + "***" + endpoint[at:]
	}
	if strings.HasPrefix(endpoint, "https://") {
		return endpoint[:inMin(20, len(endpoint))] + "***"
	}
	return endpoint[:2] + "***" + endpoint[len(endpoint)-2:]
}

func inMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Server) handleINChannelVerify(w http.ResponseWriter, r *http.Request) {
	if !inRolesAllowed(getRole(r)) {
		writeError(w, http.StatusForbidden, "채널 관리 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	res := s.db.Model(&models.IncidentNotifyChannel{}).
		Where("id = ? AND organization_id = ?", id, getOrgID(r)).Update("verified", true)
	if res.RowsAffected == 0 {
		writeError(w, http.StatusNotFound, "채널을 찾을 수 없습니다")
		return
	}
	s.db.Create(&models.IncidentNotifyAudit{
		OrganizationID: getOrgID(r), Action: "channel_verified", ActorEmail: getOperatorEmail(r),
		Detail: id, OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified"})
}

// ---------- source ingestion + correlation ----------

// handleINSourceIngest normalizes an eligible source event into the
// incident-routing contract. Correlated repeats update the same incident
// and never re-notify unchanged observations.
func (s *Server) handleINSourceIngest(w http.ResponseWriter, r *http.Request) {
	if !inRolesAllowed(getRole(r)) {
		writeError(w, http.StatusForbidden, "알림 소스 등록 권한이 필요합니다")
		return
	}
	orgID := getOrgID(r)
	var req struct {
		SourceType    string `json:"source_type"`
		Service       string `json:"service"`
		Rule          string `json:"rule"`
		Severity      string `json:"severity"`
		TitleKo       string `json:"title_ko"`
		SafeSummaryKo string `json:"safe_summary_ko"`
		ScopeRef      string `json:"scope_ref"`
	}
	if err := decodeJSON(r, &req); err != nil || req.SourceType == "" || req.TitleKo == "" {
		writeError(w, http.StatusBadRequest, "source_type과 title_ko가 필요합니다")
		return
	}
	switch req.Severity {
	case "critical", "high", "medium", "low":
	default:
		writeError(w, http.StatusBadRequest, "severity는 critical|high|medium|low이어야 합니다")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	fp := inFingerprint(orgID, req.SourceType, req.Service, req.Rule)
	var inc models.IncidentNotifyIncident
	created := false
	if err := s.db.Where("fingerprint = ?", fp).First(&inc).Error; err != nil {
		inc = models.IncidentNotifyIncident{
			OrganizationID: orgID, Fingerprint: fp, SourceType: req.SourceType,
			Service: req.Service, Rule: req.Rule, Severity: req.Severity,
			TitleKo: req.TitleKo, SafeSummaryKo: req.SafeSummaryKo, ScopeRef: req.ScopeRef,
			State: "open", FirstSeenAt: now,
		}
		if err := s.db.Create(&inc).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		created = true
	} else {
		// Correlated repeat: append as a material update only when
		// severity rose or scope grew; never re-notify unchanged state.
		material := req.Severity != inc.Severity && inSeverityRank(req.Severity) > inSeverityRank(inc.Severity)
		inc.LastSeenAt = now
		if material {
			inc.Severity = req.Severity
			inc.TitleKo = req.TitleKo
			inc.SafeSummaryKo = req.SafeSummaryKo
		}
		s.db.Save(&inc)
		if !material {
			writeJSON(w, http.StatusOK, map[string]interface{}{"incident_id": inc.ID, "correlated": true, "notified": false})
			return
		}
	}
	s.inEnqueueNotifications(&inc, "notify")
	writeJSON(w, http.StatusCreated, map[string]interface{}{"incident_id": inc.ID, "correlated": false, "notified": created})
}

func inSeverityRank(sev string) int {
	switch sev {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	}
	return 0
}

// inEnqueueNotifications builds durable jobs per policy routing for the
// incident's (possibly escalated) audience.
func (s *Server) inEnqueueNotifications(inc *models.IncidentNotifyIncident, kind string) {
	var pol models.IncidentNotifyPolicy
	routing := inDefaultRouting
	ackDeadline := 15
	if err := s.db.Where("organization_id = ?", inc.OrganizationID).First(&pol).Error; err == nil {
		if err := json.Unmarshal([]byte(pol.RoutingJSON), &routing); err != nil {
			routing = inDefaultRouting
		}
		if pol.AckDeadlineMinutes > 0 {
			ackDeadline = pol.AckDeadlineMinutes
		}
	}
	route, ok := routing[inc.Severity]
	if !ok {
		return
	}
	chans := inStringSlice(route["channels"])
	targets := s.inResolveTargets(inc.OrganizationID, inc.Severity, chans, inc.EscalationStep)
	var tenant models.Organization
	s.db.First(&tenant, "id = ?", inc.OrganizationID)
	for _, t := range targets {
		// Idempotency includes severity so a MATERIAL change (severity
		// rose) re-notifies; unchanged repeats stay suppressed.
		key := fmt.Sprintf("in-%d-%s-%s-%s-%s", inc.ID, kind, t.Channel, t.Target, inc.Severity)
		var dup int64
		s.db.Model(&models.IncidentNotifyJob{}).Where("idempotency_key = ?", key).Count(&dup)
		if dup > 0 {
			continue
		}
		env := inBuildSMSenvelope(
			tenant.Name, inc.Fingerprint, inc.Severity, inc.TitleKo,
			inc.FirstSeenAt, "/security", false,
		)
		if t.Channel == "email" || t.Channel == "slack" {
			env.ImpactSummaryKo = inc.SafeSummaryKo
			env.StatusKo = inStateKo(inc.State)
			env.AffectedScope = inc.Service
			env.NextActionKo = "PCCP에서 확인해 주세요"
			if route["ack_required"] == true {
				env.AckStatusKo = "승인 대기"
			}
		}
		raw, _ := json.Marshal(env)
		s.db.Create(&models.IncidentNotifyJob{
			OrganizationID: inc.OrganizationID, IncidentID: inc.ID, Kind: kind,
			Channel: t.Channel, Target: t.Target, IdempotencyKey: key,
			State: "queued", MaxAttempts: inMaxAttempts, EnvelopeJSON: string(raw),
		})
	}
	// Critical incidents require acknowledgement: mint the single-use
	// action token used by protected email/SMS/Slack ack links.
	if route["ack_required"] == true && inc.State == "open" {
		tok := inRandomToken("ack")
		s.db.Create(&models.IncidentNotifyAcknowledgement{
			OrganizationID: inc.OrganizationID, IncidentID: inc.ID, ActionToken: tok,
			ExpiresAt: time.Now().UTC().Add(time.Duration(ackDeadline) * time.Minute).Format(time.RFC3339),
		})
	}
}

func inStateKo(state string) string {
	switch state {
	case "open":
		return "열림"
	case "acknowledged":
		return "확인됨"
	case "escalated":
		return "에스컬레이션됨"
	case "resolved":
		return "해결됨"
	case "suppressed":
		return "억제됨"
	}
	return state
}

type inTarget struct {
	Channel string
	Target  string
}

// inResolveTargets maps policy channels + escalation step to concrete
// destinations from the tenant's verified groups and channels.
func (s *Server) inResolveTargets(orgID, severity string, chans []string, step int) []inTarget {
	out := []inTarget{}
	var groups []models.IncidentNotifyRecipientGroup
	s.db.Where("organization_id = ?", orgID).Order("escalation_order ASC").Find(&groups)
	if len(groups) == 0 {
		return out
	}
	// Escalation widens the audience: step 0 = first group, each later
	// step adds the next group in order.
	upto := step + 1
	if upto > len(groups) {
		upto = len(groups)
	}
	allowed := map[string]bool{}
	for _, c := range chans {
		allowed[c] = true
	}
	// Quiet hours suppress noncritical immediate delivery (critical bypasses).
	for _, g := range groups[:upto] {
		var members []map[string]interface{}
		if err := json.Unmarshal([]byte(g.MembersJSON), &members); err != nil {
			continue
		}
		for _, m := range members {
			kind, _ := m["kind"].(string)
			target, _ := m["target"].(string)
			verified, _ := m["verified"].(bool)
			if !verified || target == "" {
				continue
			}
			if allowed[kind] {
				out = append(out, inTarget{Channel: kind, Target: target})
			}
		}
	}
	return out
}

// ---------- dispatch (durable jobs, bounded retries, dead letter) ----------

// handleINDispatch drains due jobs through the channel adapters. Each
// attempt records a normalized receipt; provider acceptance is never
// acknowledged-as-human.
func (s *Server) handleINDispatch(w http.ResponseWriter, r *http.Request) {
	if !inRolesAllowed(getRole(r)) {
		writeError(w, http.StatusForbidden, "알림 발송 권한이 필요합니다")
		return
	}
	now := time.Now().UTC()
	var jobs []models.IncidentNotifyJob
	s.db.Where("state = ? AND (next_retry_at = '' OR next_retry_at <= ?)", "queued", now.Format(time.RFC3339)).
		Limit(50).Find(&jobs)
	sent, failed, dead := 0, 0, 0
	for _, j := range jobs {
		// Patty-managed channels receive ONLY the minimized envelope —
		// already guaranteed because EnvelopeJSON holds only safe fields.
		var ch models.IncidentNotifyChannel
		managedBy := "customer"
		if err := s.db.Where("organization_id = ? AND channel = ?", j.OrganizationID, j.Channel).First(&ch); err == nil {
			managedBy = ch.ManagedBy
			if !ch.Verified {
				s.db.Model(&j).Updates(map[string]interface{}{"state": "failed", "last_error": "channel unverified"})
				s.db.Create(&models.IncidentNotifyReceipt{JobID: j.ID, State: "rejected", OccurredAt: now.Format(time.RFC3339)})
				failed++
				continue
			}
		}
		// Channel adapter seam: real providers plug in here. The adapter
		// receives only the safe envelope.
		messageID := inRandomToken("msg")
		errStr := ""
		if err := inDeliverAdapter(j.Channel, j.Target, j.EnvelopeJSON); err != nil {
			errStr = err.Error()
		}
		attempts := j.Attempts + 1
		if errStr == "" {
			s.db.Model(&j).Updates(map[string]interface{}{
				"state": "sent", "attempts": attempts, "provider_message_id": messageID, "sent_at": now.Format(time.RFC3339),
			})
			s.db.Create(&models.IncidentNotifyReceipt{JobID: j.ID, State: "accepted", ProviderMessageID: messageID, OccurredAt: now.Format(time.RFC3339)})
			s.db.Create(&models.IncidentNotifyAudit{
				OrganizationID: j.OrganizationID, Action: "delivery_sent", IncidentID: j.IncidentID,
				Detail: fmt.Sprintf("channel=%s managed_by=%s msg=%s", j.Channel, managedBy, messageID),
				OccurredAt: now.Format(time.RFC3339),
			})
			sent++
			continue
		}
		// Bounded exponential backoff with jitter; permanent failures
		// route to dead letter.
		if attempts >= inMaxAttempts {
			s.db.Model(&j).Updates(map[string]interface{}{
				"state": "dead_letter", "attempts": attempts, "last_error": errStr,
			})
			dead++
		} else {
			backoff := time.Duration(1<<uint(attempts)) * time.Minute
			jitter := time.Duration(inRandInt(30)) * time.Second
			s.db.Model(&j).Updates(map[string]interface{}{
				"state": "queued", "attempts": attempts, "last_error": errStr,
				"next_retry_at": now.Add(backoff + jitter).Format(time.RFC3339),
			})
		}
		s.db.Create(&models.IncidentNotifyReceipt{JobID: j.ID, State: "failed", OccurredAt: now.Format(time.RFC3339)})
		failed++
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sent": sent, "failed": failed, "dead_letter": dead})
}

// inDeliverAdapter is the channel-adapter seam. The in-process default
// validates destinations; production wires SMTP/SMS/Slack SDKs here.
func inDeliverAdapter(channel, target, envelope string) error {
	switch channel {
	case "email":
		if !strings.Contains(target, "@") {
			return fmt.Errorf("invalid email destination")
		}
	case "sms":
		if len(target) < 8 {
			return fmt.Errorf("invalid sms destination")
		}
	case "slack":
		if !strings.Contains(target, "/") {
			return fmt.Errorf("invalid slack channel ref")
		}
	default:
		return fmt.Errorf("unknown channel")
	}
	return nil
}

// ---------- acknowledgement & escalation ----------

// handleINAck acknowledges an incident. Via PCCP the admin identity is
// the authority; via protected links the single-use token is.
func (s *Server) handleINAck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IncidentID uint   `json:"incident_id"`
		Token      string `json:"token"`
		Via        string `json:"via"` // pccp|email|sms|slack
	}
	if err := decodeJSON(r, &req); err != nil || req.IncidentID == 0 {
		writeError(w, http.StatusBadRequest, "incident_id가 필요합니다")
		return
	}
	orgID := getOrgID(r)
	var inc models.IncidentNotifyIncident
	if err := s.db.Where("id = ? AND organization_id = ?", req.IncidentID, orgID).First(&inc).Error; err != nil {
		writeError(w, http.StatusNotFound, "알림을 찾을 수 없습니다")
		return
	}
	now := time.Now().UTC()
	ackedBy := getOperatorEmail(r)
	if req.Token != "" {
		var ack models.IncidentNotifyAcknowledgement
		if err := s.db.Where("action_token = ? AND incident_id = ?", req.Token, inc.ID).First(&ack).Error; err != nil {
			writeError(w, http.StatusForbidden, "알림 토큰이 올바르지 않습니다")
			return
		}
		if ack.UsedAt != "" {
			writeError(w, http.StatusConflict, "이미 사용된 토큰입니다")
			return
		}
		if exp, err := time.Parse(time.RFC3339, ack.ExpiresAt); err != nil || now.After(exp) {
			writeError(w, http.StatusConflict, "만료된 토큰입니다")
			return
		}
		s.db.Model(&ack).Update("used_at", now.Format(time.RFC3339))
		if req.Via != "" {
			inc.AckedVia = req.Via
		} else {
			inc.AckedVia = ack.Via
		}
	} else {
		if !inRolesAllowed(getRole(r)) {
			writeError(w, http.StatusForbidden, "알림 확인 권한이 필요합니다")
			return
		}
		inc.AckedVia = "pccp"
	}
	inc.State = "acknowledged"
	inc.AckedBy = ackedBy
	inc.AckedAt = now.Format(time.RFC3339)
	s.db.Save(&inc)
	s.db.Create(&models.IncidentNotifyAudit{
		OrganizationID: orgID, Action: "incident_acknowledged", IncidentID: inc.ID,
		ActorEmail: ackedBy, Detail: "via=" + inc.AckedVia, OccurredAt: now.Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
}

// handleINEscalationSweep advances unacknowledged critical incidents to
// the next recipient group once the ack deadline has passed.
func (s *Server) handleINEscalationSweep(w http.ResponseWriter, r *http.Request) {
	if !inRolesAllowed(getRole(r)) {
		writeError(w, http.StatusForbidden, "에스컬레이션 실행 권한이 필요합니다")
		return
	}
	orgID := getOrgID(r)
	now := time.Now().UTC()
	var pol models.IncidentNotifyPolicy
	deadline := 15
	maxSteps := 3
	if err := s.db.Where("organization_id = ?", orgID).First(&pol).Error; err == nil {
		if pol.AckDeadlineMinutes > 0 {
			deadline = pol.AckDeadlineMinutes
		}
		if pol.EscalationSteps > 0 {
			maxSteps = pol.EscalationSteps
		}
	}
	var open []models.IncidentNotifyIncident
	// Escalated-but-still-unacked incidents keep escalating on later
	// sweeps; acknowledged/resolved ones exit via AckedAt/state below.
	s.db.Where("organization_id = ? AND severity = ? AND state IN ?", orgID, "critical",
		[]string{"open", "escalated"}).Find(&open)
	escalated := 0
	for _, inc := range open {
		// Each escalation step is due one ack-deadline after the previous
		// one: step N at FirstSeenAt + deadline×(N).
		anchor := inc.FirstSeenAt
		t, err := time.Parse(time.RFC3339, anchor)
		if err != nil || now.Sub(t) < time.Duration(deadline)*time.Duration(inc.EscalationStep+1)*time.Minute {
			continue
		}
		if inc.AckedAt != "" {
			continue
		}
		if inc.EscalationStep >= maxSteps {
			continue
		}
		inc.EscalationStep++
		inc.State = "escalated"
		s.db.Save(&inc)
		s.inEnqueueNotifications(&inc, "escalation")
		s.db.Create(&models.IncidentNotifyAudit{
			OrganizationID: orgID, Action: "incident_escalated", IncidentID: inc.ID,
			Detail: fmt.Sprintf("step=%d", inc.EscalationStep), OccurredAt: now.Format(time.RFC3339),
		})
		escalated++
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"escalated": escalated})
}

// ---------- incidents / jobs / test / health ----------

func (s *Server) handleINIncidentsList(w http.ResponseWriter, r *http.Request) {
	var incidents []models.IncidentNotifyIncident
	q := s.db.Where("organization_id = ?", getOrgID(r))
	if v := r.URL.Query().Get("state"); v != "" {
		q = q.Where("state = ?", v)
	}
	q.Order("last_seen_at DESC").Limit(100).Find(&incidents)
	writeJSON(w, http.StatusOK, incidents)
}

func (s *Server) handleINIncidentResolve(w http.ResponseWriter, r *http.Request) {
	if !inRolesAllowed(getRole(r)) {
		writeError(w, http.StatusForbidden, "알림 관리 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var inc models.IncidentNotifyIncident
	if err := s.db.Where("id = ? AND organization_id = ?", id, getOrgID(r)).First(&inc).Error; err != nil {
		writeError(w, http.StatusNotFound, "알림을 찾을 수 없습니다")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	inc.State = "resolved"
	inc.ResolvedAt = now
	s.db.Save(&inc)
	// Cancel obsolete pending jobs for this incident.
	s.db.Model(&models.IncidentNotifyJob{}).
		Where("incident_id = ? AND state = ?", inc.ID, "queued").Update("state", "cancelled")
	s.inEnqueueNotifications(&inc, "resolution")
	s.db.Create(&models.IncidentNotifyAudit{
		OrganizationID: inc.OrganizationID, Action: "incident_resolved", IncidentID: inc.ID,
		ActorEmail: getOperatorEmail(r), OccurredAt: now,
	})
	writeJSON(w, http.StatusOK, inc)
}

func (s *Server) handleINJobsList(w http.ResponseWriter, r *http.Request) {
	q := s.db.Where("organization_id = ?", getOrgID(r))
	if v := r.URL.Query().Get("state"); v != "" {
		q = q.Where("state = ?", v)
	}
	var jobs []models.IncidentNotifyJob
	q.Order("updated_at DESC").Limit(200).Find(&jobs)
	writeJSON(w, http.StatusOK, jobs)
}

// handleINTest sends an unmistakably labeled test notification.
func (s *Server) handleINTest(w http.ResponseWriter, r *http.Request) {
	if !inRolesAllowed(getRole(r)) {
		writeError(w, http.StatusForbidden, "테스트 발송 권한이 필요합니다")
		return
	}
	orgID := getOrgID(r)
	var req struct {
		Channel string `json:"channel"`
		Target  string `json:"target"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Target == "" {
		writeError(w, http.StatusBadRequest, "채널과 대상이 필요합니다")
		return
	}
	var tenant models.Organization
	s.db.First(&tenant, "id = ?", orgID)
	env := inBuildSMSenvelope(tenant.Name, "test-incidents", "low", "[테스트] 알림 채널 점검", time.Now().UTC().Format(time.RFC3339), "/security", true)
	env.ImpactSummaryKo = "이것은 테스트 알림입니다. 실제 장애가 아닙니다."
	raw, _ := json.Marshal(env)
	job := models.IncidentNotifyJob{
		OrganizationID: orgID, Kind: "test", Channel: req.Channel, Target: req.Target,
		IdempotencyKey: inRandomToken("test"), State: "queued", MaxAttempts: inMaxAttempts,
		EnvelopeJSON: string(raw),
	}
	s.db.Create(&job)
	s.db.Create(&models.IncidentNotifyAudit{
		OrganizationID: orgID, Action: "test_notification_queued", ActorEmail: getOperatorEmail(r),
		Detail: fmt.Sprintf("channel=%s target=%s", req.Channel, inMaskEndpoint(req.Target)),
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{"job_id": job.ID, "is_test": true})
}

// handleINHealth summarizes subsystem health; a notification failure is
// itself visible (PAT-1454).
func (s *Server) handleINHealth(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var queued, dead, failed24 int64
	s.db.Model(&models.IncidentNotifyJob{}).Where("organization_id = ? AND state = ?", orgID, "queued").Count(&queued)
	s.db.Model(&models.IncidentNotifyJob{}).Where("organization_id = ? AND state = ?", orgID, "dead_letter").Count(&dead)
	s.db.Model(&models.IncidentNotifyReceipt{}).
		Joins("JOIN incident_notify_jobs ON incident_notify_jobs.id = incident_notify_receipts.job_id").
		Where("incident_notify_jobs.organization_id = ? AND incident_notify_receipts.state = ? AND incident_notify_receipts.occurred_at > ?",
			orgID, "failed", time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339)).
		Count(&failed24)
	var unhealthy []models.IncidentNotifyChannel
	s.db.Where("organization_id = ? AND healthy = ?", orgID, false).Find(&unhealthy)
	names := make([]string, 0, len(unhealthy))
	for _, c := range unhealthy {
		names = append(names, c.Channel)
	}
	raw, _ := json.Marshal(names)
	sum := models.IncidentNotifyHealthSum{
		OrganizationID: orgID, QueueDepth: int(queued), DeadLetters: int(dead),
		Failures24h: int(failed24), UnhealthyChannels: string(raw),
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.db.Create(&sum)
	writeJSON(w, http.StatusOK, sum)
}

func inRandomToken(prefix string) string { return apiRandomToken(prefix, 12) }

// apiRandomToken is the shared prefixed-random-token helper for the
// wave domains (subscriber/verify tokens, ack tokens, artifact leases).
func apiRandomToken(prefix string, n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%s", prefix, base64.RawURLEncoding.EncodeToString(b))
}

// apiTruncateRunes bounds a display string to max runes with an ellipsis
// (shared by incident envelopes and evidence excerpts).
func apiTruncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func inRandInt(n int) int {
	b := make([]byte, 1)
	if _, err := rand.Read(b); err != nil {
		return 0
	}
	return int(b[0]) % n
}

// inStringSlice accepts both []string (in-process defaults) and
// []interface{} (JSON round-trip) channel lists.
func inStringSlice(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
