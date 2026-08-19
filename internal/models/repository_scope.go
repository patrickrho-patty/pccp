package models

import "gorm.io/gorm"

// repository_scope.go holds query fragments shared by the API layer and
// the Git/SCM service.

// RepositorySessionIDs returns a subquery selecting the session IDs that
// belong to one repository. Security findings only carry session_id, so
// scoping findings to a repository goes through this subquery (PAT-1490);
// the findings list and the sensitivity heatmap must use the identical
// scope so a repo's "보안 발견" count reconciles with its drill-down.
func RepositorySessionIDs(db *gorm.DB, orgID, repoID string) *gorm.DB {
	return db.Model(&Session{}).Select("session_id").
		Where("organization_id = ? AND repository_id = ?", orgID, repoID)
}
