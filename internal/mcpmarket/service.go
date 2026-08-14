package mcpmarket

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// Service implements the MCP Registry and Marketplace (PRD §6 Phase 6, §17.2).
// Provides a managed registry for MCP servers with discovery, versioning,
// and organizational approval workflows.
type Service struct {
	mu        sync.RWMutex
	listings  map[string]*MCPListing // listing ID → listing
}

// New creates a new MCP marketplace service.
func New() *Service {
	return &Service{listings: make(map[string]*MCPListing)}
}

// MCPListing represents an MCP server listing in the marketplace.
type MCPListing struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	NameKo            string            `json:"name_ko"`
	Description       string            `json:"description"`
	DescriptionKo     string            `json:"description_ko"`
	Publisher         string            `json:"publisher"`
	PublisherVerified bool              `json:"publisher_verified"`
	Category          string            `json:"category"` // filesystem, database, api, search, tools
	Tags              []string          `json:"tags"`
	IconURL           string            `json:"icon_url,omitempty"`
	// Versioning
	Versions          []MCPVersion      `json:"versions"`
	LatestVersion     string            `json:"latest_version"`
	// Security
	SecurityAudit     *SecurityAudit    `json:"security_audit,omitempty"`
	RiskLevel         string            `json:"risk_level"` // low, medium, high
	RequiredPermissions []string        `json:"required_permissions"`
	NetworkAccess     bool              `json:"network_access"`
	FileAccess        bool              `json:"file_access"`
	// Marketplace
	DownloadCount     int64             `json:"download_count"`
	Rating            float64           `json:"rating"`
	Reviews           []MCPReview       `json:"reviews,omitempty"`
	// Status
	Status            string            `json:"status"` // listed, deprecated, removed
	ListedAt          string            `json:"listed_at"`
	UpdatedAt         string            `json:"updated_at"`
}

// MCPVersion represents a specific version of an MCP server.
type MCPVersion struct {
	Version       string `json:"version"`
	PackageHash   string `json:"package_hash"`
	Signature     string `json:"signature"`
	ContainerDigest string `json:"container_digest"`
	Changelog     string `json:"changelog"`
	ChangelogKo   string `json:"changelog_ko,omitempty"`
	ReleasedAt    string `json:"released_at"`
	Deprecated    bool   `json:"deprecated"`
}

// SecurityAudit represents a security audit of an MCP server.
type SecurityAudit struct {
	Auditor       string   `json:"auditor"`
	AuditedAt     string   `json:"audited_at"`
	Result        string   `json:"result"` // passed, failed, conditional
	Findings      []string `json:"findings"`
	Recommendations []string `json:"recommendations"`
}

// MCPReview represents a user review of an MCP server.
type MCPReview struct {
	UserID    string  `json:"user_id"`
	UserName  string  `json:"user_name"`
	Rating    int     `json:"rating"` // 1-5
	Comment   string  `json:"comment"`
	CreatedAt string  `json:"created_at"`
}

// PublishListing publishes a new MCP server to the marketplace.
func (s *Service) PublishListing(listing MCPListing) (*MCPListing, error) {
	if listing.ID == "" {
		listing.ID = dari.GenerateID("mcp_market")
	}
	if listing.Status == "" {
		listing.Status = "listed"
	}
	now := time.Now().Format(time.RFC3339)
	listing.ListedAt = now
	listing.UpdatedAt = now

	s.mu.Lock()
	s.listings[listing.ID] = &listing
	s.mu.Unlock()

	return &listing, nil
}

// GetListing returns a marketplace listing.
func (s *Service) GetListing(id string) (*MCPListing, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	listing, ok := s.listings[id]
	if !ok {
		return nil, fmt.Errorf("mcpmarket: listing not found")
	}
	return listing, nil
}

// SearchListings searches the marketplace by query.
func (s *Service) SearchListings(query, category string) []*MCPListing {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*MCPListing
	for _, l := range s.listings {
		if l.Status != "listed" {
			continue
		}
		if category != "" && l.Category != category {
			continue
		}
		if query != "" {
			match := false
			if contains(l.Name, query) || contains(l.Description, query) ||
				contains(l.NameKo, query) || contains(l.DescriptionKo, query) {
				match = true
			}
			for _, tag := range l.Tags {
				if contains(tag, query) {
					match = true
				}
			}
			if !match {
				continue
			}
		}
		results = append(results, l)
	}
	return results
}

// AddVersion adds a new version to an existing listing.
func (s *Service) AddVersion(listingID string, version MCPVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	listing, ok := s.listings[listingID]
	if !ok {
		return fmt.Errorf("mcpmarket: listing not found")
	}

	version.ReleasedAt = time.Now().Format(time.RFC3339)
	listing.Versions = append(listing.Versions, version)
	listing.LatestVersion = version.Version
	listing.UpdatedAt = time.Now().Format(time.RFC3339)

	return nil
}

// AddReview adds a review to a listing.
func (s *Service) AddReview(listingID string, review MCPReview) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	listing, ok := s.listings[listingID]
	if !ok {
		return fmt.Errorf("mcpmarket: listing not found")
	}

	review.CreatedAt = time.Now().Format(time.RFC3339)
	listing.Reviews = append(listing.Reviews, review)

	// Recalculate rating
	total := 0
	for _, r := range listing.Reviews {
		total += r.Rating
	}
	listing.Rating = float64(total) / float64(len(listing.Reviews))

	return nil
}

// DeprecateListing marks a listing as deprecated.
func (s *Service) DeprecateListing(listingID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	listing, ok := s.listings[listingID]
	if !ok {
		return fmt.Errorf("mcpmarket: listing not found")
	}

	listing.Status = "deprecated"
	listing.UpdatedAt = time.Now().Format(time.RFC3339)
	return nil
}

// IncrementDownload increments the download counter.
func (s *Service) IncrementDownload(listingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.listings[listingID]; ok {
		l.DownloadCount++
	}
}

// Categories returns all available categories with Korean names.
func Categories() []struct {
	ID     string `json:"id"`
	NameKo string `json:"name_ko"`
} {
	return []struct {
		ID     string `json:"id"`
		NameKo string `json:"name_ko"`
	}{
		{"filesystem", "파일 시스템"},
		{"database", "데이터베이스"},
		{"api", "API 통합"},
		{"search", "검색"},
		{"tools", "개발 도구"},
		{"korean", "한국 특화"},
		{"security", "보안"},
		{"devops", "데브옵스"},
	}
}

// SeedDefaultListings populates the marketplace with default MCP servers.
func (s *Service) SeedDefaultListings() {
	defaults := []MCPListing{
		{
			Name: "Filesystem MCP", NameKo: "파일시스템 MCP",
			Description: "Read/write access to local filesystem",
			DescriptionKo: "로컬 파일 시스템 읽기/쓰기",
			Publisher: "patty", PublisherVerified: true,
			Category: "filesystem", RiskLevel: "medium",
			FileAccess: true, Status: "listed",
		},
		{
			Name: "PostgreSQL MCP", NameKo: "PostgreSQL MCP",
			Description: "Query PostgreSQL databases",
			DescriptionKo: "PostgreSQL 데이터베이스 쿼리",
			Publisher: "patty", PublisherVerified: true,
			Category: "database", RiskLevel: "high",
			NetworkAccess: true, Status: "listed",
		},
		{
			Name: "Korean NLP MCP", NameKo: "한국어 NLP MCP",
			Description: "Korean language processing tools",
			DescriptionKo: "한국어 자연어 처리 도구",
			Publisher: "patty-korea", PublisherVerified: true,
			Category: "korean", Tags: []string{"korean", "nlp", "형태소"},
			RiskLevel: "low", Status: "listed",
		},
	}

	for _, d := range defaults {
		s.PublishListing(d)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (indexOf(s, substr) >= 0)))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

var _ = json.Marshal
