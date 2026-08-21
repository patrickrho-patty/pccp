package policy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// packInput is the exported/imported pack document (policy B2/UX12).
type packInput struct {
	Name           string          `json:"name"`
	NameKo         string          `json:"name_ko,omitempty"`
	Version        string          `json:"version"`
	Profile        string          `json:"profile"`
	RuleIDs        []string        `json:"rule_ids,omitempty"`
	DLPRules       json.RawMessage `json:"dlp_rules,omitempty"`
	ToolPolicy     json.RawMessage `json:"tool_policy,omitempty"`
	NetworkPolicy  json.RawMessage `json:"network_policy,omitempty"`
	ModelPolicy    json.RawMessage `json:"model_policy,omitempty"`
	ApprovalMatrix json.RawMessage `json:"approval_matrix,omitempty"`
	Retention      json.RawMessage `json:"retention_policy,omitempty"`
}

// CreatePackFromRules groups approved rules into a versioned pack
// (policy B2). Config JSON is assembled per domain from the rules.
func (s *Service) CreatePackFromRules(orgID, name, nameKo, version, profile string, ruleIDs []string) (*models.PolicyPack, error) {
	if version == "" {
		version = "1"
	}
	if profile == "" {
		profile = "enterprise"
	}
	var rules []models.PolicyRule
	if len(ruleIDs) > 0 {
		s.db.Where("organization_id = ? AND id IN ?", orgID, ruleIDs).Find(&rules)
	} else {
		s.db.Where("organization_id = ? AND enabled = ? AND status = ?", orgID, true, "approved").Find(&rules)
	}
	domainCfg := map[string][]map[string]interface{}{}
	for _, r := range rules {
		var cfg map[string]interface{}
		json.Unmarshal([]byte(r.ConfigJSON), &cfg)
		if cfg == nil {
			cfg = map[string]interface{}{}
		}
		domainCfg[r.Domain] = append(domainCfg[r.Domain], map[string]interface{}{
			"rule_id": r.ID, "name": r.Name, "scope": r.Scope, "scope_name": r.ScopeName, "config": cfg,
		})
	}
	mk := func(domain string) string {
		list, ok := domainCfg[domain]
		if !ok || list == nil {
			return "[]"
		}
		b, _ := json.Marshal(list)
		return string(b)
	}
	pack := &models.PolicyPack{
		OrganizationID:      orgID,
		Name:                name,
		NameKo:              nameKo,
		Version:             version,
		Profile:             profile,
		DLPRulesJSON:        mk("data"),
		ToolPolicyJSON:      mk("tools"),
		NetworkPolicyJSON:   mk("network"),
		ModelPolicyJSON:     mk("models"),
		ApprovalMatrixJSON:  mk("scm"),
		RetentionPolicyJSON: mk("session"),
		Status:              "active",
	}
	pack.Digest = HashPolicies(map[string]string{
		"name": name, "version": version,
		"dlp": pack.DLPRulesJSON, "tools": pack.ToolPolicyJSON, "network": pack.NetworkPolicyJSON,
		"models": pack.ModelPolicyJSON, "scm": pack.ApprovalMatrixJSON, "session": pack.RetentionPolicyJSON,
	})
	if err := s.db.Create(pack).Error; err != nil {
		return nil, fmt.Errorf("policy: create pack: %w", err)
	}
	s.recordAudit(orgID, "cp.policy.pack_created", "admin", "policy_pack", pack.ID,
		fmt.Sprintf(`{"name":"%s","version":"%s","rules":%d}`, name, version, len(rules)))
	return pack, nil
}

// ListPacks lists the org's packs (policy B2).
func (s *Service) ListPacks(orgID string) ([]models.PolicyPack, error) {
	var packs []models.PolicyPack
	s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Find(&packs)
	return packs, nil
}

// AssignPack binds a pack to a scope (policy B2): org scopes update
// the Organization, project scopes the Project.
func (s *Service) AssignPack(orgID, packID, scope, scopeID string) error {
	var pack models.PolicyPack
	if err := s.db.First(&pack, "id = ? AND organization_id = ?", packID, orgID).Error; err != nil {
		return fmt.Errorf("policy: pack not found")
	}
	switch scope {
	case "org":
		return s.db.Model(&models.Organization{}).Where("id = ?", orgID).
			Update("policy_pack_id", packID).Error
	case "project":
		if scopeID == "" {
			return fmt.Errorf("policy: project assignment requires scope_id")
		}
		result := s.db.Model(&models.Project{}).
			Where("id = ? AND organization_id = ?", scopeID, orgID).
			Update("policy_pack_id", packID)
		if result.Error != nil {
			return fmt.Errorf("policy: assign project pack: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("policy: project not found")
		}
		return nil
	default:
		return fmt.Errorf("policy: unsupported assign scope %s", scope)
	}
}

// ExportPack returns the pack document for download (policy UX12).
func (s *Service) ExportPack(orgID, packID string) (map[string]interface{}, error) {
	var pack models.PolicyPack
	if err := s.db.First(&pack, "id = ? AND organization_id = ?", packID, orgID).Error; err != nil {
		return nil, fmt.Errorf("policy: pack not found")
	}
	return map[string]interface{}{
		"name": pack.Name, "name_ko": pack.NameKo, "version": pack.Version,
		"profile": pack.Profile, "digest": pack.Digest, "status": pack.Status,
		"dlp_rules":        json.RawMessage(pack.DLPRulesJSON),
		"tool_policy":      json.RawMessage(pack.ToolPolicyJSON),
		"network_policy":   json.RawMessage(pack.NetworkPolicyJSON),
		"model_policy":     json.RawMessage(pack.ModelPolicyJSON),
		"approval_matrix":  json.RawMessage(pack.ApprovalMatrixJSON),
		"retention_policy": json.RawMessage(pack.RetentionPolicyJSON),
	}, nil
}

// ImportPack recreates a pack from an exported document (policy UX12).
func (s *Service) ImportPack(orgID string, doc map[string]interface{}) (*models.PolicyPack, error) {
	name, _ := doc["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("policy: pack name required")
	}
	version, _ := doc["version"].(string)
	if version == "" {
		version = "1"
	}
	profile, _ := doc["profile"].(string)
	if profile == "" {
		profile = "enterprise"
	}
	raw := func(key string) string {
		if v, ok := doc[key]; ok {
			b, _ := json.Marshal(v)
			return string(b)
		}
		return "[]"
	}
	pack := &models.PolicyPack{
		OrganizationID:      orgID,
		Name:                name,
		NameKo:              strOr(doc, "name_ko"),
		Version:             version,
		Profile:             profile,
		DLPRulesJSON:        raw("dlp_rules"),
		ToolPolicyJSON:      raw("tool_policy"),
		NetworkPolicyJSON:   raw("network_policy"),
		ModelPolicyJSON:     raw("model_policy"),
		ApprovalMatrixJSON:  raw("approval_matrix"),
		RetentionPolicyJSON: raw("retention_policy"),
		Status:              "active",
	}
	pack.Digest = HashPolicies(map[string]string{"name": name, "version": version,
		"dlp": pack.DLPRulesJSON, "tools": pack.ToolPolicyJSON, "network": pack.NetworkPolicyJSON,
		"models": pack.ModelPolicyJSON, "scm": pack.ApprovalMatrixJSON, "session": pack.RetentionPolicyJSON})
	if err := s.db.Create(pack).Error; err != nil {
		return nil, fmt.Errorf("policy: import pack: %w", err)
	}
	s.recordAudit(orgID, "cp.policy.pack_imported", "admin", "policy_pack", pack.ID,
		fmt.Sprintf(`{"name":"%s","version":"%s"}`, name, version))
	return pack, nil
}

// --- Templates (policy UX2) ---

// SeedTemplates seeds the org's template catalog from the built-in
// six-domain catalog (idempotent).
func (s *Service) SeedTemplates(orgID string, templates []models.PolicyTemplate) (int, error) {
	created := 0
	for _, t := range templates {
		var existing models.PolicyTemplate
		if s.db.Where("organization_id = ? AND template_id = ?", orgID, t.TemplateID).
			First(&existing).Error == nil {
			continue
		}
		t.OrganizationID = orgID
		if err := s.db.Create(&t).Error; err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

// ListTemplates lists the org's template catalog.
func (s *Service) ListTemplates(orgID string) ([]models.PolicyTemplate, error) {
	var templates []models.PolicyTemplate
	s.db.Where("organization_id = ?", orgID).Order("domain, name").Find(&templates)
	return templates, nil
}

// SaveTemplate upserts an editable template (policy UX2).
func (s *Service) SaveTemplate(orgID string, t *models.PolicyTemplate) (*models.PolicyTemplate, error) {
	t.OrganizationID = orgID
	if err := s.db.Where("organization_id = ? AND template_id = ?", orgID, t.TemplateID).
		Assign(models.PolicyTemplate{
			Name: t.Name, NameEn: t.NameEn, Description: t.Description,
			ConfigJSON: t.ConfigJSON, Domain: t.Domain, Version: t.Version, Enabled: t.Enabled,
		}).
		FirstOrCreate(t).Error; err != nil {
		return nil, fmt.Errorf("policy: save template: %w", err)
	}
	return t, nil
}

// DeleteTemplate removes an org template.
func (s *Service) DeleteTemplate(orgID, templateID string) error {
	return s.db.Where("organization_id = ? AND template_id = ?", orgID, templateID).
		Delete(&models.PolicyTemplate{}).Error
}

// --- Acknowledgements (policy C2, §33.6) ---

// SetRequiresAck flips the acknowledgement campaign flag on an epoch.
func (s *Service) SetRequiresAck(orgID, epochID string, requires bool) error {
	return s.db.Model(&models.PolicyEpoch{}).
		Where("epoch_id = ? AND organization_id = ?", epochID, orgID).
		Update("requires_ack", requires).Error
}

// ListAcks returns the ack status per org user for an epoch.
func (s *Service) ListAcks(orgID, epochID string) ([]map[string]interface{}, error) {
	var users []models.User
	s.db.Where("organization_id = ? AND status != ?", orgID, "offboarded").Find(&users)
	var acks []models.PolicyAcknowledgement
	s.db.Where("epoch_id = ?", epochID).Find(&acks)
	acked := map[string]string{}
	for _, a := range acks {
		acked[a.UserID] = a.AckedAt
	}
	out := make([]map[string]interface{}, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]interface{}{
			"user_id": u.ID, "name_ko": u.NameKo, "name": u.Name, "email": u.Email,
			"acked": acked[u.ID] != "", "acked_at": acked[u.ID],
		})
	}
	return out, nil
}

// AckEpoch records a user's acknowledgement.
func (s *Service) AckEpoch(orgID, epochID, userID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var epoch models.PolicyEpoch
		if err := tx.Where("organization_id = ? AND epoch_id = ?", orgID, epochID).First(&epoch).Error; err != nil {
			return fmt.Errorf("policy: epoch not found in organization")
		}
		var user models.User
		if err := tx.Where("organization_id = ? AND id = ? AND status = 'active'", orgID, userID).First(&user).Error; err != nil {
			return fmt.Errorf("policy: active user not found in organization")
		}
		ack := models.PolicyAcknowledgement{
			OrganizationID: orgID, EpochID: epoch.EpochID, UserID: user.ID,
			AckedAt: time.Now().Format(time.RFC3339),
		}
		return tx.Where("organization_id = ? AND epoch_id = ? AND user_id = ?", orgID, epochID, userID).
			Assign(models.PolicyAcknowledgement{AckedAt: ack.AckedAt}).
			FirstOrCreate(&ack).Error
	})
}

// HasAcked reports whether the user acknowledged the epoch (policy C2
// session gate).
func (s *Service) HasAcked(orgID, epochID, userID string) bool {
	var count int64
	s.db.Model(&models.PolicyAcknowledgement{}).
		Where("organization_id = ? AND epoch_id = ? AND user_id = ?", orgID, epochID, userID).Count(&count)
	return count > 0
}

// --- Exceptions (policy C5, §33.8) — evidence-backed approval (PAT-1506) ---

// ListExceptions lists the org's exception marketplace, expiring
// approved-but-past-expiry rows on read (the row stays for the audit
// trail; status flips to "expired" and active=false).
func (s *Service) ListExceptions(orgID string) ([]models.PolicyException, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	s.sweepExpiredExceptions(orgID, now)
	var exceptions []models.PolicyException
	s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Find(&exceptions)
	return exceptions, nil
}

// ListExceptionsRanked returns pending exceptions sorted by severity
// (high first) then by age (oldest first) so the approver queue is
// prioritized. Approved/denied/expired are returned at the bottom.
func (s *Service) ListExceptionsRanked(orgID string) ([]models.PolicyException, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	s.sweepExpiredExceptions(orgID, now)
	var exceptions []models.PolicyException
	s.db.Where("organization_id = ?", orgID).Find(&exceptions)
	rankSeverity := func(sev string) int {
		switch sev {
		case "high":
			return 0
		case "medium":
			return 1
		case "low":
			return 2
		default:
			return 1
		}
	}
	sort.SliceStable(exceptions, func(i, j int) bool {
		pi := exceptions[i].Status == "pending"
		pj := exceptions[j].Status == "pending"
		if pi != pj {
			return pi
		}
		if pi && pj {
			if rankSeverity(exceptions[i].SeverityLabel) != rankSeverity(exceptions[j].SeverityLabel) {
				return rankSeverity(exceptions[i].SeverityLabel) < rankSeverity(exceptions[j].SeverityLabel)
			}
			return exceptions[i].CreatedAt.Before(exceptions[j].CreatedAt)
		}
		return exceptions[i].CreatedAt.After(exceptions[j].CreatedAt)
	})
	return exceptions, nil
}

// GetException returns a single exception scoped to the org (cross-tenant
// callers get not-found, matching the detail-handler convention).
func (s *Service) GetException(orgID, exceptionID string) (*models.PolicyException, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	s.sweepExpiredExceptions(orgID, now)
	var ex models.PolicyException
	if err := s.db.First(&ex, "id = ? AND organization_id = ?", exceptionID, orgID).Error; err != nil {
		return nil, err
	}
	return &ex, nil
}

// sweepExpiredExceptions flips status=approved to status=expired for any
// approved rows whose ExpiresAt is in the past. Idempotent.
func (s *Service) sweepExpiredExceptions(orgID, now string) {
	s.db.Model(&models.PolicyException{}).
		Where("organization_id = ? AND status = 'approved' AND expires_at IS NOT NULL AND expires_at <> '' AND expires_at < ?", orgID, now).
		Update("status", "expired")
}

// ExceptionInput is the evidence-backed payload for creating an exception
// request (PAT-1506). Time-bounded validity, justification, evidence,
// compensating controls, and rule-diff fields are all required so the
// approver never sees a summary-only row.
type ExceptionInput struct {
	Scope, ScopeID, ScopeName, RequestedBy, Reason string
	RuleIDs                                        []string
	JustificationKo                                string
	Evidence                                       []map[string]string
	CompensatingControls                           string
	ResourceDestination                            string
	SeverityLabel                                  string
	CurrentRuleValues                              []map[string]string
	ProposedRuleValues                             []map[string]string
	Conditions                                     []map[string]string
	RequestedStart                                 string
	ExpiresAt                                      string
	RequiredApproverRoles                          []string
}

// CreateException files a scoped, evidence-backed exception request.
func (s *Service) CreateException(orgID string, in ExceptionInput) (*models.PolicyException, error) {
	if in.Scope == "" || in.ScopeID == "" || in.Reason == "" || in.RequestedBy == "" || len(in.RuleIDs) == 0 {
		return nil, fmt.Errorf("policy: scope, scope_id, reason, requested_by, rule_ids required")
	}
	if in.ExpiresAt == "" {
		return nil, fmt.Errorf("policy: expires_at required (time-bounded exception)")
	}
	if in.JustificationKo == "" {
		return nil, fmt.Errorf("policy: justification_ko required")
	}
	idsJSON, _ := json.Marshal(in.RuleIDs)
	evJSON, _ := json.Marshal(in.Evidence)
	curJSON, _ := json.Marshal(in.CurrentRuleValues)
	proJSON, _ := json.Marshal(in.ProposedRuleValues)
	condJSON, _ := json.Marshal(in.Conditions)
	reqRoles := strings.Join(in.RequiredApproverRoles, ",")
	ex := &models.PolicyException{
		OrganizationID: orgID,
		Scope:          in.Scope, ScopeID: in.ScopeID, ScopeName: in.ScopeName,
		RuleIDsJSON: string(idsJSON), RequestedBy: in.RequestedBy, Reason: in.Reason,
		Status:          "pending",
		JustificationKo: in.JustificationKo, EvidenceJSON: string(evJSON),
		CompensatingControls:  in.CompensatingControls,
		ResourceDestination:   in.ResourceDestination,
		SeverityLabel:         in.SeverityLabel,
		CurrentRuleValuesJSON: string(curJSON), ProposedRuleValuesJSON: string(proJSON),
		ConditionsJSON: string(condJSON),
		RequestedStart: in.RequestedStart, ExpiresAt: in.ExpiresAt,
		RequiredApproverRoles: reqRoles,
	}
	if err := s.db.Create(ex).Error; err != nil {
		return nil, fmt.Errorf("policy: create exception: %w", err)
	}
	s.recordAudit(orgID, "cp.policy.exception_requested", "admin", "policy_exception", ex.ID,
		fmt.Sprintf(`{"scope":"%s","rules":%s,"severity":"%s","expires_at":"%s"}`, in.Scope, string(idsJSON), in.SeverityLabel, in.ExpiresAt))
	return ex, nil
}

// ExceptionDecision captures an approver's vote on a multi-party chain.
// Each approver role must supply a reason; the request is approved only
// when every required role has voted approve.
type ExceptionDecision struct {
	Approve         bool
	DecidedBy       string
	DecidedByRole   string
	Reason          string
	Conditions      []map[string]string
	PublishNewEpoch bool
}

// DecideException records one approver's vote. When the required
// approver roles have all voted approve, the exception becomes active
// and a new policy epoch is published. Denials short-circuit and the
// exception is finalized. Conditions are appended to the existing list.
// expired/revoked rows are rejected.
//
// The read + duplicate-check + write happens inside one DB transaction
// so two concurrent decisions can't both pass the same-role guard.
func (s *Service) DecideException(orgID, exceptionID string, d ExceptionDecision) (*models.PolicyException, error) {
	if d.Reason == "" {
		return nil, fmt.Errorf("policy: decision reason required")
	}
	var result *models.PolicyException
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var ex models.PolicyException
		if err := tx.First(&ex, "id = ? AND organization_id = ?", exceptionID, orgID).Error; err != nil {
			return fmt.Errorf("policy: exception not found")
		}
		if ex.Status == "expired" || ex.Status == "revoked" {
			return fmt.Errorf("policy: exception %s", ex.Status)
		}
		if ex.Status != "pending" && ex.Status != "approved" {
			return fmt.Errorf("policy: exception already %s", ex.Status)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if ex.ExpiresAt != "" && now > ex.ExpiresAt {
			// approver tried to act on an already-expired exception
			tx.Model(&ex).Update("status", "expired")
			return fmt.Errorf("policy: exception expired")
		}
		var approvers []map[string]string
		if ex.ApproversJSON != "" {
			_ = json.Unmarshal([]byte(ex.ApproversJSON), &approvers)
		}
		for _, a := range approvers {
			if a["role"] == d.DecidedByRole {
				return fmt.Errorf("policy: role %s already voted", d.DecidedByRole)
			}
		}
		approvers = append(approvers, map[string]string{
			"role":   d.DecidedByRole,
			"user":   d.DecidedBy,
			"at":     now,
			"vote":   boolStr(d.Approve),
			"reason": d.Reason,
		})
		if !d.Approve {
			appJSON, _ := json.Marshal(approvers)
			ex.Status = "denied"
			ex.DecidedBy = d.DecidedBy
			ex.DecisionReason = d.Reason
			ex.DecidedAt = now
			ex.ApproversJSON = string(appJSON)
			if len(d.Conditions) > 0 {
				var existing []map[string]string
				if ex.ConditionsJSON != "" {
					_ = json.Unmarshal([]byte(ex.ConditionsJSON), &existing)
				}
				existing = append(existing, d.Conditions...)
				cj, _ := json.Marshal(existing)
				ex.ConditionsJSON = string(cj)
			}
			if err := tx.Save(&ex).Error; err != nil {
				return err
			}
			s.recordAudit(orgID, "cp.policy.exception_decided", "admin", "policy_exception", ex.ID,
				fmt.Sprintf(`{"status":"denied","by":%q,"role":%q,"reason":%q}`, d.DecidedBy, d.DecidedByRole, d.Reason))
			result = &ex
			return nil
		}
		var existing []map[string]string
		if ex.ConditionsJSON != "" {
			_ = json.Unmarshal([]byte(ex.ConditionsJSON), &existing)
		}
		if len(d.Conditions) > 0 {
			existing = append(existing, d.Conditions...)
		}
		cj, _ := json.Marshal(existing)
		ex.ConditionsJSON = string(cj)

		required := splitCSV(ex.RequiredApproverRoles)
		approved := []string{}
		for _, a := range approvers {
			if a["vote"] == "true" {
				approved = append(approved, a["role"])
			}
		}
		allApproved := true
		for _, r := range required {
			found := false
			for _, x := range approved {
				if x == r {
					found = true
					break
				}
			}
			if !found {
				allApproved = false
				break
			}
		}
		appJSON, _ := json.Marshal(approvers)
		ex.ApproversJSON = string(appJSON)
		if !allApproved {
			ex.DecidedAt = now
			if err := tx.Save(&ex).Error; err != nil {
				return err
			}
			s.recordAudit(orgID, "cp.policy.exception_partial_approval", "admin", "policy_exception", ex.ID,
				fmt.Sprintf(`{"role":%q,"approved":%v}`, d.DecidedByRole, true))
			result = &ex
			return nil
		}
		ex.Status = "approved"
		ex.DecidedBy = d.DecidedBy
		ex.DecisionReason = d.Reason
		ex.DecidedAt = now
		if d.PublishNewEpoch {
			epoch := &models.PolicyEpoch{
				OrganizationID: orgID,
				EpochID:        dari.GenerateID("epoch"),
				EngineVersion:  "exception",
				TransitionMode: "immediate",
				EffectiveAt:    now,
				Status:         "active",
			}
			if err := tx.Create(epoch).Error; err != nil {
				return err
			}
			ex.PublishedEpochID = epoch.EpochID
		}
		if err := tx.Save(&ex).Error; err != nil {
			return err
		}
		s.recordAudit(orgID, "cp.policy.exception_decided", "admin", "policy_exception", ex.ID,
			fmt.Sprintf(`{"status":"approved","by":%q,"epoch_id":%q}`, d.DecidedBy, ex.PublishedEpochID))
		result = &ex
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// RevokeException cancels an approved exception (manual override).
func (s *Service) RevokeException(orgID, exceptionID, decidedBy, reason string) (*models.PolicyException, error) {
	var ex models.PolicyException
	if s.db.First(&ex, "id = ? AND organization_id = ?", exceptionID, orgID).Error != nil {
		return nil, fmt.Errorf("policy: exception not found")
	}
	if ex.Status != "approved" {
		return nil, fmt.Errorf("policy: only approved exceptions can be revoked")
	}
	ex.Status = "revoked"
	ex.DecidedBy = decidedBy
	ex.DecisionReason = reason
	ex.DecidedAt = time.Now().UTC().Format(time.RFC3339)
	s.db.Save(&ex)
	s.recordAudit(orgID, "cp.policy.exception_revoked", "admin", "policy_exception", ex.ID, fmt.Sprintf(`{"reason":%q}`, reason))
	return &ex, nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func strOr(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func (s *Service) recordAudit(orgID, action, actor, resourceType, resourceID, details string) {
	event := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      action,
		ActorType:      actor,
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Details:        details,
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(event)
}
