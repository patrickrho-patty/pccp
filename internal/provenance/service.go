package provenance

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/paper"
	"gorm.io/gorm"
)

// Service manages the provenance spine: ChangeSets, ProvenanceSpans,
// CommitBindings, EvidenceReceipts, and the audit stream (ActionEnvelopes).
type Service struct {
	db         *gorm.DB
	signingKey ed25519.PrivateKey
	relayID    string
}

// New creates a new provenance service.
func New(db *gorm.DB, relayID string) (*Service, error) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("provenance: generate signing key: %w", err)
	}
	return &Service{db: db, signingKey: priv, relayID: relayID}, nil
}

// RecordAction creates a signed ActionEnvelope for a governed action.
type RecordActionRequest struct {
	OrganizationID string
	SessionID      string
	ExchangeID     string
	UserID         string
	HarnessID      string
	ModelPackageID string
	EndpointID     string
	ProjectID      string
	RepositoryID   string
	Branch         string
	PolicyEpochID  string
	LeaseID        string
	ActionType     string
	ActionPayload  interface{}
	VerdictResult  string
	Classification string
}

// RecordAction creates a signed ActionEnvelope and writes it to the audit stream.
func (s *Service) RecordAction(req RecordActionRequest) (*models.ActionEnvelope, error) {
	var payloadJSON string
	if req.ActionPayload != nil {
		b, _ := json.Marshal(req.ActionPayload)
		payloadJSON = string(b)
	}

	if req.Classification == "" {
		req.Classification = "internal"
	}

	envelope := &models.ActionEnvelope{
		OrganizationID: req.OrganizationID,
		ActionID:       paper.GenerateID("act"),
		SessionID:      req.SessionID,
		ExchangeID:     req.ExchangeID,
		UserID:         req.UserID,
		HarnessID:      req.HarnessID,
		ModelPackageID: req.ModelPackageID,
		EndpointID:     req.EndpointID,
		ProjectID:      req.ProjectID,
		RepositoryID:   req.RepositoryID,
		Branch:         req.Branch,
		PolicyEpochID:  req.PolicyEpochID,
		LeaseID:        req.LeaseID,
		ActionType:     req.ActionType,
		ActionPayload:  payloadJSON,
		VerdictResult:  req.VerdictResult,
		Classification: req.Classification,
		OccurredAt:     time.Now().Format(time.RFC3339),
	}

	// Compute envelope digest
	envelope.EnvelopeDigest = s.computeEnvelopeDigest(envelope)

	// CP signs the envelope
	sig := ed25519.Sign(s.signingKey, []byte(envelope.EnvelopeDigest))
	envelope.CPSignature = hex.EncodeToString(sig)

	if err := s.db.Create(envelope).Error; err != nil {
		return nil, fmt.Errorf("provenance: record action: %w", err)
	}

	// Also record an audit event
	auditEvent := &models.AuditEvent{
		OrganizationID: req.OrganizationID,
		EventType:      req.ActionType,
		ActorID:        req.UserID,
		ActorType:      "user",
		Action:         req.ActionType,
		ResourceType:   "session",
		ResourceID:     req.SessionID,
		Result:         "success",
		EventDigest:    envelope.EnvelopeDigest,
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(auditEvent) // best-effort

	return envelope, nil
}

// CreateChangeSet records a code patch with full provenance lineage.
type CreateChangeSetRequest struct {
	OrganizationID string
	SessionID      string
	ExchangeID     string
	RepositoryID   string
	Branch         string
	BaselineID     string
	UserID         string
	HarnessID      string
	ModelPackageID string
	EndpointID     string
	FilesChanged   []string
	DiffSummary    string
	LinesAdded     int
	LinesRemoved   int
	AttributionState string
	Confidence     float64
}

// CreateChangeSet creates a ChangeSet with provenance.
func (s *Service) CreateChangeSet(req CreateChangeSetRequest) (*models.ChangeSet, error) {
	filesJSON, _ := json.Marshal(req.FilesChanged)

	cs := &models.ChangeSet{
		OrganizationID:  req.OrganizationID,
		SessionID:       req.SessionID,
		ExchangeID:      req.ExchangeID,
		RepositoryID:    req.RepositoryID,
		Branch:          req.Branch,
		BaselineID:      req.BaselineID,
		UserID:          req.UserID,
		HarnessID:       req.HarnessID,
		ModelPackageID:  req.ModelPackageID,
		EndpointID:      req.EndpointID,
		FilesChanged:    string(filesJSON),
		DiffSummary:     req.DiffSummary,
		LinesAdded:      req.LinesAdded,
		LinesRemoved:    req.LinesRemoved,
		AttributionState: req.AttributionState,
		Confidence:      req.Confidence,
		Status:          "pending",
	}

	cs.ChangeSetDigest = s.computeChangeSetDigest(cs)

	if err := s.db.Create(cs).Error; err != nil {
		return nil, fmt.Errorf("provenance: create changeset: %w", err)
	}
	return cs, nil
}

// CreateProvenanceSpan maps a code region to its origin (PRD §19, Appendix B.1).
type CreateSpanRequest struct {
	OrganizationID   string
	RepositoryID     string
	ChangeSetID      string
	FilePath         string
	CommitSHA        string
	SymbolLang       string
	SymbolName       string
	StartLine        int
	EndLine          int
	AttributionState string
	Confidence       float64
	SessionID        string
	UserID           string
	HarnessID        string
	ModelPackageID   string
	EndpointID       string
	ContextRefs      []string
	ParentSpanRefs   []string
}

// CreateProvenanceSpan creates a provenance span.
func (s *Service) CreateProvenanceSpan(req CreateSpanRequest) (*models.ProvenanceSpan, error) {
	contextRefs, _ := json.Marshal(req.ContextRefs)
	parentRefs, _ := json.Marshal(req.ParentSpanRefs)

	span := &models.ProvenanceSpan{
		OrganizationID:   req.OrganizationID,
		RepositoryID:     req.RepositoryID,
		ChangeSetID:      req.ChangeSetID,
		FilePath:         req.FilePath,
		CommitSHA:        req.CommitSHA,
		SymbolLang:       req.SymbolLang,
		SymbolName:       req.SymbolName,
		StartLine:        req.StartLine,
		EndLine:          req.EndLine,
		AttributionState: req.AttributionState,
		Confidence:       req.Confidence,
		SessionID:        req.SessionID,
		UserID:           req.UserID,
		HarnessID:        req.HarnessID,
		ModelPackageID:   req.ModelPackageID,
		EndpointID:       req.EndpointID,
		ContextRefsJSON:  string(contextRefs),
		ParentSpanRefs:   string(parentRefs),
	}

	span.SpanDigest = s.computeSpanDigest(span)

	if err := s.db.Create(span).Error; err != nil {
		return nil, fmt.Errorf("provenance: create span: %w", err)
	}
	return span, nil
}

// BindCommit links a git commit to a ChangeSet (PRD §18.6).
func (s *Service) BindCommit(orgID, repoID, commitSHA, changeSetID, sessionID, branch string) (*models.CommitBinding, error) {
	binding := &models.CommitBinding{
		OrganizationID: orgID,
		RepositoryID:   repoID,
		CommitSHA:      commitSHA,
		ChangeSetID:    changeSetID,
		SessionID:      sessionID,
		Branch:         branch,
		BoundAt:        time.Now().Format(time.RFC3339),
	}
	bindingData := fmt.Sprintf("%s|%s|%s|%s", orgID, repoID, commitSHA, changeSetID)
	h := sha256.Sum256([]byte(bindingData))
	binding.BindingDigest = "sha256:" + hex.EncodeToString(h[:])

	if err := s.db.Create(binding).Error; err != nil {
		return nil, fmt.Errorf("provenance: bind commit: %w", err)
	}
	return binding, nil
}

// IssueEvidenceReceipt creates a signed evidence receipt for a completed exchange (PAPER §34).
type IssueReceiptRequest struct {
	OrganizationID string
	ExchangeID     string
	SessionID      string
	FinalState     string
	FirstEventSeq  uint64
	LastEventSeq   uint64
	ChainRoot      string
	ProvenanceRoot string
	PolicyEpochID  string
	LeaseDigest    string
	ModelPackageID string
	EndpointID     string
}

// IssueEvidenceReceipt creates and signs an evidence receipt.
func (s *Service) IssueEvidenceReceipt(req IssueReceiptRequest) (*models.EvidenceReceipt, error) {
	receipt := &models.EvidenceReceipt{
		OrganizationID: req.OrganizationID,
		ExchangeID:     req.ExchangeID,
		SessionID:      req.SessionID,
		FinalState:     req.FinalState,
		FirstEventSeq:  req.FirstEventSeq,
		LastEventSeq:   req.LastEventSeq,
		ChainRoot:      req.ChainRoot,
		ProvenanceRoot: req.ProvenanceRoot,
		PolicyEpochID:  req.PolicyEpochID,
		LeaseDigest:    req.LeaseDigest,
		RelayIdentity:  s.relayID,
		ModelPackageID: req.ModelPackageID,
		EndpointID:     req.EndpointID,
		KeyAlgorithm:   "ed25519",
		IssuedAt:       time.Now().Format(time.RFC3339),
	}

	// Relay signs the receipt
	receiptData := fmt.Sprintf("%s|%s|%s|%s|%s", receipt.ExchangeID, receipt.FinalState, receipt.ChainRoot, receipt.RelayIdentity, receipt.IssuedAt)
	sig := ed25519.Sign(s.signingKey, []byte(receiptData))
	receipt.Signature = hex.EncodeToString(sig)

	if err := s.db.Create(receipt).Error; err != nil {
		return nil, fmt.Errorf("provenance: issue receipt: %w", err)
	}
	return receipt, nil
}

// CodeSpanLookup looks up provenance by file path and line range (PRD §19.1, Phase 2 gate).
// This answers: "who wrote this code, when, using which model, in which session?"
type CodeSpanLookup struct {
	Spans      []models.ProvenanceSpan `json:"spans"`
	Sessions   map[string]models.Session `json:"sessions"`
	Users      map[string]models.User `json:"users"`
	Harnesses  map[string]models.Harness `json:"harnesses"`
	Models     map[string]models.ModelPackage `json:"models"`
	Endpoints  map[string]models.InferenceEndpoint `json:"endpoints"`
	ChangeSets map[string]models.ChangeSet `json:"change_sets"`
}

// LookupCodeSpan finds all provenance spans for a file path and optional line range.
func (s *Service) LookupCodeSpan(orgID, repoID, filePath string, startLine, endLine int) (*CodeSpanLookup, error) {
	result := &CodeSpanLookup{
		Sessions:   make(map[string]models.Session),
		Users:      make(map[string]models.User),
		Harnesses:  make(map[string]models.Harness),
		Models:     make(map[string]models.ModelPackage),
		Endpoints:  make(map[string]models.InferenceEndpoint),
		ChangeSets: make(map[string]models.ChangeSet),
	}

	// Find spans matching the file path
	query := s.db.Model(&models.ProvenanceSpan{}).
		Where("organization_id = ? AND repository_id = ? AND file_path = ?", orgID, repoID, filePath)

	if startLine > 0 && endLine > 0 {
		// Find spans that overlap with the requested line range
		query = query.Where("start_line <= ? AND end_line >= ?", endLine, startLine)
	}

	if err := query.Order("created_at DESC").Find(&result.Spans).Error; err != nil {
		return nil, fmt.Errorf("provenance: lookup code span: %w", err)
	}

	// Hydrate related entities
	seenSessions := make(map[string]bool)
	seenUsers := make(map[string]bool)
	seenHarnesses := make(map[string]bool)
	seenModels := make(map[string]bool)
	seenEndpoints := make(map[string]bool)
	seenChangeSets := make(map[string]bool)

	for _, span := range result.Spans {
		if span.SessionID != "" && !seenSessions[span.SessionID] {
			seenSessions[span.SessionID] = true
			var sess models.Session
			if s.db.Where("session_id = ?", span.SessionID).First(&sess).Error == nil {
				result.Sessions[span.SessionID] = sess
			}
		}
		if span.UserID != "" && !seenUsers[span.UserID] {
			seenUsers[span.UserID] = true
			var user models.User
			if s.db.Where("id = ?", span.UserID).First(&user).Error == nil {
				result.Users[span.UserID] = user
			}
		}
		if span.HarnessID != "" && !seenHarnesses[span.HarnessID] {
			seenHarnesses[span.HarnessID] = true
			var harness models.Harness
			if s.db.Where("id = ? OR harness_id = ?", span.HarnessID, span.HarnessID).First(&harness).Error == nil {
				result.Harnesses[span.HarnessID] = harness
			}
		}
		if span.ModelPackageID != "" && !seenModels[span.ModelPackageID] {
			seenModels[span.ModelPackageID] = true
			var pkg models.ModelPackage
			if s.db.Where("package_id = ?", span.ModelPackageID).First(&pkg).Error == nil {
				result.Models[span.ModelPackageID] = pkg
			}
		}
		if span.EndpointID != "" && !seenEndpoints[span.EndpointID] {
			seenEndpoints[span.EndpointID] = true
			var ep models.InferenceEndpoint
			if s.db.Where("endpoint_id = ?", span.EndpointID).First(&ep).Error == nil {
				result.Endpoints[span.EndpointID] = ep
			}
		}
		if span.ChangeSetID != "" && !seenChangeSets[span.ChangeSetID] {
			seenChangeSets[span.ChangeSetID] = true
			var cs models.ChangeSet
			if s.db.Where("id = ?", span.ChangeSetID).First(&cs).Error == nil {
				result.ChangeSets[span.ChangeSetID] = cs
			}
		}
	}

	return result, nil
}


// GetProvenanceChain retrieves the full provenance chain for a session or exchange.
func (s *Service) GetProvenanceChain(orgID, sessionID string) (*ProvenanceChain, error) {
	chain := &ProvenanceChain{}

	// Get actions
	s.db.Where("organization_id = ? AND session_id = ?", orgID, sessionID).
		Order("occurred_at ASC").Find(&chain.Actions)

	// Get change sets
	s.db.Where("organization_id = ? AND session_id = ?", orgID, sessionID).
		Order("created_at ASC").Find(&chain.ChangeSets)

	// Get provenance spans for the change sets
	changeSetIDs := make([]string, 0, len(chain.ChangeSets))
	for _, cs := range chain.ChangeSets {
		changeSetIDs = append(changeSetIDs, cs.ID)
	}
	if len(changeSetIDs) > 0 {
		s.db.Where("change_set_id IN ?", changeSetIDs).Find(&chain.Spans)
	}

	// Get evidence receipts
	s.db.Where("organization_id = ? AND session_id = ?", orgID, sessionID).
		Order("issued_at ASC").Find(&chain.Receipts)

	// Get the session
	var session models.Session
	if err := s.db.Where("session_id = ? AND organization_id = ?", sessionID, orgID).First(&session).Error; err == nil {
		chain.Session = &session
	}

	return chain, nil
}

// ProvenanceChain is the full provenance evidence for a session.
type ProvenanceChain struct {
	Session    *models.Session       `json:"session"`
	Actions    []models.ActionEnvelope `json:"actions"`
	ChangeSets []models.ChangeSet     `json:"change_sets"`
	Spans      []models.ProvenanceSpan `json:"spans"`
	Receipts   []models.EvidenceReceipt `json:"receipts"`
}

func (s *Service) computeEnvelopeDigest(env *models.ActionEnvelope) string {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
		env.ActionID, env.OrganizationID, env.SessionID, env.ExchangeID,
		env.UserID, env.HarnessID, env.ModelPackageID, env.EndpointID,
		env.ActionType, env.ActionPayload, env.OccurredAt)
	h := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(h[:])
}

func (s *Service) computeChangeSetDigest(cs *models.ChangeSet) string {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		cs.SessionID, cs.RepositoryID, cs.Branch, cs.FilesChanged,
		cs.DiffSummary, cs.ModelPackageID, cs.EndpointID)
	h := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(h[:])
}

func (s *Service) computeSpanDigest(span *models.ProvenanceSpan) string {
	data := fmt.Sprintf("%s|%s|%s|%d-%d|%s|%s",
		span.RepositoryID, span.FilePath, span.CommitSHA,
		span.StartLine, span.EndLine, span.AttributionState, span.SessionID)
	h := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(h[:])
}
