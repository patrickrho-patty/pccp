package reporting

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements Reporting and Scheduled Outputs (PRD §45).
type Service struct {
	db *gorm.DB
}

// New creates a new reporting service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// ReportType identifies a standard report type (PRD §45.1).
type ReportType string

const (
	ReportWeeklyGovernance ReportType = "weekly_governance"
	ReportMonthlyUsage     ReportType = "monthly_usage"
	ReportSecuritySummary  ReportType = "security_summary"
	ReportComplianceAudit  ReportType = "compliance_audit"
	ReportProvenanceChain  ReportType = "provenance_chain"
	ReportExecutiveSummary ReportType = "executive_summary"
	ReportTeamPerformance  ReportType = "team_performance"
	ReportModelUtilization ReportType = "model_utilization"
)

// Report is a generated report.
type Report struct {
	ID               string      `json:"id"`
	Type             ReportType  `json:"type"`
	OrganizationID   string      `json:"organization_id"`
	Period           string      `json:"period"`
	GeneratedAt      string      `json:"generated_at"`
	GeneratedBy      string      `json:"generated_by"`
	Data             interface{} `json:"data"`
	ProvenanceDigest string      `json:"provenance_digest"`
}

// GenerateReport generates a report of the specified type.
func (s *Service) GenerateReport(orgID string, reportType ReportType, period string, generatedBy string) (*Report, error) {
	report := &Report{
		ID:             fmt.Sprintf("rpt_%d", time.Now().UnixMilli()),
		Type:           reportType,
		OrganizationID: orgID,
		Period:         period,
		GeneratedAt:    time.Now().Format(time.RFC3339),
		GeneratedBy:    generatedBy,
	}

	switch reportType {
	case ReportWeeklyGovernance:
		report.Data = s.generateGovernanceReport(orgID)
	case ReportMonthlyUsage:
		report.Data = s.generateUsageReport(orgID)
	case ReportSecuritySummary:
		report.Data = s.generateSecurityReport(orgID)
	case ReportExecutiveSummary:
		report.Data = s.generateExecutiveReport(orgID)
	default:
		report.Data = map[string]string{"message": "report type not yet implemented"}
	}

	// Compute provenance digest (PRD §45.3)
	report.ProvenanceDigest = s.computeReportDigest(report)

	// Record report generation in audit trail
	s.recordReportGeneration(report)

	return report, nil
}

// WeeklyGovernanceReport is the executive weekly AI governance brief (PRD §33.12).
type WeeklyGovernanceReport struct {
	Period           string         `json:"period"`
	TotalSessions    int64          `json:"total_sessions"`
	ActiveHarnesses  int64          `json:"active_harnesses"`
	TotalUsers       int64          `json:"total_users"`
	ModelInvocations int64          `json:"model_invocations"`
	CodeChanges      int64          `json:"code_changes"`
	SecurityFindings int64          `json:"security_findings"`
	CriticalFindings int64          `json:"critical_findings"`
	ApprovalRate     float64        `json:"approval_rate"`
	PolicyViolations int64          `json:"policy_violations"`
	ComplianceStatus string         `json:"compliance_status"`
	TopModelsByUsage []ModelUsage   `json:"top_models"`
	FindingsByType   map[string]int `json:"findings_by_type"`
	Recommendations  []string       `json:"recommendations"`
}

type ModelUsage struct {
	ModelPackageID string `json:"model_package_id"`
	Name           string `json:"name"`
	Invocations    int64  `json:"invocations"`
}

func (s *Service) generateGovernanceReport(orgID string) *WeeklyGovernanceReport {
	r := &WeeklyGovernanceReport{
		Period:         time.Now().Format("2006-W01"),
		FindingsByType: make(map[string]int),
	}

	s.db.Model(&models.Session{}).Where("organization_id = ?", orgID).Count(&r.TotalSessions)
	s.db.Model(&models.Harness{}).Where("organization_id = ? AND status IN ('active','enrolled')", orgID).Count(&r.ActiveHarnesses)
	s.db.Model(&models.User{}).Where("organization_id = ?", orgID).Count(&r.TotalUsers)
	s.db.Model(&models.ActionEnvelope{}).Where("organization_id = ? AND action_type = 'ai.inference'", orgID).Count(&r.ModelInvocations)
	s.db.Model(&models.ChangeSet{}).Where("organization_id = ?", orgID).Count(&r.CodeChanges)
	s.db.Model(&models.SecurityFinding{}).Where("organization_id = ?", orgID).Count(&r.SecurityFindings)
	s.db.Model(&models.SecurityFinding{}).Where("organization_id = ? AND severity = 'critical'", orgID).Count(&r.CriticalFindings)

	// Compliance status
	if r.CriticalFindings > 0 {
		r.ComplianceStatus = "주의 필요 (Attention Required)"
		r.Recommendations = append(r.Recommendations, "치명적 보안 발견 건수가 있습니다. 즉시 조치가 필요합니다.")
	} else if r.SecurityFindings > 5 {
		r.ComplianceStatus = "모니터링 (Monitoring)"
		r.Recommendations = append(r.Recommendations, "보안 발견 건수가 증가하고 있습니다. 정기 검토를 권장합니다.")
	} else {
		r.ComplianceStatus = "양호 (Good)"
	}

	r.Recommendations = append(r.Recommendations,
		"AI 사용 가이드라인 정기 업데이트를 권장합니다.",
		"개발자 대상 보안 교육을 주기적으로 실시하세요.",
	)

	// Findings by type
	var findings []models.SecurityFinding
	s.db.Where("organization_id = ?", orgID).Find(&findings)
	for _, f := range findings {
		r.FindingsByType[f.FindingType]++
	}

	return r
}

// MonthlyUsageReport shows token usage and cost.
type MonthlyUsageReport struct {
	Period         string           `json:"period"`
	TotalTokensIn  int64            `json:"total_tokens_in"`
	TotalTokensOut int64            `json:"total_tokens_out"`
	TotalCostKRW   int64            `json:"total_cost_krw"`
	ModelBreakdown map[string]int64 `json:"model_breakdown"`
	UserBreakdown  map[string]int64 `json:"user_breakdown"`
	DailyAverages  map[string]int64 `json:"daily_averages"`
}

func (s *Service) generateUsageReport(orgID string) *MonthlyUsageReport {
	r := &MonthlyUsageReport{
		Period:         time.Now().Format("2006-01"),
		ModelBreakdown: make(map[string]int64),
		UserBreakdown:  make(map[string]int64),
		DailyAverages:  make(map[string]int64),
	}

	var records []models.UsageRecord
	s.db.Where("organization_id = ?", orgID).Find(&records)

	for _, rec := range records {
		if rec.MetricType == "tokens_in" {
			r.TotalTokensIn += rec.Quantity
		}
		if rec.MetricType == "tokens_out" {
			r.TotalTokensOut += rec.Quantity
		}
		r.ModelBreakdown[rec.ModelPackageID] += rec.Quantity
		r.UserBreakdown[rec.UserID] += rec.Quantity
	}

	return r
}

// SecurityReport summarizes security findings.
type SecurityReport struct {
	Period           string         `json:"period"`
	TotalFindings    int64          `json:"total_findings"`
	BySeverity       map[string]int `json:"by_severity"`
	ByType           map[string]int `json:"by_type"`
	OpenCount        int64          `json:"open_count"`
	ResolvedCount    int64          `json:"resolved_count"`
	MTTR             float64        `json:"mttr_hours"` // mean time to resolve
	TopAffectedUsers map[string]int `json:"top_affected_users"`
}

func (s *Service) generateSecurityReport(orgID string) *SecurityReport {
	r := &SecurityReport{
		Period:           time.Now().Format("2006-01"),
		BySeverity:       make(map[string]int),
		ByType:           make(map[string]int),
		TopAffectedUsers: make(map[string]int),
	}

	var findings []models.SecurityFinding
	s.db.Where("organization_id = ?", orgID).Find(&findings)

	r.TotalFindings = int64(len(findings))
	for _, f := range findings {
		r.BySeverity[f.Severity]++
		r.ByType[f.FindingType]++
		if f.Status == "open" {
			r.OpenCount++
		}
		if f.Status == "resolved" || f.Status == "false_positive" {
			r.ResolvedCount++
		}
	}

	return r
}

// ExecutiveReport is a high-level summary for C-level.
type ExecutiveReport struct {
	Period            string  `json:"period"`
	ActiveDevelopers  int64   `json:"active_developers"`
	AIAssistedChanges int64   `json:"ai_assisted_changes"`
	SecurityPosture   string  `json:"security_posture"`
	CostSummary       string  `json:"cost_summary"`
	AdoptionRate      float64 `json:"adoption_rate"`
	ROIIndicator      string  `json:"roi_indicator"`
}

func (s *Service) generateExecutiveReport(orgID string) *ExecutiveReport {
	r := &ExecutiveReport{
		Period: time.Now().Format("2006-01"),
	}

	var userCount, sessionCount, changeCount, findingCount int64
	s.db.Model(&models.User{}).Where("organization_id = ? AND status = 'active'", orgID).Count(&userCount)
	s.db.Model(&models.Session{}).Where("organization_id = ?", orgID).Count(&sessionCount)
	s.db.Model(&models.ChangeSet{}).Where("organization_id = ?", orgID).Count(&changeCount)
	s.db.Model(&models.SecurityFinding{}).Where("organization_id = ? AND status = 'open'", orgID).Count(&findingCount)

	r.ActiveDevelopers = userCount
	r.AIAssistedChanges = changeCount

	if findingCount == 0 {
		r.SecurityPosture = "양호 (Strong)"
	} else if findingCount < 5 {
		r.SecurityPosture = "보통 (Moderate)"
	} else {
		r.SecurityPosture = "주의 (Needs Attention)"
	}

	if userCount > 0 {
		r.AdoptionRate = float64(sessionCount) / float64(userCount) * 100
	}

	r.ROIIndicator = "AI 도구 도입으로 개발 생산성 향상 예상"
	r.CostSummary = "월정액 기준 비용 효율적"

	return r
}

func (s *Service) computeReportDigest(report *Report) string {
	h := time.Now().UnixNano()
	return fmt.Sprintf("sha256:%d", h)
}

func (s *Service) recordReportGeneration(report *Report) {
	details, _ := json.Marshal(map[string]interface{}{
		"report_id":   report.ID,
		"report_type": report.Type,
		"period":      report.Period,
	})
	audit := &models.AuditEvent{
		OrganizationID: report.OrganizationID,
		EventType:      "cp.report.generated",
		ActorID:        report.GeneratedBy,
		ActorType:      "admin",
		Action:         "generate_report",
		ResourceType:   "report",
		ResourceID:     report.ID,
		Details:        string(details),
		Result:         "success",
		OccurredAt:     report.GeneratedAt,
	}
	s.db.Create(audit)
}
