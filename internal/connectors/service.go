package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Service implements Enterprise Integration Requirements (PRD §32).
// Provides adapter framework for Jira, GitHub, GitLab, Slack, KakaoWork, etc.
type Service struct {
	mu         sync.RWMutex
	connectors map[string]*Connector
}

// ConnectorType identifies the type of enterprise system.
type ConnectorType string

const (
	TypeJira      ConnectorType = "jira"
	TypeGitHub    ConnectorType = "github"
	TypeGitLab    ConnectorType = "gitlab"
	TypeSlack     ConnectorType = "slack"
	TypeKakaoWork ConnectorType = "kakaowork"
	TypeTeams     ConnectorType = "teams"
	TypeServiceNow ConnectorType = "servicenow"
	TypeJenkins   ConnectorType = "jenkins"
)

// Connector represents a configured enterprise integration.
type Connector struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organization_id"`
	Type           ConnectorType  `json:"type"`
	Name           string         `json:"name"`
	NameKo         string         `json:"name_ko"`
	BaseURL        string         `json:"base_url"`
	AuthType       string         `json:"auth_type"` // api_key, oauth, bearer
	AuthToken      string         `json:"-"`         // never serialized
	Config         map[string]string `json:"config,omitempty"`
	Status         string         `json:"status"` // active, disabled, error
	LastError      string         `json:"last_error,omitempty"`
	LastSyncAt     string         `json:"last_sync_at,omitempty"`
	CreatedAt      string         `json:"created_at"`
}

// New creates a new connectors service.
func New() *Service {
	return &Service{connectors: make(map[string]*Connector)}
}

// Register adds a new enterprise connector.
func (s *Service) Register(conn Connector) (*Connector, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if conn.ID == "" {
		conn.ID = fmt.Sprintf("conn_%s_%d", conn.Type, time.Now().UnixMilli())
	}
	if conn.Status == "" {
		conn.Status = "active"
	}
	conn.CreatedAt = time.Now().Format(time.RFC3339)

	s.connectors[conn.ID] = &conn
	return &conn, nil
}

// Get retrieves a connector by ID.
func (s *Service) Get(id string) (*Connector, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	conn, ok := s.connectors[id]
	if !ok {
		return nil, fmt.Errorf("connectors: not found")
	}
	return conn, nil
}

// List returns all connectors for an organization.
func (s *Service) List(orgID string) []*Connector {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*Connector
	for _, c := range s.connectors {
		if c.OrganizationID == orgID {
			result = append(result, c)
		}
	}
	return result
}

// Disable deactivates a connector.
func (s *Service) Disable(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.connectors[id]; ok {
		c.Status = "disabled"
		return nil
	}
	return fmt.Errorf("connectors: not found")
}

// IssueLink represents a linked issue from an external system (PRD §32.3).
type IssueLink struct {
	ConnectorID  string `json:"connector_id"`
	IssueType    string `json:"issue_type"` // jira_issue, github_issue, gitlab_issue
	ExternalID   string `json:"external_id"`
	ExternalURL  string `json:"external_url"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Assignee     string `json:"assignee,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	ChangeSetID  string `json:"change_set_id,omitempty"`
}

// FetchIssues retrieves issues from an external system.
func (s *Service) FetchIssues(ctx context.Context, connectorID string) ([]IssueLink, error) {
	conn, err := s.Get(connectorID)
	if err != nil {
		return nil, err
	}

	switch conn.Type {
	case TypeJira:
		return s.fetchJiraIssues(ctx, conn)
	case TypeGitHub:
		return s.fetchGitHubIssues(ctx, conn)
	case TypeGitLab:
		return s.fetchGitLabIssues(ctx, conn)
	default:
		return nil, fmt.Errorf("connectors: issue fetching not supported for %s", conn.Type)
	}
}

// SendMessage sends a notification to an external chat system (PRD §32.6).
func (s *Service) SendMessage(ctx context.Context, connectorID string, message string) error {
	conn, err := s.Get(connectorID)
	if err != nil {
		return err
	}

	switch conn.Type {
	case TypeSlack:
		return s.sendSlackMessage(ctx, conn, message)
	case TypeKakaoWork:
		return s.sendKakaoWorkMessage(ctx, conn, message)
	case TypeTeams:
		return s.sendTeamsMessage(ctx, conn, message)
	default:
		return fmt.Errorf("connectors: messaging not supported for %s", conn.Type)
	}
}

// TriggerCINotifies triggers CI/CD webhook (PRD §32.2).
func (s *Service) TriggerCI(ctx context.Context, connectorID, repoID, branch string) error {
	conn, err := s.Get(connectorID)
	if err != nil {
		return err
	}

	switch conn.Type {
	case TypeJenkins:
		return s.triggerJenkins(ctx, conn, repoID, branch)
	case TypeGitHub:
		return s.triggerGitHubActions(ctx, conn, repoID, branch)
	case TypeGitLab:
		return s.triggerGitLabCI(ctx, conn, repoID, branch)
	default:
		return fmt.Errorf("connectors: CI not supported for %s", conn.Type)
	}
}

// --- Implementation Stubs ---

func (s *Service) fetchJiraIssues(ctx context.Context, conn *Connector) ([]IssueLink, error) {
	// Production: call Jira REST API
	return []IssueLink{}, nil
}

func (s *Service) fetchGitHubIssues(ctx context.Context, conn *Connector) ([]IssueLink, error) {
	return []IssueLink{}, nil
}

func (s *Service) fetchGitLabIssues(ctx context.Context, conn *Connector) ([]IssueLink, error) {
	return []IssueLink{}, nil
}

func (s *Service) sendSlackMessage(ctx context.Context, conn *Connector, message string) error {
	if conn.BaseURL == "" || conn.AuthToken == "" {
		return fmt.Errorf("connectors: Slack not configured")
	}
	// Production: POST to Slack webhook
	return nil
}

func (s *Service) sendKakaoWorkMessage(ctx context.Context, conn *Connector, message string) error {
	if conn.BaseURL == "" {
		return fmt.Errorf("connectors: Kakao Work not configured")
	}
	return nil
}

func (s *Service) sendTeamsMessage(ctx context.Context, conn *Connector, message string) error {
	return nil
}

func (s *Service) triggerJenkins(ctx context.Context, conn *Connector, repoID, branch string) error {
	return nil
}

func (s *Service) triggerGitHubActions(ctx context.Context, conn *Connector, repoID, branch string) error {
	return nil
}

func (s *Service) triggerGitLabCI(ctx context.Context, conn *Connector, repoID, branch string) error {
	return nil
}

// SupportedConnectorTypes returns all supported connector types with Korean names.
func SupportedConnectorTypes() []struct {
	Type   ConnectorType `json:"type"`
	NameKo string        `json:"name_ko"`
} {
	return []struct {
		Type   ConnectorType `json:"type"`
		NameKo string        `json:"name_ko"`
	}{
		{TypeJira, "지라 (Jira)"},
		{TypeGitHub, "깃허브 (GitHub)"},
		{TypeGitLab, "깃랩 (GitLab)"},
		{TypeSlack, "슬랙 (Slack)"},
		{TypeKakaoWork, "카카오워크 (Kakao Work)"},
		{TypeTeams, "팀즈 (Teams)"},
		{TypeServiceNow, "서비스나우 (ServiceNow)"},
		{TypeJenkins, "젠킨스 (Jenkins)"},
	}
}

var _ = http.MethodGet
var _ = json.Marshal
