package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/metering"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

const delayedMeterThreshold = 15 * time.Minute

var expectedMeterUnits = metering.KnownMetrics()

var errInvalidUsageCursor = errors.New("invalid usage ledger cursor")

type usageProjection uint8

const (
	usageProjectionUser usageProjection = 1 << iota
	usageProjectionSession
	usageProjectionModel
	usageProjectionLedger
)

type usageFilter struct {
	UserID       string
	SessionID    string
	ProjectID    string
	LedgerCursor string
	LedgerLimit  int
	Context      context.Context
	SnapshotAt   time.Time
	WindowStart  time.Time
	WindowEnd    time.Time
	SummaryOnly  bool
	// Projection is zero for the complete public report contract. Internal
	// consumers set only the sections they render so they do not execute
	// unrelated full-window aggregations.
	Projection usageProjection
}

func (filter usageFilter) includesProjection(section usageProjection) bool {
	return filter.Projection == 0 || filter.Projection&section != 0
}

func usageFilterFromRequest(r *http.Request, filter usageFilter) usageFilter {
	filter.Context = r.Context()
	filter.SummaryOnly = r.URL.Query().Get("summary_only") == "1"
	filter.LedgerCursor = r.URL.Query().Get("cursor")
	if limit, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
		filter.LedgerLimit = limit
	}
	return filter
}

type fxRate struct {
	Rate    string `json:"rate"`
	AsOf    string `json:"as_of"`
	Source  string `json:"source"`
	Version string `json:"version"`
}

func (s *Server) buildUsageReport(orgID string, filter usageFilter, rangeLabel, since, until string) (UsageTotal, error) {
	if filter.Context == nil {
		filter.Context = context.Background()
	}
	if filter.LedgerCursor != "" {
		return s.buildUsageContinuation(orgID, filter)
	}
	filter.SnapshotAt = time.Now().UTC()
	var report UsageTotal
	var err error
	run := func(tx *gorm.DB) error {
		clone := *s
		clone.db = tx
		report, err = clone.buildUsageReportSnapshot(orgID, filter, rangeLabel, since, until)
		return err
	}
	db := s.db.WithContext(filter.Context)
	if s.db.Dialector.Name() == "postgres" {
		err = db.Transaction(run, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	} else {
		err = db.Transaction(run)
	}
	return report, err
}

func (s *Server) buildUsageReportSnapshot(orgID string, filter usageFilter, rangeLabel, since, until string) (UsageTotal, error) {
	report := UsageTotal{
		Range:          rangeLabel,
		WindowStart:    since,
		WindowEnd:      until,
		SnapshotAt:     filter.SnapshotAt.UTC().Format(time.RFC3339),
		ByUnit:         map[string]Usage{},
		CostByCurrency: map[string]UsageAmount{},
		Conversions:    []UsageConversion{},
		Meters:         []UsageMeter{},
		ByModel:        map[string]Usage{},
		ByUser:         map[string]Usage{},
		BySession:      map[string]Usage{},
		ModelTotals:    map[string]UsageModelTotal{},
		DimensionMeta:  map[string]UsageDimensionMeta{},
		Drilldown:      []UsageLedgerRow{},
		Reconciled:     true,
	}

	sinceTime, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return report, fmt.Errorf("usage window start: %w", err)
	}
	untilTime, err := time.Parse(time.RFC3339, until)
	if err != nil {
		return report, fmt.Errorf("usage window end: %w", err)
	}
	if !sinceTime.Before(untilTime) {
		return report, fmt.Errorf("usage window start must be before end")
	}
	filter.WindowStart = sinceTime
	filter.WindowEnd = untilTime

	type aggregateRow struct {
		MetricType           string
		Unit                 string
		Currency             string
		Quantity             int64
		AmountMicros         int64
		RecordCount          int64
		PricedCount          int64
		UnpricedCount        int64
		PendingCount         int64
		PricingErrorCount    int64
		InvalidCurrencyCount int64
		LegacyCount          int64
		DelayedCount         int64
		LastUpdated          string
	}
	delayedExpr := "SUM(CASE WHEN timing_source = 'reported' AND created_at > datetime(metered_at, '+15 minutes') THEN 1 ELSE 0 END)"
	if s.db.Dialector.Name() == "postgres" {
		delayedExpr = "SUM(CASE WHEN timing_source = 'reported' AND created_at > metered_at + INTERVAL '15 minutes' THEN 1 ELSE 0 END)"
	}
	selectExpr := strings.Join([]string{
		"metric_type", "unit", "UPPER(currency) AS currency", "SUM(quantity) AS quantity",
		"SUM(CASE WHEN pricing_state = 'priced' THEN cost_micros ELSE 0 END) AS amount_micros",
		"COUNT(*) AS record_count",
		"SUM(CASE WHEN pricing_state = 'priced' THEN 1 ELSE 0 END) AS priced_count",
		"SUM(CASE WHEN pricing_state = 'unpriced' THEN 1 ELSE 0 END) AS unpriced_count",
		"SUM(CASE WHEN pricing_state = 'pending' THEN 1 ELSE 0 END) AS pending_count",
		"SUM(CASE WHEN pricing_state = 'error' OR pricing_state NOT IN ('priced','unpriced','pending') THEN 1 ELSE 0 END) AS pricing_error_count",
		"SUM(CASE WHEN pricing_state = 'priced' AND (currency IS NULL OR TRIM(currency) = '') THEN 1 ELSE 0 END) AS invalid_currency_count",
		"SUM(CASE WHEN timing_source = 'created_at_fallback' THEN 1 ELSE 0 END) AS legacy_count",
		delayedExpr + " AS delayed_count",
		"MAX(metered_at) AS last_updated",
	}, ", ")
	var aggregates []aggregateRow
	if err := s.usageRecordsQuery(orgID, filter, sinceTime, untilTime).
		Select(selectExpr).
		Group("metric_type, unit, UPPER(currency)").
		Order("metric_type ASC, unit ASC, currency ASC").
		Scan(&aggregates).Error; err != nil {
		return report, err
	}

	type meterKey struct{ metric, unit, currency string }
	meters := map[meterKey]*UsageMeter{}
	seenMetric := map[string]bool{}
	unitLedger := map[string]int64{}
	unitOverflow := map[string]bool{}
	currencyLedger := map[string]UsageAmount{}
	currencyOverflow := map[string]bool{}
	meterQuantityOverflow := map[meterKey]bool{}
	meterCostOverflow := map[meterKey]bool{}
	inputTokensOverflow, outputTokensOverflow := false, false
	var recordCount, legacyCount, unpricedCount, pendingCount, pricingErrorCount, invalidCurrencyCount int64
	countOverflow := map[string]bool{}
	addCount := func(name string, target *int64, value int64) {
		if countOverflow[name] {
			return
		}
		if total, ok := addInt64(*target, value); ok {
			*target = total
		} else {
			*target = 0
			countOverflow[name] = true
			addUsageReconciliationIssue(&report, "quantity_total_overflow", name+" count exceeds the supported range")
		}
	}
	for _, aggregate := range aggregates {
		addCount("usage record", &recordCount, aggregate.RecordCount)
		addCount("legacy timing", &legacyCount, aggregate.LegacyCount)
		addCount("unpriced usage", &unpricedCount, aggregate.UnpricedCount)
		addCount("pending pricing", &pendingCount, aggregate.PendingCount)
		addCount("pricing error", &pricingErrorCount, aggregate.PricingErrorCount)
		addCount("invalid currency", &invalidCurrencyCount, aggregate.InvalidCurrencyCount)
		if aggregate.LegacyCount > 0 {
			report.Reconciled = false
		}
		unit := normalizeUsageUnit(aggregate.Unit)
		expected := expectedMeterUnits[aggregate.MetricType]
		validUnit := expected != "" && unit == expected
		if unit == "" {
			unit = UnitUnknown
		}
		if !validUnit {
			report.Reconciled = false
			message := fmt.Sprintf("%d %s records use %s; expected %s", aggregate.RecordCount, aggregate.MetricType, unit, expected)
			report.ReconciliationErrors = append(report.ReconciliationErrors, message)
			report.ReconciliationIssues = append(report.ReconciliationIssues, UsageReconciliationIssue{Code: "invalid_meter_unit", Message: message})
		}

		currency := strings.ToUpper(strings.TrimSpace(aggregate.Currency))
		if aggregate.InvalidCurrencyCount > 0 {
			report.Reconciled = false
		}
		key := meterKey{aggregate.MetricType, unit, currency}
		meter := meters[key]
		if meter == nil {
			meter = &UsageMeter{MetricType: aggregate.MetricType, Unit: unit, Currency: currency, State: MeterStateRecorded, CostState: MeterStateZero}
			meters[key] = meter
		}
		if !meterQuantityOverflow[key] {
			if total, ok := addInt64(meter.Quantity, aggregate.Quantity); ok {
				meter.Quantity = total
			} else {
				meter.Quantity = 0
				meterQuantityOverflow[key] = true
				addUsageReconciliationIssue(&report, "quantity_total_overflow", "meter quantity total exceeds the supported range for "+aggregate.MetricType)
			}
		}
		if !meterCostOverflow[key] {
			if total, ok := addInt64(meter.AmountMicros, aggregate.AmountMicros); ok {
				meter.AmountMicros = total
			} else {
				meter.AmountMicros = 0
				meterCostOverflow[key] = true
				addUsageReconciliationIssue(&report, "cost_total_overflow", "meter cost total exceeds the supported range for "+aggregate.MetricType)
			}
		}
		if updated := normalizeUsageTimestamp(aggregate.LastUpdated); updated > meter.LastUpdated {
			meter.LastUpdated = updated
		}
		seenMetric[aggregate.MetricType] = true
		if meter.LastUpdated > report.LastUpdated {
			report.LastUpdated = meter.LastUpdated
		}
		if !validUnit {
			meter.State = MeterStateError
			meter.Reason = "invalid or unknown meter unit"
			meter.ReasonCode = "invalid_meter_unit"
		} else if aggregate.DelayedCount > 0 && meter.State != MeterStateError {
			meter.State = worseMeterState(meter.State, MeterStateDelayed)
			meter.Reason = fmt.Sprintf("%d meter events arrived more than 15 minutes after occurrence", aggregate.DelayedCount)
			meter.ReasonCode = "meter_delayed"
		}
		nextCostState := MeterStateZero
		nextCostReason := ""
		nextCostReasonCode := ""
		switch {
		case aggregate.PricingErrorCount > 0 || aggregate.InvalidCurrencyCount > 0:
			nextCostState = MeterStateError
			nextCostReason = "invalid pricing metadata"
			nextCostReasonCode = "pricing_error"
		case aggregate.PendingCount > 0:
			nextCostState = MeterStateUnavailable
			nextCostReason = "pricing is pending"
			nextCostReasonCode = "pricing_pending"
		case aggregate.UnpricedCount > 0:
			nextCostState = MeterStateUnavailable
			nextCostReason = "usage was metered without an asserted price"
			nextCostReasonCode = "pricing_unavailable"
		default:
			nextCostState = usageStateForQuantity(aggregate.AmountMicros)
		}
		if !validUnit {
			nextCostState = MeterStateError
			nextCostReason = "cost belongs to an invalid meter unit"
			nextCostReasonCode = "pricing_error"
		}
		if meterQuantityOverflow[key] {
			meter.State = MeterStateError
			meter.Reason = "meter quantity total exceeds the supported range"
			meter.ReasonCode = "quantity_total_overflow"
		}
		if meterCostOverflow[key] {
			nextCostState = MeterStateError
			nextCostReason = "meter cost total exceeds the supported range"
			nextCostReasonCode = "cost_total_overflow"
		}
		if worseMeterState(meter.CostState, nextCostState) != meter.CostState || meter.CostReason == "" {
			meter.CostReason = nextCostReason
			meter.CostReasonCode = nextCostReasonCode
		}
		meter.CostState = worseMeterState(meter.CostState, nextCostState)
		if validUnit && !unitOverflow[unit] {
			if total, ok := addInt64(unitLedger[unit], aggregate.Quantity); ok {
				unitLedger[unit] = total
			} else {
				unitLedger[unit] = 0
				unitOverflow[unit] = true
				addUsageReconciliationIssue(&report, "quantity_total_overflow", "usage quantity total exceeds the supported range for unit "+unit)
			}
		}
		if validUnit && currency != "" {
			amount := currencyLedger[currency]
			if !currencyOverflow[currency] {
				if total, ok := addInt64(amount.AmountMicros, aggregate.AmountMicros); ok {
					amount.AmountMicros = total
				} else {
					amount.AmountMicros = 0
					amount.State = MeterStateError
					amount.Reason = "currency cost total exceeds the supported range"
					amount.ReasonCode = "cost_total_overflow"
					currencyOverflow[currency] = true
					addUsageReconciliationIssue(&report, "cost_total_overflow", "usage cost total exceeds the supported range for currency "+currency)
				}
			}
			amount.Currency = currency
			if amount.State == "" {
				amount.State = MeterStateZero
			}
			if worseMeterState(amount.State, nextCostState) != amount.State || amount.Reason == "" {
				amount.Reason = nextCostReason
				amount.ReasonCode = nextCostReasonCode
			}
			amount.State = worseMeterState(amount.State, nextCostState)
			currencyLedger[currency] = amount
		}

		if validUnit && aggregate.MetricType == "tokens_in" && !inputTokensOverflow {
			if total, ok := addInt64(report.InputTokens, aggregate.Quantity); ok {
				report.InputTokens = total
			} else {
				report.InputTokens = 0
				inputTokensOverflow = true
				addUsageReconciliationIssue(&report, "quantity_total_overflow", "input token total exceeds the supported range")
			}
		}
		if validUnit && aggregate.MetricType == "tokens_out" && !outputTokensOverflow {
			if total, ok := addInt64(report.OutputTokens, aggregate.Quantity); ok {
				report.OutputTokens = total
			} else {
				report.OutputTokens = 0
				outputTokensOverflow = true
				addUsageReconciliationIssue(&report, "quantity_total_overflow", "output token total exceeds the supported range")
			}
		}
	}
	report.RecordCount = int(recordCount)
	if legacyCount > 0 {
		message := fmt.Sprintf("%d usage records use created_at fallback timing", legacyCount)
		report.ReconciliationErrors = append(report.ReconciliationErrors, message)
		report.ReconciliationIssues = append(report.ReconciliationIssues, UsageReconciliationIssue{Code: "fallback_timing", Message: message})
	}
	if unpricedCount > 0 {
		report.Reconciled = false
		message := fmt.Sprintf("%d usage records are metered but unpriced", unpricedCount)
		report.ReconciliationErrors = append(report.ReconciliationErrors, message)
		report.ReconciliationIssues = append(report.ReconciliationIssues, UsageReconciliationIssue{Code: "pricing_unavailable", Message: message})
	}
	if pendingCount > 0 {
		report.Reconciled = false
		message := fmt.Sprintf("%d usage records have pending pricing", pendingCount)
		report.ReconciliationErrors = append(report.ReconciliationErrors, message)
		report.ReconciliationIssues = append(report.ReconciliationIssues, UsageReconciliationIssue{Code: "pricing_pending", Message: message})
	}
	if pricingErrorCount > 0 {
		report.Reconciled = false
		message := fmt.Sprintf("%d usage records have invalid pricing state", pricingErrorCount)
		report.ReconciliationErrors = append(report.ReconciliationErrors, message)
		report.ReconciliationIssues = append(report.ReconciliationIssues, UsageReconciliationIssue{Code: "pricing_error", Message: message})
	}
	if invalidCurrencyCount > 0 {
		report.Reconciled = false
		message := fmt.Sprintf("%d priced usage records have no currency", invalidCurrencyCount)
		report.ReconciliationErrors = append(report.ReconciliationErrors, message)
		report.ReconciliationIssues = append(report.ReconciliationIssues, UsageReconciliationIssue{Code: "currency_missing", Message: message})
	}

	totalTokensOverflow := inputTokensOverflow || outputTokensOverflow
	if !totalTokensOverflow {
		if total, ok := addInt64(report.InputTokens, report.OutputTokens); ok {
			report.TotalTokens = total
		} else {
			totalTokensOverflow = true
			addUsageReconciliationIssue(&report, "quantity_total_overflow", "combined token total exceeds the supported range")
		}
	}

	for _, meter := range meters {
		if meter.Quantity == 0 && meter.State == MeterStateRecorded {
			if meter.AmountMicros == 0 {
				meter.State = MeterStateZero
			}
		}
		meter.RateMicrosPerUnit = rateMicros(meter.AmountMicros, meter.Quantity)
		report.Meters = append(report.Meters, *meter)
	}
	for metric, unit := range expectedMeterUnits {
		if !seenMetric[metric] {
			report.Meters = append(report.Meters, UsageMeter{MetricType: metric, Unit: unit, State: MeterStateUnavailable, Reason: "no meter event in selected window", ReasonCode: "no_meter_event", CostState: MeterStateUnavailable, CostReason: "no meter event in selected window", CostReasonCode: "no_meter_event"})
		}
	}
	sort.Slice(report.Meters, func(i, j int) bool {
		if report.Meters[i].MetricType == report.Meters[j].MetricType {
			if report.Meters[i].Unit == report.Meters[j].Unit {
				return report.Meters[i].Currency < report.Meters[j].Currency
			}
			return report.Meters[i].Unit < report.Meters[j].Unit
		}
		return report.Meters[i].MetricType < report.Meters[j].MetricType
	})
	report.InputTokensState = tokenMetricState(report.Meters, "tokens_in")
	report.OutputTokensState = tokenMetricState(report.Meters, "tokens_out")
	report.TotalTokensState = combineTokenStates(report.InputTokensState, report.OutputTokensState, report.TotalTokens)
	if inputTokensOverflow {
		report.InputTokensState = MeterStateError
	}
	if outputTokensOverflow {
		report.OutputTokensState = MeterStateError
	}
	if totalTokensOverflow {
		report.TotalTokens = 0
		report.TotalTokensState = MeterStateError
	}
	for unit, quantity := range unitLedger {
		state := usageStateForQuantity(quantity)
		reason, reasonCode := "", ""
		if unitOverflow[unit] {
			state = MeterStateError
			reason = "usage quantity total exceeds the supported range"
			reasonCode = "quantity_total_overflow"
		}
		for _, meter := range report.Meters {
			if !unitOverflow[unit] && meter.Unit == unit && meter.State == MeterStateDelayed {
				state = MeterStateDelayed
				break
			}
		}
		report.ByUnit[unit] = Usage{Quantity: quantity, Unit: unit, WindowStart: since, WindowEnd: until, LastUpdated: report.LastUpdated, Reconciled: report.Reconciled, State: state, Reason: reason, ReasonCode: reasonCode}
	}
	for _, unit := range []string{UnitTokens, UnitSeconds, UnitBytes, UnitCount, UnitUSDMicro} {
		if _, ok := report.ByUnit[unit]; !ok {
			report.ByUnit[unit] = Usage{Unit: unit, WindowStart: since, WindowEnd: until, LastUpdated: report.LastUpdated, Reconciled: report.Reconciled, State: MeterStateUnavailable, Reason: "no meter event in selected window", ReasonCode: "no_meter_event"}
		}
	}
	for currency, amount := range currencyLedger {
		report.CostByCurrency[currency] = amount
	}
	if !filter.SummaryOnly {
		if filter.includesProjection(usageProjectionUser) || filter.includesProjection(usageProjectionSession) {
			if err := s.aggregateUsageDimensions(orgID, filter, sinceTime, untilTime, &report); err != nil {
				return report, err
			}
		}
		if filter.includesProjection(usageProjectionModel) {
			if err := s.aggregateUsageModels(orgID, filter, sinceTime, untilTime, &report); err != nil {
				return report, err
			}
		}
		if filter.includesProjection(usageProjectionUser) || filter.includesProjection(usageProjectionModel) {
			if err := s.resolveUsageDimensionLabels(orgID, &report); err != nil {
				return report, err
			}
		}
		if filter.includesProjection(usageProjectionLedger) {
			if err := s.loadUsageLedgerPage(orgID, filter, sinceTime, untilTime, &report); err != nil {
				return report, err
			}
		}
	}
	report, err = s.finishUsageReport(orgID, filter, sinceTime, untilTime, report)
	if err != nil {
		return report, err
	}
	setUsageDimensionReconciliation(report.ByModel, report.Reconciled)
	setUsageDimensionReconciliation(report.ByUser, report.Reconciled)
	setUsageDimensionReconciliation(report.BySession, report.Reconciled)
	setUsageDimensionReconciliation(report.ByUnit, report.Reconciled)
	return report, nil
}

func (s *Server) resolveUsageDimensionLabels(orgID string, report *UsageTotal) error {
	type labelRow struct{ Kind, ID, Label string }
	parts := []string{}
	args := []interface{}{}
	userIDs := make([]string, 0, len(report.ByUser))
	for id := range report.ByUser {
		if !strings.HasPrefix(id, "__") {
			userIDs = append(userIDs, id)
		}
	}
	modelIDs := make([]string, 0, len(report.ByModel))
	for id := range report.ByModel {
		if !strings.HasPrefix(id, "__") {
			modelIDs = append(modelIDs, id)
		}
	}
	if len(userIDs) > 0 {
		parts = append(parts, "SELECT 'user' AS kind, id, COALESCE(NULLIF(name_ko, ''), NULLIF(name, ''), email) AS label FROM users WHERE organization_id = ? AND deleted_at IS NULL AND id IN ?")
		args = append(args, orgID, userIDs)
	}
	if len(modelIDs) > 0 {
		parts = append(parts, "SELECT 'model' AS kind, package_id AS id, COALESCE(NULLIF(name, ''), package_id) AS label FROM model_packages WHERE deleted_at IS NULL AND package_id IN ?")
		args = append(args, modelIDs)
		parts = append(parts, "SELECT 'model' AS kind, id, COALESCE(NULLIF(name, ''), package_id) AS label FROM model_packages WHERE deleted_at IS NULL AND id IN ?")
		args = append(args, modelIDs)
	}
	labels := map[string]string{}
	if len(parts) > 0 {
		var rows []labelRow
		if err := s.db.Raw(strings.Join(parts, " UNION ALL "), args...).Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			labels[row.Kind+"\x00"+row.ID] = row.Label
		}
	}
	for id, usage := range report.ByUser {
		switch id {
		case "__other__":
			usage.Label = "기타 사용자"
		case "__unattributed__":
			usage.Label = "사용자 미귀속"
		default:
			usage.Label = labels["user\x00"+id]
		}
		report.ByUser[id] = usage
	}
	for id, usage := range report.ByModel {
		switch id {
		case "__other__":
			usage.Label = "기타 모델"
		case "__unattributed__":
			usage.Label = "모델 미귀속"
		default:
			usage.Label = labels["model\x00"+id]
		}
		report.ByModel[id] = usage
	}
	return nil
}

func (s *Server) usageRecordsQuery(orgID string, filter usageFilter, since, until time.Time) *gorm.DB {
	ctx := filter.Context
	if ctx == nil {
		ctx = context.Background()
	}
	query := s.db.WithContext(ctx).Model(&models.UsageRecord{}).
		Where("organization_id = ? AND metered_at >= ? AND metered_at < ?", orgID, since, until)
	if !filter.SnapshotAt.IsZero() {
		if s.db.Dialector.Name() == "sqlite" {
			// SQLite stores timestamps with their original offsets. julianday
			// compares the instants instead of their textual representations.
			query = query.Where("julianday(created_at) <= julianday(?)", filter.SnapshotAt.UTC().Format(time.RFC3339Nano))
		} else {
			query = query.Where("created_at <= ?", filter.SnapshotAt)
		}
	}
	if filter.UserID != "" {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if filter.SessionID != "" {
		query = query.Where("session_id = ?", filter.SessionID)
	}
	if filter.ProjectID != "" {
		query = query.Where("project_id = ?", filter.ProjectID)
	}
	return query
}

func validTokenUsageQuery(query *gorm.DB) *gorm.DB {
	return query.Where("metric_type IN ? AND LOWER(TRIM(unit)) IN ?", []string{"tokens_in", "tokens_out"}, []string{"token", "tokens"})
}

func (s *Server) aggregateUsageDimensions(orgID string, filter usageFilter, since, until time.Time, report *UsageTotal) error {
	for _, dimension := range []struct {
		name, column string
		target       map[string]Usage
		fixedID      string
		projection   usageProjection
	}{
		{"user", "user_id", report.ByUser, filter.UserID, usageProjectionUser},
		{"session", "session_id", report.BySession, filter.SessionID, usageProjectionSession},
	} {
		if !filter.includesProjection(dimension.projection) {
			continue
		}
		if dimension.fixedID != "" {
			usage := usageDimensionValue(report.TotalTokens, since, until, report.LastUpdated, report.Reconciled)
			if report.TotalTokensState == MeterStateError {
				usage.State = MeterStateError
				usage.Reason = "usage quantity total exceeds the supported range"
				usage.ReasonCode = "quantity_total_overflow"
			}
			dimension.target[dimension.fixedID] = usage
			report.DimensionMeta[dimension.name] = UsageDimensionMeta{Returned: 1}
			continue
		}
		meta, err := s.aggregateUsageDimension(orgID, filter, since, until, dimension.column, dimension.target, report)
		if err != nil {
			return err
		}
		report.DimensionMeta[dimension.name] = meta
	}
	return nil
}

func (s *Server) aggregateUsageDimension(orgID string, filter usageFilter, since, until time.Time, column string, target map[string]Usage, report *UsageTotal) (UsageDimensionMeta, error) {
	rankExpression := "SUM(quantity)"
	if s.db.Dialector.Name() == "sqlite" {
		rankExpression = "TOTAL(quantity)"
	}
	var topIDs []string
	if err := validTokenUsageQuery(s.usageRecordsQuery(orgID, filter, since, until)).
		Where(column+" IS NOT NULL AND "+column+" <> ''").
		Select(column).Group(column).Order(rankExpression+" DESC, "+column+" ASC").Limit(250).Pluck(column, &topIDs).Error; err != nil {
		return UsageDimensionMeta{}, err
	}
	expression := "CASE WHEN " + column + " IS NULL OR " + column + " = '' THEN '__unattributed__'"
	args := []interface{}{}
	if len(topIDs) > 0 {
		expression += " WHEN " + column + " IN ? THEN " + column
		args = append(args, topIDs)
	}
	expression += " ELSE '__other__' END"
	var rows []struct {
		DimensionKey string
		Quantity     int64
		LastUpdated  string
	}
	selectArgs := append([]interface{}{expression + " AS dimension_key, SUM(quantity) AS quantity, MAX(metered_at) AS last_updated"}, args...)
	// Use an alias that cannot collide with usage_records.id. Grouping by
	// `id` binds to the primary key and silently turns every bucket into a
	// one-row group on both supported databases.
	query := validTokenUsageQuery(s.usageRecordsQuery(orgID, filter, since, until)).Select(selectArgs[0], selectArgs[1:]...).Group("dimension_key, metric_type, unit").Order("dimension_key ASC, metric_type ASC, unit ASC")
	if err := query.Scan(&rows).Error; err != nil {
		return UsageDimensionMeta{}, err
	}
	meta := UsageDimensionMeta{}
	overflow := map[string]bool{}
	for _, row := range rows {
		usage, exists := target[row.DimensionKey]
		if !exists {
			usage = usageDimensionValue(0, since, until, "", report.Reconciled)
		}
		if !overflow[row.DimensionKey] {
			if total, ok := addInt64(usage.Quantity, row.Quantity); ok {
				usage.Quantity = total
				usage.State = usageStateForQuantity(total)
			} else {
				usage.Quantity = 0
				usage.State = MeterStateError
				usage.Reason = "usage quantity total exceeds the supported range"
				usage.ReasonCode = "quantity_total_overflow"
				overflow[row.DimensionKey] = true
				addUsageReconciliationIssue(report, "quantity_total_overflow", "usage dimension total exceeds the supported range for "+row.DimensionKey)
			}
		}
		if updated := normalizeUsageTimestamp(row.LastUpdated); updated > usage.LastUpdated {
			usage.LastUpdated = updated
		}
		target[row.DimensionKey] = usage
		if exists {
			continue
		}
		if row.DimensionKey == "__other__" {
			meta.HasOther = true
		} else if row.DimensionKey == "__unattributed__" {
			meta.HasUnattributed = true
		} else {
			meta.Returned++
		}
	}
	return meta, nil
}

func usageDimensionValue(quantity int64, since, until time.Time, lastUpdated string, reconciled bool) Usage {
	return Usage{
		Quantity: quantity, Unit: UnitTokens,
		WindowStart: since.UTC().Format(time.RFC3339), WindowEnd: until.UTC().Format(time.RFC3339),
		LastUpdated: lastUpdated, Reconciled: reconciled, State: usageStateForQuantity(quantity),
	}
}

func (s *Server) aggregateUsageModels(orgID string, filter usageFilter, since, until time.Time, report *UsageTotal) error {
	rankExpression := "SUM(quantity)"
	if s.db.Dialector.Name() == "sqlite" {
		rankExpression = "TOTAL(quantity)"
	}
	var topIDs []string
	if err := validTokenUsageQuery(s.usageRecordsQuery(orgID, filter, since, until)).
		Where("model_package_id IS NOT NULL AND model_package_id <> ''").
		Select("model_package_id").Group("model_package_id").Order(rankExpression+" DESC, model_package_id ASC").Limit(250).Pluck("model_package_id", &topIDs).Error; err != nil {
		return err
	}
	modelExpression := "CASE WHEN model_package_id IS NULL OR model_package_id = '' THEN '__unattributed__'"
	modelArgs := []interface{}{}
	if len(topIDs) > 0 {
		modelExpression += " WHEN model_package_id IN ? THEN model_package_id"
		modelArgs = append(modelArgs, topIDs)
	}
	modelExpression += " ELSE '__other__' END"
	var rows []struct {
		ModelKey     string
		MetricType   string
		Unit         string
		Currency     string
		PricingState string
		Quantity     int64
		AmountMicros int64
	}
	selectArgs := append([]interface{}{modelExpression + " AS model_key, metric_type, unit, UPPER(currency) AS currency, pricing_state, SUM(quantity) AS quantity, SUM(CASE WHEN pricing_state = 'priced' THEN cost_micros ELSE 0 END) AS amount_micros"}, modelArgs...)
	if err := validTokenUsageQuery(s.usageRecordsQuery(orgID, filter, since, until)).
		Select(selectArgs[0], selectArgs[1:]...).
		Group("model_key, metric_type, unit, UPPER(currency), pricing_state").
		Order("model_key ASC, metric_type ASC, unit ASC, currency ASC, pricing_state ASC").Scan(&rows).Error; err != nil {
		return err
	}
	modelInputOverflow := map[string]bool{}
	modelOutputOverflow := map[string]bool{}
	modelCostOverflow := map[string]bool{}
	for _, row := range rows {
		total := report.ModelTotals[row.ModelKey]
		if total.CostByCurrency == nil {
			total.CostByCurrency = map[string]int64{}
		}
		if row.MetricType == "tokens_in" {
			if !modelInputOverflow[row.ModelKey] {
				if next, ok := addInt64(total.InputTokens, row.Quantity); ok {
					total.InputTokens = next
				} else {
					total.InputTokens = 0
					modelInputOverflow[row.ModelKey] = true
				}
			}
		} else {
			if !modelOutputOverflow[row.ModelKey] {
				if next, ok := addInt64(total.OutputTokens, row.Quantity); ok {
					total.OutputTokens = next
				} else {
					total.OutputTokens = 0
					modelOutputOverflow[row.ModelKey] = true
				}
			}
		}
		costKey := row.ModelKey + "\x00" + row.Currency
		if row.PricingState == models.UsagePricingPriced && row.Currency != "" && !modelCostOverflow[costKey] {
			if next, ok := addInt64(total.CostByCurrency[row.Currency], row.AmountMicros); ok {
				total.CostByCurrency[row.Currency] = next
			} else {
				total.CostByCurrency[row.Currency] = 0
				modelCostOverflow[costKey] = true
			}
		}
		if modelInputOverflow[row.ModelKey] || modelOutputOverflow[row.ModelKey] {
			total.PricingState = MeterStateError
			addUsageReconciliationIssue(report, "quantity_total_overflow", "model usage total exceeds the supported range for "+row.ModelKey)
		} else if modelCostOverflow[costKey] {
			total.PricingState = MeterStateError
			addUsageReconciliationIssue(report, "cost_total_overflow", "model cost total exceeds the supported range for "+row.ModelKey+" in "+row.Currency)
		} else if row.PricingState == models.UsagePricingError {
			total.PricingState = MeterStateError
		} else if row.PricingState != models.UsagePricingPriced && total.PricingState != MeterStateError {
			total.PricingState = MeterStateUnavailable
		} else if total.PricingState == "" {
			total.PricingState = MeterStateRecorded
		}
		report.ModelTotals[row.ModelKey] = total
	}
	for id, total := range report.ModelTotals {
		quantity, ok := addInt64(total.InputTokens, total.OutputTokens)
		quantityOverflow := !ok || modelInputOverflow[id] || modelOutputOverflow[id]
		if quantityOverflow {
			quantity = 0
			total.PricingState = MeterStateError
			report.ModelTotals[id] = total
			addUsageReconciliationIssue(report, "quantity_total_overflow", "model usage total exceeds the supported range for "+id)
		}
		usage := usageDimensionValue(quantity, since, until, report.LastUpdated, report.Reconciled)
		if quantityOverflow {
			usage.State = MeterStateError
			usage.Reason = "model usage total exceeds the supported range"
			usage.ReasonCode = "quantity_total_overflow"
		}
		report.ByModel[id] = usage
	}
	report.DimensionMeta["model"] = UsageDimensionMeta{Returned: len(topIDs), HasOther: report.ByModel["__other__"].Unit != "", HasUnattributed: report.ByModel["__unattributed__"].Unit != ""}
	return nil
}

func (s *Server) loadUsageLedgerPage(orgID string, filter usageFilter, since, until time.Time, report *UsageTotal) error {
	limit := filter.LedgerLimit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	query := s.usageRecordsQuery(orgID, filter, since, until)
	var cursor usageCursor
	if filter.LedgerCursor != "" {
		var err error
		cursor, err = s.decodeUsageCursor(filter.LedgerCursor)
		if err != nil {
			return err
		}
		query = query.Where("(metered_at < ? OR (metered_at = ? AND id < ?))", cursor.OccurredAt, cursor.OccurredAt, cursor.ID)
	}
	var records []models.UsageRecord
	if err := query.Order("metered_at DESC, id DESC").Limit(limit + 1).Find(&records).Error; err != nil {
		return err
	}
	if len(records) > limit {
		report.LedgerHasMore = true
		records = records[:limit]
	}
	for _, record := range records {
		occurred := effectiveUsageTime(record)
		unit := normalizeUsageUnit(record.Unit)
		if unit == "" {
			unit = UnitUnknown
		}
		meterState := usageStateForQuantity(record.Quantity)
		reasonCode := ""
		includedInTotals := true
		if _, err := metering.Validate(record.MetricType, record.Unit); err != nil {
			meterState = MeterStateError
			reasonCode = "invalid_meter_unit"
			includedInTotals = false
		}
		amount := int64(0)
		currency := ""
		if record.PricingState == models.UsagePricingPriced {
			amount = record.CostMicros
			currency = strings.ToUpper(strings.TrimSpace(record.Currency))
		}
		row := UsageLedgerRow{
			ID: record.ID, OccurredAt: occurred.UTC().Format(time.RFC3339), Bucket: record.MetricType,
			Unit: unit, Quantity: record.Quantity, RateMicrosPerUnit: rateMicros(amount, record.Quantity),
			AmountMicros: amount, Currency: currency, PricingState: record.PricingState,
			MeterState: meterState, ReasonCode: reasonCode, IncludedInTotals: includedInTotals,
			AppliedPriceVersion: record.AppliedPriceVersion, AppliedPriceSource: record.AppliedPriceSource,
			RefType: "usage_record", RefID: record.ID,
			UserID: record.UserID, HarnessID: record.HarnessID, SessionID: record.SessionID,
			ModelPackageID: record.ModelPackageID, EndpointID: record.EndpointID, ProjectID: record.ProjectID, Adjustment: record.Adjustment,
		}
		if strings.TrimSpace(record.AppliedPriceVersion) != "" || strings.TrimSpace(record.AppliedPriceSource) != "" {
			rate := record.AppliedRateMicrosPer1K
			row.AppliedRateMicrosPer1K = &rate
		}
		report.Drilldown = append(report.Drilldown, row)
	}
	if err := s.resolveUsageRelations(filter.Context, orgID, report.Drilldown); err != nil {
		return err
	}
	if report.LedgerHasMore && len(records) > 0 {
		last := records[len(records)-1]
		report.LedgerNextCursor = s.encodeUsageCursor(usageCursor{
			OccurredAt: effectiveUsageTime(last), ID: last.ID,
			WindowStart: since, WindowEnd: until, SnapshotAt: filter.SnapshotAt,
			OrganizationID: orgID, UserID: filter.UserID, SessionID: filter.SessionID, ProjectID: filter.ProjectID,
		})
	}
	return nil
}

func (s *Server) resolveUsageRelations(ctx context.Context, orgID string, ledger []UsageLedgerRow) error {
	if ctx == nil {
		ctx = context.Background()
	}
	idsByType := map[string][]string{}
	seen := map[string]bool{}
	add := func(kind, id string) {
		if id == "" || seen[kind+"\x00"+id] {
			return
		}
		seen[kind+"\x00"+id] = true
		idsByType[kind] = append(idsByType[kind], id)
	}
	for _, row := range ledger {
		add("user", row.UserID)
		add("harness", row.HarnessID)
		add("session", row.SessionID)
		add("model", row.ModelPackageID)
		add("endpoint", row.EndpointID)
		add("project", row.ProjectID)
	}
	type relationRow struct {
		Kind       string
		ID         string
		ExternalID string
		Label      string
	}
	parts := []string{}
	args := []interface{}{}
	if ids := idsByType["user"]; len(ids) > 0 {
		parts = append(parts, "SELECT 'user' AS kind, id, '' AS external_id, COALESCE(NULLIF(name_ko, ''), NULLIF(name, ''), email) AS label FROM users WHERE organization_id = ? AND deleted_at IS NULL AND id IN ?")
		args = append(args, orgID, ids)
	}
	if ids := idsByType["harness"]; len(ids) > 0 {
		parts = append(parts, "SELECT 'harness' AS kind, id, harness_id AS external_id, COALESCE(NULLIF(name, ''), harness_id) AS label FROM harnesses WHERE organization_id = ? AND deleted_at IS NULL AND (id IN ? OR harness_id IN ?)")
		args = append(args, orgID, ids, ids)
	}
	if ids := idsByType["session"]; len(ids) > 0 {
		parts = append(parts, "SELECT 'session' AS kind, id, session_id AS external_id, COALESCE(NULLIF(title, ''), session_id) AS label FROM sessions WHERE organization_id = ? AND deleted_at IS NULL AND (id IN ? OR session_id IN ?)")
		args = append(args, orgID, ids, ids)
	}
	if ids := idsByType["model"]; len(ids) > 0 {
		parts = append(parts, "SELECT 'model' AS kind, id, package_id AS external_id, COALESCE(NULLIF(name, ''), package_id) AS label FROM model_packages WHERE deleted_at IS NULL AND (id IN ? OR package_id IN ?)")
		args = append(args, ids, ids)
	}
	if ids := idsByType["endpoint"]; len(ids) > 0 {
		parts = append(parts, "SELECT 'endpoint' AS kind, id, endpoint_id AS external_id, COALESCE(NULLIF(name, ''), endpoint_id) AS label FROM inference_endpoints WHERE organization_id = ? AND deleted_at IS NULL AND (id IN ? OR endpoint_id IN ?)")
		args = append(args, orgID, ids, ids)
	}
	if ids := idsByType["project"]; len(ids) > 0 {
		parts = append(parts, "SELECT 'project' AS kind, id, '' AS external_id, COALESCE(NULLIF(name_ko, ''), NULLIF(name, ''), slug) AS label FROM projects WHERE organization_id = ? AND deleted_at IS NULL AND id IN ?")
		args = append(args, orgID, ids)
	}
	if len(parts) == 0 {
		return nil
	}
	var relations []relationRow
	if err := s.db.WithContext(ctx).Raw(strings.Join(parts, " UNION ALL "), args...).Scan(&relations).Error; err != nil {
		return err
	}
	resolved := map[string]relationRow{}
	for _, relation := range relations {
		resolved[relation.Kind+"\x00"+relation.ID] = relation
		if relation.ExternalID != "" {
			resolved[relation.Kind+"\x00"+relation.ExternalID] = relation
		}
	}
	for i := range ledger {
		if relation, ok := resolved["user\x00"+ledger[i].UserID]; ok {
			ledger[i].UserResolved, ledger[i].UserLabel = true, relation.Label
		}
		if relation, ok := resolved["harness\x00"+ledger[i].HarnessID]; ok {
			ledger[i].HarnessResolved, ledger[i].HarnessLabel = true, relation.Label
		}
		if relation, ok := resolved["session\x00"+ledger[i].SessionID]; ok {
			ledger[i].SessionResolved, ledger[i].SessionLabel = true, relation.Label
		}
		if relation, ok := resolved["model\x00"+ledger[i].ModelPackageID]; ok {
			ledger[i].ModelResolved, ledger[i].ModelLabel = true, relation.Label
		}
		if relation, ok := resolved["endpoint\x00"+ledger[i].EndpointID]; ok {
			ledger[i].EndpointResolved, ledger[i].EndpointLabel = true, relation.Label
		}
		if relation, ok := resolved["project\x00"+ledger[i].ProjectID]; ok {
			ledger[i].ProjectResolved, ledger[i].ProjectLabel = true, relation.Label
		}
	}
	return nil
}

func normalizeUsageTimestamp(value string) string {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return value
}

func effectiveUsageTime(record models.UsageRecord) time.Time {
	if record.MeteredAt != nil && !record.MeteredAt.IsZero() {
		return record.MeteredAt.UTC()
	}
	return record.CreatedAt.UTC()
}

type usageCursor struct {
	Version        int       `json:"v"`
	OccurredAt     time.Time `json:"at"`
	ID             string    `json:"id"`
	WindowStart    time.Time `json:"start"`
	WindowEnd      time.Time `json:"end"`
	SnapshotAt     time.Time `json:"snapshot"`
	OrganizationID string    `json:"org"`
	UserID         string    `json:"user,omitempty"`
	SessionID      string    `json:"session,omitempty"`
	ProjectID      string    `json:"project,omitempty"`
}

func (s *Server) encodeUsageCursor(cursor usageCursor) string {
	cursor.Version = 1
	encoded, _ := json.Marshal(cursor)
	payload := base64.RawURLEncoding.EncodeToString(encoded)
	mac := hmac.New(sha256.New, []byte(s.jwtSecret))
	_, _ = mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) decodeUsageCursor(value string) (usageCursor, error) {
	var cursor usageCursor
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return cursor, errInvalidUsageCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return cursor, errInvalidUsageCursor
	}
	mac := hmac.New(sha256.New, []byte(s.jwtSecret))
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return cursor, errInvalidUsageCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return cursor, errInvalidUsageCursor
	}
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.Version != 1 || cursor.ID == "" || cursor.OccurredAt.IsZero() || cursor.WindowStart.IsZero() || cursor.WindowEnd.IsZero() || cursor.SnapshotAt.IsZero() {
		return usageCursor{}, errInvalidUsageCursor
	}
	return cursor, nil
}

func (s *Server) buildUsageContinuation(orgID string, filter usageFilter) (UsageTotal, error) {
	cursor, err := s.decodeUsageCursor(filter.LedgerCursor)
	if err != nil {
		return UsageTotal{}, err
	}
	if cursor.OrganizationID != orgID || cursor.UserID != filter.UserID || cursor.SessionID != filter.SessionID || cursor.ProjectID != filter.ProjectID {
		return UsageTotal{}, errInvalidUsageCursor
	}
	filter.WindowStart = cursor.WindowStart
	filter.WindowEnd = cursor.WindowEnd
	filter.SnapshotAt = cursor.SnapshotAt
	report := UsageTotal{
		WindowStart: cursor.WindowStart.UTC().Format(time.RFC3339),
		WindowEnd:   cursor.WindowEnd.UTC().Format(time.RFC3339),
		SnapshotAt:  cursor.SnapshotAt.UTC().Format(time.RFC3339),
		Drilldown:   []UsageLedgerRow{},
	}
	if err := s.loadUsageLedgerPage(orgID, filter, cursor.WindowStart, cursor.WindowEnd, &report); err != nil {
		return UsageTotal{}, err
	}
	return report, nil
}

func writeUsageReportError(w http.ResponseWriter, err error) {
	if errors.Is(err, errInvalidUsageCursor) {
		writeError(w, http.StatusBadRequest, "사용량 원장 커서가 올바르지 않습니다")
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	writeError(w, http.StatusInternalServerError, "사용량 보고서를 생성하지 못했습니다")
}

func (s *Server) finishUsageReport(orgID string, filter usageFilter, since, until time.Time, report UsageTotal) (UsageTotal, error) {
	report.DisplayCurrency = "KRW"
	var settings []models.OrgSetting
	if err := s.db.Where("organization_id = ? AND key IN ?", orgID, []string{"billing.display_currency", "billing.fx_rates"}).Find(&settings).Error; err != nil {
		return report, fmt.Errorf("usage billing settings: %w", err)
	}
	settingByKey := make(map[string]models.OrgSetting, len(settings))
	for _, setting := range settings {
		settingByKey[setting.Key] = setting
	}
	if value := strings.ToUpper(strings.TrimSpace(settingByKey["billing.display_currency"].Value)); value != "" {
		report.DisplayCurrency = value
	} else if len(report.CostByCurrency) == 1 {
		for currency := range report.CostByCurrency {
			report.DisplayCurrency = currency
		}
	}
	report.Currency = report.DisplayCurrency
	display := UsageAmount{Currency: report.DisplayCurrency, State: MeterStateZero}
	setDisplayFailure := func(state MeterState, reason, code string, addReconciliationIssue bool) {
		if worseMeterState(display.State, state) != display.State {
			display.State = state
			display.Reason = reason
			display.ReasonCode = code
		}
		if addReconciliationIssue {
			report.Reconciled = false
			report.ReconciliationErrors = append(report.ReconciliationErrors, reason)
			report.ReconciliationIssues = append(report.ReconciliationIssues, UsageReconciliationIssue{Code: code, Message: reason})
		}
	}
	if setting, ok := settingByKey["billing.fx_rates"]; ok && !setting.UpdatedAt.After(until) {
		configured := map[string]fxRate{}
		invalid := json.Unmarshal([]byte(setting.Value), &configured) != nil
		if !invalid {
			for _, definition := range configured {
				effectiveAt, err := time.Parse("2006-01-02", strings.TrimSpace(definition.AsOf))
				rate := new(big.Rat)
				_, validRate := rate.SetString(definition.Rate)
				if err == nil && !effectiveAt.Before(until) {
					continue
				}
				if err != nil || !validRate || rate.Sign() <= 0 || strings.TrimSpace(definition.Source) == "" || strings.TrimSpace(definition.Version) == "" {
					invalid = true
					break
				}
			}
		}
		if invalid {
			setDisplayFailure(MeterStateError, "conversion rate configuration is invalid", "fx_rate_invalid", true)
		}
	}
	pricingState := MeterStateRecorded
	for _, meter := range report.Meters {
		if meter.ReasonCode == "no_meter_event" {
			continue
		}
		pricingState = worseMeterState(pricingState, meter.CostState)
	}
	sourceCurrencies := make([]string, 0, len(report.CostByCurrency))
	for source := range report.CostByCurrency {
		sourceCurrencies = append(sourceCurrencies, source)
	}
	sort.Strings(sourceCurrencies)
	conversionSources := make([]string, 0, len(sourceCurrencies))
	for _, source := range sourceCurrencies {
		amount := report.CostByCurrency[source]
		if amount.State == MeterStateError {
			setDisplayFailure(MeterStateError, amount.Reason, amount.ReasonCode, false)
			continue
		}
		if source == report.DisplayCurrency {
			if total, ok := addInt64(display.AmountMicros, amount.AmountMicros); ok {
				display.AmountMicros = total
			} else {
				setDisplayFailure(MeterStateError, "display currency total exceeds the supported range", "cost_total_overflow", true)
			}
			continue
		}
		conversionSources = append(conversionSources, source)
	}
	if len(conversionSources) > 0 {
		type conversionBucket struct {
			SourceCurrency  string
			SourceMicros    int64
			RateID          string
			Rate            string
			RateSource      string
			RateVersion     string
			RateEffectiveAt *time.Time
		}
		var buckets []conversionBucket
		usage := s.usageRecordsQuery(orgID, filter, since, until).Select("usage_records.*")
		rateJoin := `LEFT JOIN billing_fx_rates AS applied_fx ON applied_fx.id = (
			SELECT candidate.id FROM billing_fx_rates AS candidate
			WHERE candidate.organization_id = ?
				AND candidate.source_currency = UPPER(TRIM(window_usage.currency))
				AND candidate.target_currency = ?
				AND candidate.effective_at <= window_usage.metered_at
				AND candidate.deleted_at IS NULL
			ORDER BY candidate.effective_at DESC, candidate.created_at DESC, candidate.id DESC
			LIMIT 1
		)`
		selectColumns := `UPPER(TRIM(window_usage.currency)) AS source_currency,
			SUM(window_usage.cost_micros) AS source_micros,
			COALESCE(applied_fx.id, '') AS rate_id,
			COALESCE(applied_fx.rate, '') AS rate,
			COALESCE(applied_fx.source, '') AS rate_source,
			COALESCE(applied_fx.version, '') AS rate_version,
			applied_fx.effective_at AS rate_effective_at`
		if err := s.db.Table("(?) AS window_usage", usage).
			Joins(rateJoin, orgID, report.DisplayCurrency).
			Where("window_usage.pricing_state = ? AND UPPER(TRIM(window_usage.currency)) IN ?", models.UsagePricingPriced, conversionSources).
			Select(selectColumns).
			Group("UPPER(TRIM(window_usage.currency)), applied_fx.id, applied_fx.rate, applied_fx.source, applied_fx.version, applied_fx.effective_at").
			Order("source_currency ASC, rate_effective_at ASC, rate_id ASC").
			Scan(&buckets).Error; err != nil {
			return report, fmt.Errorf("usage billing rate history: %w", err)
		}
		for _, bucket := range buckets {
			if bucket.SourceMicros == 0 {
				continue
			}
			conversion := UsageConversion{SourceCurrency: bucket.SourceCurrency, TargetCurrency: report.DisplayCurrency, SourceAmountMicros: bucket.SourceMicros, State: MeterStateRecorded}
			if bucket.RateID == "" {
				reason := "missing conversion rate from " + bucket.SourceCurrency + " to " + report.DisplayCurrency
				conversion.State, conversion.Reason, conversion.ReasonCode = MeterStateUnavailable, reason, "fx_rate_missing"
				report.Conversions = append(report.Conversions, conversion)
				setDisplayFailure(MeterStateUnavailable, reason, "fx_rate_missing", true)
				continue
			}
			rate := new(big.Rat)
			if _, ok := rate.SetString(bucket.Rate); !ok || rate.Sign() <= 0 || strings.TrimSpace(bucket.RateSource) == "" || strings.TrimSpace(bucket.RateVersion) == "" || bucket.RateEffectiveAt == nil {
				reason := "invalid or unversioned conversion rate for " + bucket.SourceCurrency + "->" + report.DisplayCurrency
				conversion.State, conversion.Reason, conversion.ReasonCode = MeterStateError, reason, "fx_rate_invalid"
				report.Conversions = append(report.Conversions, conversion)
				setDisplayFailure(MeterStateError, reason, "fx_rate_invalid", true)
				continue
			}
			conversion.Rate = bucket.Rate
			conversion.RateSource = bucket.RateSource
			conversion.RateAsOf = bucket.RateEffectiveAt.UTC().Format("2006-01-02")
			conversion.RateVersion = bucket.RateVersion
			convertedMicros, ok := roundRatInt64(new(big.Rat).Mul(new(big.Rat).SetInt64(bucket.SourceMicros), rate))
			if !ok {
				reason := "conversion result exceeds the supported range for " + bucket.SourceCurrency + "->" + report.DisplayCurrency
				conversion.State, conversion.Reason, conversion.ReasonCode = MeterStateError, reason, "fx_conversion_overflow"
				report.Conversions = append(report.Conversions, conversion)
				setDisplayFailure(MeterStateError, reason, "fx_conversion_overflow", true)
				continue
			}
			if total, ok := addInt64(display.AmountMicros, convertedMicros); ok {
				display.AmountMicros = total
			} else {
				reason := "display currency total exceeds the supported range"
				conversion.State, conversion.Reason, conversion.ReasonCode = MeterStateError, reason, "cost_total_overflow"
				report.Conversions = append(report.Conversions, conversion)
				setDisplayFailure(MeterStateError, reason, "cost_total_overflow", true)
				continue
			}
			conversion.ConvertedAmountMicros = convertedMicros
			report.Conversions = append(report.Conversions, conversion)
		}
	}
	if len(report.Conversions) == 1 && report.Conversions[0].State == MeterStateRecorded {
		display.Rate = report.Conversions[0].Rate
		display.RateSource = report.Conversions[0].RateSource
		display.RateAsOf = report.Conversions[0].RateAsOf
	}
	if report.RecordCount == 0 {
		setDisplayFailure(MeterStateUnavailable, "no usage ledger records in selected window", "no_ledger_records", false)
	} else if pricingState == MeterStateError {
		setDisplayFailure(MeterStateError, "one or more ledger records contain invalid pricing metadata", "pricing_error", false)
	} else if pricingState == MeterStateUnavailable {
		setDisplayFailure(MeterStateUnavailable, "one or more ledger records are not priced yet", "pricing_unavailable", false)
	} else if display.State != MeterStateUnavailable && display.State != MeterStateError {
		display.State = usageStateForQuantity(display.AmountMicros)
	}
	if display.State == MeterStateUnavailable || display.State == MeterStateError {
		display.AmountMicros = 0
	}
	report.DisplayTotal = display
	if display.State == MeterStateRecorded || display.State == MeterStateZero {
		total := display.AmountMicros
		report.TotalCostMicros = &total
		report.CostMicros = &total
	}
	return report, nil
}

func normalizeUsageUnit(unit string) string {
	return metering.NormalizeUnit(unit)
}

func worseMeterState(current, next MeterState) MeterState {
	severity := func(state MeterState) int {
		switch state {
		case MeterStateError:
			return 4
		case MeterStateUnavailable:
			return 3
		case MeterStateDelayed:
			return 2
		case MeterStateRecorded:
			return 1
		default:
			return 0
		}
	}
	if severity(next) >= severity(current) {
		return next
	}
	return current
}

func tokenMetricState(meters []UsageMeter, metric string) MeterState {
	state := MeterStateUnavailable
	found := false
	for _, meter := range meters {
		if meter.MetricType != metric || meter.Unit != UnitTokens {
			continue
		}
		if !found {
			state = meter.State
			found = true
			continue
		}
		state = worseMeterState(state, meter.State)
	}
	return state
}

func combineTokenStates(input, output MeterState, total int64) MeterState {
	if input == MeterStateUnavailable || output == MeterStateUnavailable {
		return MeterStateUnavailable
	}
	state := worseMeterState(input, output)
	if state == MeterStateRecorded && total == 0 {
		return MeterStateZero
	}
	return state
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

func roundRatInt64(value *big.Rat) (int64, bool) {
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
	if !quotient.IsInt64() {
		return 0, false
	}
	return quotient.Int64(), true
}

func addInt64(left, right int64) (int64, bool) {
	if (right > 0 && left > math.MaxInt64-right) || (right < 0 && left < math.MinInt64-right) {
		return 0, false
	}
	return left + right, true
}

func addUsageReconciliationIssue(report *UsageTotal, code, message string) {
	report.Reconciled = false
	for _, issue := range report.ReconciliationIssues {
		if issue.Code == code && issue.Message == message {
			return
		}
	}
	report.ReconciliationErrors = append(report.ReconciliationErrors, message)
	report.ReconciliationIssues = append(report.ReconciliationIssues, UsageReconciliationIssue{Code: code, Message: message})
}

func usageStateForQuantity(quantity int64) MeterState {
	if quantity == 0 {
		return MeterStateZero
	}
	return MeterStateRecorded
}

func setUsageDimensionReconciliation(target map[string]Usage, reconciled bool) {
	for id, usage := range target {
		usage.Reconciled = reconciled
		target[id] = usage
	}
}
