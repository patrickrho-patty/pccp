package security

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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

	// 5. Org custom rules (07 A1): admin-authored regex patterns —
	// a persisted custom rule actually scans.
	findings = append(findings, s.detectCustomRules(orgID, text)...)

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
	{
		RuleID:   "pii-kr-foreign-rrn",
		Type:     "korean_pii_rrn",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`\b\d{6}[-‐]?[5-8]\d{6}\b`),
		Title:    "Foreign Resident Registration Number",
		TitleKo:  "외국인등록번호",
	},
	{
		RuleID:   "pii-kr-driver-license",
		Type:     "korean_pii_driver_license",
		Severity: "high",
		Pattern:  regexp.MustCompile(`\b\d{2}[-‐]\d{2}[-‐]\d{6}[-‐]\d{2}\b`),
		Title:    "Korean Driver's License Number",
		TitleKo:  "운전면허번호",
	},
	{
		RuleID:   "pii-kr-passport",
		Type:     "korean_pii_passport",
		Severity: "high",
		Pattern:  regexp.MustCompile(`\b[A-Z]\d{8}\b`),
		Title:    "Korean Passport Number",
		TitleKo:  "여권번호",
	},
	{
		RuleID:   "pii-kr-phone-landline",
		Type:     "korean_pii_phone",
		Severity: "medium",
		Pattern:  regexp.MustCompile(`\b0[2-6][1-5]?[-‐]?\d{3,4}[-‐]?\d{4}\b`),
		Title:    "Korean Landline Number",
		TitleKo:  "한국 유선전화번호",
	},
	{
		RuleID:   "pii-kr-credit-card",
		Type:     "korean_pii_credit_card",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`\b(?:4\d{3}|5[1-5]\d{2}|3[47]\d{2}|6(?:011|5\d{2}))[-‐]?\d{4}[-‐]?\d{4}[-‐]?\d{4}\b`),
		Title:    "Credit Card Number",
		TitleKo:  "신용카드번호",
	},
	{
		RuleID:   "pii-kr-health-insurance",
		Type:     "korean_pii_health_insurance",
		Severity: "medium",
		Pattern:  regexp.MustCompile(`(?:건강보험|의료보험|건보)[^\n]{0,20}\b\d{10}\b`),
		Title:    "Korean Health Insurance Number",
		TitleKo:  "건강보험번호",
	},
	{
		RuleID:   "pii-kr-email-with-name",
		Type:     "korean_pii_email",
		Severity: "medium",
		Pattern:  regexp.MustCompile(`[\p{Hangul}]{2,4}\s*[:：]\s*[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),
		Title:    "Email Address with Korean Name",
		TitleKo:  "이름 포함 이메일 주소",
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
	{
		RuleID:   "secret-gcp-key",
		Type:     "secret_gcp",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`),
		Title:    "Google Cloud API Key",
		TitleKo:  "구글 클라우드 API 키",
	},
	{
		RuleID:   "secret-azure-key",
		Type:     "secret_azure",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`AccountKey=[A-Za-z0-9+/=]{60,}`),
		Title:    "Azure Storage Account Key",
		TitleKo:  "Azure 스토리지 계정 키",
	},
	{
		RuleID:   "secret-ncloud-key",
		Type:     "secret_ncloud",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)(?:ncp|ncloud)[_-]?(?:access|secret)[_-]?key["'\s:=]+[A-Za-z0-9+/=]{16,}`),
		Title:    "Naver Cloud Platform Key",
		TitleKo:  "네이버클라우드플랫폼 키",
	},
	{
		RuleID:   "secret-gitlab-token",
		Type:     "secret_gitlab",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{20}\b`),
		Title:    "GitLab Personal Access Token",
		TitleKo:  "GitLab 개인 접근 토큰",
	},
	{
		RuleID:   "secret-openai-key",
		Type:     "secret_openai",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{20,}\b`),
		Title:    "OpenAI API Key",
		TitleKo:  "OpenAI API 키",
	},
	{
		RuleID:   "secret-slack-webhook",
		Type:     "secret_slack",
		Severity: "high",
		Pattern:  regexp.MustCompile(`https://hooks\.slack\.com/services/T[A-Za-z0-9]+/B[A-Za-z0-9]+/[A-Za-z0-9]+`),
		Title:    "Slack Webhook URL",
		TitleKo:  "Slack 웹훅 URL",
	},
	{
		RuleID:   "secret-mysql-connstring",
		Type:     "secret_db_connstring",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)\bmysql://[^\s:@/]+:[^\s@]+@`),
		Title:    "MySQL Connection String",
		TitleKo:  "MySQL 연결 문자열",
	},
	{
		RuleID:   "secret-postgres-connstring",
		Type:     "secret_db_connstring",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)\bpostgres(?:ql)?://[^\s:@/]+:[^\s@]+@`),
		Title:    "PostgreSQL Connection String",
		TitleKo:  "PostgreSQL 연결 문자열",
	},
	{
		RuleID:   "secret-redis-connstring",
		Type:     "secret_db_connstring",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)\bredis[s]?://[^\s:@/]+:[^\s@]+@`),
		Title:    "Redis Connection String",
		TitleKo:  "Redis 연결 문자열",
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
	{
		RuleID:   "injection-exfil-email",
		Type:     "prompt_injection",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)(?:email|send|forward)[^\n]{0,30}(?:this|the|my|your)[^\n]{0,20}(?:output|response|answer|conversation)[^\n]{0,20}(?:to|at)\s+[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),
		Title:    "Exfiltration via Email",
		TitleKo:  "이메일을 통한 데이터 유출 시도",
	},
	{
		RuleID:   "injection-exfil-url",
		Type:     "prompt_injection",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)(?:post|upload|exfiltrate)[^\n]{0,40}(?:this|the|my|your|all)[^\n]{0,30}(?:to|at)\s+https?://`),
		Title:    "Exfiltration via URL",
		TitleKo:  "URL을 통한 데이터 유출 시도",
	},
	{
		RuleID:   "injection-base64-decode",
		Type:     "prompt_injection",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)(?:base64|b64)[^\n]{0,20}(?:decode|decrypt|디코딩|실행)[^\n]{0,30}["'\x60][A-Za-z0-9+/]{40,}={0,2}["'\x60]`),
		Title:    "Base64-Encoded Instruction",
		TitleKo:  "Base64 인코딩 명령 시도",
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
	{
		RuleID:   "path-etc-passwd",
		Type:     "sensitive_file",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`(?i)/etc/(?:passwd|shadow|gshadow|sudoers)\b`),
		Title:    "System Credential File Reference",
		TitleKo:  "시스템 자격증명 파일 참조",
	},
	{
		RuleID:   "path-proc-self",
		Type:     "sensitive_file",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)/proc/self/(?:environ|cmdline|fd|mem)\b`),
		Title:    "Process Memory/Environment Reference",
		TitleKo:  "프로세스 메모리/환경변수 참조",
	},
	{
		RuleID:   "path-aws-credentials",
		Type:     "sensitive_file",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`(?i)(?:~?/\.aws/credentials|aws[_-]?credentials)`),
		Title:    "AWS Credentials File Reference",
		TitleKo:  "AWS 자격증명 파일 참조",
	},
	{
		RuleID:   "path-gcp-key",
		Type:     "sensitive_file",
		Severity: "critical",
		Pattern:  regexp.MustCompile(`(?i)(?:service[_-]?account[_-]?key|gcp[_-]?credentials)\.json`),
		Title:    "GCP Service Account Key Reference",
		TitleKo:  "GCP 서비스 계정 키 참조",
	},
	{
		RuleID:   "path-kube-config",
		Type:     "sensitive_file",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)(?:\.kube/config|kubeconfig|/admin\.conf)\b`),
		Title:    "Kubernetes Config Reference",
		TitleKo:  "쿠버네티스 구성 파일 참조",
	},
	{
		RuleID:   "path-git-config",
		Type:     "sensitive_file",
		Severity: "medium",
		Pattern:  regexp.MustCompile(`(?i)(?:\.git/config|\.git-credentials)\b`),
		Title:    "Git Config/Credentials Reference",
		TitleKo:  "Git 구성/자격증명 참조",
	},
	{
		RuleID:   "path-npmrc",
		Type:     "sensitive_file",
		Severity: "medium",
		Pattern:  regexp.MustCompile(`(?i)\.npmrc\b`),
		Title:    "npm Auth File Reference",
		TitleKo:  "npm 인증 파일 참조",
	},
	{
		RuleID:   "path-ssh-config",
		Type:     "sensitive_file",
		Severity: "high",
		Pattern:  regexp.MustCompile(`(?i)\.ssh/(?:config|authorized_keys|known_hosts)\b`),
		Title:    "SSH Config Reference",
		TitleKo:  "SSH 구성 파일 참조",
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
// MUST match the detector arrays' finding RuleIDs exactly — DisabledRuleIDs
// keys off them, so a mismatched row silently toggles nothing. The catalog
// spans four classes: korean_pii, secret, prompt_injection, sensitive_path.
// Compliance-framework controls (PIPA/KISA/CSAP) are NOT DLP text rules —
// they map to features via internal/compliance and the harness's compliance
// skills; do not add them here.
func defaultSecurityRuleDefs() []models.SecurityRule {
	return []models.SecurityRule{
		// --- korean_pii (11) ---
		{RuleID: "pii-kr-rrn", Type: "korean_pii", Severity: "critical", Name: "Korean RRN", NameKo: "주민등록번호", Enabled: true, Action: "block"},
		{RuleID: "pii-kr-foreign-rrn", Type: "korean_pii", Severity: "critical", Name: "Foreign RRN", NameKo: "외국인등록번호", Enabled: true, Action: "block"},
		{RuleID: "pii-kr-business", Type: "korean_pii", Severity: "high", Name: "Business Registration", NameKo: "사업자등록번호", Enabled: true, Action: "mask"},
		{RuleID: "pii-kr-driver-license", Type: "korean_pii", Severity: "high", Name: "Driver's License", NameKo: "운전면허번호", Enabled: true, Action: "block"},
		{RuleID: "pii-kr-passport", Type: "korean_pii", Severity: "high", Name: "Passport Number", NameKo: "여권번호", Enabled: true, Action: "block"},
		{RuleID: "pii-kr-phone", Type: "korean_pii", Severity: "medium", Name: "Korean Phone", NameKo: "휴대전화번호", Enabled: true, Action: "mask"},
		{RuleID: "pii-kr-phone-landline", Type: "korean_pii", Severity: "medium", Name: "Landline Phone", NameKo: "유선전화번호", Enabled: true, Action: "mask"},
		{RuleID: "pii-kr-account", Type: "korean_pii", Severity: "high", Name: "Bank Account", NameKo: "계좌번호", Enabled: true, Action: "block"},
		{RuleID: "pii-kr-credit-card", Type: "korean_pii", Severity: "critical", Name: "Credit Card", NameKo: "신용카드번호", Enabled: true, Action: "block"},
		{RuleID: "pii-kr-health-insurance", Type: "korean_pii", Severity: "medium", Name: "Health Insurance", NameKo: "건강보험번호", Enabled: true, Action: "mask"},
		{RuleID: "pii-kr-email-with-name", Type: "korean_pii", Severity: "medium", Name: "Email with Name", NameKo: "이름 포함 이메일", Enabled: true, Action: "mask"},
		// --- secret (14) ---
		{RuleID: "secret-aws-key", Type: "secret", Severity: "critical", Name: "AWS Access Key", NameKo: "AWS 접근키", Enabled: true, Action: "block"},
		{RuleID: "secret-gcp-key", Type: "secret", Severity: "critical", Name: "GCP API Key", NameKo: "구글 클라우드 API 키", Enabled: true, Action: "block"},
		{RuleID: "secret-azure-key", Type: "secret", Severity: "critical", Name: "Azure Account Key", NameKo: "Azure 계정 키", Enabled: true, Action: "block"},
		{RuleID: "secret-ncloud-key", Type: "secret", Severity: "high", Name: "Naver Cloud Key", NameKo: "네이버클라우드플랫폼 키", Enabled: true, Action: "block"},
		{RuleID: "secret-github-pat", Type: "secret", Severity: "high", Name: "GitHub PAT", NameKo: "GitHub 토큰", Enabled: true, Action: "block"},
		{RuleID: "secret-gitlab-token", Type: "secret", Severity: "critical", Name: "GitLab Token", NameKo: "GitLab 토큰", Enabled: true, Action: "block"},
		{RuleID: "secret-openai-key", Type: "secret", Severity: "critical", Name: "OpenAI Key", NameKo: "OpenAI API 키", Enabled: true, Action: "block"},
		{RuleID: "secret-slack-webhook", Type: "secret", Severity: "high", Name: "Slack Webhook", NameKo: "Slack 웹훅", Enabled: true, Action: "block"},
		{RuleID: "secret-jwt", Type: "secret", Severity: "high", Name: "JWT Token", NameKo: "JWT 토큰", Enabled: true, Action: "block"},
		{RuleID: "secret-private-key", Type: "secret", Severity: "critical", Name: "Private Key", NameKo: "개인키", Enabled: true, Action: "block"},
		{RuleID: "secret-generic-api-key", Type: "secret", Severity: "medium", Name: "Generic API Key", NameKo: "일반 API 키", Enabled: true, Action: "mask"},
		{RuleID: "secret-mysql-connstring", Type: "secret", Severity: "high", Name: "MySQL DSN", NameKo: "MySQL 연결 문자열", Enabled: true, Action: "block"},
		{RuleID: "secret-postgres-connstring", Type: "secret", Severity: "high", Name: "PostgreSQL DSN", NameKo: "PostgreSQL 연결 문자열", Enabled: true, Action: "block"},
		{RuleID: "secret-redis-connstring", Type: "secret", Severity: "high", Name: "Redis DSN", NameKo: "Redis 연결 문자열", Enabled: true, Action: "block"},
		// --- prompt_injection (7) ---
		{RuleID: "injection-ignore-instructions", Type: "prompt_injection", Severity: "high", Name: "Instruction Override", NameKo: "명령어 재정의", Enabled: true, Action: "block"},
		{RuleID: "injection-system-prompt", Type: "prompt_injection", Severity: "high", Name: "Role Hijack", NameKo: "역할 하이재킹", Enabled: true, Action: "block"},
		{RuleID: "injection-data-exfil", Type: "prompt_injection", Severity: "critical", Name: "System Prompt Extraction", NameKo: "시스템 프롬프트 추출", Enabled: true, Action: "block"},
		{RuleID: "injection-jailbreak", Type: "prompt_injection", Severity: "high", Name: "Jailbreak Attempt", NameKo: "탈옥 시도", Enabled: true, Action: "block"},
		{RuleID: "injection-exfil-email", Type: "prompt_injection", Severity: "high", Name: "Exfil via Email", NameKo: "이메일 유출 시도", Enabled: true, Action: "block"},
		{RuleID: "injection-exfil-url", Type: "prompt_injection", Severity: "high", Name: "Exfil via URL", NameKo: "URL 유출 시도", Enabled: true, Action: "block"},
		{RuleID: "injection-base64-decode", Type: "prompt_injection", Severity: "high", Name: "Base64 Instruction", NameKo: "Base64 명령 시도", Enabled: true, Action: "block"},
		// --- sensitive_path (11) ---
		{RuleID: "path-env", Type: "sensitive_path", Severity: "high", Name: "Env File", NameKo: "환경변수 파일", Enabled: true, Action: "block"},
		{RuleID: "path-secrets", Type: "sensitive_path", Severity: "high", Name: "Secrets File", NameKo: "시크릿 파일", Enabled: true, Action: "block"},
		{RuleID: "path-private-key", Type: "sensitive_path", Severity: "critical", Name: "SSH Key File", NameKo: "SSH 키 파일", Enabled: true, Action: "block"},
		{RuleID: "path-etc-passwd", Type: "sensitive_path", Severity: "critical", Name: "System Credential File", NameKo: "시스템 자격증명 파일", Enabled: true, Action: "block"},
		{RuleID: "path-proc-self", Type: "sensitive_path", Severity: "high", Name: "Process Memory", NameKo: "프로세스 메모리", Enabled: true, Action: "block"},
		{RuleID: "path-aws-credentials", Type: "sensitive_path", Severity: "critical", Name: "AWS Credentials File", NameKo: "AWS 자격증명 파일", Enabled: true, Action: "block"},
		{RuleID: "path-gcp-key", Type: "sensitive_path", Severity: "critical", Name: "GCP Key File", NameKo: "GCP 키 파일", Enabled: true, Action: "block"},
		{RuleID: "path-kube-config", Type: "sensitive_path", Severity: "high", Name: "Kubernetes Config", NameKo: "쿠버네티스 구성", Enabled: true, Action: "block"},
		{RuleID: "path-git-config", Type: "sensitive_path", Severity: "medium", Name: "Git Config", NameKo: "Git 구성", Enabled: true, Action: "mask"},
		{RuleID: "path-npmrc", Type: "sensitive_path", Severity: "medium", Name: "npm Auth File", NameKo: "npm 인증 파일", Enabled: true, Action: "mask"},
		{RuleID: "path-ssh-config", Type: "sensitive_path", Severity: "high", Name: "SSH Config", NameKo: "SSH 구성", Enabled: true, Action: "block"},
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
func (s *Service) SetRule(orgID, ruleID string, enabled *bool, action string, pattern ...string) error {
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
	// Custom rules carry a regex pattern (07 A1): validated at save —
	// an uncompilable pattern is rejected at the API, never stored to
	// surface later as a runtime finding.
	if len(pattern) > 0 && pattern[0] != "" {
		if _, err := regexp.Compile(pattern[0]); err != nil {
			return fmt.Errorf("security: rule %s pattern does not compile: %w", ruleID, err)
		}
		if row.Type == "" {
			row.Type = "custom"
		}
		row.Pattern = pattern[0]
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

// customRuleCacheEntry memoizes one compiled org pattern.
type customRuleCacheEntry struct {
	re      *regexp.Regexp
	ruleID  string
	action  string
	enabled bool
}

// detectCustomRules runs the org's ENABLED custom regex rules.
func (s *Service) detectCustomRules(orgID, text string) []SecurityFinding {
	if s == nil || s.db == nil {
		return nil // no store → no custom rules (built-ins still run)
	}
	var rules []models.SecurityRule
	s.db.Where("organization_id = ? AND enabled = ? AND pattern != ''", orgID, true).Find(&rules)
	var out []SecurityFinding
	for _, r := range rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			// A rule that cannot compile is an admin error: it surfaces
			// as a finding naming the rule, not a silent skip.
			out = append(out, SecurityFinding{
				RuleID: r.RuleID, Severity: "medium", Title: "커스텀 규칙 컴파일 실패: " + r.RuleID,
				TitleKo: "커스텀 규칙 패턴이 잘못되었습니다: " + r.RuleID,
			})
			continue
		}
		if loc := re.FindStringIndex(text); loc != nil {
			out = append(out, SecurityFinding{
				RuleID: r.RuleID, Severity: r.Severity,
				Title:   "Custom rule matched: " + r.RuleID,
				TitleKo: "커스텀 규칙 매칭: " + r.NameKo,
				Match:   maskRange(text, loc[0], loc[1]),
				Type:    "custom",
			})
		}
	}
	return out
}

// Redact masks every finding's matched span (per-rule action=mask →
// full mask; others → snippet-level masking) for inspector-safe text.
func (s *Service) Redact(orgID, text string) string {
	res := s.CheckContext(orgID, text)
	out := []rune(text)
	masked := false
	for _, f := range res.Findings {
		if idx := strings.Index(text, snippetPlain(f.Match)); f.Match != "" && idx >= 0 {
			start := utf8.RuneCountInString(text[:idx])
			end := start + utf8.RuneCountInString(snippetPlain(f.Match))
			for i := start; i < end && i < len(out); i++ {
				out[i] = '•'
			}
			masked = true
		}
	}
	if !masked && len(res.Findings) > 0 {
		// Findings without a locatable snippet: redact the whole text —
		// never render what was flagged but cannot be surgically masked.
		return strings.Repeat("•", utf8.RuneCountInString(text))
	}
	return string(out)
}

func snippetPlain(s string) string { return strings.TrimPrefix(strings.TrimPrefix(s, "…"), "…") }

func maskRange(text string, start, end int) string {
	r := []rune(text)
	rs, re_ := utf8.RuneCountInString(text[:start]), utf8.RuneCountInString(text[:end])
	if rs >= len(r) {
		return ""
	}
	if re_ > len(r) {
		re_ = len(r)
	}
	return "…" + strings.Repeat("•", max(0, re_-rs)) + "…"
}

// --- PAT-1432: scoped rule overrides (team/user/harness deltas) ---

// ValidScopeLevels are the override levels (org lives in SecurityRule).
var validScopeLevels = map[string]bool{"team": true, "user": true, "harness": true}

// SetRuleOverride persists (or replaces) one scoped delta. level must
// be team/user/harness; at least one of enabled/severity/action must
// be set so the row is never a pure no-op. Severity, when set, must
// match the catalog's vocabulary.
func (s *Service) SetRuleOverride(orgID, level, scopeID, ruleID string, enabled *bool, severity, action string) error {
	if !validScopeLevels[level] {
		return fmt.Errorf("security: invalid scope level %q (want team/user/harness)", level)
	}
	if scopeID == "" || ruleID == "" {
		return fmt.Errorf("security: scope id and rule id are required")
	}
	if enabled == nil && severity == "" && action == "" {
		return fmt.Errorf("security: override must set at least one of enabled/severity/action")
	}
	if severity != "" {
		switch severity {
		case "low", "medium", "high", "critical":
		default:
			return fmt.Errorf("security: invalid severity %q", severity)
		}
	}
	// The rule must exist in the org catalog (seeded or custom).
	var count int64
	s.db.Model(&models.SecurityRule{}).Where("organization_id = ? AND rule_id = ?", orgID, ruleID).Count(&count)
	if count == 0 {
		return fmt.Errorf("security: rule %s not in org catalog", ruleID)
	}
	var row models.SecurityRuleOverride
	err := s.db.Where("organization_id = ? AND scope_level = ? AND scope_id = ? AND rule_id = ?",
		orgID, level, scopeID, ruleID).First(&row).Error
	if err != nil {
		row = models.SecurityRuleOverride{
			OrganizationID: orgID, ScopeLevel: level, ScopeID: scopeID, RuleID: ruleID,
		}
	}
	row.Enabled, row.Severity, row.Action = enabled, severity, action
	if err != nil {
		return s.db.Create(&row).Error
	}
	return s.db.Save(&row).Error
}

// DeleteRuleOverride removes a scoped delta (the rule reverts to the
// next-wider scope's setting).
func (s *Service) DeleteRuleOverride(orgID, level, scopeID, ruleID string) error {
	return s.db.Where("organization_id = ? AND scope_level = ? AND scope_id = ? AND rule_id = ?",
		orgID, level, scopeID, ruleID).Delete(&models.SecurityRuleOverride{}).Error
}

// ListRuleOverrides returns the scoped deltas for one scope target,
// ordered by rule id.
func (s *Service) ListRuleOverrides(orgID, level, scopeID string) ([]models.SecurityRuleOverride, error) {
	var rows []models.SecurityRuleOverride
	err := s.db.Where("organization_id = ? AND scope_level = ? AND scope_id = ?",
		orgID, level, scopeID).Order("rule_id").Find(&rows).Error
	return rows, err
}

// ScopedOverride is the pack-push projection of one delta row.
type ScopedOverride struct {
	RuleID   string
	Enabled  *bool
	Severity string
	Action   string
}

// ResolvedScope is one applicable scope target with its delta rows,
// ordered by the resolver (team → user → harness).
type ResolvedScope struct {
	Level     string
	ScopeID   string
	Overrides []ScopedOverride
}

// OverridesFor resolves the delta rows that apply to a session's
// subject: the user's business unit (team level), the user, and the
// harness peer — returned in ascending specificity order. Scopes
// with no override rows are omitted.
func (s *Service) OverridesFor(orgID, userID, harnessPeerID string) []ResolvedScope {
	var out []ResolvedScope
	if s == nil || s.db == nil {
		return out
	}
	add := func(level, scopeID string) {
		if scopeID == "" {
			return
		}
		rows, err := s.ListRuleOverrides(orgID, level, scopeID)
		if err != nil || len(rows) == 0 {
			return
		}
		ov := make([]ScopedOverride, 0, len(rows))
		for _, r := range rows {
			ov = append(ov, ScopedOverride{RuleID: r.RuleID, Enabled: r.Enabled, Severity: r.Severity, Action: r.Action})
		}
		out = append(out, ResolvedScope{Level: level, ScopeID: scopeID, Overrides: ov})
	}
	// Team level: the user's business unit (org hierarchy §12.1).
	if userID != "" {
		var user models.User
		if err := s.db.Where("id = ?", userID).First(&user).Error; err == nil && user.BusinessUnitID != "" {
			add("team", user.BusinessUnitID)
		}
	}
	add("user", userID)
	add("harness", harnessPeerID)
	return out
}
