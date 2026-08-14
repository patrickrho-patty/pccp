package events

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"gorm.io/gorm"
)

// Service implements the durable event spine (PRD §39).
// All dashboards, audit, provenance, billing, security, and analytics
// derive from common events rather than duplicative logging.
type Service struct {
	db       *gorm.DB
	signingKey ed25519.PrivateKey
	mu       sync.Mutex
	sequences map[string]uint64 // session_id → sequence number
}

// New creates a new event service.
func New(db *gorm.DB) (*Service, error) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("events: generate signing key: %w", err)
	}
	return &Service{
		db:         db,
		signingKey: priv,
		sequences:  make(map[string]uint64),
	}, nil
}

// EventEnvelope is the canonical event envelope (PRD §39.1).
type EventEnvelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    string          `json:"occurred_at"`
	ReceivedAt    string          `json:"received_at"`
	OrganizationID string         `json:"organization_id"`
	Actor         EventActor      `json:"actor"`
	SessionID     string          `json:"session_id,omitempty"`
	ProjectID     string          `json:"project_id,omitempty"`
	RepositoryID  string          `json:"repository_id,omitempty"`
	TraceID       string          `json:"trace_id,omitempty"`
	Classification string         `json:"classification"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	SequenceNum   uint64          `json:"sequence_num,omitempty"`
	PrevEventDigest string        `json:"prev_event_digest,omitempty"`
	EventDigest   string          `json:"event_digest"`
	Signature     string          `json:"signature,omitempty"`
}

// EventActor identifies who caused the event.
type EventActor struct {
	UserID    string `json:"user_id,omitempty"`
	HarnessID string `json:"harness_id,omitempty"`
	ActorType string `json:"actor_type"` // user, harness, system, admin, model
}

// Emit creates, signs, and persists an event on the durable event spine.
type EmitRequest struct {
 	EventType      string `json:"event_type"`
 	OrganizationID string `json:"organization_i_d"`
 	UserID         string `json:"user_i_d"`
 	HarnessID      string `json:"harness_i_d"`
 	ActorType      string `json:"actor_type"`
 	SessionID      string `json:"session_i_d"`
 	ProjectID      string `json:"project_i_d"`
 	RepositoryID   string `json:"repository_i_d"`
 	Classification string `json:"classification"`
 	Payload        interface{} `json:"payload"`
}

// Emit creates a signed event and writes it to the event spine.
func (s *Service) Emit(req EmitRequest) (*EventEnvelope, error) {
	if req.EventType == "" {
		return nil, fmt.Errorf("events: event_type is required")
	}
	if req.OrganizationID == "" {
		return nil, fmt.Errorf("events: organization_id is required")
	}
	if req.ActorType == "" {
		req.ActorType = "system"
	}
	if req.Classification == "" {
		req.Classification = "internal"
	}

	// Serialize payload
	var payloadBytes []byte
	if req.Payload != nil {
		var err error
		payloadBytes, err = json.Marshal(req.Payload)
		if err != nil {
			return nil, fmt.Errorf("events: marshal payload: %w", err)
		}
	}

	now := time.Now().Format(time.RFC3339Nano)
	eventID := dari.GenerateID("evt")

	// Get session sequence number for chained hashing (PRD §39.3)
	seq := s.nextSequence(req.SessionID)

	// Get previous event digest for chaining
	prevDigest := s.getPrevDigest(req.SessionID)

	envelope := EventEnvelope{
		EventID:        eventID,
		EventType:      req.EventType,
		SchemaVersion:  1,
		OccurredAt:     now,
		ReceivedAt:     now,
		OrganizationID: req.OrganizationID,
		Actor: EventActor{
			UserID:    req.UserID,
			HarnessID: req.HarnessID,
			ActorType: req.ActorType,
		},
		SessionID:     req.SessionID,
		ProjectID:     req.ProjectID,
		RepositoryID:  req.RepositoryID,
		Classification: req.Classification,
		Payload:       payloadBytes,
		SequenceNum:   seq,
		PrevEventDigest: prevDigest,
	}

	// Compute event digest (chained hash)
	envelope.EventDigest = s.computeDigest(&envelope)

	// Sign
	sig := ed25519.Sign(s.signingKey, []byte(envelope.EventDigest))
	envelope.Signature = hex.EncodeToString(sig)

	// Persist as an audit event (immutable record)
	auditEvent := &models.AuditEvent{
		Base: models.Base{ID: eventID},
		OrganizationID: req.OrganizationID,
		EventType:      req.EventType,
		ActorID:        req.UserID,
		ActorType:      req.ActorType,
		Action:         req.EventType,
		ResourceType:   "event",
		ResourceID:     req.SessionID,
		Details:        string(payloadBytes),
		Result:         "success",
		EventDigest:    envelope.EventDigest,
		OccurredAt:     now,
	}
	if err := s.db.Create(auditEvent).Error; err != nil {
		return nil, fmt.Errorf("events: persist: %w", err)
	}

	return &envelope, nil
}

// Query retrieves events by various filters.
type QueryFilter struct {
 	OrganizationID string `json:"organization_i_d"`
 	EventType      string `json:"event_type"`
 	SessionID      string `json:"session_i_d"`
 	UserID         string `json:"user_i_d"`
 	HarnessID      string `json:"harness_i_d"`
 	Since          string `json:"since"`  // RFC3339 timestamp
 	Limit          int `json:"limit"`
}

// Query returns events matching the filter.
func (s *Service) Query(filter QueryFilter) ([]models.AuditEvent, error) {
	if filter.Limit == 0 {
		filter.Limit = 100
	}
	q := s.db.Model(&models.AuditEvent{})
	if filter.OrganizationID != "" {
		q = q.Where("organization_id = ?", filter.OrganizationID)
	}
	if filter.EventType != "" {
		q = q.Where("event_type = ?", filter.EventType)
	}
	if filter.SessionID != "" {
		q = q.Where("resource_id = ?", filter.SessionID)
	}
	if filter.UserID != "" {
		q = q.Where("actor_id = ?", filter.UserID)
	}
	if filter.Since != "" {
		q = q.Where("occurred_at > ?", filter.Since)
	}

	var events []models.AuditEvent
	err := q.Order("occurred_at DESC").Limit(filter.Limit).Find(&events).Error
	return events, err
}

// Core event type constants (PRD §39.2).
const (
	TypeIdentityLifecycle       = "cp.identity.lifecycle"
	TypeHarnessLifecycle        = "cp.harness.lifecycle"
	TypeHarnessAttestation      = "cp.harness.attestation"
	TypeSessionLifecycle        = "cp.session.lifecycle"
	TypePromptExchange          = "cp.prompt.exchange"
	TypeContextDecision         = "cp.context.decision"
	TypeActionRequest           = "cp.action.request"
	TypePolicyDecision          = "cp.policy.decision"
	TypeApprovalLifecycle       = "cp.approval.lifecycle"
	TypeToolRequest             = "cp.tool.request"
	TypeMCPRequest              = "cp.mcp.request"
	TypeNetworkGrant            = "cp.network.grant"
	TypeRuntimeEvent            = "cp.runtime.event"
	TypeModelRequest            = "cp.model.request"
	TypeModelEndpointAttestation = "cp.model.endpoint.attestation"
	TypeModelLifecycle          = "cp.model.lifecycle"
	TypeGitChange               = "cp.git.change"
	TypeProvenanceSpan          = "cp.provenance.span"
	TypeSecurityFinding         = "cp.security.finding"
	TypeIncidentLifecycle       = "cp.incident.lifecycle"
	TypeCommunicationMessage    = "cp.communication.message"
	TypeCommunicationPresence   = "cp.communication.presence"
	TypeFileTransfer            = "cp.file.transfer"
	TypeBroadcastLifecycle      = "cp.broadcast.lifecycle"
	TypeUsageRecord             = "cp.usage.record"
	TypeEntitlementLifecycle    = "cp.entitlement.lifecycle"
	TypeWorkMetric              = "cp.work.metric"
	TypeEvaluationSnapshot      = "cp.evaluation.snapshot"
	TypeEvidenceBundle          = "cp.evidence.bundle"
	TypeAdminAction             = "cp.admin.action"
	TypeConfigLifecycle         = "cp.config.lifecycle"
)

func (s *Service) nextSequence(sessionID string) uint64 {
	if sessionID == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequences[sessionID]++
	return s.sequences[sessionID]
}

func (s *Service) getPrevDigest(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	// Query the last event for this session
	var event models.AuditEvent
	err := s.db.Where("resource_id = ? AND event_type LIKE 'cp.%'", sessionID).
		Order("occurred_at DESC").First(&event).Error
	if err != nil {
		return ""
	}
	return event.EventDigest
}

func (s *Service) computeDigest(env *EventEnvelope) string {
	// Chained hash: prev_digest + event fields
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%d|%s|%s",
		env.EventID, env.EventType, env.OrganizationID,
		env.SessionID, env.OccurredAt, env.SequenceNum,
		env.PrevEventDigest, string(env.Payload))
	h := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(h[:])
}
