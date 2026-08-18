package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
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
	UserID       string
	SessionID    string
	ProjectID    string
	LedgerCursor string
	LedgerLimit  int
}

func usageFilterFromRequest(r *http.Request, filter usageFilter) usageFilter {
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
	report := UsageTotal{
		Range:          rangeLabel,
		WindowStart:    since,
		WindowEnd:      until,
		ByUnit:         map[string]Usage{},
		CostByCurrency: map[string]UsageAmount{},
		Conversions:    []UsageConversion{},
		Meters:         []UsageMeter{},
		Metrics:        []UsageMeter{},
		ByMetric:       map[string]UsageMeter{},
		ByModel:        map[string]Usage{},
		ByUser:         map[string]Usage{},
		BySession:      map[string]Usage{},
		ModelTotals:    map[string]UsageModelTotal{},
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
	delayedExpr := "SUM(CASE WHEN metered_at IS NOT NULL AND created_at > datetime(metered_at, '+15 minutes') THEN 1 ELSE 0 END)"
	if s.db.Dialector.Name() == "postgres" {
		delayedExpr = "SUM(CASE WHEN metered_at IS NOT NULL AND created_at > metered_at + INTERVAL '15 minutes' THEN 1 ELSE 0 END)"
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
		"SUM(CASE WHEN metered_at IS NULL THEN 1 ELSE 0 END) AS legacy_count",
		delayedExpr + " AS delayed_count",
		"MAX(COALESCE(metered_at, created_at)) AS last_updated",
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
	costLedger := map[string]int64{}
	var legacyCount, unpricedCount, pendingCount, pricingErrorCount, invalidCurrencyCount int64
	for _, aggregate := range aggregates {
		report.RecordCount += int(aggregate.RecordCount)
		legacyCount += aggregate.LegacyCount
		unpricedCount += aggregate.UnpricedCount
		pendingCount += aggregate.PendingCount
		pricingErrorCount += aggregate.PricingErrorCount
		invalidCurrencyCount += aggregate.InvalidCurrencyCount
		if aggregate.LegacyCount > 0 {
			report.Reconciled = false
		}
		unit := normalizeUsageUnit(aggregate.Unit)
		expected := expectedMeterUnits[aggregate.MetricType]
		if unit == "" {
			unit = expected
		}
		validUnit := unit != ""
		if !validUnit {
			unit = UnitUnknown
			report.Reconciled = false
		} else if expected != "" && unit != expected {
			report.Reconciled = false
			report.ReconciliationErrors = append(report.ReconciliationErrors, fmt.Sprintf("%d %s records use %s; expected %s", aggregate.RecordCount, aggregate.MetricType, unit, expected))
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
		meter.Quantity += aggregate.Quantity
		meter.AmountMicros += aggregate.AmountMicros
		if updated := normalizeUsageTimestamp(aggregate.LastUpdated); updated > meter.LastUpdated {
			meter.LastUpdated = updated
		}
		seenMetric[aggregate.MetricType] = true
		if meter.LastUpdated > report.LastUpdated {
			report.LastUpdated = meter.LastUpdated
		}
		if aggregate.DelayedCount > 0 {
			meter.State = MeterStateDelayed
			meter.Reason = fmt.Sprintf("%d meter events arrived more than 15 minutes after occurrence", aggregate.DelayedCount)
		}
		switch {
		case aggregate.PricingErrorCount > 0 || aggregate.InvalidCurrencyCount > 0:
			meter.CostState = MeterStateError
			meter.CostReason = "invalid pricing metadata"
		case aggregate.PendingCount > 0:
			meter.CostState = MeterStateUnavailable
			meter.CostReason = "pricing is pending"
		case aggregate.UnpricedCount > 0:
			meter.CostState = MeterStateUnavailable
			meter.CostReason = "usage was metered without an asserted price"
		default:
			meter.CostState = usageStateForQuantity(meter.AmountMicros)
		}
		if validUnit {
			unitLedger[unit] += aggregate.Quantity
		}
		if aggregate.PricedCount > 0 && currency != "" {
			costLedger[currency] += aggregate.AmountMicros
		}

		if aggregate.MetricType == "tokens_in" {
			report.InputTokens += aggregate.Quantity
		}
		if aggregate.MetricType == "tokens_out" {
			report.OutputTokens += aggregate.Quantity
		}
	}
	if legacyCount > 0 {
		report.ReconciliationErrors = append(report.ReconciliationErrors, fmt.Sprintf("%d usage records use created_at because metered_at is unavailable", legacyCount))
	}
	if unpricedCount > 0 {
		report.Reconciled = false
		report.ReconciliationErrors = append(report.ReconciliationErrors, fmt.Sprintf("%d usage records are metered but unpriced", unpricedCount))
	}
	if pendingCount > 0 {
		report.Reconciled = false
		report.ReconciliationErrors = append(report.ReconciliationErrors, fmt.Sprintf("%d usage records have pending pricing", pendingCount))
	}
	if pricingErrorCount > 0 {
		report.Reconciled = false
		report.ReconciliationErrors = append(report.ReconciliationErrors, fmt.Sprintf("%d usage records have invalid pricing state", pricingErrorCount))
	}
	if invalidCurrencyCount > 0 {
		report.Reconciled = false
		report.ReconciliationErrors = append(report.ReconciliationErrors, fmt.Sprintf("%d priced usage records have no currency", invalidCurrencyCount))
	}

	report.TotalTokens = report.InputTokens + report.OutputTokens

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
			report.Meters = append(report.Meters, UsageMeter{MetricType: metric, Unit: unit, State: MeterStateUnavailable, Reason: "no meter event in selected window"})
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
	populateLegacyMeterAliases(&report)
	for unit, quantity := range unitLedger {
		state := usageStateForQuantity(quantity)
		for _, meter := range report.Meters {
			if meter.Unit == unit && meter.State == MeterStateDelayed {
				state = MeterStateDelayed
				break
			}
		}
		report.ByUnit[unit] = Usage{Quantity: quantity, Unit: unit, WindowStart: since, WindowEnd: until, LastUpdated: report.LastUpdated, Reconciled: report.Reconciled, State: state}
	}
	for _, unit := range []string{UnitTokens, UnitSeconds, UnitBytes, UnitCount, UnitUSDMicro} {
		if _, ok := report.ByUnit[unit]; !ok {
			report.ByUnit[unit] = Usage{Unit: unit, WindowStart: since, WindowEnd: until, LastUpdated: report.LastUpdated, Reconciled: report.Reconciled, State: MeterStateUnavailable, Reason: "no meter event in selected window"}
		}
	}
	for currency, amount := range costLedger {
		report.CostByCurrency[currency] = UsageAmount{AmountMicros: amount, Currency: currency, State: usageStateForQuantity(amount)}
	}
	if err := s.aggregateUsageDimensions(orgID, filter, sinceTime, untilTime, &report); err != nil {
		return report, err
	}
	if err := s.aggregateUsageModels(orgID, filter, sinceTime, untilTime, &report); err != nil {
		return report, err
	}
	if err := s.loadUsageLedgerPage(orgID, filter, sinceTime, untilTime, &report); err != nil {
		return report, err
	}
	report, err = s.finishUsageReport(orgID, report)
	if err != nil {
		return report, err
	}
	setUsageDimensionReconciliation(report.ByModel, report.Reconciled)
	setUsageDimensionReconciliation(report.ByUser, report.Reconciled)
	setUsageDimensionReconciliation(report.BySession, report.Reconciled)
	setUsageDimensionReconciliation(report.ByUnit, report.Reconciled)
	return report, nil
}

func (s *Server) usageRecordsQuery(orgID string, filter usageFilter, since, until time.Time) *gorm.DB {
	query := s.db.Model(&models.UsageRecord{}).Where(
		"organization_id = ? AND ((metered_at >= ? AND metered_at < ?) OR (metered_at IS NULL AND created_at >= ? AND created_at < ?))",
		orgID, since, until, since, until,
	)
	if filter.UserID != "" {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if filter.SessionID != "" {
		query = query.Where("session_id = ?", filter.SessionID)
	}
	if filter.ProjectID != "" {
		sessions := s.db.Model(&models.Session{}).Select("session_id").Where("organization_id = ? AND project_id = ?", orgID, filter.ProjectID)
		query = query.Where("session_id IN (?)", sessions)
	}
	return query
}

func (s *Server) aggregateUsageDimensions(orgID string, filter usageFilter, since, until time.Time, report *UsageTotal) error {
	for column, target := range map[string]map[string]Usage{
		"model_package_id": report.ByModel,
		"user_id":          report.ByUser,
		"session_id":       report.BySession,
	} {
		var rows []struct {
			ID          string
			Quantity    int64
			LastUpdated string
		}
		if err := s.usageRecordsQuery(orgID, filter, since, until).
			Select(column+" AS id, SUM(quantity) AS quantity, MAX(COALESCE(metered_at, created_at)) AS last_updated").
			Where("metric_type IN ? AND "+column+" <> ''", []string{"tokens_in", "tokens_out"}).
			Group(column).Order("quantity DESC, id ASC").Limit(250).Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			target[row.ID] = Usage{
				Quantity: row.Quantity, Unit: UnitTokens,
				WindowStart: since.UTC().Format(time.RFC3339), WindowEnd: until.UTC().Format(time.RFC3339),
				LastUpdated: normalizeUsageTimestamp(row.LastUpdated), Reconciled: report.Reconciled,
				State: usageStateForQuantity(row.Quantity),
			}
		}
	}
	return nil
}

func (s *Server) aggregateUsageModels(orgID string, filter usageFilter, since, until time.Time, report *UsageTotal) error {
	var rows []struct {
		ModelPackageID string
		MetricType     string
		Currency       string
		PricingState   string
		Quantity       int64
		AmountMicros   int64
	}
	if err := s.usageRecordsQuery(orgID, filter, since, until).
		Select("model_package_id, metric_type, UPPER(currency) AS currency, pricing_state, SUM(quantity) AS quantity, SUM(CASE WHEN pricing_state = 'priced' THEN cost_micros ELSE 0 END) AS amount_micros").
		Where("model_package_id <> '' AND metric_type IN ?", []string{"tokens_in", "tokens_out"}).
		Group("model_package_id, metric_type, UPPER(currency), pricing_state").
		Order("model_package_id ASC, metric_type ASC, currency ASC, pricing_state ASC").Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		total := report.ModelTotals[row.ModelPackageID]
		if total.CostByCurrency == nil {
			total.CostByCurrency = map[string]int64{}
		}
		if row.MetricType == "tokens_in" {
			total.InputTokens += row.Quantity
		} else {
			total.OutputTokens += row.Quantity
		}
		if row.PricingState == models.UsagePricingPriced && row.Currency != "" {
			total.CostByCurrency[row.Currency] += row.AmountMicros
		}
		if row.PricingState == models.UsagePricingError {
			total.PricingState = MeterStateError
		} else if row.PricingState != models.UsagePricingPriced && total.PricingState != MeterStateError {
			total.PricingState = MeterStateUnavailable
		} else if total.PricingState == "" {
			total.PricingState = MeterStateRecorded
		}
		report.ModelTotals[row.ModelPackageID] = total
	}
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
	if cursorTime, cursorID, ok := decodeUsageCursor(filter.LedgerCursor); ok {
		query = query.Where("(COALESCE(metered_at, created_at) < ? OR (COALESCE(metered_at, created_at) = ? AND id < ?))", cursorTime, cursorTime, cursorID)
	}
	var records []models.UsageRecord
	if err := query.Order("COALESCE(metered_at, created_at) DESC, id DESC").Limit(limit + 1).Find(&records).Error; err != nil {
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
			unit = expectedMeterUnits[record.MetricType]
		}
		if unit == "" {
			unit = UnitUnknown
		}
		amount := int64(0)
		currency := ""
		if record.PricingState == models.UsagePricingPriced {
			amount = record.CostMicros
			currency = strings.ToUpper(strings.TrimSpace(record.Currency))
		}
		report.Drilldown = append(report.Drilldown, UsageLedgerRow{
			ID: record.ID, OccurredAt: occurred.UTC().Format(time.RFC3339), Bucket: record.MetricType,
			Unit: unit, Quantity: record.Quantity, RateMicrosPerUnit: rateMicros(amount, record.Quantity),
			AmountMicros: amount, Currency: currency, PricingState: record.PricingState,
			RefType: "usage_record", RefID: record.ID,
			UserID: record.UserID, HarnessID: record.HarnessID, SessionID: record.SessionID,
			ModelPackageID: record.ModelPackageID, EndpointID: record.EndpointID,
		})
	}
	if report.LedgerHasMore && len(records) > 0 {
		last := records[len(records)-1]
		report.LedgerNextCursor = encodeUsageCursor(effectiveUsageTime(last), last.ID)
	}
	return nil
}

func populateLegacyMeterAliases(report *UsageTotal) {
	report.Metrics = append([]UsageMeter(nil), report.Meters...)
	for _, meter := range report.Meters {
		if _, exists := report.ByMetric[meter.MetricType]; !exists {
			report.ByMetric[meter.MetricType] = meter
		}
	}
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

func encodeUsageCursor(occurred time.Time, id string) string {
	value := occurred.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeUsageCursor(cursor string) (time.Time, string, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", false
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 || parts[1] == "" {
		return time.Time{}, "", false
	}
	parsed, err := time.Parse(time.RFC3339Nano, parts[0])
	return parsed, parts[1], err == nil
}

func (s *Server) finishUsageReport(orgID string, report UsageTotal) (UsageTotal, error) {
	report.DisplayCurrency = "KRW"
	var settings []models.OrgSetting
	if err := s.db.Where("organization_id = ? AND key IN ?", orgID, []string{"billing.display_currency", "billing.fx_rates"}).Find(&settings).Error; err != nil {
		return report, fmt.Errorf("usage billing settings: %w", err)
	}
	settingByKey := make(map[string]string, len(settings))
	for _, setting := range settings {
		settingByKey[setting.Key] = setting.Value
	}
	if value := strings.ToUpper(strings.TrimSpace(settingByKey["billing.display_currency"])); value != "" {
		report.DisplayCurrency = value
	} else if len(report.CostByCurrency) == 1 {
		for currency := range report.CostByCurrency {
			report.DisplayCurrency = currency
		}
	}
	report.Currency = report.DisplayCurrency

	rates := map[string]fxRate{}
	if raw := strings.TrimSpace(settingByKey["billing.fx_rates"]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &rates); err != nil {
			report.Reconciled = false
			report.ReconciliationErrors = append(report.ReconciliationErrors, "billing.fx_rates is invalid JSON")
			report.DisplayTotal = UsageAmount{Currency: report.DisplayCurrency, State: MeterStateError, Reason: "conversion rate configuration is invalid"}
			return report, nil
		}
	}
	display := UsageAmount{Currency: report.DisplayCurrency, State: MeterStateZero}
	pricingState := MeterStateRecorded
	for _, meter := range report.Meters {
		if meter.CostState == MeterStateError {
			pricingState = MeterStateError
			break
		}
		if meter.CostState == MeterStateUnavailable {
			pricingState = MeterStateUnavailable
		}
	}
	sourceCurrencies := make([]string, 0, len(report.CostByCurrency))
	for source := range report.CostByCurrency {
		sourceCurrencies = append(sourceCurrencies, source)
	}
	sort.Strings(sourceCurrencies)
	for _, source := range sourceCurrencies {
		amount := report.CostByCurrency[source]
		if source == report.DisplayCurrency {
			display.AmountMicros += amount.AmountMicros
			continue
		}
		conversion := UsageConversion{
			SourceCurrency: source, TargetCurrency: report.DisplayCurrency,
			SourceAmountMicros: amount.AmountMicros, State: MeterStateRecorded,
		}
		rateKey := source + "->" + report.DisplayCurrency
		rateDef, ok := rates[rateKey]
		if !ok {
			display.State = MeterStateUnavailable
			display.Reason = "missing conversion rate from " + source + " to " + report.DisplayCurrency
			conversion.State = MeterStateUnavailable
			conversion.Reason = display.Reason
			report.Conversions = append(report.Conversions, conversion)
			report.Reconciled = false
			report.ReconciliationErrors = append(report.ReconciliationErrors, display.Reason)
			continue
		}
		rate := new(big.Rat)
		if _, ok := rate.SetString(rateDef.Rate); !ok || rate.Sign() <= 0 || strings.TrimSpace(rateDef.Source) == "" || strings.TrimSpace(rateDef.AsOf) == "" || strings.TrimSpace(rateDef.Version) == "" {
			display.State = MeterStateError
			display.Reason = "invalid or unversioned conversion rate for " + rateKey
			conversion.State = MeterStateError
			conversion.Reason = display.Reason
			report.Conversions = append(report.Conversions, conversion)
			report.Reconciled = false
			report.ReconciliationErrors = append(report.ReconciliationErrors, display.Reason)
			continue
		}
		converted := new(big.Rat).Mul(new(big.Rat).SetInt64(amount.AmountMicros), rate)
		convertedMicros := roundRat(converted)
		display.AmountMicros += convertedMicros
		conversion.Rate = rateDef.Rate
		conversion.ConvertedAmountMicros = convertedMicros
		conversion.RateSource = rateDef.Source
		conversion.RateAsOf = rateDef.AsOf
		conversion.RateVersion = rateDef.Version
		report.Conversions = append(report.Conversions, conversion)
		display.Rate = rateDef.Rate
		display.RateSource = rateDef.Source
		display.RateAsOf = rateDef.AsOf
	}
	if report.RecordCount == 0 {
		display.State = MeterStateUnavailable
		display.Reason = "no usage ledger records in selected window"
	} else if pricingState == MeterStateError {
		display.AmountMicros = 0
		display.State = MeterStateError
		display.Reason = "one or more ledger records contain invalid pricing metadata"
	} else if pricingState == MeterStateUnavailable {
		display.AmountMicros = 0
		display.State = MeterStateUnavailable
		display.Reason = "one or more ledger records are not priced yet"
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

func setUsageDimensionReconciliation(target map[string]Usage, reconciled bool) {
	for id, usage := range target {
		usage.Reconciled = reconciled
		target[id] = usage
	}
}
