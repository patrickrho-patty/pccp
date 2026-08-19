package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// --- Suppress / accept-risk (security C1) ---

// SuppressFinding suppresses a finding with a reason + expiry; the
// sweep reopens it after the window.
func (s *Service) SuppressFinding(orgID, findingID, reason, by string, days int) error {
	if days <= 0 {
		days = 30
	}
	return s.db.Model(&models.SecurityFinding{}).
		Where("id = ? AND organization_id = ?", findingID, orgID).
		Updates(map[string]interface{}{
			"status":          "suppressed",
			"suppressed":      true,
			"suppress_reason": reason,
			"suppress_expiry": time.Now().Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339),
			"suppressed_by":   by,
		}).Error
}

// ReopenFinding clears the suppression and returns the finding to open.
func (s *Service) ReopenFinding(orgID, findingID string) error {
	return s.db.Model(&models.SecurityFinding{}).
		Where("id = ? AND organization_id = ?", findingID, orgID).
		Updates(map[string]interface{}{
			"status": "open", "suppressed": false,
		}).Error
}

// SweepSuppressions reopens findings whose suppression window expired
// (run on the API server's sweep ticker).
func (s *Service) SweepSuppressions() int {
	now := time.Now().Format(time.RFC3339)
	result := s.db.Model(&models.SecurityFinding{}).
		Where("suppressed = ? AND suppress_expiry != '' AND suppress_expiry <= ?", true, now).
		Updates(map[string]interface{}{"status": "open", "suppressed": false})
	return int(result.RowsAffected)
}

// --- Alert routing (security C2/C3, §10C.14/§32.4) ---

// resolveAlertTarget decrypts an envelope if present, otherwise
// falls back to the plaintext column. PAT-1502 PR 2.
func (s *Service) resolveAlertTarget(ep models.AlertEndpoint) (string, error) {
	return keymgmt.OpenAlertSecret(s.alertKeyProvider, ep.TargetEnc, ep.TargetKEKID, ep.Target,
		ep.TargetBindingVersion, ep.CredentialID, keymgmt.AlertSecretContext{
			OrganizationID: ep.OrganizationID, EndpointID: ep.ID, ProviderType: ep.Type,
		})
}

// DispatchAlerts routes a recorded finding to the org's enabled alert
// endpoints whose severity filter matches. Slack gets a compact text
// card; webhooks get the full finding; SIEM gets a CEF-style JSON
// record. Dispatch is best-effort: a failed delivery logs and returns
// the count actually delivered.
func (s *Service) DispatchAlerts(orgID string, finding models.SecurityFinding) int {
	var endpoints []models.AlertEndpoint
	s.db.Where("organization_id = ? AND enabled = ? AND rotation_required = ?", orgID, true, false).Find(&endpoints)
	delivered := 0
	for _, ep := range endpoints {
		if !severityRouted(ep.SeveritiesJSON, finding.Severity) {
			continue
		}
		if s.deliverAlert(ep, finding) {
			delivered++
		}
	}
	return delivered
}

func severityRouted(severitiesJSON, severity string) bool {
	var list []string
	if len(severitiesJSON) == 0 {
		return true // no filter → all severities
	}
	if err := json.Unmarshal([]byte(severitiesJSON), &list); err != nil {
		return false
	}
	if len(list) == 0 {
		return true
	}
	for _, sev := range list {
		if sev == severity || sev == "all" {
			return true
		}
	}
	return false
}

func (s *Service) deliverAlert(ep models.AlertEndpoint, finding models.SecurityFinding) bool {
	if !ep.Enabled || ep.RotationRequired {
		return false
	}
	var payload []byte
	var contentType string
	switch ep.Type {
	case "slack":
		payload, _ = json.Marshal(map[string]interface{}{
			"text": fmt.Sprintf("🚨 *PCCP 보안 발견 · %s* (%s)\n%s\n세션: %s · 규칙: %s",
				finding.TitleKo, finding.Severity, finding.Title, finding.SessionID, finding.RuleID),
		})
		contentType = "application/json"
	case "siem":
		// CEF-style JSON forwarder (§32.4): syslog-ish key/values
		// flattened for SIEM ingestion.
		payload, _ = json.Marshal(map[string]interface{}{
			"cef_version": 0, "device_vendor": "pccp", "device_product": "security",
			"name": finding.FindingType, "severity": finding.Severity,
			"title": finding.Title, "title_ko": finding.TitleKo,
			"session_id": finding.SessionID, "rule_id": finding.RuleID,
			"occurred_at": finding.OccurredAt, "direction": finding.Direction,
		})
		contentType = "application/json"
	default: // webhook (on-call)
		payload, _ = json.Marshal(finding)
		contentType = "application/json"
	}
	target, err := s.resolveAlertTarget(ep)
	if err != nil {
		log.Printf("security: alert endpoint=%s credential=%s resolution_failed", ep.ID, ep.CredentialID)
		return false
	}
	if target == "" {
		return false
	}
	if err := ValidateAlertTarget(ep.Type, target); err != nil {
		log.Printf("security: alert endpoint=%s credential=%s invalid_target", ep.ID, ep.CredentialID)
		return false
	}
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		log.Printf("security: alert endpoint=%s credential=%s request_build_failed", ep.ID, ep.CredentialID)
		return false
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := s.alertHTTPClient.Do(req)
	if err != nil {
		log.Printf("security: alert endpoint=%s credential=%s delivery_failed reason=%s", ep.ID, ep.CredentialID, AlertDeliveryErrorClass(err))
		return false
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 64<<10)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("security: alert endpoint=%s credential=%s rejected status_class=non_2xx http_status=%d", ep.ID, ep.CredentialID, resp.StatusCode)
		return false
	}
	return true
}

// StartAlertDeliveryWorker starts one durable-outbox consumer per service.
func (s *Service) StartAlertDeliveryWorker(ctx context.Context) {
	s.alertWorkerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				if _, err := s.ProcessAlertDeliveries(ctx, 50); err != nil && ctx.Err() == nil {
					log.Printf("security: alert delivery worker cycle failed")
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

// ProcessAlertDeliveries atomically claims pending jobs before performing
// network I/O, making concurrent workers safe across replicas.
func (s *Service) ProcessAlertDeliveries(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	now := time.Now().UTC()
	if err := s.db.Model(&models.AlertDeliveryJob{}).
		Where("status = ? AND updated_at < ?", "processing", now.Add(-5*time.Minute)).
		Updates(map[string]interface{}{"status": "pending", "available_at": now}).Error; err != nil {
		return 0, err
	}
	var candidates []models.AlertDeliveryJob
	if err := s.db.Where("status = ? AND available_at <= ?", "pending", now).
		Order("available_at ASC, id ASC").Limit(limit).Find(&candidates).Error; err != nil {
		return 0, err
	}
	delivered := 0
	for _, job := range candidates {
		if ctx.Err() != nil {
			return delivered, ctx.Err()
		}
		claim := s.db.Model(&models.AlertDeliveryJob{}).
			Where("id = ? AND status = ?", job.ID, "pending").
			Updates(map[string]interface{}{"status": "processing", "attempts": gorm.Expr("attempts + 1")})
		if claim.Error != nil {
			return delivered, claim.Error
		}
		if claim.RowsAffected != 1 {
			continue
		}
		var endpoint models.AlertEndpoint
		var finding models.SecurityFinding
		errEndpoint := s.db.Where("id = ? AND organization_id = ?", job.EndpointID, job.OrganizationID).First(&endpoint).Error
		errFinding := s.db.Where("id = ? AND organization_id = ?", job.FindingID, job.OrganizationID).First(&finding).Error
		ok := errEndpoint == nil && errFinding == nil && s.deliverAlert(endpoint, finding)
		if ok {
			if err := s.db.Model(&models.AlertDeliveryJob{}).Where("id = ?", job.ID).
				Updates(map[string]interface{}{"status": "delivered", "last_reason": ""}).Error; err != nil {
				return delivered, err
			}
			delivered++
			continue
		}
		attempts := job.Attempts + 1
		status := "pending"
		if attempts >= 5 || errEndpoint != nil || errFinding != nil || endpoint.RotationRequired || !endpoint.Enabled {
			status = "failed"
		}
		delay := time.Duration(1<<min(attempts, 6)) * time.Second
		retryAt := time.Now().UTC().Add(delay)
		if err := s.db.Model(&models.AlertDeliveryJob{}).Where("id = ?", job.ID).Updates(map[string]interface{}{
			"status": status, "available_at": retryAt, "last_reason": "delivery_failed",
		}).Error; err != nil {
			return delivered, err
		}
	}
	return delivered, nil
}

// --- PII lexicon versioning (security C5, §16.3) ---

// GetLexicon returns the org's published lexicon, or nil.
func (s *Service) GetLexicon(orgID string) *models.PIILexicon {
	if s.db == nil {
		return nil
	}
	var lexicon models.PIILexicon
	if err := s.db.Where("organization_id = ? AND enabled = ?", orgID, true).
		Order("version DESC").First(&lexicon).Error; err != nil {
		return nil
	}
	return &lexicon
}

// SetLexicon publishes a new lexicon version.
func (s *Service) SetLexicon(orgID, version, updatedBy string, patterns map[string]string) (*models.PIILexicon, error) {
	if version == "" {
		version = fmt.Sprintf("%d", time.Now().Unix())
	}
	// PAT-1508: invalid/unsafe rules must not reach the detector. Every
	// pattern must compile, and the conservative regex-safety guard rejects
	// catastrophic constructs (kept in lockstep with the UI validator in
	// web/src/securityLexicon.ts). Compilation also fails fast here instead
	// of silently falling back to the built-in rule at scan time.
	patternsJSON, _ := json.Marshal(patterns)
	var decoded map[string]string
	if err := json.Unmarshal(patternsJSON, &decoded); err != nil {
		return nil, fmt.Errorf("security: lexicon patterns invalid: %w", err)
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("security: lexicon cannot be empty")
	}
	for id, pattern := range decoded {
		if pattern == "" {
			return nil, fmt.Errorf("security: lexicon rule %s has an empty pattern", id)
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, fmt.Errorf("security: lexicon rule %s has an invalid regex: %v", id, err)
		}
		if reason := unsafeRegexReason(pattern); reason != "" {
			return nil, fmt.Errorf("security: lexicon rule %s is unsafe: %s", id, reason)
		}
	}
	lexicon := &models.PIILexicon{
		OrganizationID: orgID, Version: version, UpdatedBy: updatedBy,
		PatternsJSON: string(patternsJSON), Enabled: true,
	}
	if err := s.db.Create(lexicon).Error; err != nil {
		return nil, fmt.Errorf("security: publish lexicon: %w", err)
	}
	return lexicon, nil
}

// unsafeRegexReason returns a non-empty reason when a pattern uses a
// construct the console blocks (catastrophic quantifiers, lookaround, atomic
// groups). This mirrors web/src/securityLexicon.ts regexSafety so the UI and
// the API reject the same inputs.
func unsafeRegexReason(pattern string) string {
	if pattern == "" {
		return "empty pattern"
	}
	if look := regexp.MustCompile(`\(\?[<=!]|\(\?>|\(\?P?[<']`); look.MatchString(pattern) {
		return "lookaround/atomic/backreference"
	}
	if nested := regexp.MustCompile(`\((?:[^()]*[*+])+\)[*+]`); nested.MatchString(pattern) {
		return "nested quantifier (catastrophic backtracking)"
	}
	if group := regexp.MustCompile(`\)[*+]\s*[*+]`); group.MatchString(pattern) {
		return "group followed by repeated quantifier"
	}
	if adjacent := regexp.MustCompile(`(?:\*|\+|\{\d+(?:,\d*)?\})\s*(?:\*|\+|\{\d+(?:,\d*)?\})`); adjacent.MatchString(pattern) {
		return "adjacent quantifiers"
	}
	return ""
}

// piiPatternsFor resolves the effective PII pattern set: the org's
// published lexicon overrides the built-in defaults per rule id.
func (s *Service) piiPatternsFor(orgID string) []piiPattern {
	lexicon := s.GetLexicon(orgID)
	if lexicon == nil {
		return koreanPIIPatterns
	}
	var overrides map[string]string
	if err := json.Unmarshal([]byte(lexicon.PatternsJSON), &overrides); err != nil {
		return koreanPIIPatterns
	}
	out := make([]piiPattern, 0, len(koreanPIIPatterns))
	for _, p := range koreanPIIPatterns {
		if pattern, ok := overrides[p.RuleID]; ok {
			if re, err := regexp.Compile(pattern); err == nil {
				cp := p
				cp.Pattern = re
				out = append(out, cp)
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

// --- Session scanning (security UX8) ---

// ScanSession replays the detection pipeline over a session's recorded
// exchanges (request AND response sides) and records any findings.
func (s *Service) ScanSession(orgID, sessionID string) (map[string]interface{}, error) {
	var exchanges []models.PromptExchange
	s.db.Where("session_id = ?", sessionID).Find(&exchanges)
	result := map[string]interface{}{
		"session_id": sessionID, "exchanges_scanned": len(exchanges),
		"findings": []models.SecurityFinding{}, "verdict": "ALLOW",
	}
	totalFindings := 0
	blocked := false
	for _, ex := range exchanges {
		for side, text := range map[string]string{"request": ex.PromptText, "response": ex.ResponseText} {
			if text == "" {
				continue
			}
			scan := s.CheckContext(orgID, text)
			for _, f := range scan.Findings {
				f.Direction = side
				if _, err := s.RecordFinding(orgID, sessionID, ex.ExchangeID, f); err != nil {
					return nil, err
				}
				totalFindings++
				if f.Severity == "critical" || f.Severity == "high" {
					blocked = true
				}
			}
		}
	}
	result["total_findings"] = totalFindings
	if blocked {
		result["verdict"] = "DENY"
	} else if totalFindings > 0 {
		result["verdict"] = "REQUIRE_REVIEW"
	}
	return result, nil
}
