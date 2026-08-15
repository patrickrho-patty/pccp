package security

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements security checks for governed exchanges.
// Phase 2: DLP, Korean PII detection, secret scanning, prompt injection defense.
type Service struct {
	db *gorm.DB
}

// New creates a new security service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// CheckResult is the result of a security scan.
type CheckResult struct {
	Passed      bool              `json:"passed"`
	Findings    []SecurityFinding `json:"findings,omitempty"`
	Verdict     string            `json:"verdict"`               // ALLOW, DENY, REQUIRE_REVIEW
	Transformed string            `json:"transformed,omitempty"` // redacted text if ALLOW_TRANSFORM
}

// SecurityFinding is a detected security issue.
type SecurityFinding struct {
	Type        string `json:"type"`
	Severity    string `json:"severity"` // info, low, medium, high, critical
	Title       string `json:"title"`
	TitleKo     string `json:"title_ko"`
	Description string `json:"description"`
	RuleID      string `json:"rule_id"`
	Match       string `json:"match,omitempty"` // the matched text (may be redacted)
	Position    int    `json:"position,omitempty"`
	// Direction is request|response (security C4): response scans flag
	// exfiltration in model output.
	Direction string `json:"direction,omitempty"`
}

// CheckContext performs security checks on prompt/context content.
// This is the DLP + PII + secret + injection check pipeline (PRD §16).
func (s *Service) CheckContext(orgID, text string) CheckResult {
	var findings []SecurityFinding

	// 1. Korean PII detection (PRD §16.3) — org lexicon override first.
	findings = append(findings, s.detectKoreanPII(orgID, text)...)

	// 2. Secret/API key detection
	findings = append(findings, s.detectSecrets(text)...)

	// 3. Prompt injection indicators (PRD §16.4)
	findings = append(findings, s.detectInjection(text)...)

	// 4. Sensitive file paths
	findings = append(findings, s.detectSensitivePaths(text)...)

	// Honor per-org disabled detection rules (admin tunability, PRD §16) — a rule
	// toggled off in the console no longer produces actionable findings.
	if disabled := s.DisabledRuleIDs(orgID); len(disabled) > 0 {
		kept := make([]SecurityFinding, 0, len(findings))
		for _, f := range findings {
			if !disabled[f.RuleID] {
				kept = append(kept, f)
			}
		}
		findings = kept
	}

	// Determine verdict
	verdict := "ALLOW"
	for _, f := range findings {
		if f.Severity == "critical" || f.Severity == "high" {
			verdict = "DENY"
			break
		}
		if f.Severity == "medium" {
			if verdict == "ALLOW" {
				verdict = "REQUIRE_REVIEW"
			}
		}
	}

	return CheckResult{
		Passed:   verdict == "ALLOW",
		Findings: findings,
		Verdict:  verdict,
	}
}

// piiPattern is one Korean PII detection pattern (PRD §16.3).
type piiPattern struct {
	RuleID   string
	Type     string
	Severity string
	Pattern  *regexp.Regexp
	Title    string
	TitleKo  string
}

// Korean PII patterns (PRD §16.3 — Korean-sensitive data detection)
var koreanPIIPatterns = []piiPattern{
	{
		RuleID:   "pii-kr-rrn",
		Type:     "korean_pii_rrn",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`\b\d{6}[-‐]?\d{7}\b`),
		Title:    "Korean Resident Registration Number",
		TitleKo:  "주민등록번호",
	},
	{
		RuleID:   "pii-kr-business",
		Type:     "korean_pii_business",
		Severity: "high",
		Pattern:  regexp.MustCompile(`\b\d{3}[-‐]?\d{2}[-‐]?\d{5}\b`),
		Title:    "Korean Business Registration Number",
		TitleKo:  "사업자등록번호",
	},
	{
		RuleID:   "pii-kr-phone",
		Type:     "korean_pii_phone",
		Severity: "medium",
		Pattern:  regexp.MustCompile(`\b0\d{1,2}[-‐]?\d{3,4}[-‐]?\d{4}\b`),
		Title:    "Korean Phone Number",
		TitleKo:  "한국 전화번호",
	},
	{
		RuleID:   "pii-kr-account",
		Type:     "korean_pii_account",
		Severity: "high",
		Pattern:  regexp.MustCompile(`\b\d{3}[-‐]?\d{6,8}[-‐]?\d{3}\b`),
		Title:    "Korean Bank Account Number",
		TitleKo:  "은행 계좌번호",
	},
}

// Secret patterns
var secretPatterns = []struct {
	RuleID   string
	Type     string
	Severity string
	Pattern  *regexp.Regexp
	Title    string
	TitleKo  string
}{
	{
		RuleID:   "secret-aws-key",
		Type:     "secret_aws",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		Title:    "AWS Access Key",
		TitleKo:  "AWS 접근 키",
	},
	{
		RuleID:   "secret-jwt",
		Type:     "secret_jwt",
		Severity: "high",
		Pattern:  regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`),
		Title:    "JWT Token",
		TitleKo:  "JWT 토큰",
	},
	{
		RuleID:   "secret-private-key",
		Type:     "secret_private_key",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`),
		Title:    "Private Key",
		TitleKo:  "개인 키",
	},
	{
		RuleID:   "secret-github-pat",
		Type:     "secret_github",
		Severity: "high",
		Pattern:  regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36}`),
		Title:    "GitHub Personal Access Token",
		TitleKo:  "GitHub 개인 접근 토큰",
	},
	{
		RuleID:   "secret-generic-api-key",
		Type:     "secret_api_key",
		Severity: "medium",
		Pattern:  regexp.MustCompile(`(?i)(api[_-]?key|secret[_-]?key|access[_-]?token)["'\s]*[:=]\s*["']?[A-Za-z0-9_\-]{32,}["']?`),
		Title:    "Generic API Key",
		TitleKo:  "일반 API 키",
	},
}

// Prompt injection patterns (PRD §16.4)
var injectionPatterns = []struct {
	RuleID   string
	Type     string
	Severity string
	Pattern  *regexp.Regexp
	Title    string
	TitleKo  string
}{
	{
		RuleID:   "injection-ignore-instructions",
		Type:     "prompt_injection",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)(ignore|disregard|forget)\s+(all\s+)?(previous|prior|above)\s+instructions`),
		Title:    "Instruction Override Attempt",
		TitleKo:  "명령어 재정의 시도",
	},
	{
		RuleID:   "injection-system-prompt",
		Type:     "prompt_injection",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)(you\s+are|act\s+as|pretend\s+to\s+be)\s+(now|a)\s+`),
		Title:    "Role Hijack Attempt",
		TitleKo:  "역할 하이재킹 시도",
	},
	{
		RuleID:   "injection-data-exfil",
		Type:     "prompt_injection",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`(?i)(print|show|reveal|output|send)\s+(me\s+)?(your|the|all)\s+(system\s+)?(prompt|instructions|rules|secret)`),
		Title:    "System Prompt Extraction",
		TitleKo:  "시스템 프롬프트 추출 시도",
	},
	{
		RuleID:   "injection-jailbreak",
		Type:     "prompt_injection",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)(jailbreak|DAN|developer\s+mode|god\s+mode|unlimited)`),
		Title:    "Jailbreak Attempt",
		TitleKo:  "탈옥 시도",
	},
}

// Sensitive file paths
var sensitivePathPatterns = []struct {
	RuleID   string
	Type     string
	Severity string
	Pattern  *regexp.Regexp
	Title    string
	TitleKo  string
}{
	{
		RuleID:   "path-env",
		Type:     "sensitive_file",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)\.env(\.local|\.production|\.development)?\b`),
		Title:    "Environment File Reference",
		TitleKo:  "환경 변수 파일 참조",
	},
	{
		RuleID:   "path-secrets",
		Type:     "sensitive_file",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)(secrets?|credentials?|passwords?|keys?)\.(ya?ml|json|txt|conf)`),
		Title:    "Secrets File Reference",
		TitleKo:  "시크릿 파일 참조",
	},
	{
		RuleID:   "path-private-key",
		Type:     "sensitive_file",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`(?i)(id_rsa|id_ecdsa|id_ed25519)\b`),
		Title:    "SSH Private Key Reference",
		TitleKo:  "SSH 개인 키 참조",
	},
}

func (s *Service) detectKoreanPII(orgID, text string) []SecurityFinding {
	var findings []SecurityFinding
	for _, p := range s.piiPatternsFor(orgID) {
		matches := p.Pattern.FindAllStringIndex(text, -1)
		for _, m := range matches {
			findings = append(findings, SecurityFinding{
				Type:        p.Type,
				Severity:    p.Severity,
				Title:       p.Title,
				TitleKo:     p.TitleKo,
				RuleID:      p.RuleID,
				Match:       redactMatch(text[m[0]:m[1]]),
				Position:    m[0],
				Description: fmt.Sprintf("Korean PII detected: %s", p.TitleKo),
			})
		}
	}
	return findings
}

func (s *Service) detectSecrets(text string) []SecurityFinding {
	var findings []SecurityFinding
	for _, p := range secretPatterns {
		matches := p.Pattern.FindAllStringIndex(text, -1)
		for _, m := range matches {
			findings = append(findings, SecurityFinding{
				Type:        p.Type,
				Severity:    p.Severity,
				Title:       p.Title,
				TitleKo:     p.TitleKo,
				RuleID:      p.RuleID,
				Match:       redactMatch(text[m[0]:m[1]]),
				Position:    m[0],
				Description: fmt.Sprintf("Secret detected: %s", p.TitleKo),
			})
		}
	}
	return findings
}

func (s *Service) detectInjection(text string) []SecurityFinding {
	var findings []SecurityFinding
	for _, p := range injectionPatterns {
		matches := p.Pattern.FindAllStringIndex(text, -1)
		for _, m := range matches {
			findings = append(findings, SecurityFinding{
				Type:        p.Type,
				Severity:    p.Severity,
				Title:       p.Title,
				TitleKo:     p.TitleKo,
				RuleID:      p.RuleID,
				Match:       text[m[0]:m[1]],
				Position:    m[0],
				Description: fmt.Sprintf("Prompt injection detected: %s", p.TitleKo),
			})
		}
	}
	return findings
}

func (s *Service) detectSensitivePaths(text string) []SecurityFinding {
	var findings []SecurityFinding
	for _, p := range sensitivePathPatterns {
		matches := p.Pattern.FindAllStringIndex(text, -1)
		for _, m := range matches {
			findings = append(findings, SecurityFinding{
				Type:        p.Type,
				Severity:    p.Severity,
				Title:       p.Title,
				TitleKo:     p.TitleKo,
				RuleID:      p.RuleID,
				Match:       text[m[0]:m[1]],
				Position:    m[0],
				Description: fmt.Sprintf("Sensitive path detected: %s", p.TitleKo),
			})
		}
	}
	return findings
}

// RecordFinding persists a security finding to the database and
// dispatches it to the org's alert endpoints (security C2).
func (s *Service) RecordFinding(orgID, sessionID, exchangeID string, finding SecurityFinding) error {
	f := &models.SecurityFinding{
		OrganizationID: orgID,
		SessionID:      sessionID,
		ExchangeID:     exchangeID,
		FindingType:    finding.Type,
		Severity:       finding.Severity,
		Title:          finding.Title,
		TitleKo:        finding.TitleKo,
		Description:    finding.Description,
		RuleID:         finding.RuleID,
		Direction:      finding.Direction,
		Status:         "open",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	if err := s.db.Create(f).Error; err != nil {
		return err
	}
	s.DispatchAlerts(orgID, *f)
	return nil
}

// HasKoreanText checks if text contains Korean characters.
func HasKoreanText(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

// redactMatch masks most of the matched text for logging.
func redactMatch(match string) string {
	if len(match) <= 4 {
		return strings.Repeat("*", len(match))
	}
	return match[:2] + strings.Repeat("*", len(match)-4) + match[len(match)-2:]
}

// CheckContextJSON is a convenience wrapper for API responses.
func (s *Service) CheckContextJSON(orgID, text string) ([]byte, error) {
	result := s.CheckContext(orgID, text)
	return json.Marshal(result)
}

// defaultSecurityRuleDefs is the built-in detection catalog (PRD §16). Rule IDs
// match the detectors' findings so admin toggles take effect in CheckContext.
func defaultSecurityRuleDefs() []models.SecurityRule {
	return []models.SecurityRule{
		{RuleID: "pii-kr-rrn", Type: "korean_pii", Severity: "critical", Name: "Korean RRN", NameKo: "주민등록번호", Enabled: true, Action: "block"},
		{RuleID: "pii-kr-business", Type: "korean_pii", Severity: "high", Name: "Business Registration", NameKo: "사업자등록번호", Enabled: true, Action: "mask"},
		{RuleID: "pii-kr-phone", Type: "korean_pii", Severity: "medium", Name: "Korean Phone", NameKo: "전화번호", Enabled: true, Action: "mask"},
		{RuleID: "pii-kr-account", Type: "korean_pii", Severity: "high", Name: "Bank Account", NameKo: "계좌번호", Enabled: true, Action: "block"},
		{RuleID: "secret-aws", Type: "secret", Severity: "critical", Name: "AWS Access Key", NameKo: "AWS 접근키", Enabled: true, Action: "block"},
		{RuleID: "secret-jwt", Type: "secret", Severity: "high", Name: "JWT Token", NameKo: "JWT 토큰", Enabled: true, Action: "block"},
		{RuleID: "secret-private-key", Type: "secret", Severity: "critical", Name: "Private Key", NameKo: "개인키", Enabled: true, Action: "block"},
		{RuleID: "secret-github", Type: "secret", Severity: "high", Name: "GitHub PAT", NameKo: "GitHub 토큰", Enabled: true, Action: "block"},
		{RuleID: "injection-ignore", Type: "prompt_injection", Severity: "high", Name: "Instruction Override", NameKo: "명령어 재정의", Enabled: true, Action: "block"},
		{RuleID: "injection-jailbreak", Type: "prompt_injection", Severity: "high", Name: "Jailbreak Attempt", NameKo: "탈옥 시도", Enabled: true, Action: "block"},
	}
}

// EnsureRulesSeeded idempotently creates the default catalog rows for an org and
// returns the current persisted configuration.
func (s *Service) EnsureRulesSeeded(orgID string) ([]models.SecurityRule, error) {
	for _, def := range defaultSecurityRuleDefs() {
		var existing models.SecurityRule
		if err := s.db.Where("organization_id = ? AND rule_id = ?", orgID, def.RuleID).First(&existing).Error; err != nil {
			row := def
			row.OrganizationID = orgID
			s.db.Create(&row)
		}
	}
	var rules []models.SecurityRule
	s.db.Where("organization_id = ?", orgID).Order("type, severity desc, rule_id").Find(&rules)
	return rules, nil
}

// ListRules returns the persisted rule configuration for an org.
func (s *Service) ListRules(orgID string) ([]models.SecurityRule, error) {
	var rules []models.SecurityRule
	s.db.Where("organization_id = ?", orgID).Order("type, severity desc, rule_id").Find(&rules)
	return rules, nil
}

// SetRule persists an enabled/action override for a rule (creating the row if the
// rule isn't yet in the default catalog).
func (s *Service) SetRule(orgID, ruleID string, enabled *bool, action string) error {
	var row models.SecurityRule
	found := s.db.Where("organization_id = ? AND rule_id = ?", orgID, ruleID).First(&row).Error == nil
	if !found {
		row = models.SecurityRule{OrganizationID: orgID, RuleID: ruleID, Enabled: true, Action: "block"}
		for _, def := range defaultSecurityRuleDefs() {
			if def.RuleID == ruleID {
				row.Type, row.Severity, row.Name, row.NameKo, row.Action = def.Type, def.Severity, def.Name, def.NameKo, def.Action
				break
			}
		}
	}
	if enabled != nil {
		row.Enabled = *enabled
	}
	if action != "" {
		row.Action = action
	}
	if found {
		return s.db.Save(&row).Error
	}
	return s.db.Create(&row).Error
}

// DisabledRuleIDs returns the set of disabled rule IDs for an org (used by
// CheckContext to honor admin toggles).
func (s *Service) DisabledRuleIDs(orgID string) map[string]bool {
	if s.db == nil {
		return nil // no persisted config (e.g. unit tests) → all rules enabled
	}
	var rows []models.SecurityRule
	s.db.Where("organization_id = ? AND enabled = ?", orgID, false).Find(&rows)
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.RuleID] = true
	}
	return out
}
