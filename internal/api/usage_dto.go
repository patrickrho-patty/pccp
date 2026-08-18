package api

// Usage DTOs — PAT-1501.
//
// Every metric in the response carries an explicit unit discriminator.
// Cross-unit aggregation at the type level is impossible; consumers
// must sum per-unit rows or use the explicit TotalByUnit shape.

const (
	UnitTokens       = "tokens"
	UnitSeconds      = "seconds"
	UnitBytes        = "bytes"
	UnitCount        = "count"
	UnitUSDMicro     = "usd_micro"
	UnitKRW          = "krw"
)

// Usage is one row of metered consumption with its unit, window, and
// freshness. PAT-1501: a single number without a unit is a bug.
type Usage struct {
	Quantity    int64  `json:"quantity"`
	Unit        string `json:"unit"` // tokens | seconds | bytes | count | usd_micro | krw
	Currency    string `json:"currency,omitempty"` // for usd_micro / krw
	WindowStart string `json:"window_start,omitempty"` // RFC3339
	WindowEnd   string `json:"window_end,omitempty"`
	LastUpdated string `json:"last_updated,omitempty"` // RFC3339
	Reconciled  bool   `json:"reconciled"`             // true when line items match the displayed total
	// SourceQuantity preserves the original quantity in the source
	// unit so the UI can display both source and display values
	// (e.g., µ¢ and KRW for chargeback rows).
	SourceQuantity int64  `json:"source_quantity,omitempty"`
	SourceUnit     string `json:"source_unit,omitempty"`
	DisplayRate    string `json:"display_rate,omitempty"` // "1 USD = 1,389 KRW (2026-08-18 NBR)"
}

// UsageTotal is a window-aggregated total with one row per unit.
// PAT-1501: the UI MUST NOT compute cross-unit totals.
type UsageTotal struct {
	WindowStart string            `json:"window_start,omitempty"`
	WindowEnd   string            `json:"window_end,omitempty"`
	ByUnit      map[string]Usage  `json:"by_unit"`             // keyed by Unit
	DisplayCurrency string         `json:"display_currency,omitempty"`
	DisplayTotal   Usage           `json:"display_total"`      // totals in display currency; zero-value unit when no display
	Reconciled     bool            `json:"reconciled"`
	Drilldown      []UsageLedgerRow `json:"drilldown,omitempty"` // ledger lines that compose the total
}

// UsageLedgerRow is one ledger line (the audit trail behind a total).
// PAT-1501: every visible total must reconcile to a drill-down.
type UsageLedgerRow struct {
	OccurredAt  string `json:"occurred_at"`
	Bucket      string `json:"bucket"`        // workspace_chat, editor_lex, tool_call, reservation, refund, ...
	Unit        string `json:"unit"`
	Quantity    int64  `json:"quantity"`
	Note        string `json:"note,omitempty"`
	RefType     string `json:"ref_type,omitempty"`
	RefID       string `json:"ref_id,omitempty"`
}

// MeterState distinguishes zero from unavailable from error in the UI.
// PAT-1501: meter ran + zero result ≠ meter hasn't run yet.
type MeterState int

const (
	MeterStateZero MeterState = iota
	MeterStateUnavailable
	MeterStateError
)

// MeterCell is the cell the UI renders for one (scope, unit) pair.
// PAT-1501: UI components MUST render this struct; they MUST NOT
// format raw numbers that arrive without the Unit field.
type MeterCell struct {
	State      MeterState `json:"state"`
	Usage      Usage      `json:"usage,omitempty"`
	Reason     string     `json:"reason,omitempty"` // for Unavailable / Error
}
