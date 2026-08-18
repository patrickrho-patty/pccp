package api

import (
	"encoding/json"
	"strconv"
)

const (
	UnitTokens        = "tokens"
	UnitSeconds       = "seconds"
	UnitBytes         = "bytes"
	UnitCount         = "count"
	UnitUSDMicro      = "usd_micro"
	UnitKRW           = "krw"
	UnitCurrencyMicro = "currency_micro"
	UnitUnknown       = "unknown"
)

type MeterState string

const (
	MeterStateRecorded    MeterState = "recorded"
	MeterStateZero        MeterState = "zero"
	MeterStateUnavailable MeterState = "unavailable"
	MeterStateDelayed     MeterState = "delayed"
	MeterStateError       MeterState = "error"
)

// Usage is one unit-safe aggregate. A quantity without its unit, window,
// freshness, and state is not a valid usage value.
type Usage struct {
	Label          string     `json:"label,omitempty"`
	Quantity       int64      `json:"quantity,string"`
	Unit           string     `json:"unit"`
	Currency       string     `json:"currency,omitempty"`
	WindowStart    string     `json:"window_start,omitempty"`
	WindowEnd      string     `json:"window_end,omitempty"`
	LastUpdated    string     `json:"last_updated,omitempty"`
	Reconciled     bool       `json:"reconciled"`
	SourceQuantity int64      `json:"source_quantity,omitempty,string"`
	SourceUnit     string     `json:"source_unit,omitempty"`
	DisplayRate    string     `json:"display_rate,omitempty"`
	State          MeterState `json:"state,omitempty"`
	Reason         string     `json:"reason,omitempty"`
	ReasonCode     string     `json:"reason_code,omitempty"`
}

type UsageMeter struct {
	MetricType        string     `json:"metric_type"`
	Unit              string     `json:"unit"`
	Quantity          int64      `json:"quantity,string"`
	RateMicrosPerUnit string     `json:"rate_micros_per_unit,omitempty"`
	AmountMicros      int64      `json:"amount_micros,string"`
	Currency          string     `json:"currency,omitempty"`
	State             MeterState `json:"state"`
	Reason            string     `json:"reason,omitempty"`
	ReasonCode        string     `json:"reason_code,omitempty"`
	CostState         MeterState `json:"cost_state"`
	CostReason        string     `json:"cost_reason,omitempty"`
	CostReasonCode    string     `json:"cost_reason_code,omitempty"`
	LastUpdated       string     `json:"last_updated,omitempty"`
}

type UsageAmount struct {
	AmountMicros int64      `json:"amount_micros,string"`
	Currency     string     `json:"currency"`
	State        MeterState `json:"state"`
	Reason       string     `json:"reason,omitempty"`
	ReasonCode   string     `json:"reason_code,omitempty"`
	Rate         string     `json:"rate,omitempty"`
	RateSource   string     `json:"rate_source,omitempty"`
	RateAsOf     string     `json:"rate_as_of,omitempty"`
}

type UsageConversion struct {
	SourceCurrency        string     `json:"source_currency"`
	TargetCurrency        string     `json:"target_currency"`
	SourceAmountMicros    int64      `json:"source_amount_micros,string"`
	Rate                  string     `json:"rate,omitempty"`
	ConvertedAmountMicros int64      `json:"converted_amount_micros,string"`
	RateSource            string     `json:"rate_source,omitempty"`
	RateAsOf              string     `json:"rate_as_of,omitempty"`
	RateVersion           string     `json:"rate_version,omitempty"`
	State                 MeterState `json:"state"`
	Reason                string     `json:"reason,omitempty"`
	ReasonCode            string     `json:"reason_code,omitempty"`
}

type UsageModelTotal struct {
	InputTokens    int64            `json:"input_tokens,string"`
	OutputTokens   int64            `json:"output_tokens,string"`
	CostByCurrency map[string]int64 `json:"cost_by_currency"`
	PricingState   MeterState       `json:"pricing_state"`
}

func (u UsageModelTotal) MarshalJSON() ([]byte, error) {
	costs := make(map[string]string, len(u.CostByCurrency))
	for currency, amount := range u.CostByCurrency {
		costs[currency] = strconv.FormatInt(amount, 10)
	}
	return json.Marshal(struct {
		InputTokens    string            `json:"input_tokens"`
		OutputTokens   string            `json:"output_tokens"`
		CostByCurrency map[string]string `json:"cost_by_currency"`
		PricingState   MeterState        `json:"pricing_state"`
	}{
		InputTokens: strconv.FormatInt(u.InputTokens, 10), OutputTokens: strconv.FormatInt(u.OutputTokens, 10),
		CostByCurrency: costs, PricingState: u.PricingState,
	})
}

func (u *UsageModelTotal) UnmarshalJSON(data []byte) error {
	var wire struct {
		InputTokens    json.Number            `json:"input_tokens"`
		OutputTokens   json.Number            `json:"output_tokens"`
		CostByCurrency map[string]json.Number `json:"cost_by_currency"`
		PricingState   MeterState             `json:"pricing_state"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	var err error
	if u.InputTokens, err = strconv.ParseInt(wire.InputTokens.String(), 10, 64); err != nil {
		return err
	}
	if u.OutputTokens, err = strconv.ParseInt(wire.OutputTokens.String(), 10, 64); err != nil {
		return err
	}
	u.CostByCurrency = make(map[string]int64, len(wire.CostByCurrency))
	for currency, amount := range wire.CostByCurrency {
		parsed, parseErr := strconv.ParseInt(amount.String(), 10, 64)
		if parseErr != nil {
			return parseErr
		}
		u.CostByCurrency[currency] = parsed
	}
	u.PricingState = wire.PricingState
	return nil
}

type UsageDimensionMeta struct {
	Returned        int  `json:"returned"`
	HasOther        bool `json:"has_other"`
	HasUnattributed bool `json:"has_unattributed"`
}

type UsageReconciliationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// UsageTotal is the canonical usage response shared by organization, user,
// session, project, and analytics surfaces.
type UsageTotal struct {
	Range                string                        `json:"range,omitempty"`
	WindowStart          string                        `json:"window_start,omitempty"`
	WindowEnd            string                        `json:"window_end,omitempty"`
	SnapshotAt           string                        `json:"snapshot_at,omitempty"`
	LastUpdated          string                        `json:"last_updated,omitempty"`
	ByUnit               map[string]Usage              `json:"by_unit"`
	DisplayCurrency      string                        `json:"display_currency,omitempty"`
	DisplayTotal         UsageAmount                   `json:"display_total"`
	CostByCurrency       map[string]UsageAmount        `json:"cost_by_currency"`
	Conversions          []UsageConversion             `json:"conversions"`
	Meters               []UsageMeter                  `json:"meters"`
	ByModel              map[string]Usage              `json:"by_model"`
	ByUser               map[string]Usage              `json:"by_user"`
	BySession            map[string]Usage              `json:"by_session"`
	ModelTotals          map[string]UsageModelTotal    `json:"model_totals"`
	DimensionMeta        map[string]UsageDimensionMeta `json:"dimension_meta,omitempty"`
	InputTokens          int64                         `json:"input_tokens,string"`
	OutputTokens         int64                         `json:"output_tokens,string"`
	TotalTokens          int64                         `json:"total_tokens,string"`
	InputTokensState     MeterState                    `json:"input_tokens_state"`
	OutputTokensState    MeterState                    `json:"output_tokens_state"`
	TotalTokensState     MeterState                    `json:"total_tokens_state"`
	TotalCostMicros      *int64                        `json:"total_cost_micros,string"`
	CostMicros           *int64                        `json:"cost_micros,string"`
	Currency             string                        `json:"currency,omitempty"`
	RecordCount          int                           `json:"record_count"`
	SessionCount         int                           `json:"session_count,omitempty"`
	Reconciled           bool                          `json:"reconciled"`
	ReconciliationErrors []string                      `json:"reconciliation_errors,omitempty"`
	ReconciliationIssues []UsageReconciliationIssue    `json:"reconciliation_issues,omitempty"`
	Drilldown            []UsageLedgerRow              `json:"drilldown"`
	LedgerHasMore        bool                          `json:"ledger_has_more"`
	LedgerNextCursor     string                        `json:"ledger_next_cursor,omitempty"`
}

type UsageLedgerRow struct {
	ID                     string     `json:"id"`
	OccurredAt             string     `json:"occurred_at"`
	Bucket                 string     `json:"bucket"`
	Unit                   string     `json:"unit"`
	Quantity               int64      `json:"quantity,string"`
	RateMicrosPerUnit      string     `json:"rate_micros_per_unit,omitempty"`
	AmountMicros           int64      `json:"amount_micros,string"`
	Currency               string     `json:"currency,omitempty"`
	PricingState           string     `json:"pricing_state,omitempty"`
	MeterState             MeterState `json:"meter_state"`
	ReasonCode             string     `json:"reason_code,omitempty"`
	IncludedInTotals       bool       `json:"included_in_totals"`
	AppliedRateMicrosPer1K *int64     `json:"applied_rate_micros_per_1k,string,omitempty"`
	AppliedPriceVersion    string     `json:"applied_price_version,omitempty"`
	AppliedPriceSource     string     `json:"applied_price_source,omitempty"`
	Note                   string     `json:"note,omitempty"`
	RefType                string     `json:"ref_type,omitempty"`
	RefID                  string     `json:"ref_id,omitempty"`
	UserID                 string     `json:"user_id,omitempty"`
	UserLabel              string     `json:"user_label,omitempty"`
	UserResolved           bool       `json:"user_resolved"`
	HarnessID              string     `json:"harness_id,omitempty"`
	HarnessLabel           string     `json:"harness_label,omitempty"`
	HarnessResolved        bool       `json:"harness_resolved"`
	SessionID              string     `json:"session_id,omitempty"`
	SessionLabel           string     `json:"session_label,omitempty"`
	SessionResolved        bool       `json:"session_resolved"`
	ModelPackageID         string     `json:"model_package_id,omitempty"`
	ModelLabel             string     `json:"model_label,omitempty"`
	ModelResolved          bool       `json:"model_resolved"`
	EndpointID             string     `json:"endpoint_id,omitempty"`
	EndpointLabel          string     `json:"endpoint_label,omitempty"`
	EndpointResolved       bool       `json:"endpoint_resolved"`
	ProjectID              string     `json:"project_id,omitempty"`
	ProjectLabel           string     `json:"project_label,omitempty"`
	ProjectResolved        bool       `json:"project_resolved"`
	Adjustment             bool       `json:"adjustment,omitempty"`
}
