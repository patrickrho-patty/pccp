package network

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"gorm.io/gorm"
)

// Service implements the Network Broker (PRD §17.4).
// No agent or sandbox receives broad internet access by default.
type Service struct {
	db *gorm.DB
	mu sync.RWMutex
}

// New creates a new network broker service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// NetworkGrant is an approved scoped network access grant (PRD §17.4).
type NetworkGrant struct {
	ID              string `json:"id"`
	OrganizationID  string `json:"organization_id"`
	SessionID       string `json:"session_id,omitempty"`
	HarnessID       string `json:"harness_id,omitempty"`
	SandboxID       string `json:"sandbox_id,omitempty"`
	// Destination scoping
	DestinationName string `json:"destination_name"` // human-readable label
	DNSPattern      string `json:"dns_pattern"`      // e.g. "*.github.com"
	IPRange         string `json:"ip_range"`         // CIDR e.g. "192.168.1.0/24"
	Port            int    `json:"port"`             // 0 = any
	Protocol        string `json:"protocol"`         // tcp, udp, http, https
	Method          string `json:"method,omitempty"` // HTTP method if applicable
	Path            string `json:"path,omitempty"`   // URL path pattern
	// Purpose and constraints
	Purpose         string `json:"purpose"`          // why this grant exists
	Duration        string `json:"duration"`         // how long it's valid
	ByteBudget      int64  `json:"byte_budget"`      // max bytes (0 = unlimited)
	ContentClass    string `json:"content_class"`    // public, internal, confidential
	// State
	Status          string `json:"status"`           // active, expired, revoked
	GrantedAt       string `json:"granted_at"`
	ExpiresAt       string `json:"expires_at"`
	BytesUsed       int64  `json:"bytes_used"`
	GrantedBy       string `json:"granted_by"`       // admin or "auto"
}

// ConnectionRequest is a request to make a network connection.
type ConnectionRequest struct {
	OrganizationID string `json:"organization_id"`
	SessionID      string `json:"session_id"`
	HarnessID      string `json:"harness_id"`
	SandboxID      string `json:"sandbox_id,omitempty"`
	Destination    string `json:"destination"` // hostname:port or IP:port
	Protocol       string `json:"protocol"`
	Method         string `json:"method,omitempty"`
	Path           string `json:"path,omitempty"`
	Purpose        string `json:"purpose"`
}

// ConnectionDecision is the broker's decision.
type ConnectionDecision struct {
	Allowed   bool   `json:"allowed"`
	GrantID   string `json:"grant_id,omitempty"`
	Reason    string `json:"reason"`
	ProxyAddr string `json:"proxy_addr,omitempty"` // if connection must go through proxy
	ByteLimit int64  `json:"byte_limit,omitempty"`
	RuleIDs   []string `json:"rule_ids,omitempty"`
}

// DefaultPolicy returns the default deny-all network policy.
func DefaultPolicy() string {
	return "deny-all"
}

// EvaluateConnection checks whether a network connection request is permitted.
func (s *Service) EvaluateConnection(req ConnectionRequest) (*ConnectionDecision, error) {
	decision := &ConnectionDecision{
		Allowed: false,
	}

	// Parse destination
	host, port, err := net.SplitHostPort(req.Destination)
	if err != nil {
		host = req.Destination
		port = "0"
	}

	// 1. Check for explicitly blocked destinations
	if isBlockedDestination(host) {
		decision.Reason = fmt.Sprintf("destination %s is blocked (metadata/cloud-internal)", host)
		decision.RuleIDs = append(decision.RuleIDs, "network.blocked_destination")
		return decision, nil
	}

	// 2. Check for matching grants
	grants := s.getActiveGrants(req.OrganizationID, req.SessionID)
	for _, grant := range grants {
		if s.matchGrant(grant, host, port, req.Protocol, req.Method, req.Path) {
			decision.Allowed = true
			decision.GrantID = grant.ID
			decision.ByteLimit = grant.ByteBudget - grant.BytesUsed
			decision.Reason = fmt.Sprintf("matched grant: %s (%s)", grant.DestinationName, grant.Purpose)
			return decision, nil
		}
	}

	// 3. Check well-known safe destinations (package registries, etc.)
	if isKnownSafeDestination(host) {
		decision.Allowed = true
		decision.Reason = "well-known package/registry destination"
		decision.RuleIDs = append(decision.RuleIDs, "network.known_safe")
		return decision, nil
	}

	decision.Reason = fmt.Sprintf("no matching network grant for %s", req.Destination)
	decision.RuleIDs = append(decision.RuleIDs, "network.no_grant")
	return decision, nil
}

// Grant creates a new network access grant.
func (s *Service) Grant(grant NetworkGrant) (*NetworkGrant, error) {
	if grant.ID == "" {
		grant.ID = dari.GenerateID("net")
	}
	if grant.Status == "" {
		grant.Status = "active"
	}
	grant.GrantedAt = time.Now().Format(time.RFC3339)

	if grant.Duration != "" {
		dur, err := time.ParseDuration(grant.Duration)
		if err == nil {
			grant.ExpiresAt = time.Now().Add(dur).Format(time.RFC3339)
		}
	}

	// Store in audit
	details, _ := json.Marshal(grant)
	audit := &models.AuditEvent{
		OrganizationID: grant.OrganizationID,
		EventType:      "cp.network.grant_issued",
		ActorType:      "admin",
		Action:         "network_grant",
		ResourceType:   "network_grant",
		ResourceID:     grant.ID,
		Details:        string(details),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(audit)

	return &grant, nil
}

// RevokeGrant revokes a network grant.
func (s *Service) RevokeGrant(orgID, grantID, reason string) error {
	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.network.grant_revoked",
		ActorType:      "admin",
		Action:         "revoke_network_grant",
		ResourceType:   "network_grant",
		ResourceID:     grantID,
		Details:        fmt.Sprintf(`{"reason":"%s"}`, reason),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return s.db.Create(audit).Error
}

// RecordUsage records bytes used against a grant.
func (s *Service) RecordUsage(orgID, grantID string, bytes int64) error {
	// In production this would update a counter. For now we record in audit.
	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.network.usage",
		ActorType:      "system",
		Action:         "network_usage",
		ResourceType:   "network_grant",
		ResourceID:     grantID,
		Details:        fmt.Sprintf(`{"bytes":%d}`, bytes),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return s.db.Create(audit).Error
}

func (s *Service) getActiveGrants(orgID, sessionID string) []NetworkGrant {
	// Reconstruct from audit events (production would use a grants table)
	var events []models.AuditEvent
	s.db.Where("organization_id = ? AND event_type = 'cp.network.grant_issued' AND resource_type = 'network_grant'",
		orgID).Find(&events)

	var grants []NetworkGrant
	for _, e := range events {
		var grant NetworkGrant
		if json.Unmarshal([]byte(e.Details), &grant); grant.Status == "active" {
			// Check if it matches the session or is org-wide
			if grant.SessionID == "" || grant.SessionID == sessionID {
				grants = append(grants, grant)
			}
		}
	}
	return grants
}

func (s *Service) matchGrant(grant NetworkGrant, host, port, protocol, method, path string) bool {
	// DNS pattern matching
	if grant.DNSPattern != "" {
		if !matchDNSPattern(grant.DNSPattern, host) {
			return false
		}
	}
	// Port matching
	if grant.Port > 0 {
		var p int
		fmt.Sscanf(port, "%d", &p)
		if grant.Port != p {
			return false
		}
	}
	// Protocol matching
	if grant.Protocol != "" && grant.Protocol != protocol {
		return false
	}
	// Check expiry
	if grant.ExpiresAt != "" {
		expiry, err := time.Parse(time.RFC3339, grant.ExpiresAt)
		if err == nil && time.Now().After(expiry) {
			return false
		}
	}
	return true
}

func matchDNSPattern(pattern, host string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		return strings.HasSuffix(host, suffix)
	}
	return host == pattern
}

// isBlockedDestination checks if a host is in a blocklist (cloud metadata, etc.).
func isBlockedDestination(host string) bool {
	blocked := []string{
		"169.254.169.254", // AWS/GCP metadata
		"metadata.google.internal",
		"metadata.azure.com",
		"0.0.0.0",
		"::1",
	}
	for _, b := range blocked {
		if host == b {
			return true
		}
	}
	return false
}

// isKnownSafeDestination checks if a host is a well-known safe package registry.
func isKnownSafeDestination(host string) bool {
	safe := []string{
		"registry.npmjs.org",
		"pypi.org",
		"files.pythonhosted.org",
		"proxy.golang.org",
		"sum.golang.org",
		"crates.io",
		"index.crates.io",
		"static.crates.io",
		"repo1.maven.org",
		"search.maven.org",
		"plugins.gradle.org",
		"services.gradle.org",
		"dl.google.com",
		"packages.confluent.io",
	}
	for _, s := range safe {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}
