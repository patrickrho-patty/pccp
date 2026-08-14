package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/security"
	"gorm.io/gorm"
)

// Service implements the Context Firewall (PRD §16, DARI §41).
// Context is a governed resource — every item entering model context
// is classified, inspected, and decisioned.
type Service struct {
	db     *gorm.DB
	secSvc *security.Service
}

// New creates a new context firewall service.
func New(db *gorm.DB, secSvc *security.Service) *Service {
	return &Service{db: db, secSvc: secSvc}
}

// TrustLabel classifies the trust level of a context item (DARI §41.3).
type TrustLabel string

const (
	TrustPolicyApproved   TrustLabel = "TRUSTED_POLICY"
	TrustRepository       TrustLabel = "TRUSTED_REPOSITORY"
	TrustAuthorized       TrustLabel = "AUTHORIZED_INTERNAL"
	TrustUserSupplied     TrustLabel = "USER_SUPPLIED"
	TrustExternalUntrusted TrustLabel = "EXTERNAL_UNTRUSTED"
	TrustModelGenerated   TrustLabel = "MODEL_GENERATED"
	TrustUnknown          TrustLabel = "UNKNOWN"
)

// ContextItem represents a single item being considered for model context.
type ContextItem struct {
	ID            string    `json:"id"`
	Source        string    `json:"source"`        // repository, document, chat, etc.
	Repository    string    `json:"repository,omitempty"`
	Commit        string    `json:"commit,omitempty"`
	Path          string    `json:"path,omitempty"`
	Symbol        string    `json:"symbol,omitempty"`
	Content       string    `json:"content"`
	TokenEstimate int       `json:"token_estimate"`
	Classification string   `json:"classification"` // public, internal, confidential, restricted
	TrustLabel    TrustLabel `json:"trust_label"`
	ReasonForInclusion string `json:"reason_for_inclusion"`
	Transformations []string `json:"transformations,omitempty"`
	ProvenanceDigest string  `json:"provenance_digest,omitempty"`
}

// ContextManifest is a manifest of context items before disclosure (DARI §41.2).
type ContextManifest struct {
	Items    []ContextItem `json:"items"`
	TotalTokens int        `json:"total_tokens"`
	Classification string   `json:"highest_classification"`
}

// ContextDecision is the relay's per-item decision (DARI §41.4).
type ContextDecision struct {
	ItemID    string         `json:"item_id"`
	Decision  string         `json:"decision"` // allow, metadata_only, allow_transformed, require_approval, deny
	Reason    string         `json:"reason"`
	Transformed string       `json:"transformed,omitempty"` // redacted content
	RuleIDs   []string       `json:"rule_ids,omitempty"`
}

// EvaluateManifest inspects all context items and returns per-item decisions.
func (s *Service) EvaluateManifest(orgID string, manifest *ContextManifest) []ContextDecision {
	var decisions []ContextDecision

	for _, item := range manifest.Items {
		decision := s.evaluateItem(orgID, item)
		decisions = append(decisions, decision)
	}

	return decisions
}

// evaluateItem inspects a single context item and returns a decision.
func (s *Service) evaluateItem(orgID string, item ContextItem) ContextDecision {
	decision := ContextDecision{
		ItemID:   item.ID,
		Decision: "allow",
	}

	// 1. Classification check — restricted content cannot enter model context without approval
	if item.Classification == "restricted" {
		decision.Decision = "require_approval"
		decision.Reason = "restricted classification requires approval"
		decision.RuleIDs = append(decision.RuleIDs, "classification.restricted")
		return decision
	}

	// 2. Security checks (DLP, PII, secrets, injection)
	if s.secSvc != nil {
		result := s.secSvc.CheckContext(orgID, item.Content)
		if !result.Passed {
			if result.Verdict == "DENY" {
				decision.Decision = "deny"
				decision.Reason = fmt.Sprintf("security check failed: %d findings", len(result.Findings))
				for _, f := range result.Findings {
					decision.RuleIDs = append(decision.RuleIDs, f.RuleID)
				}
				return decision
			}
			if result.Verdict == "REQUIRE_REVIEW" {
				decision.Decision = "allow_transformed"
				decision.Reason = "security findings require transformation"
				decision.Transformed = s.redactContent(item.Content, result.Findings)
				for _, f := range result.Findings {
					decision.RuleIDs = append(decision.RuleIDs, f.RuleID)
				}
			}
		}
	}

	// 3. Trust label check — untrusted content gets metadata-only
	if item.TrustLabel == TrustExternalUntrusted || item.TrustLabel == TrustUnknown {
		decision.Decision = "metadata_only"
		decision.Reason = "untrusted context item — metadata only"
		decision.RuleIDs = append(decision.RuleIDs, "trust.untrusted")
	}

	return decision
}

// redactContent replaces detected sensitive content with redaction markers.
func (s *Service) redactContent(content string, findings []security.SecurityFinding) string {
	result := content
	for _, f := range findings {
		if f.Position > 0 && f.Position < len(result) {
			// Replace the detected pattern with [REDACTED]
			end := f.Position + len(f.Match)
			if end > len(result) {
				end = len(result)
			}
			result = result[:f.Position] + "[REDACTED:" + f.Type + "]" + result[end:]
		}
	}
	return result
}

// CreateContextManifest builds a manifest from raw context items.
func CreateContextManifest(items []ContextItem) *ContextManifest {
	manifest := &ContextManifest{
		Items: items,
	}
	highestClass := "public"
	classOrder := map[string]int{"public": 0, "internal": 1, "confidential": 2, "restricted": 3}
	for _, item := range items {
		manifest.TotalTokens += item.TokenEstimate
		if classOrder[item.Classification] > classOrder[highestClass] {
			highestClass = item.Classification
		}
		// Compute provenance digest
		if item.ProvenanceDigest == "" {
			h := sha256.Sum256([]byte(item.Content))
			item.ProvenanceDigest = "sha256:" + hex.EncodeToString(h[:])
		}
	}
	manifest.Classification = highestClass
	return manifest
}

// EstimateTokens provides a rough token count estimate.
func EstimateTokens(text string) int {
	// Rough approximation: 1 token ≈ 4 characters for English, 1-2 for Korean
	// This is a placeholder — production should use an actual tokenizer.
	chars := len(text)
	// Check for Korean content ratio
	koreanCount := 0
	for _, r := range text {
		if r >= 0xAC00 && r <= 0xD7A3 {
			koreanCount++
		}
	}
	if chars == 0 {
		return 0
	}
	koreanRatio := float64(koreanCount) / float64(chars)
	// Korean: ~1.5 chars per token, English: ~4 chars per token
	avgCharsPerToken := 4.0*(1-koreanRatio) + 1.5*koreanRatio
	if avgCharsPerToken < 1 {
		avgCharsPerToken = 1
	}
	return int(float64(chars) / avgCharsPerToken)
}

// ClassifyFile determines the trust label and classification for a file.
func ClassifyFile(path, content string, sensitivity string) (TrustLabel, string) {
	// Default classification from repository sensitivity
	classification := sensitivity
	if classification == "" {
		classification = "internal"
	}

	// Determine trust label based on source
	trustLabel := TrustRepository

	// Files from secrets/env paths are always restricted
	secretPatterns := []string{".env", "secret", "credential", "password", "private_key"}
	for _, p := range secretPatterns {
		if contains(path, p) {
			classification = "restricted"
			trustLabel = TrustAuthorized
			break
		}
	}

	return trustLabel, classification
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// RecordContextDecision persists a context decision for audit.
func (s *Service) RecordContextDecision(orgID, sessionID, exchangeID string, decision ContextDecision) error {
	details, _ := json.Marshal(decision)
	auditEvent := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.context.decision",
		ActorType:      "system",
		Action:         "context_decision",
		ResourceType:   "session",
		ResourceID:     sessionID,
		Details:        string(details),
		Result:         decision.Decision,
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return s.db.Create(auditEvent).Error
}
