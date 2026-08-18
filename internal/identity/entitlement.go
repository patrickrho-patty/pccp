package identity

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// entitlement.go: developer entitlement (web/01 B5). Roles/UserRoles are
// scoped developer entitlements — what a governed developer may do through
// their harness (distinct from console operator RBAC, which lives in
// admin_credentials). The relay pipeline evaluates these on the live path.

// defaultDeveloperRoles are the seeded entitlement roles every org gets.
var defaultDeveloperRoles = []struct {
	Key         string
	Name        string
	NameKo      string
	Permissions []string
}{
	{"project-developer", "Project Developer", "프로젝트 개발자",
		[]string{"session:open", "inference:use", "project:read", "repo:read"}},
	{"repo-reader", "Repository Reader", "저장소 읽기",
		[]string{"session:open", "inference:use", "repo:read"}},
	{"model-user", "Model User", "모델 사용자",
		[]string{"session:open", "inference:use"}},
	{"global-developer", "Global Developer", "글로벌 개발자",
		[]string{"session:open", "inference:use", "project:read", "repo:read", "class:interactive-paid"}},
}

// EnsureDefaultRoles seeds the org's developer entitlement roles
// (idempotent — keyed by unique name within org).
func (s *Service) EnsureDefaultRoles(orgID string) error {
	for _, def := range defaultDeveloperRoles {
		var existing models.Role
		err := s.db.Where("organization_id = ? AND name = ?", orgID, def.Key).
			First(&existing).Error
		if err == nil {
			continue
		}
		perms, _ := json.Marshal(def.Permissions)
		role := models.Role{
			Base:           models.Base{ID: models.GenerateID("role")},
			OrganizationID: orgID,
			Name:           def.Key,
			NameKo:         def.NameKo,
			Permissions:    string(perms),
			IsSystem:       true,
		}
		if err := s.db.Create(&role).Error; err != nil {
			return fmt.Errorf("identity: seed role %s: %w", def.Key, err)
		}
	}
	return nil
}

// ListRoles returns the org's entitlement roles.
func (s *Service) ListRoles(orgID string) ([]models.Role, error) {
	if err := s.EnsureDefaultRoles(orgID); err != nil {
		return nil, err
	}
	var roles []models.Role
	if err := s.db.Where("organization_id = ?", orgID).Order("name").Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// UserEntitlements returns the user's role assignments with role names.
func (s *Service) UserEntitlements(orgID, userID string) ([]models.UserRole, []models.Role, error) {
	var assignments []models.UserRole
	if err := s.db.Where("organization_id = ? AND user_id = ?", orgID, userID).
		Find(&assignments).Error; err != nil {
		return nil, nil, err
	}
	var roles []models.Role
	for _, a := range assignments {
		var role models.Role
		if s.db.Where("organization_id = ? AND id = ?", orgID, a.RoleID).First(&role).Error == nil {
			roles = append(roles, role)
		}
	}
	return assignments, roles, nil
}

// AssignUserRole grants a scoped entitlement to a developer.
func (s *Service) AssignUserRole(orgID, userID, roleID, scope, scopeID string) (*models.UserRole, error) {
	var role models.Role
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, roleID).First(&role).Error; err != nil {
		return nil, fmt.Errorf("identity: role %s not found in org", roleID)
	}
	var existing models.UserRole
	err := s.db.Where("organization_id = ? AND user_id = ? AND role_id = ? AND scope_id = ?",
		orgID, userID, roleID, scopeID).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	assignment := models.UserRole{
		Base:           models.Base{ID: models.GenerateID("ur")},
		UserID:         userID,
		RoleID:         roleID,
		OrganizationID: orgID,
		Scope:          scope,
		ScopeID:        scopeID,
	}
	if err := s.db.Create(&assignment).Error; err != nil {
		return nil, err
	}
	s.recordAudit(orgID, "cp.user.entitlement.granted", "admin", "user", userID,
		fmt.Sprintf(`{"role_id":"%s","scope":"%s","scope_id":"%s"}`, roleID, scope, scopeID))
	return &assignment, nil
}

// RemoveUserRole revokes an entitlement assignment.
func (s *Service) RemoveUserRole(orgID, userID, roleID, scopeID string) error {
	err := s.db.Where("organization_id = ? AND user_id = ? AND role_id = ? AND scope_id = ?",
		orgID, userID, roleID, scopeID).Delete(&models.UserRole{}).Error
	if err != nil {
		return err
	}
	s.recordAudit(orgID, "cp.user.entitlement.revoked", "admin", "user", userID,
		fmt.Sprintf(`{"role_id":"%s","scope_id":"%s"}`, roleID, scopeID))
	return nil
}

// DeveloperPermissions flattens a developer's entitlements into a
// permission set the relay evaluates on the live path.
func (s *Service) DeveloperPermissions(orgID, userID string) (map[string]bool, error) {
	assignments, roles, err := s.UserEntitlements(orgID, userID)
	if err != nil {
		return nil, err
	}
	perms := map[string]bool{}
	for i := range assignments {
		var list []string
		if err := json.Unmarshal([]byte(roles[i].Permissions), &list); err != nil {
			continue
		}
		for _, p := range list {
			perms[p] = true
		}
	}
	return perms, nil
}

// EvaluateEntitlement answers whether the developer holds a permission.
func (s *Service) EvaluateEntitlement(orgID, userID, permission string) (bool, error) {
	perms, err := s.DeveloperPermissions(orgID, userID)
	if err != nil {
		return false, err
	}
	return perms[permission], nil
}

// SweepExpiredContractors auto-disables users whose contract window has
// ended (web/01 A5). Returns the number of users disabled.
//
// INVARIANT: this system actor only ever performs active → suspended,
// which is a legal edge in the canonical user lifecycle table
// (internal/models/user_lifecycle.go, shared by all status writers).
// If that table changes, this sweep must change with it.
func (s *Service) SweepExpiredContractors() int {
	var users []models.User
	s.db.Where("contractor_info IS NOT NULL AND contractor_info != '' AND status = 'active'").
		Find(&users)
	today := time.Now().Format("2006-01-02")
	n := 0
	for _, u := range users {
		var profile ContractorProfile
		if err := json.Unmarshal([]byte(u.ContractorInfo), &profile); err != nil {
			continue
		}
		if profile.ContractEnd != "" && profile.ContractEnd < today {
			s.db.Model(&u).Update("status", "suspended")
			s.recordAudit(u.OrganizationID, "cp.user.contract_expired", "system", "user", u.ID,
				fmt.Sprintf(`{"contract_end":"%s"}`, profile.ContractEnd))
			n++
		}
	}
	return n
}

// ContractorProfile is the structured contractor record (web/01 A5);
// serialized into User.ContractorInfo.
type ContractorProfile struct {
	SponsorUserID     string   `json:"sponsor_user_id"`
	Company           string   `json:"company"`
	ContractStart     string   `json:"contract_start"` // YYYY-MM-DD
	ContractEnd       string   `json:"contract_end"`   // YYYY-MM-DD
	AllowedRepoIDs    []string `json:"allowed_repo_ids"`
	AllowedModelClass []string `json:"allowed_model_classes"`
	NetworkZone       string   `json:"network_zone"`
}
