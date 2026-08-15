package security

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
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

var alertHTTPClient = &http.Client{Timeout: 10 * time.Second}

// DispatchAlerts routes a recorded finding to the org's enabled alert
// endpoints whose severity filter matches. Slack gets a compact text
// card; webhooks get the full finding; SIEM gets a CEF-style JSON
// record. Dispatch is best-effort: a failed delivery logs and returns
// the count actually delivered.
func (s *Service) DispatchAlerts(orgID string, finding models.SecurityFinding) int {
	var endpoints []models.AlertEndpoint
	s.db.Where("organization_id = ? AND enabled = ?", orgID, true).Find(&endpoints)
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
	json.Unmarshal([]byte(severitiesJSON), &list)
	for _, sev := range list {
		if sev == severity || sev == "all" {
			return true
		}
	}
	return false
}

func (s *Service) deliverAlert(ep models.AlertEndpoint, finding models.SecurityFinding) bool {
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
	resp, err := alertHTTPClient.Post(ep.Target, contentType, bytes.NewReader(payload))
	if err != nil {
		log.Printf("security: alert delivery to %s failed: %v", ep.Name, err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("security: alert delivery to %s rejected: %s", ep.Name, resp.Status)
		return false
	}
	return true
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
	patternsJSON, _ := json.Marshal(patterns)
	lexicon := &models.PIILexicon{
		OrganizationID: orgID, Version: version, UpdatedBy: updatedBy,
		PatternsJSON: string(patternsJSON), Enabled: true,
	}
	if err := s.db.Create(lexicon).Error; err != nil {
		return nil, fmt.Errorf("security: publish lexicon: %w", err)
	}
	return lexicon, nil
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
				recorded := models.SecurityFinding{
					OrganizationID: orgID, SessionID: sessionID, ExchangeID: ex.ExchangeID,
					FindingType: f.Type, Severity: f.Severity, Title: f.Title, TitleKo: f.TitleKo,
					Description: f.Description, RuleID: f.RuleID, Direction: side,
					Status: "open", OccurredAt: time.Now().Format(time.RFC3339),
				}
				s.db.Create(&recorded)
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
