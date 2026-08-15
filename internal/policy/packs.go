package policy

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
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
		return s.db.Model(&models.Project{}).Where("id = ?", scopeID).
			Update("policy_pack_id", packID).Error
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
	ack := models.PolicyAcknowledgement{
		OrganizationID: orgID, EpochID: epochID, UserID: userID,
		AckedAt: time.Now().Format(time.RFC3339),
	}
	return s.db.Where("epoch_id = ? AND user_id = ?", epochID, userID).
		Assign(models.PolicyAcknowledgement{AckedAt: ack.AckedAt}).
		FirstOrCreate(&ack).Error
}

// HasAcked reports whether the user acknowledged the epoch (policy C2
// session gate).
func (s *Service) HasAcked(orgID, epochID, userID string) bool {
	var count int64
	s.db.Model(&models.PolicyAcknowledgement{}).
		Where("epoch_id = ? AND user_id = ?", epochID, userID).Count(&count)
	return count > 0
}

// --- Exceptions (policy C5, §33.8) ---

// ListExceptions lists the org's exception marketplace.
func (s *Service) ListExceptions(orgID string) ([]models.PolicyException, error) {
	var exceptions []models.PolicyException
	s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Find(&exceptions)
	return exceptions, nil
}

// CreateException files a scoped exception request.
func (s *Service) CreateException(orgID, scope, scopeID, scopeName, reason, requestedBy string, ruleIDs []string) (*models.PolicyException, error) {
	idsJSON, _ := json.Marshal(ruleIDs)
	ex := &models.PolicyException{
		OrganizationID: orgID, Scope: scope, ScopeID: scopeID, ScopeName: scopeName,
		RuleIDsJSON: string(idsJSON), Reason: reason, RequestedBy: requestedBy,
		Status: "pending",
	}
	if err := s.db.Create(ex).Error; err != nil {
		return nil, fmt.Errorf("policy: create exception: %w", err)
	}
	s.recordAudit(orgID, "cp.policy.exception_requested", "admin", "policy_exception", ex.ID,
		fmt.Sprintf(`{"scope":"%s","rules":%s}`, scope, string(idsJSON)))
	return ex, nil
}

// DecideException approves or denies an exception request.
func (s *Service) DecideException(orgID, exceptionID string, approve bool, decidedBy, reason string) (*models.PolicyException, error) {
	var ex models.PolicyException
	if s.db.First(&ex, "id = ? AND organization_id = ?", exceptionID, orgID).Error != nil {
		return nil, fmt.Errorf("policy: exception not found")
	}
	if ex.Status != "pending" {
		return nil, fmt.Errorf("policy: exception already decided")
	}
	status := "approved"
	if !approve {
		status = "denied"
	}
	ex.Status = status
	ex.DecidedBy = decidedBy
	ex.DecisionReason = reason
	ex.DecidedAt = time.Now().Format(time.RFC3339)
	s.db.Save(&ex)
	s.recordAudit(orgID, "cp.policy.exception_decided", "admin", "policy_exception", ex.ID,
		fmt.Sprintf(`{"status":"%s"}`, status))
	return &ex, nil
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
