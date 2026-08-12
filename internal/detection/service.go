package detection

import (
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements account sharing and integrity detection (PCCP v2 §10C.9).
// Collects signals and computes risk scores per account.
// Per §10C.9: "IP/geolocation alone is never enough for automatic permanent ban."
type Service struct {
	db  *gorm.DB
	mu  sync.Mutex
	// In-memory signal tracking
	signals map[string][]*Signal // accountID → signals
}

// Signal represents a single integrity detection signal.
type Signal struct {
	AccountID  string    `json:"account_id"`
	Type       string    `json:"type"`       // concurrent_harnesses, geo_implausible, etc.
	Severity   string    `json:"severity"`   // info, low, medium, high
	Details    string    `json:"details"`
	DetailsKo  string    `json:"details_ko"`
	DetectedAt time.Time `json:"detected_at"`
	IPAddress  string    `json:"ip_address,omitempty"`
	ASN        string    `json:"asn,omitempty"`
}

// New creates a new detection service.
func New(db *gorm.DB) *Service {
	return &Service{
		db:      db,
		signals: make(map[string][]*Signal),
	}
}

// RecordConcurrentHarness tracks concurrent harness activity (§10C.9).
func (s *Service) RecordConcurrentHarness(accountID, harnessID, ipAddr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if this harness is already tracked
	// In production, this would query active harness sessions
	s.addSignal(accountID, Signal{
		Type:       "concurrent_harness",
		Severity:   "info",
		Details:    fmt.Sprintf("Harness %s active from %s", harnessID, ipAddr),
		DetailsKo:  fmt.Sprintf("하네스 %s 활동 (IP: %s)", harnessID, ipAddr),
		IPAddress:  ipAddr,
		DetectedAt: time.Now(),
	})
}

// CheckGeoImplausible detects geographically impossible simultaneous activity (§10C.9).
// Two harnesses active from locations that couldn't be reached in the time delta.
func (s *Service) CheckGeoImplausible(accountID string, ipAddrs []string, locations []string) []*Signal {
	var findings []*Signal

	// Simple heuristic: different ASNs from different regions
	uniqueASNs := make(map[string]bool)
	for _, loc := range locations {
		uniqueASNs[loc] = true
	}

	if len(uniqueASNs) > 2 {
		findings = append(findings, &Signal{
			AccountID:  accountID,
			Type:       "geo_implausible",
			Severity:   "high",
			Details:    fmt.Sprintf("Activity from %d distinct locations simultaneously", len(uniqueASNs)),
			DetailsKo:  fmt.Sprintf("%d개 지역에서 동시 활동 감지", len(uniqueASNs)),
			DetectedAt: time.Now(),
		})
	}

	s.mu.Lock()
	for _, f := range findings {
		s.addSignal(accountID, *f)
	}
	s.mu.Unlock()

	return findings
}

// CheckCredentialReplay detects credential replay across connections (§10C.9).
func (s *Service) CheckCredentialReplay(accountID, credentialHash string, connectionIDs []string) []*Signal {
	if len(connectionIDs) > 1 {
		sig := &Signal{
			AccountID:  accountID,
			Type:       "credential_replay",
			Severity:   "critical",
			Details:    fmt.Sprintf("Same credential used on %d connections", len(connectionIDs)),
			DetailsKo:  fmt.Sprintf("동일 자격증명이 %d개 연결에서 사용됨", len(connectionIDs)),
			DetectedAt: time.Now(),
		}
		s.mu.Lock()
		s.addSignal(accountID, *sig)
		s.mu.Unlock()
		return []*Signal{sig}
	}
	return nil
}

// CheckMultiShiftPattern detects continuous multi-shift usage patterns (§10C.9).
// 24/7 activity suggesting account sharing for a paid service.
func (s *Service) CheckMultiShiftPattern(accountID string, activeHours []int) []*Signal {
	// Check if activity spans >20 hours per day
	hourSet := make(map[int]bool)
	for _, h := range activeHours {
		hourSet[h] = true
	}
	if len(hourSet) > 20 {
		sig := &Signal{
			AccountID:  accountID,
			Type:       "multi_shift",
			Severity:   "medium",
			Details:    fmt.Sprintf("Activity in %d of 24 hours", len(hourSet)),
			DetailsKo:  fmt.Sprintf("24시간 중 %d시간 활동 (교대 근무 의심)", len(hourSet)),
			DetectedAt: time.Now(),
		}
		s.mu.Lock()
		s.addSignal(accountID, *sig)
		s.mu.Unlock()
		return []*Signal{sig}
	}
	return nil
}

// GetSignals returns all signals for an account.
func (s *Service) GetSignals(accountID string) []*Signal {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.signals[accountID]
}

// GetRiskScore computes an overall risk score for an account (0-100).
// Per §10C.9: Multiple signals compound, but no single signal causes permanent ban.
func (s *Service) GetRiskScore(accountID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	signals := s.signals[accountID]
	if len(signals) == 0 {
		return 0
	}

	score := 0
	for _, sig := range signals {
		switch sig.Severity {
		case "critical":
			score += 30
		case "high":
			score += 20
		case "medium":
			score += 10
		case "info":
			score += 1
		}
	}

	// Cap at 100
	if score > 100 {
		score = 100
	}
	return score
}

// GetRecommendedAction returns the recommended graduated response (§10C.10).
func (s *Service) GetRecommendedAction(accountID string) string {
	score := s.GetRiskScore(accountID)
	switch {
	case score >= 80:
		return "suspend" // Immediate suspension for review
	case score >= 60:
		return "restrict" // Temporary account restriction
	case score >= 40:
		return "reduce_slots" // Reduce concurrency
	case score >= 20:
		return "revoke_harness" // Revoke suspicious harness
	case score >= 10:
		return "step_up_auth" // Require re-authentication
	default:
		return "observe" // Just monitor
	}
}

// ClearSignals removes old signals (for cleanup).
func (s *Service) ClearSignals(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.signals, accountID)
}

func (s *Service) addSignal(accountID string, sig Signal) {
	sig.AccountID = accountID
	s.signals[accountID] = append(s.signals[accountID], &sig)

	// Record in audit
	event := &models.AuditEvent{
		OrganizationID: accountID, // Use account ID as org for public accounts
		EventType:      "cp.integrity.signal." + sig.Type,
		ActorType:      "system",
		Action:         sig.Type,
		ResourceType:   "account",
		ResourceID:     accountID,
		Details:        sig.Details,
		Result:         sig.Severity,
		OccurredAt:     sig.DetectedAt.Format(time.RFC3339),
	}
	if s.db != nil {
		s.db.Create(event)
	}
}
