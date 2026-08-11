package gitscm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements Git/SCM as a first-class control-plane subsystem (PRD §18).
type Service struct {
	db *gorm.DB
}

// New creates a new Git/SCM service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// CreateBaseline records an immutable task baseline at session start (PRD §18.3).
type BaselineRequest struct {
	OrganizationID string
	RepositoryID   string
	Branch         string
	CommitSHA      string
	CommitMessage  string
	AuthorName     string
	AuthorEmail    string
	CommittedAt    string
	WorktreeStatus string // git status output
	SessionID      string
	SubmodulesJSON string
	DependencyLock string // package-lock.json/go.sum hashes
}

// CreateBaseline creates a repository baseline.
func (s *Service) CreateBaseline(req BaselineRequest) (*models.RepoBaseline, error) {
	// Compute tree digest from commit + worktree state
	treeInput := fmt.Sprintf("%s|%s|%s", req.CommitSHA, req.Branch, req.WorktreeStatus)
	h := sha256.Sum256([]byte(treeInput))

	baseline := &models.RepoBaseline{
		RepositoryID:  req.RepositoryID,
		Branch:        req.Branch,
		CommitSHA:     req.CommitSHA,
		CommitMessage: req.CommitMessage,
		AuthorName:    req.AuthorName,
		AuthorEmail:   req.AuthorEmail,
		CommittedAt:   req.CommittedAt,
		TreeDigest:    "sha256:" + hex.EncodeToString(h[:]),
		OrgID:         req.OrganizationID,
		CreatedBy:     req.SessionID,
	}

	if err := s.db.Create(baseline).Error; err != nil {
		return nil, fmt.Errorf("gitscm: create baseline: %w", err)
	}
	return baseline, nil
}

// GetBaseline retrieves a baseline by ID.
func (s *Service) GetBaseline(baselineID string) (*models.RepoBaseline, error) {
	var baseline models.RepoBaseline
	if err := s.db.Where("id = ?", baselineID).First(&baseline).Error; err != nil {
		return nil, fmt.Errorf("gitscm: baseline not found")
	}
	return &baseline, nil
}

// SetBranchProtection sets branch protection level (PRD §18.5).
type BranchProtection string

const (
	BranchProtectionStandard  BranchProtection = "standard"
	BranchProtectionProtected BranchProtection = "protected"
	BranchProtectionLocked    BranchProtection = "locked"
	BranchProtectionRelease   BranchProtection = "release"
	BranchProtectionProduction BranchProtection = "production"
)

// SetBranchProtection configures protection level and approval requirements.
func (s *Service) SetBranchProtection(repoID, branchName string, level BranchProtection, requiresApproval bool) error {
	result := s.db.Model(&models.Branch{}).
		Where("repository_id = ? AND name = ?", repoID, branchName).
		Updates(map[string]interface{}{
			"protection_level":  string(level),
			"requires_approval": requiresApproval,
		})
	if result.Error != nil {
		return fmt.Errorf("gitscm: set branch protection: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		// Create branch record if it doesn't exist
		branch := &models.Branch{
			RepositoryID:     repoID,
			Name:             branchName,
			ProtectionLevel:  string(level),
			RequiresApproval: requiresApproval,
			Status:           "active",
		}
		return s.db.Create(branch).Error
	}
	return nil
}

// GetBranchProtection returns the protection level for a branch.
func (s *Service) GetBranchProtection(repoID, branchName string) (*models.Branch, error) {
	var branch models.Branch
	err := s.db.Where("repository_id = ? AND name = ?", repoID, branchName).First(&branch).Error
	if err != nil {
		return nil, fmt.Errorf("gitscm: branch not found")
	}
	return &branch, nil
}

// IsEditAllowed checks if AI edits are allowed on a branch based on protection level (PRD §18.5).
func (s *Service) IsEditAllowed(repoID, branchName string) (bool, string, error) {
	branch, err := s.GetBranchProtection(repoID, branchName)
	if err != nil {
		// No branch record = default standard
		return true, "standard", nil
	}

	switch BranchProtection(branch.ProtectionLevel) {
	case BranchProtectionLocked, BranchProtectionProduction:
		return false, branch.ProtectionLevel, nil
	case BranchProtectionRelease:
		// Release branches require approval
		return !branch.RequiresApproval, branch.ProtectionLevel, nil
	default:
		return true, branch.ProtectionLevel, nil
	}
}

// RecordCommitBinding links a git commit to a change set (PRD §18.6).
type CommitBindingRequest struct {
	OrganizationID string
	RepositoryID   string
	CommitSHA      string
	ChangeSetID    string
	SessionID      string
	Branch         string
}

// RecordCommitBinding creates a binding between a commit and provenance.
func (s *Service) RecordCommitBinding(req CommitBindingRequest) error {
	bindingData := fmt.Sprintf("%s|%s|%s|%s", req.OrganizationID, req.RepositoryID, req.CommitSHA, req.ChangeSetID)
	h := sha256.Sum256([]byte(bindingData))

	binding := &models.CommitBinding{
		OrganizationID: req.OrganizationID,
		RepositoryID:   req.RepositoryID,
		CommitSHA:      req.CommitSHA,
		ChangeSetID:    req.ChangeSetID,
		SessionID:      req.SessionID,
		Branch:         req.Branch,
		BoundAt:        time.Now().Format(time.RFC3339),
		BindingDigest:  "sha256:" + hex.EncodeToString(h[:]),
	}
	return s.db.Create(binding).Error
}

// LookupCommitProvenance finds provenance for a specific commit.
func (s *Service) LookupCommitProvenance(orgID, repoID, commitSHA string) ([]models.CommitBinding, error) {
	var bindings []models.CommitBinding
	err := s.db.Where("organization_id = ? AND repository_id = ? AND commit_sha = ?",
		orgID, repoID, commitSHA).Find(&bindings).Error
	return bindings, err
}

// SCMType identifies the source code management system.
type SCMType string

const (
	SCMGitHub   SCMType = "github"
	SCMGitLab   SCMType = "gitlab"
	SCMGitea    SCMType = "gitea"
	SCMBitbucket SCMType = "bitbucket"
	SCMGit      SCMType = "git"
)

// RegisterRepository registers a new repository under a project.
type RegisterRepoRequest struct {
	OrganizationID  string
	ProjectID       string
	Name            string
	FullName        string
	CloneURL        string
	SCMType         SCMType
	SCMProvider     string // e.g. "github.example.com"
	DefaultBranch   string
	Sensitivity     string // public, internal, confidential, restricted
	CodeOwnersJSON  string
	CIConfigRef     string
}

// RegisterRepository creates a new repository record.
func (s *Service) RegisterRepository(req RegisterRepoRequest) (*models.Repository, error) {
	if req.SCMType == "" {
		req.SCMType = SCMGit
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}
	if req.Sensitivity == "" {
		req.Sensitivity = "internal"
	}

	repo := &models.Repository{
		AuditBase: models.AuditBase{
			OrganizationID: req.OrganizationID,
			ProjectID:      req.ProjectID,
		},
		Name:          req.Name,
		FullName:      req.FullName,
		CloneURL:      req.CloneURL,
		SCMType:       string(req.SCMType),
		SCMProvider:   req.SCMProvider,
		DefaultBranch: req.DefaultBranch,
		Sensitivity:   req.Sensitivity,
		Status:        "active",
	}

	if err := s.db.Create(repo).Error; err != nil {
		return nil, fmt.Errorf("gitscm: register repository: %w", err)
	}
	return repo, nil
}

// ListRepositoriesByProject returns repositories for a project.
func (s *Service) ListRepositoriesByProject(orgID, projectID string) ([]models.Repository, error) {
	var repos []models.Repository
	err := s.db.Where("organization_id = ? AND project_id = ?", orgID, projectID).Find(&repos).Error
	return repos, err
}

// RepositoryHeatmapData represents repository sensitivity heatmap data (PRD §33.5).
type RepositoryHeatmapData struct {
	RepositoryID   string  `json:"repository_id"`
	RepositoryName string  `json:"repository_name"`
	Sensitivity    string  `json:"sensitivity"`
	AISessions     int     `json:"ai_sessions"`
	ChangesMade    int     `json:"changes_made"`
	SecurityFindings int   `json:"security_findings"`
	RiskScore      float64 `json:"risk_score"`
}

// GetRepositoryHeatmap returns sensitivity heatmap data for all repos in an org.
func (s *Service) GetRepositoryHeatmap(orgID string) ([]RepositoryHeatmapData, error) {
	var repos []models.Repository
	s.db.Where("organization_id = ?", orgID).Find(&repos)

	var result []RepositoryHeatmapData
	for _, repo := range repos {
		var sessionCount, changeCount, findingCount int64
		s.db.Model(&models.Session{}).Where("organization_id = ? AND repository_id = ?", orgID, repo.ID).Count(&sessionCount)
		s.db.Model(&models.ChangeSet{}).Where("organization_id = ? AND repository_id = ?", orgID, repo.ID).Count(&changeCount)
		s.db.Model(&models.SecurityFinding{}).Where("organization_id = ?", orgID).Count(&findingCount)

		riskScore := 0.0
		switch repo.Sensitivity {
		case "restricted":
			riskScore = 0.9
		case "confidential":
			riskScore = 0.7
		case "internal":
			riskScore = 0.4
		default:
			riskScore = 0.1
		}
		if findingCount > 0 {
			riskScore = min(riskScore+0.1*float64(findingCount), 1.0)
		}

		result = append(result, RepositoryHeatmapData{
			RepositoryID:   repo.ID,
			RepositoryName: repo.Name,
			Sensitivity:    repo.Sensitivity,
			AISessions:     int(sessionCount),
			ChangesMade:    int(changeCount),
			SecurityFindings: int(findingCount),
			RiskScore:      riskScore,
		})
	}
	return result, nil
}

// ParseGitURL extracts components from a Git remote URL.
func ParseGitURL(url string) (host, owner, repo string) {
	// Handle SSH: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@") {
		rest := strings.TrimPrefix(url, "git@")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) == 2 {
			host = parts[0]
			path := strings.TrimSuffix(parts[1], ".git")
			pathParts := strings.SplitN(path, "/", 2)
			if len(pathParts) == 2 {
				owner = pathParts[0]
				repo = pathParts[1]
			}
		}
		return
	}
	// Handle HTTPS: https://github.com/owner/repo.git
	if strings.HasPrefix(url, "http") {
		rest := strings.TrimPrefix(url, "https://")
		rest = strings.TrimPrefix(rest, "http://")
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) >= 3 {
			host = parts[0]
			owner = parts[1]
			repo = strings.TrimSuffix(parts[2], ".git")
		}
	}
	return
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// Ensure json import is used
var _ = json.Marshal
