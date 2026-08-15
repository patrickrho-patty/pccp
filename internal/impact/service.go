package impact

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements Change Impact Intelligence (PRD §20).
// It maintains an impact graph and calculates AI Change Risk Scores.
type Service struct {
	db *gorm.DB
}

// New creates a new change impact service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// ImpactGraph represents the code dependency/impact graph (PRD §20.1).
type ImpactGraph struct {
	Nodes              []ImpactNode `json:"nodes"`
	Edges              []ImpactEdge `json:"edges"`
	AffectedCallers    []ImpactNode `json:"affected_callers"`
	TestsCovering      []string     `json:"tests_covering"`
	SuggestedReviewers []string     `json:"suggested_reviewers"`
}

// ImpactNode represents a node in the impact graph.
type ImpactNode struct {
	Type       string `json:"type"` // repository, module, symbol, api, db_table, service, test
	ID         string `json:"id"`
	Name       string `json:"name"`
	Repository string `json:"repository,omitempty"`
	Path       string `json:"path,omitempty"`
	Owner      string `json:"owner,omitempty"`
}

// ImpactEdge represents an edge in the impact graph.
type ImpactEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"` // calls, imports, depends_on, tests, owns
}

// AnalyzeChange analyzes the potential impact of a code change.
type AnalyzeRequest struct {
	OrganizationID string   `json:"organization_id"`
	RepositoryID   string   `json:"repository_id"`
	FilePath       string   `json:"file_path"`
	SymbolsChanged []string `json:"symbols_changed"`
	Languages      []string `json:"languages"`
	IsAuth         bool     `json:"is_auth"`           // touches authentication/authorization
	IsCrypto       bool     `json:"is_crypto"`         // touches cryptography
	IsDBMigration  bool     `json:"is_db_migration"`  // touches database schema/migration
	IsAPIContract  bool     `json:"is_api_contract"` // touches external API contract
	IsConfig       bool     `json:"is_config"`         // touches production configuration
	Dependencies   []string `json:"dependencies"`      // dependency changes
}

// RiskScore is the AI Change Risk Score (PRD §20.2).
type RiskScore struct {
	Score            float64      `json:"score"` // 0-100
	Level            string       `json:"level"` // low, medium, high, critical
	Factors          []RiskFactor `json:"factors"`
	RequiresApproval bool         `json:"requires_approval"`
	Reviewers        []string     `json:"suggested_reviewers"`
}

// RiskFactor is a single contributor to the risk score.
type RiskFactor struct {
	Name         string  `json:"name"`
	NameKo       string  `json:"name_ko"`
	Weight       float64 `json:"weight"`
	Contribution float64 `json:"contribution"`
	Description  string  `json:"description"`
}

// AnalyzeChange analyzes the potential impact of a code change.
func (s *Service) AnalyzeChange(req AnalyzeRequest) (*ImpactGraph, *RiskScore, error) {
	graph := &ImpactGraph{
		Nodes: []ImpactNode{},
		Edges: []ImpactEdge{},
	}

	// Build graph nodes for changed symbols
	for _, sym := range req.SymbolsChanged {
		graph.Nodes = append(graph.Nodes, ImpactNode{
			Type:       "symbol",
			ID:         fmt.Sprintf("%s/%s/%s", req.RepositoryID, req.FilePath, sym),
			Name:       sym,
			Repository: req.RepositoryID,
			Path:       req.FilePath,
		})
	}

	// Calculate risk score
	score := s.calculateRiskScore(req)

	// Find suggested reviewers (from ownership)
	graph.SuggestedReviewers = s.findReviewers(req.OrganizationID, req.RepositoryID, req.FilePath)

	return graph, score, nil
}

// calculateRiskScore computes the transparent risk score (PRD §20.2).
func (s *Service) calculateRiskScore(req AnalyzeRequest) *RiskScore {
	var factors []RiskFactor
	totalScore := 0.0

	// Factor: Authentication/authorization code touched
	if req.IsAuth {
		f := RiskFactor{
			Name:         "auth_code_touched",
			NameKo:       "인증/인가 코드 수정",
			Weight:       25,
			Contribution: 25,
			Description:  "인증 또는 인가 관련 코드가 수정됨",
		}
		factors = append(factors, f)
		totalScore += 25
	}

	// Factor: Cryptography
	if req.IsCrypto {
		f := RiskFactor{
			Name:         "crypto_touched",
			NameKo:       "암호화 코드 수정",
			Weight:       20,
			Contribution: 20,
			Description:  "암호화 관련 코드가 수정됨",
		}
		factors = append(factors, f)
		totalScore += 20
	}

	// Factor: Database migration
	if req.IsDBMigration {
		f := RiskFactor{
			Name:         "db_migration",
			NameKo:       "데이터베이스 마이그레이션",
			Weight:       20,
			Contribution: 20,
			Description:  "데이터베이스 스키마 또는 마이그레이션 수정",
		}
		factors = append(factors, f)
		totalScore += 20
	}

	// Factor: API contract
	if req.IsAPIContract {
		f := RiskFactor{
			Name:         "api_contract",
			NameKo:       "API 계약 수정",
			Weight:       15,
			Contribution: 15,
			Description:  "외부 API 계약이 수정됨",
		}
		factors = append(factors, f)
		totalScore += 15
	}

	// Factor: Production configuration
	if req.IsConfig {
		f := RiskFactor{
			Name:         "prod_config",
			NameKo:       "프로덕션 설정 수정",
			Weight:       15,
			Contribution: 15,
			Description:  "프로덕션 설정 파일이 수정됨",
		}
		factors = append(factors, f)
		totalScore += 15
	}

	// Factor: Number of symbols changed
	symbolCount := len(req.SymbolsChanged)
	if symbolCount > 5 {
		f := RiskFactor{
			Name:         "broad_change",
			NameKo:       "광범위한 변경",
			Weight:       10,
			Contribution: 10,
			Description:  fmt.Sprintf("%d개 심볼 수정됨", symbolCount),
		}
		factors = append(factors, f)
		totalScore += 10
	}

	// Factor: Dependency changes
	if len(req.Dependencies) > 0 {
		f := RiskFactor{
			Name:         "dependency_change",
			NameKo:       "의존성 변경",
			Weight:       10,
			Contribution: float64(len(req.Dependencies)) * 3,
			Description:  fmt.Sprintf("%d개 의존성 변경됨", len(req.Dependencies)),
		}
		factors = append(factors, f)
		totalScore += f.Contribution
	}

	// Clamp
	if totalScore > 100 {
		totalScore = 100
	}

	// Determine level
	level := "low"
	requiresApproval := false
	switch {
	case totalScore >= 75:
		level = "critical"
		requiresApproval = true
	case totalScore >= 50:
		level = "high"
		requiresApproval = true
	case totalScore >= 25:
		level = "medium"
	}

	return &RiskScore{
		Score:            totalScore,
		Level:            level,
		Factors:          factors,
		RequiresApproval: requiresApproval,
	}
}

// findReviewers suggests reviewers based on ownership and impact area.
func (s *Service) findReviewers(orgID, repoID, filePath string) []string {
	if s.db == nil {
		return nil
	}
	var reviewers []string

	// Look for repository owner
	var repo models.Repository
	if s.db.Where("id = ? AND organization_id = ?", repoID, orgID).First(&repo).Error == nil {
		if repo.OwnerID != "" {
			reviewers = append(reviewers, repo.OwnerID)
		}
	}

	// Find users who have edited this file recently (from provenance)
	var spans []models.ProvenanceSpan
	s.db.Where("repository_id = ? AND file_path = ?", repoID, filePath).
		Distinct("user_id").Limit(5).Find(&spans)
	for _, span := range spans {
		if span.UserID != "" {
			reviewers = append(reviewers, span.UserID)
		}
	}

	return reviewers
}

// RecordImpactAnalysis persists an impact analysis for a change set.
type RecordRequest struct {
	OrganizationID string       `json:"organization_id"`
	RepositoryID   string       `json:"repository_id"`
	ChangeSetID    string       `json:"change_set_id"`
	SessionID      string       `json:"session_id"`
	RiskScore      *RiskScore   `json:"risk_score"`
	ImpactGraph    *ImpactGraph `json:"impact_graph"`
}

// RecordImpactAnalysis stores the analysis result for future reference.
func (s *Service) RecordImpactAnalysis(req RecordRequest) error {
	// Store as JSON in audit event details
	details, _ := json.Marshal(map[string]interface{}{
		"change_set_id": req.ChangeSetID,
		"risk_score":    req.RiskScore,
		"impact_graph":  req.ImpactGraph,
	})

	auditEvent := &models.AuditEvent{
		OrganizationID: req.OrganizationID,
		EventType:      "cp.change.impact_analysis",
		Action:         "impact_analysis",
		ResourceType:   "change_set",
		ResourceID:     req.ChangeSetID,
		Details:        string(details),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return s.db.Create(auditEvent).Error
}

// DetectSecuritySensitivePath checks if a file path contains security-sensitive patterns.
type PathSensitivity struct {
	IsAuth   bool     `json:"is_auth"`
	IsCrypto bool     `json:"is_crypto"`
	IsDB     bool     `json:"is_db"`
	IsAPI    bool     `json:"is_api"`
	IsConfig bool     `json:"is_config"`
	Patterns []string `json:"patterns"`
}

// DetectPathSensitivity analyzes a file path for security-sensitive areas.
func DetectPathSensitivity(filePath string) PathSensitivity {
	result := PathSensitivity{}
	pathLower := strings.ToLower(filePath)

	authPatterns := []string{"auth", "login", "session", "password", "credential", "token", "permission", "rbac", "acl"}
	for _, p := range authPatterns {
		if strings.Contains(pathLower, p) {
			result.IsAuth = true
			result.Patterns = append(result.Patterns, p)
			break
		}
	}

	cryptoPatterns := []string{"crypto", "cipher", "encrypt", "decrypt", "hash", "sign", "key", "tls", "ssl"}
	for _, p := range cryptoPatterns {
		if strings.Contains(pathLower, p) {
			result.IsCrypto = true
			result.Patterns = append(result.Patterns, p)
			break
		}
	}

	dbPatterns := []string{"migration", "schema", "model", "entity", "dao", "repository", "sql"}
	for _, p := range dbPatterns {
		if strings.Contains(pathLower, p) {
			result.IsDB = true
			result.Patterns = append(result.Patterns, p)
			break
		}
	}

	apiPatterns := []string{"api", "controller", "handler", "endpoint", "route", "grpc", "proto"}
	for _, p := range apiPatterns {
		if strings.Contains(pathLower, p) {
			result.IsAPI = true
			result.Patterns = append(result.Patterns, p)
			break
		}
	}

	configPatterns := []string{".env", "config", "docker-compose", "k8s", "helm", "terraform"}
	for _, p := range configPatterns {
		if strings.Contains(pathLower, p) {
			result.IsConfig = true
			result.Patterns = append(result.Patterns, p)
			break
		}
	}

	return result
}
