package compliance

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// vault.go: web/08 — certification scope/level (B1), evidence vault (C1),
// remediation tracking (C2), persisted assessments + continuous
// re-assessment (C3), audit-ready export (C5).

// CertMeta describes a certification's target options (B1).
type CertMeta struct {
	Certification  CertificationType `json:"certification"`
	Name           string            `json:"name"`
	NameKo         string            `json:"name_ko"`
	Levels         []LevelOption     `json:"levels"`
	Scopes         []string          `json:"scopes"`          // SaaS, PaaS, IaaS
	SelfAssessment bool              `json:"self_assessment"` // honest disclaimer surface
}

// LevelOption is one selectable certification level.
type LevelOption struct {
	Value   string `json:"value"`
	Label   string `json:"label"`
	LabelKo string `json:"label_ko"`
}

// CertMetaList returns the certification catalog with level/scope
// options for the admin's target selection (B1).
func CertMetaList() []CertMeta {
	return []CertMeta{
		{
			Certification: CertCSAP, Name: "CSAP", NameKo: "클라우드 보안 인증 (CSAP)",
			Levels: []LevelOption{
				{Value: "simple", Label: "간편", LabelKo: "간편"},
				{Value: "standard", Label: "일반", LabelKo: "일반"},
			},
			Scopes: []string{"SaaS", "PaaS", "IaaS"}, SelfAssessment: true,
		},
		{
			Certification: CertISMSP, Name: "ISMS-P", NameKo: "정보보호 및 개인정보보호 관리체계 (ISMS-P)",
			Levels: []LevelOption{
				{Value: "1", Label: "Level 1", LabelKo: "1등급"},
				{Value: "2", Label: "Level 2", LabelKo: "2등급"},
				{Value: "3", Label: "Level 3", LabelKo: "3등급"},
			},
			Scopes: []string{"SaaS", "PaaS", "IaaS"}, SelfAssessment: true,
		},
		{
			Certification: CertKISA, Name: "KISA", NameKo: "KISA 정보보호 가이드라인",
			Levels: []LevelOption{{Value: "guide", Label: "Guideline", LabelKo: "가이드라인"}},
			Scopes: []string{"SaaS"}, SelfAssessment: true,
		},
		{
			Certification: CertPrivacy, Name: "PRIVACY", NameKo: "개인정보보호법 이행 점검",
			Levels: []LevelOption{{Value: "law", Label: "Statutory", LabelKo: "법령 이행"}},
			Scopes: []string{"SaaS"}, SelfAssessment: true,
		},
		{
			Certification: CertAIBasic, Name: "AI-BASIC", NameKo: "인공지능 기본법 대응",
			Levels: []LevelOption{{Value: "law", Label: "Statutory", LabelKo: "법령 이행"}},
			Scopes: []string{"SaaS"}, SelfAssessment: true,
		},
	}
}

// AssessWithTarget runs an assessment for a specific scope/level target
// and persists the snapshot (C3).
func (s *Service) AssessWithTarget(orgID string, cert CertificationType, scope, level string) (*ComplianceAssessment, error) {
	assessment, err := s.AssessCompliance(orgID, cert)
	if err != nil {
		return nil, err
	}
	assessment.Scope = scope
	assessment.Level = level
	resultsJSON, _ := json.Marshal(assessment.ControlResults)
	record := models.ComplianceAssessmentRecord{
		Base:           models.Base{ID: models.GenerateID("ca")},
		OrganizationID: orgID,
		Certification:  string(cert),
		Scope:          scope,
		Level:          level,
		AssessedAt:     assessment.AssessedAt,
		OverallStatus:  assessment.OverallStatus,
		OpenGaps:       assessment.OpenGaps,
		ResultsJSON:    string(resultsJSON),
	}
	if s.db != nil {
		if err := s.db.Create(&record).Error; err != nil {
			return nil, fmt.Errorf("compliance: persist assessment: %w", err)
		}
	}
	return assessment, nil
}

// RecentAssessments returns the org's persisted assessment history.
func (s *Service) RecentAssessments(orgID string, limit int) ([]models.ComplianceAssessmentRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	var records []models.ComplianceAssessmentRecord
	if err := s.db.Where("organization_id = ?", orgID).
		Order("created_at DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// AddEvidence attaches evidence to a control (C1).
func (s *Service) AddEvidence(orgID, cert, controlID, title, description, source, reference string) (*models.ComplianceEvidence, error) {
	ev := models.ComplianceEvidence{
		Base:           models.Base{ID: models.GenerateID("ce")},
		OrganizationID: orgID,
		Certification:  cert,
		ControlID:      controlID,
		Title:          title,
		Description:    description,
		Source:         source,
		Reference:      reference,
		CollectedAt:    time.Now().Format(time.RFC3339),
	}
	if err := s.db.Create(&ev).Error; err != nil {
		return nil, err
	}
	s.recordAudit(orgID, "compliance.evidence.added", controlID, fmt.Sprintf(`{"cert":"%s","source":"%s"}`, cert, source))
	return &ev, nil
}

// ListEvidence returns evidence for a certification (optionally one control).
func (s *Service) ListEvidence(orgID, cert, controlID string) ([]models.ComplianceEvidence, error) {
	q := s.db.Where("organization_id = ? AND certification = ?", orgID, cert)
	if controlID != "" {
		q = q.Where("control_id = ?", controlID)
	}
	var items []models.ComplianceEvidence
	if err := q.Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// DeleteEvidence removes one evidence item.
func (s *Service) DeleteEvidence(orgID, id string) error {
	res := s.db.Where("organization_id = ? AND id = ?", orgID, id).Delete(&models.ComplianceEvidence{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("compliance: evidence %s not found", id)
	}
	s.recordAudit(orgID, "compliance.evidence.deleted", id, "")
	return nil
}

// AddRemediation opens a gap → task (C2).
func (s *Service) AddRemediation(orgID, cert, controlID, owner, dueDate, sla, notes string) (*models.ComplianceRemediation, error) {
	task := models.ComplianceRemediation{
		Base:           models.Base{ID: models.GenerateID("cr")},
		OrganizationID: orgID,
		Certification:  cert,
		ControlID:      controlID,
		Owner:          owner,
		DueDate:        dueDate,
		SLA:            sla,
		Status:         "open",
		Notes:          notes,
	}
	if err := s.db.Create(&task).Error; err != nil {
		return nil, err
	}
	s.recordAudit(orgID, "compliance.remediation.created", controlID, fmt.Sprintf(`{"cert":"%s","owner":"%s","due":"%s"}`, cert, owner, dueDate))
	return &task, nil
}

// ListRemediations returns gap tasks for a certification.
func (s *Service) ListRemediations(orgID, cert, status string) ([]models.ComplianceRemediation, error) {
	q := s.db.Where("organization_id = ? AND certification = ?", orgID, cert)
	// Status filter accepts the reserved token "unresolved" (PAT-1484):
	// any remediation still open for action (status != done), which is the
	// same predicate the dashboard "진행 중 컴플라이언스 개선 과제" KPI
	// counts, so the destination list reconciles with the card count.
	switch status {
	case "unresolved":
		q = q.Where("status != ?", "done")
	case "":
		// no status filter
	default:
		q = q.Where("status = ?", status)
	}
	var tasks []models.ComplianceRemediation
	if err := q.Order("created_at DESC").Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// UpdateRemediation transitions a gap task.
func (s *Service) UpdateRemediation(orgID, id, status, owner, dueDate, notes string) (*models.ComplianceRemediation, error) {
	var task models.ComplianceRemediation
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, id).First(&task).Error; err != nil {
		return nil, fmt.Errorf("compliance: remediation %s not found", id)
	}
	if status != "" {
		task.Status = status
	}
	if owner != "" {
		task.Owner = owner
	}
	if dueDate != "" {
		task.DueDate = dueDate
	}
	if notes != "" {
		task.Notes = notes
	}
	if err := s.db.Save(&task).Error; err != nil {
		return nil, err
	}
	s.recordAudit(orgID, "compliance.remediation.updated", id, fmt.Sprintf(`{"status":"%s"}`, task.Status))
	return &task, nil
}

// BulkRemediate converts every open gap of a certification into a task
// (UX15 bulk gap→task).
func (s *Service) BulkRemediate(orgID, cert, owner, sla string) (int, error) {
	assessment, err := s.AssessCompliance(orgID, CertificationType(cert))
	if err != nil {
		return 0, err
	}
	created := 0
	for _, result := range assessment.ControlResults {
		if result.Status == "gap" || result.Status == "partial" {
			if _, err := s.AddRemediation(orgID, cert, result.ControlID, owner, "", sla, result.GapDescKo); err == nil {
				created++
			}
		}
	}
	return created, nil
}

// ContinuousReassess re-runs assessments for orgs that have assessed
// within the window (C3). Called by the API ticker.
func (s *Service) ContinuousReassess(window time.Duration) int {
	var records []models.ComplianceAssessmentRecord
	if err := s.db.Select("organization_id, certification, scope, level").
		Where("created_at >= ?", time.Now().Add(-window)).
		Find(&records).Error; err != nil {
		return 0
	}
	seen := map[string]bool{}
	ran := 0
	for _, r := range records {
		key := r.OrganizationID + "|" + r.Certification + "|" + r.Scope + "|" + r.Level
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := s.AssessWithTarget(r.OrganizationID, CertificationType(r.Certification), r.Scope, r.Level); err == nil {
			ran++
		}
	}
	return ran
}
