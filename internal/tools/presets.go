package tools

import (
	"encoding/json"
	"fmt"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// presets.go: web/14 — classification presets + custom-tool wizard
// guidance (D), seed feedback with counts (UX2), per-project allowlist
// (feature 7), signature/digest verification (E).

// PresetOption is one guided classification choice.
type PresetOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	LabelKo     string `json:"label_ko"`
	Description string `json:"description"`
}

// ToolPresets returns the classification presets with Korean guidance
// so admins know what each choice means (D).
func ToolPresets() map[string]interface{} {
	return map[string]interface{}{
		"categories": []PresetOption{
			{"read", "Read", "읽기", "파일/저장소 읽기만 수행 — 위험 낮음"},
			{"write", "Write", "쓰기", "파일/저장소 수정 — 변경 추적 필요"},
			{"execute", "Execute", "실행", "명령/프로세스 실행 — 승인 권장"},
			{"network", "Network", "네트워크", "외부 네트워크 접근 — 승인 권장"},
		},
		"tool_classes": []PresetOption{
			{"read", "read", "읽기", "저장소/파일 읽기 도구군"},
			{"write", "write", "쓰기", "파일 쓰기 도구군"},
			{"delete", "delete", "삭제", "파일 삭제 도구군"},
			{"shell", "shell", "셸", "셸/프로세스 실행 도구군"},
			{"network", "network", "네트워크", "네트워크 접근 도구군"},
			{"browser", "browser", "브라우저", "브라우저 자동화 도구군"},
		},
		"danger_levels": []PresetOption{
			{"low", "Low", "낮음", "읽기 전용 — 기본 허용 가능"},
			{"medium", "Medium", "중간", "수정 가능 — 감사 기록됨"},
			{"high", "High", "높음", "삭제/실행 — 승인 필요 권장"},
			{"critical", "Critical", "심각", "인프라/네트워크 — 항상 승인 필요"},
		},
	}
}

// SeedDefaultToolsCount seeds defaults and reports how many were
// actually added (UX2: honest feedback, no silent no-op).
func (s *Service) SeedDefaultToolsCount(orgID string) (int, error) {
	before, err := s.ListTools(orgID)
	if err != nil {
		return 0, err
	}
	beforeCount := len(before)
	if err := s.SeedDefaultTools(orgID); err != nil {
		return 0, err
	}
	after, err := s.ListTools(orgID)
	if err != nil {
		return 0, err
	}
	return len(after) - beforeCount, nil
}

// SetProjectAllowlist replaces a project's tool allowlist (feature 7).
func (s *Service) SetProjectAllowlist(orgID, projectID, grantedBy string, toolNames []string) error {
	if err := s.db.Where("organization_id = ? AND project_id = ?", orgID, projectID).
		Delete(&models.ProjectToolAllowlist{}).Error; err != nil {
		return err
	}
	for _, name := range toolNames {
		if name == "" {
			continue
		}
		row := models.ProjectToolAllowlist{
			Base:           models.Base{ID: models.GenerateID("pta")},
			OrganizationID: orgID,
			ProjectID:      projectID,
			ToolName:       name,
			GrantedBy:      grantedBy,
		}
		if err := s.db.Create(&row).Error; err != nil {
			return fmt.Errorf("tools: allowlist add %s: %w", name, err)
		}
	}
	return nil
}

// GetProjectAllowlist returns the project's allowed tool names.
func (s *Service) GetProjectAllowlist(orgID, projectID string) ([]models.ProjectToolAllowlist, error) {
	var rows []models.ProjectToolAllowlist
	if err := s.db.Where("organization_id = ? AND project_id = ?", orgID, projectID).
		Order("tool_name").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// VerifyToolDigest checks a reported runtime digest against the
// registered tool's integrity digest (E). Empty registered digest =
// no integrity pin (not enforced).
func (s *Service) VerifyToolDigest(tool models.Tool, reportedDigest string) (bool, string) {
	if tool.Signature == "" {
		return true, "no integrity digest registered"
	}
	if reportedDigest == "" {
		return false, "runtime digest missing — tool integrity pinned"
	}
	if tool.Signature != reportedDigest {
		return false, "runtime digest mismatch"
	}
	return true, ""
}

var _ = json.Marshal
