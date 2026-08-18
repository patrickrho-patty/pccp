package api

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

const delayedMeterThreshold = 15 * time.Minute

var expectedMeterUnits = map[string]string{
	"tokens_in":     UnitTokens,
	"tokens_out":    UnitTokens,
	"cache_read":    UnitTokens,
	"cache_write":   UnitTokens,
	"media_tokens":  UnitTokens,
	"gpu_seconds":   UnitSeconds,
	"storage_bytes": UnitBytes,
	"tool_call":     UnitCount,
	"reservation":   UnitCount,
}

type usageFilter struct {
	UserID     string
	SessionID  string
	SessionIDs []string
}

type fxRate struct {
	Rate   string `json:"rate"`
	AsOf   string `json:"as_of"`
	Source string `json:"source"`
}

func (s *Server) buildUsageReport(orgID string, filter usageFilter, rangeLabel, since, until string) (UsageTotal, error) {
	report := UsageTotal{
		Range:          rangeLabel,
		WindowStart:    since,
		WindowEnd:      until,
		ByUnit:         map[string]Usage{},
		CostByCurrency: map[string]UsageAmount{},
		Meters:         []UsageMeter{},
		Metrics:        []UsageMeter{},
		ByMetric:       map[string]UsageMeter{},
		ByModel:        map[string]Usage{},
		ByUser:         map[string]Usage{},
		BySession:      map[string]Usage{},
		Drilldown:      []UsageLedgerRow{},
		Reconciled:     true,
	}

	sinceTime, _ := time.Parse(time.RFC3339, since)
	untilTime, _ := time.Parse(time.RFC3339, until)
	query := s.db.Where(
		"organization_id = ? AND ((occurred_at >= ? AND occurred_at <= ?) OR ((occurred_at = '' OR occurred_at IS NULL) AND created_at >= ? AND created_at <= ?))",
		orgID, since, until, sinceTime, untilTime,
	)
	if filter.UserID != "" {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if filter.SessionID != "" {
		query = query.Where("session_id = ?", filter.SessionID)
	}
	if filter.SessionIDs != nil {
		if len(filter.SessionIDs) == 0 {
			query = query.Where("1 = 0")
		} else {
			query = query.Where("session_id IN ?", filter.SessionIDs)
		}
	}
	var records []models.UsageRecord
	if err := query.Order("occurred_at DESC, id DESC").Find(&records).Error; err != nil {
		return report, err
	}
	report.RecordCount = len(records)

	type meterKey struct{ metric, unit, currency string }
	meters := map[meterKey]*UsageMeter{}
	seenMetric := map[string]bool{}
	unitLedger := map[string]int64{}
	costLedger := map[string]int64{}
	for _, record := range records {
		occurredAt := record.OccurredAt
		usedCreatedAt := occurredAt == "" || strings.HasPrefix(occurredAt, "0001-01-01")
		if usedCreatedAt {
			occurredAt = record.CreatedAt.UTC().Format(time.RFC3339)
			report.Reconciled = false
			report.ReconciliationErrors = append(report.ReconciliationErrors, "usage record "+record.ID+" has no occurred_at; created_at was used for the selected window")
		}
		unit := normalizeUsageUnit(record.Unit)
		expected := expectedMeterUnits[record.MetricType]
		if unit == "" {
			unit = expected
		}
		validUnit := unit != ""
		if !validUnit {
			unit = UnitUnknown
			report.Reconciled = false
			report.ReconciliationErrors = append(report.ReconciliationErrors, "usage record "+record.ID+" has no recognized unit")
		} else if expected != "" && unit != expected {
			report.Reconciled = false
			report.ReconciliationErrors = append(report.ReconciliationErrors, fmt.Sprintf("usage record %s has unit %s; expected %s", record.ID, unit, expected))
		}

		currency := strings.ToUpper(strings.TrimSpace(record.Currency))
		if record.CostMicros != 0 && currency == "" {
			report.Reconciled = false
			report.ReconciliationErrors = append(report.ReconciliationErrors, "usage record "+record.ID+" has cost without currency")
		}
		key := meterKey{record.MetricType, unit, currency}
		meter := meters[key]
		if meter == nil {
			meter = &UsageMeter{MetricType: record.MetricType, Unit: unit, Currency: currency, State: MeterStateRecorded}
			meters[key] = meter
		}
		meter.Quantity += record.Quantity
		meter.AmountMicros += record.CostMicros
		if occurredAt > meter.LastUpdated {
			meter.LastUpdated = occurredAt
		}
		seenMetric[record.MetricType] = true
		if occurredAt > report.LastUpdated {
			report.LastUpdated = occurredAt
		}
		if !usedCreatedAt && isDelayedUsageRecord(record) {
			meter.State = MeterStateDelayed
			meter.Reason = "meter event arrived more than 15 minutes after occurrence"
		}
		if validUnit {
			unitLedger[unit] += record.Quantity
		}
		if currency != "" {
			costLedger[currency] += record.CostMicros
		}

		report.Drilldown = append(report.Drilldown, UsageLedgerRow{
			ID: record.ID, OccurredAt: occurredAt, Bucket: record.MetricType,
			Unit: unit, Quantity: record.Quantity, RateMicrosPerUnit: rateMicros(record.CostMicros, record.Quantity),
			AmountMicros: record.CostMicros, Currency: currency,
			RefType: "usage_record", RefID: record.ID,
			UserID: record.UserID, HarnessID: record.HarnessID, SessionID: record.SessionID,
			ModelPackageID: record.ModelPackageID, EndpointID: record.EndpointID,
		})

		if record.MetricType == "tokens_in" {
			report.InputTokens += record.Quantity
		}
		if record.MetricType == "tokens_out" {
			report.OutputTokens += record.Quantity
		}
		if record.MetricType == "tokens_in" || record.MetricType == "tokens_out" {
			addUsageDimension(report.ByModel, record.ModelPackageID, record.Quantity, since, until, occurredAt)
			addUsageDimension(report.ByUser, record.UserID, record.Quantity, since, until, occurredAt)
			addUsageDimension(report.BySession, record.SessionID, record.Quantity, since, until, occurredAt)
		}
	}
	report.TotalTokens = report.InputTokens + report.OutputTokens
	setUsageDimensionReconciliation(report.ByModel, report.Reconciled)
	setUsageDimensionReconciliation(report.ByUser, report.Reconciled)
	setUsageDimensionReconciliation(report.BySession, report.Reconciled)

	for _, meter := range meters {
		if meter.Quantity == 0 && meter.State == MeterStateRecorded {
			meter.State = MeterStateZero
		}
		meter.RateMicrosPerUnit = rateMicros(meter.AmountMicros, meter.Quantity)
		report.Meters = append(report.Meters, *meter)
	}
	for metric, unit := range expectedMeterUnits {
		if !seenMetric[metric] {
			report.Meters = append(report.Meters, UsageMeter{MetricType: metric, Unit: unit, State: MeterStateUnavailable, Reason: "no meter event in selected window"})
		}
	}
	sort.Slice(report.Meters, func(i, j int) bool {
		if report.Meters[i].MetricType == report.Meters[j].MetricType {
			return report.Meters[i].Currency < report.Meters[j].Currency
		}
		return report.Meters[i].MetricType < report.Meters[j].MetricType
	})
	report.Metrics = append(report.Metrics, report.Meters...)
	for _, meter := range report.Meters {
		if existing, ok := report.ByMetric[meter.MetricType]; !ok || existing.State == MeterStateUnavailable {
			report.ByMetric[meter.MetricType] = meter
		}
	}
	for unit, quantity := range unitLedger {
		report.ByUnit[unit] = Usage{Quantity: quantity, Unit: unit, WindowStart: since, WindowEnd: until, LastUpdated: report.LastUpdated, Reconciled: report.Reconciled, State: usageStateForQuantity(quantity)}
	}
	for _, unit := range []string{UnitTokens, UnitSeconds, UnitBytes, UnitCount, UnitUSDMicro} {
		if _, ok := report.ByUnit[unit]; !ok {
			report.ByUnit[unit] = Usage{Unit: unit, WindowStart: since, WindowEnd: until, LastUpdated: report.LastUpdated, Reconciled: report.Reconciled, State: MeterStateUnavailable, Reason: "no meter event in selected window"}
		}
	}
	for currency, amount := range costLedger {
		report.CostByCurrency[currency] = UsageAmount{AmountMicros: amount, Currency: currency, State: usageStateForQuantity(amount)}
	}
	return s.finishUsageReport(orgID, report), nil
}

func (s *Server) finishUsageReport(orgID string, report UsageTotal) UsageTotal {
	report.DisplayCurrency = "KRW"
	var displaySetting models.OrgSetting
	if err := s.db.Where("organization_id = ? AND key = ?", orgID, "billing.display_currency").First(&displaySetting).Error; err == nil {
		if value := strings.ToUpper(strings.TrimSpace(displaySetting.Value)); value != "" {
			report.DisplayCurrency = value
		}
	} else if len(report.CostByCurrency) == 1 {
		for currency := range report.CostByCurrency {
			report.DisplayCurrency = currency
		}
	}
	report.Currency = report.DisplayCurrency

	rates := map[string]fxRate{}
	var rateSetting models.OrgSetting
	if err := s.db.Where("organization_id = ? AND key = ?", orgID, "billing.fx_rates").First(&rateSetting).Error; err == nil {
		_ = json.Unmarshal([]byte(rateSetting.Value), &rates)
	}
	display := UsageAmount{Currency: report.DisplayCurrency, State: MeterStateZero}
	for source, amount := range report.CostByCurrency {
		if source == report.DisplayCurrency {
			display.AmountMicros += amount.AmountMicros
			continue
		}
		rateDef, ok := rates[source]
		if !ok {
			display.State = MeterStateUnavailable
			display.Reason = "missing conversion rate from " + source + " to " + report.DisplayCurrency
			report.Reconciled = false
			report.ReconciliationErrors = append(report.ReconciliationErrors, display.Reason)
			continue
		}
		rate := new(big.Rat)
		if _, ok := rate.SetString(rateDef.Rate); !ok {
			display.State = MeterStateError
			display.Reason = "invalid conversion rate for " + source
			report.Reconciled = false
			report.ReconciliationErrors = append(report.ReconciliationErrors, display.Reason)
			continue
		}
		converted := new(big.Rat).Mul(new(big.Rat).SetInt64(amount.AmountMicros), rate)
		display.AmountMicros += roundRat(converted)
		display.Rate = rateDef.Rate
		display.RateSource = rateDef.Source
		display.RateAsOf = rateDef.AsOf
	}
	if display.State != MeterStateUnavailable && display.State != MeterStateError {
		display.State = usageStateForQuantity(display.AmountMicros)
	}
	report.DisplayTotal = display
	report.TotalCostMicros = display.AmountMicros
	report.CostMicros = display.AmountMicros
	return report
}

func normalizeUsageUnit(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "token", "tokens":
		return UnitTokens
	case "second", "seconds", "sec", "s":
		return UnitSeconds
	case "byte", "bytes", "b":
		return UnitBytes
	case "count", "request", "requests", "call", "calls":
		return UnitCount
	case "usd_micro", "micro_usd":
		return UnitUSDMicro
	default:
		return strings.ToLower(strings.TrimSpace(unit))
	}
}

func isDelayedUsageRecord(record models.UsageRecord) bool {
	if record.CreatedAt.IsZero() || record.OccurredAt == "" {
		return false
	}
	occurred, err := time.Parse(time.RFC3339, record.OccurredAt)
	return err == nil && record.CreatedAt.Sub(occurred) > delayedMeterThreshold
}

func rateMicros(amount, quantity int64) string {
	if quantity == 0 {
		return ""
	}
	value := new(big.Rat).SetFrac(big.NewInt(amount), big.NewInt(quantity)).FloatString(6)
	value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	if value == "-0" {
		return "0"
	}
	return value
}

func roundRat(value *big.Rat) int64 {
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(value.Num(), value.Denom(), remainder)
	twiceRemainder := new(big.Int).Lsh(new(big.Int).Abs(remainder), 1)
	if twiceRemainder.Cmp(value.Denom()) >= 0 {
		if value.Sign() >= 0 {
			quotient.Add(quotient, big.NewInt(1))
		} else {
			quotient.Sub(quotient, big.NewInt(1))
		}
	}
	return quotient.Int64()
}

func usageStateForQuantity(quantity int64) MeterState {
	if quantity == 0 {
		return MeterStateZero
	}
	return MeterStateRecorded
}

func addUsageDimension(target map[string]Usage, id string, quantity int64, since, until, occurredAt string) {
	if id == "" {
		return
	}
	usage := target[id]
	usage.Unit = UnitTokens
	usage.Quantity += quantity
	usage.WindowStart = since
	usage.WindowEnd = until
	usage.Reconciled = true
	usage.State = usageStateForQuantity(usage.Quantity)
	if occurredAt > usage.LastUpdated {
		usage.LastUpdated = occurredAt
	}
	target[id] = usage
}

func setUsageDimensionReconciliation(target map[string]Usage, reconciled bool) {
	for id, usage := range target {
		usage.Reconciled = reconciled
		target[id] = usage
	}
}
