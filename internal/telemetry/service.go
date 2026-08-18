package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/metering"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Service implements telemetry and metering (DARI §50).
// Prompt content is never telemetry. Metering events used for billing
// MUST be tied to authenticated Exchange/Session IDs.
type Service struct {
	db         *gorm.DB
	mu         sync.Mutex
	counters   map[string]int64
	gauges     map[string]float64
	histograms map[string][]float64
}

// New creates a new telemetry service.
func New(db *gorm.DB) *Service {
	return &Service{
		db:         db,
		counters:   make(map[string]int64),
		gauges:     make(map[string]float64),
		histograms: make(map[string][]float64),
	}
}

// MetricType identifies a telemetry metric type (DARI §50).
type MetricType = metering.Metric

const (
	MetricConnectionHealth   MetricType = "connection.health"
	MetricHarnessHealth      MetricType = "harness.health"
	MetricSessionCount       MetricType = "session.count"
	MetricLatency            MetricType = "latency"
	MetricTokens             MetricType = "tokens"
	MetricModelUsage         MetricType = "model.usage"
	MetricRelayResources     MetricType = "relay.resources"
	MetricPIACapacity        MetricType = "pia.capacity"
	MetricSecurityCounters   MetricType = "security.counters"
	MetricCollaborationCount MetricType = "collaboration.count"
	MetricTransferBytes      MetricType = "transfer.bytes"
	MetricErrorRate          MetricType = "error.rate"
	MetricTokensIn           MetricType = metering.TokensIn
	MetricTokensOut          MetricType = metering.TokensOut
	MetricCacheRead          MetricType = metering.CacheRead
	MetricCacheWrite         MetricType = metering.CacheWrite
	MetricMediaTokens        MetricType = metering.MediaTokens
	MetricGPUSeconds         MetricType = metering.GPUSeconds
	MetricStorageBytes       MetricType = metering.StorageBytes
	MetricToolCall           MetricType = metering.ToolCall
	MetricReservation        MetricType = metering.Reservation
	MetricFlatFee            MetricType = metering.FlatFee
	MetricRefund             MetricType = metering.Refund
)

// IncrementCounter increments a named counter.
func (s *Service) IncrementCounter(name string, delta int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters[name] += delta
}

// SetGauge sets a named gauge value.
func (s *Service) SetGauge(name string, value float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gauges[name] = value
}

// RecordHistogram adds a value to a named histogram.
func (s *Service) RecordHistogram(name string, value float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.histograms[name] = append(s.histograms[name], value)
	// Keep only last 1000 values
	if len(s.histograms[name]) > 1000 {
		s.histograms[name] = s.histograms[name][len(s.histograms[name])-1000:]
	}
}

// GetCounter returns a counter value.
func (s *Service) GetCounter(name string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counters[name]
}

// GetGauge returns a gauge value.
func (s *Service) GetGauge(name string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gauges[name]
}

// GetHistogramStats returns statistics for a histogram.
type HistogramStats struct {
	Count int     `json:"count"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Avg   float64 `json:"avg"`
	P50   float64 `json:"p50"`
	P95   float64 `json:"p95"`
	P99   float64 `json:"p99"`
}

func (s *Service) GetHistogramStats(name string) HistogramStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := s.histograms[name]
	if len(values) == 0 {
		return HistogramStats{}
	}

	stats := HistogramStats{Count: len(values)}
	stats.Min = values[0]
	stats.Max = values[0]
	sum := 0.0
	for _, v := range values {
		if v < stats.Min {
			stats.Min = v
		}
		if v > stats.Max {
			stats.Max = v
		}
		sum += v
	}
	stats.Avg = sum / float64(len(values))

	// Simple percentile estimation
	idx := func(p float64) float64 {
		i := int(p * float64(len(values)-1))
		if i >= len(values) {
			i = len(values) - 1
		}
		return values[i]
	}
	stats.P50 = idx(0.50)
	stats.P95 = idx(0.95)
	stats.P99 = idx(0.99)

	return stats
}

// RecordMetering records a metering event for billing (DARI §50).
// Metering events MUST be tied to authenticated Exchange/Session IDs.
type MeteringEvent struct {
	OrganizationID         string     `json:"organization_id"`
	SessionID              string     `json:"session_id"`
	ExchangeID             string     `json:"exchange_id"`
	UserID                 string     `json:"user_id"`
	HarnessID              string     `json:"harness_id"`
	ModelPackageID         string     `json:"model_package_id"`
	EndpointID             string     `json:"endpoint_id"`
	Sequence               uint64     `json:"sequence"`
	MetricType             MetricType `json:"metric_type"`
	Quantity               int64      `json:"quantity"`
	Unit                   string     `json:"unit"`
	CostMicros             int64      `json:"cost_micros"`
	Currency               string     `json:"currency"`
	PricingState           string     `json:"pricing_state"`
	Adjustment             bool       `json:"adjustment"`
	AppliedRateMicrosPer1K int64      `json:"applied_rate_micros_per_1k"`
	AppliedPriceVersion    string     `json:"applied_price_version"`
	AppliedPriceSource     string     `json:"applied_price_source"`
	OccurredAt             time.Time  `json:"occurred_at"`
}

// RecordMetering records a metering event.
func (s *Service) RecordMetering(event MeteringEvent) error {
	return s.RecordMeteringBatch([]MeteringEvent{event})
}

func (s *Service) RecordMeteringBatch(events []MeteringEvent) error {
	return s.RecordMeteringBatchContext(context.Background(), events)
}

// RecordMeteringBatchContext bounds both ownership lookup and durable ledger
// insertion. Callers that have already completed inference can supply a short
// background deadline so client disconnects neither cancel billing nor leave
// its database work unbounded.
func (s *Service) RecordMeteringBatchContext(ctx context.Context, events []MeteringEvent) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(events) == 0 {
		return nil
	}
	first := events[0]
	if first.SessionID == "" {
		return fmt.Errorf("telemetry: metering requires authenticated session ID")
	}
	if first.OrganizationID == "" {
		return fmt.Errorf("telemetry: metering requires organization ID")
	}
	for _, event := range events {
		if event.OrganizationID != first.OrganizationID || event.SessionID != first.SessionID {
			return fmt.Errorf("telemetry: metering batch must share organization and session")
		}
		if event.ExchangeID == "" {
			return fmt.Errorf("telemetry: metering requires authenticated exchange ID")
		}
		if event.OccurredAt.IsZero() {
			return fmt.Errorf("telemetry: metering requires occurrence time")
		}
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("telemetry: metering database is unavailable")
	}
	var session models.Session
	if err := s.db.WithContext(ctx).Where("organization_id = ? AND (session_id = ? OR id = ?)", first.OrganizationID, first.SessionID, first.SessionID).First(&session).Error; err != nil {
		return fmt.Errorf("telemetry: session is not owned by organization: %w", err)
	}
	records := make([]models.UsageRecord, 0, len(events))
	for _, event := range events {
		unit, err := metering.Validate(string(event.MetricType), event.Unit)
		if err != nil {
			return err
		}
		if event.Quantity < 0 && !event.Adjustment {
			return fmt.Errorf("telemetry: negative quantity requires an adjustment event")
		}
		if event.UserID != "" && event.UserID != session.UserID {
			return fmt.Errorf("telemetry: user does not own session")
		}
		if event.HarnessID != "" && event.HarnessID != session.HarnessID {
			return fmt.Errorf("telemetry: harness does not own session")
		}
		// Attribution is derived from the authenticated session. Callers may
		// repeat it for clarity, but cannot omit it to create unattributed rows.
		event.SessionID = session.SessionID
		event.UserID = session.UserID
		event.HarnessID = session.HarnessID
		pricingState := event.PricingState
		if pricingState == "" {
			pricingState = models.UsagePricingUnpriced
		}
		switch pricingState {
		case models.UsagePricingPriced:
			if strings.TrimSpace(event.Currency) == "" {
				return fmt.Errorf("telemetry: priced metering requires currency")
			}
		case models.UsagePricingUnpriced, models.UsagePricingPending, models.UsagePricingError:
		default:
			return fmt.Errorf("telemetry: invalid pricing state %q", pricingState)
		}
		if pricingState == models.UsagePricingPriced && (event.MetricType == MetricTokensIn || event.MetricType == MetricTokensOut) {
			if strings.TrimSpace(event.AppliedPriceVersion) == "" || strings.TrimSpace(event.AppliedPriceSource) == "" {
				return fmt.Errorf("telemetry: priced token metering requires applied price provenance")
			}
			if event.AppliedRateMicrosPer1K < 0 {
				return fmt.Errorf("telemetry: applied token rate must not be negative")
			}
			expectedCost, err := metering.TokenCostMicros(event.Quantity, event.AppliedRateMicrosPer1K)
			if err != nil {
				return fmt.Errorf("telemetry: calculate token cost: %w", err)
			}
			if event.CostMicros != expectedCost {
				return fmt.Errorf("telemetry: priced token cost does not match applied rate")
			}
		}

		keyInput := fmt.Sprintf("usage:v1:%s:%s:%s:%s:%d", event.OrganizationID, event.SessionID, event.ExchangeID, event.MetricType, event.Sequence)
		digest := sha256.Sum256([]byte(keyInput))
		eventKey := hex.EncodeToString(digest[:])
		meteredAt := event.OccurredAt.UTC()
		records = append(records, models.UsageRecord{
			OrganizationID:         event.OrganizationID,
			UserID:                 event.UserID,
			HarnessID:              event.HarnessID,
			SessionID:              event.SessionID,
			ProjectID:              session.ProjectID,
			ExchangeID:             event.ExchangeID,
			EventKey:               &eventKey,
			ModelPackageID:         event.ModelPackageID,
			EndpointID:             event.EndpointID,
			MetricType:             string(event.MetricType),
			Quantity:               event.Quantity,
			Unit:                   unit,
			CostMicros:             event.CostMicros,
			Currency:               strings.ToUpper(strings.TrimSpace(event.Currency)),
			PricingState:           pricingState,
			Adjustment:             event.Adjustment,
			AppliedRateMicrosPer1K: event.AppliedRateMicrosPer1K,
			AppliedPriceVersion:    event.AppliedPriceVersion,
			AppliedPriceSource:     event.AppliedPriceSource,
			OccurredAt:             meteredAt.Format(time.RFC3339),
			MeteredAt:              &meteredAt,
		})
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_key"}}, DoNothing: true}).Create(&records).Error
}

// Snapshot returns all current telemetry values.
type Snapshot struct {
	Counters   map[string]int64          `json:"counters"`
	Gauges     map[string]float64        `json:"gauges"`
	Histograms map[string]HistogramStats `json:"histograms"`
	Timestamp  string                    `json:"timestamp"`
}

// Snapshot returns all current telemetry values.
func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := Snapshot{
		Counters:   make(map[string]int64),
		Gauges:     make(map[string]float64),
		Histograms: make(map[string]HistogramStats),
		Timestamp:  time.Now().Format(time.RFC3339),
	}

	for k, v := range s.counters {
		snap.Counters[k] = v
	}
	for k, v := range s.gauges {
		snap.Gauges[k] = v
	}
	// Note: histogram stats computed separately to avoid deadlock

	return snap
}
