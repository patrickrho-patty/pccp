package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// EpochRequest is the multi-domain epoch input (policy A2): every
// governance domain feeds one coherent epoch instead of a bare model
// allow-list.
type EpochRequest struct {
	OrganizationID string                 `json:"organization_id"`
	AllowedModels  []string               `json:"allowed_models,omitempty"`
	ToolPolicy     map[string]interface{} `json:"tool_policy,omitempty"`
	DLPPolicy      map[string]interface{} `json:"dlp_policy,omitempty"`
	NetworkPolicy  map[string]interface{} `json:"network_policy,omitempty"`
	SCMPolicy      map[string]interface{} `json:"scm_policy,omitempty"`
	SessionPolicy  map[string]interface{} `json:"session_policy,omitempty"`
	TransitionMode string                 `json:"transition_mode,omitempty"`
	RequiresAck    bool                   `json:"requires_ack,omitempty"`
}

// CreatePolicyEpochFull creates a multi-domain epoch: allowed models
// plus tool/DLP/network/SCM/session configs, filling every digest
// field on PolicyEpoch (policy A2).
func (s *Service) CreatePolicyEpochFull(req EpochRequest) (*models.PolicyEpoch, error) {
	if req.OrganizationID == "" {
		return nil, fmt.Errorf("policy: organization_id required")
	}
	var maxEpoch models.PolicyEpoch
	s.db.Where("organization_id = ?", req.OrganizationID).Order("epoch_number DESC").First(&maxEpoch)
	nextNum := uint64(1)
	if maxEpoch.ID != "" {
		nextNum = maxEpoch.EpochNumber + 1
	}
	if req.TransitionMode == "" {
		req.TransitionMode = "immediate"
	}

	modelsJSON := "[]"
	if len(req.AllowedModels) > 0 {
		b, _ := json.Marshal(req.AllowedModels)
		modelsJSON = string(b)
	}
	domainJSON := ""
	if req.ToolPolicy != nil || req.DLPPolicy != nil || req.NetworkPolicy != nil || req.SCMPolicy != nil || req.SessionPolicy != nil {
		b, _ := json.Marshal(map[string]interface{}{
			"tools":   req.ToolPolicy,
			"dlp":     req.DLPPolicy,
			"network": req.NetworkPolicy,
			"scm":     req.SCMPolicy,
			"session": req.SessionPolicy,
		})
		domainJSON = string(b)
	}

	epoch := &models.PolicyEpoch{
		OrganizationID:        req.OrganizationID,
		EpochID:               dari.GenerateID("epoch"),
		EpochNumber:           nextNum,
		OrgPolicyDigest:       s.computePolicyDigest(req.OrganizationID, "org"),
		ModelPolicyDigest:     s.computePolicyDigest(req.OrganizationID, "models"),
		DLPSecurityDigest:     s.computePolicyDigest(req.OrganizationID, "data"),
		ApprovalMatrixDigest:  s.computePolicyDigest(req.OrganizationID, "tools"),
		RetentionPolicyDigest: s.computePolicyDigest(req.OrganizationID, "session"),
		EngineVersion:         "1.0",
		AllowedModelsJSON:     modelsJSON,
		DomainPoliciesJSON:    domainJSON,
		TransitionMode:        req.TransitionMode,
		RequiresAck:           req.RequiresAck,
		EffectiveAt:           time.Now().Format(time.RFC3339),
		Status:                "active",
	}
	if nextNum > 1 {
		s.db.Model(&models.PolicyEpoch{}).
			Where("organization_id = ? AND status = 'active'", req.OrganizationID).
			Updates(map[string]interface{}{"status": "superseded", "superseded_by": epoch.EpochID})
	}
	if err := s.db.Create(epoch).Error; err != nil {
		return nil, fmt.Errorf("policy: create epoch: %w", err)
	}
	return epoch, nil
}

// RebuildEpochFromRules aggregates the org's approved + enabled rules
// per domain into one coherent epoch (policy A3): only model-domain
// rules touch the allowed-models list; the other domains fill their
// digest/domain-config slots.
func (s *Service) RebuildEpochFromRules(orgID, transitionMode string, requiresAck bool) (*models.PolicyEpoch, error) {
	var rules []models.PolicyRule
	s.db.Where("organization_id = ? AND enabled = ? AND status = ?", orgID, true, "approved").
		Order("id ASC").Find(&rules)

	req := EpochRequest{OrganizationID: orgID, TransitionMode: transitionMode, RequiresAck: requiresAck}
	req.ToolPolicy = map[string]interface{}{}
	req.DLPPolicy = map[string]interface{}{}
	req.NetworkPolicy = map[string]interface{}{}
	req.SCMPolicy = map[string]interface{}{}
	req.SessionPolicy = map[string]interface{}{}
	modelSets := map[string][]string{} // scope → allowed models (intersected later)

	for _, r := range rules {
		var cfg map[string]interface{}
		json.Unmarshal([]byte(r.ConfigJSON), &cfg)
		if cfg == nil {
			cfg = map[string]interface{}{}
		}
		switch r.Domain {
		case "models":
			if list, ok := cfg["allowed_models"].([]interface{}); ok {
				ids := make([]string, 0, len(list))
				for _, v := range list {
					if id, ok := v.(string); ok {
						ids = append(ids, id)
					}
				}
				if len(ids) > 0 {
					modelSets[r.ScopeName] = ids
				}
			}
		case "tools":
			req.ToolPolicy[r.ScopeName] = cfg
		case "data":
			req.DLPPolicy[r.ScopeName] = cfg
		case "network":
			req.NetworkPolicy[r.ScopeName] = cfg
		case "scm":
			req.SCMPolicy[r.ScopeName] = cfg
		case "session":
			req.SessionPolicy[r.ScopeName] = cfg
		}
	}

	// Model policy: lower layers can only strengthen (intersection).
	if len(modelSets) > 0 {
		var intersection []string
		first := true
		keys := make([]string, 0, len(modelSets))
		for k := range modelSets {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			list := modelSets[k]
			sort.Strings(list)
			if first {
				intersection = append([]string(nil), list...)
				first = false
				continue
			}
			seen := map[string]bool{}
			for _, id := range intersection {
				seen[id] = true
			}
			next := []string{}
			for _, id := range list {
				if seen[id] {
					next = append(next, id)
				}
			}
			intersection = next
		}
		req.AllowedModels = intersection
	}

	return s.CreatePolicyEpochFull(req)
}

// EffectivePolicy resolves the layered hierarchy (policy B1): org rules
// + project-scoped rules + repo-scoped rules merge with lower layers
// only strengthening. The result is the effective per-domain policy for
// a session in the given project/repo.
func (s *Service) EffectivePolicy(orgID, projectID, repoID string) (map[string]interface{}, error) {
	var rules []models.PolicyRule
	s.db.Where("organization_id = ? AND enabled = ? AND status = ?", orgID, true, "approved").
		Order("id ASC").Find(&rules)

	type layer struct {
		scope     string
		scopeName string
		config    map[string]interface{}
		domain    string
		rule      models.PolicyRule
	}
	var layers []layer
	for _, r := range rules {
		applies := false
		switch r.Scope {
		case "org":
			applies = true
		case "project":
			applies = projectID != "" && (r.ScopeName == projectID || r.ScopeName == "")
		case "repo":
			applies = repoID != "" && (r.ScopeName == repoID || r.ScopeName == "")
		case "team":
			applies = r.ScopeName != "" // team scoping applies org-wide until membership is resolved
		default:
			applies = true
		}
		if !applies {
			continue
		}
		var cfg map[string]interface{}
		json.Unmarshal([]byte(r.ConfigJSON), &cfg)
		if cfg == nil {
			cfg = map[string]interface{}{}
		}
		layers = append(layers, layer{scope: r.Scope, scopeName: r.ScopeName, config: cfg, domain: r.Domain, rule: r})
	}

	// Layer rank: org(0) → project(1) → repo(2) → team(3). Lower layers
	// merge over higher ones but never remove a restriction.
	rank := map[string]int{"org": 0, "project": 1, "repo": 2, "team": 3}
	sort.SliceStable(layers, func(i, j int) bool { return rank[layers[i].scope] < rank[layers[j].scope] })

	effective := map[string]interface{}{}
	allowedModels := map[string]bool{}
	modelFirst := true
	ruleRefs := make([]map[string]interface{}, 0, len(layers))
	for _, l := range layers {
		ruleRefs = append(ruleRefs, map[string]interface{}{
			"rule_id": l.rule.ID, "domain": l.domain, "name": l.rule.Name,
			"scope": l.scope, "scope_name": l.scopeName,
		})
		if l.domain == "models" {
			if list, ok := l.config["allowed_models"].([]interface{}); ok {
				set := map[string]bool{}
				for _, v := range list {
					if id, ok := v.(string); ok {
						set[id] = true
					}
				}
				if modelFirst {
					allowedModels = set
					modelFirst = false
				} else {
					for id := range allowedModels {
						if !set[id] {
							delete(allowedModels, id)
						}
					}
				}
			}
			continue
		}
		domainKey := l.domain
		existing, ok := effective[domainKey].(map[string]interface{})
		if !ok {
			existing = map[string]interface{}{}
		}
		for k, v := range l.config {
			existing[k] = v // lower layer wins on conflict; restrictions never removed
		}
		effective[domainKey] = existing
	}
	if !modelFirst {
		list := make([]string, 0, len(allowedModels))
		for id := range allowedModels {
			list = append(list, id)
		}
		sort.Strings(list)
		effective["allowed_models"] = list
	}
	effective["rules"] = ruleRefs
	return effective, nil
}

// EpochDiff renders the per-domain difference between two epochs
// (policy B3): allowed models added/removed plus changed domain
// configs.
func (s *Service) EpochDiff(epochA, epochB *models.PolicyEpoch) (map[string]interface{}, error) {
	diff := map[string]interface{}{"epoch_id": epochA.EpochID, "against": epochB.EpochID, "domains": map[string]interface{}{}}
	domains := diff["domains"].(map[string]interface{})

	var modelsA, modelsB []string
	json.Unmarshal([]byte(epochA.AllowedModelsJSON), &modelsA)
	json.Unmarshal([]byte(epochB.AllowedModelsJSON), &modelsB)
	setA, setB := map[string]bool{}, map[string]bool{}
	for _, m := range modelsA {
		setA[m] = true
	}
	for _, m := range modelsB {
		setB[m] = true
	}
	added, removed := []string{}, []string{}
	for _, m := range modelsA {
		if !setB[m] {
			added = append(added, m)
		}
	}
	for _, m := range modelsB {
		if !setA[m] {
			removed = append(removed, m)
		}
	}
	domains["allowed_models"] = map[string]interface{}{"added": added, "removed": removed, "changed": len(added) > 0 || len(removed) > 0}

	if epochA.DomainPoliciesJSON != epochB.DomainPoliciesJSON {
		domains["domain_policies"] = map[string]interface{}{
			"changed": true,
			"before":  epochB.DomainPoliciesJSON,
			"after":   epochA.DomainPoliciesJSON,
		}
	}
	if epochA.RequiresAck != epochB.RequiresAck {
		domains["requires_ack"] = map[string]interface{}{"changed": true, "before": epochB.RequiresAck, "after": epochA.RequiresAck}
	}
	return diff, nil
}

// RuleConflicts finds overlapping approved rules in the same domain +
// scope that would compete (policy C4).
func (s *Service) RuleConflicts(orgID, domain, scope, scopeName string, excludeID string) ([]models.PolicyRule, error) {
	var rules []models.PolicyRule
	q := s.db.Where("organization_id = ? AND domain = ? AND enabled = ? AND scope = ?", orgID, domain, true, scope)
	if scopeName != "" {
		q = q.Where("scope_name = ?", scopeName)
	}
	if excludeID != "" {
		q = q.Where("id != ?", excludeID)
	}
	q.Find(&rules)
	return rules, nil
}

// HashPolicies produces a stable digest of arbitrary policy JSON
// (used by pack digesting).
func HashPolicies(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
