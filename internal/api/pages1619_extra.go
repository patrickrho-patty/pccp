package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"github.com/patrickrho-patty/pccp/internal/impact"
	"github.com/patrickrho-patty/pccp/internal/metering"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/registry"
)

// pages1619_extra.go: web/16 analytics (range+cost+export), web/17 audit
// (server query, legal holds, SIEM forward, evidence bundle, live tail),
// web/18 model infra (publish verification, recall impact, ring), web/19
// code explorer (spans, attribution, blast radius).

// --- 16: analytics ---

// handleUsageSummaryExtended returns the same reconciled report used by the
// user, session, and project scopes. CSV exports the exact ledger rows rather
// than a second independently calculated summary.
func (s *Server) handleUsageSummaryExtended(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	action := usageReadAction(r)
	if r.URL.Query().Get("format") == "csv" {
		action = UsageActionExport
	}
	if !requireUsagePermission(w, r, action, "organization", orgID) {
		return
	}
	days, since, until := usageWindowFromRequest(r, time.Now())
	if r.URL.Query().Get("format") == "csv" {
		sinceTime, sinceErr := time.Parse(time.RFC3339, since)
		untilTime, untilErr := time.Parse(time.RFC3339, until)
		if sinceErr != nil || untilErr != nil {
			writeError(w, http.StatusBadRequest, "사용량 기간이 올바르지 않습니다")
			return
		}
		s.streamUsageCSV(w, orgID, usageFilter{Context: r.Context(), SnapshotAt: time.Now().UTC()}, sinceTime, untilTime)
		return
	}
	report, err := s.buildUsageReport(orgID, usageFilterFromRequest(r, usageFilter{Projection: usageProjectionUser | usageProjectionModel | usageProjectionLedger}), fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeUsageReportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) streamUsageCSV(w http.ResponseWriter, orgID string, filter usageFilter, since, until time.Time) {
	rows, err := s.usageRecordsQuery(orgID, filter, since, until).
		Order("metered_at DESC, id DESC").Rows()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=usage-summary.csv")
	w.Header().Set("Trailer", "X-Export-Error")
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"occurred_at", "record_id", "metric_type", "unit", "meter_state", "reason_code", "included_in_totals", "quantity", "pricing_state", "rate_micros_per_unit", "amount_micros", "currency", "user_id", "harness_id", "session_id", "project_id", "model_package_id", "endpoint_id", "adjustment", "applied_rate_micros_per_1k", "applied_price_version", "applied_price_source"}); err != nil {
		return
	}
	written := 0
	for rows.Next() {
		var record models.UsageRecord
		if err := s.db.ScanRows(rows, &record); err != nil {
			w.Header().Set("X-Export-Error", "row_scan_failed")
			return
		}
		unit := normalizeUsageUnit(record.Unit)
		if unit == "" {
			unit = UnitUnknown
		}
		meterState, reasonCode, includedInTotals := usageStateForQuantity(record.Quantity), "", true
		if _, err := metering.Validate(record.MetricType, record.Unit); err != nil {
			meterState, reasonCode, includedInTotals = MeterStateError, "invalid_meter_unit", false
		}
		amount, currency := int64(0), ""
		if record.PricingState == models.UsagePricingPriced {
			amount = record.CostMicros
			currency = strings.ToUpper(strings.TrimSpace(record.Currency))
		}
		appliedRate := ""
		if strings.TrimSpace(record.AppliedPriceVersion) != "" || strings.TrimSpace(record.AppliedPriceSource) != "" {
			appliedRate = strconv.FormatInt(record.AppliedRateMicrosPer1K, 10)
		}
		if err := cw.Write([]string{
			csvSafe(effectiveUsageTime(record).Format(time.RFC3339)), csvSafe(record.ID), csvSafe(record.MetricType), csvSafe(unit),
			csvSafe(string(meterState)), csvSafe(reasonCode), strconv.FormatBool(includedInTotals), strconv.FormatInt(record.Quantity, 10), csvSafe(record.PricingState), rateMicros(amount, record.Quantity),
			strconv.FormatInt(amount, 10), csvSafe(currency), csvSafe(record.UserID), csvSafe(record.HarnessID), csvSafe(record.SessionID), csvSafe(record.ProjectID),
			csvSafe(record.ModelPackageID), csvSafe(record.EndpointID), strconv.FormatBool(record.Adjustment), appliedRate, csvSafe(record.AppliedPriceVersion), csvSafe(record.AppliedPriceSource),
		}); err != nil {
			w.Header().Set("X-Export-Error", "write_failed")
			return
		}
		written++
		if written%256 == 0 {
			cw.Flush()
			if cw.Error() != nil {
				w.Header().Set("X-Export-Error", "write_failed")
				return
			}
		}
	}
	if err := rows.Err(); err != nil {
		w.Header().Set("X-Export-Error", "row_read_failed")
		return
	}
	cw.Flush()
	if cw.Error() != nil {
		w.Header().Set("X-Export-Error", "write_failed")
	}
}

type usageExportClaims struct {
	OrganizationID string `json:"org_id"`
	Range          string `json:"range"`
	WindowStart    string `json:"window_start"`
	WindowEnd      string `json:"window_end"`
	SnapshotAt     string `json:"snapshot_at"`
	Actor          string `json:"actor"`
	Role           string `json:"role"`
	UserID         string `json:"user_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	ProjectID      string `json:"project_id,omitempty"`
	Purpose        string `json:"purpose"`
	jwt.RegisteredClaims
}

func (s *Server) handleUsageCSVTicket(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	userID, sessionID, projectID := strings.TrimSpace(r.URL.Query().Get("user_id")), strings.TrimSpace(r.URL.Query().Get("session_id")), strings.TrimSpace(r.URL.Query().Get("project_id"))
	if (userID != "" && sessionID != "") || (userID != "" && projectID != "") || (sessionID != "" && projectID != "") {
		writeError(w, http.StatusBadRequest, "내보내기 범위는 하나만 지정할 수 있습니다")
		return
	}
	resourceType, resourceID := "organization", orgID
	if userID != "" {
		resourceType, resourceID = "user", userID
	} else if sessionID != "" {
		resourceType, resourceID = "session", sessionID
	} else if projectID != "" {
		resourceType, resourceID = "project", projectID
	}
	if !requireUsagePermission(w, r, UsageActionExport, resourceType, resourceID) {
		return
	}
	var scopeCount int64
	switch resourceType {
	case "user":
		s.db.Model(&models.User{}).Where("organization_id = ? AND id = ?", orgID, userID).Count(&scopeCount)
	case "session":
		var session models.Session
		if err := s.db.Where("organization_id = ? AND (id = ? OR session_id = ?)", orgID, sessionID, sessionID).First(&session).Error; err == nil && strings.TrimSpace(session.SessionID) != "" {
			scopeCount = 1
			sessionID = session.SessionID
		}
	case "project":
		s.db.Model(&models.Project{}).Where("organization_id = ? AND id = ?", orgID, projectID).Count(&scopeCount)
	default:
		scopeCount = 1
	}
	if scopeCount != 1 {
		writeError(w, http.StatusNotFound, "내보내기 범위를 찾을 수 없습니다")
		return
	}
	rangeLabel := r.URL.Query().Get("range")
	switch rangeLabel {
	case "7d", "30d", "90d", "365d":
	default:
		rangeLabel = "30d"
	}
	now := time.Now().UTC()
	_, defaultStart, defaultEnd := usageWindowFromRequest(r, now)
	windowStart, windowEnd, snapshotAt := defaultStart, defaultEnd, now.Format(time.RFC3339Nano)
	explicitWindow := r.URL.Query().Get("window_start") != "" || r.URL.Query().Get("window_end") != "" || r.URL.Query().Get("snapshot_at") != ""
	if explicitWindow {
		windowStart, windowEnd, snapshotAt = r.URL.Query().Get("window_start"), r.URL.Query().Get("window_end"), r.URL.Query().Get("snapshot_at")
		startTime, startErr := time.Parse(time.RFC3339, windowStart)
		endTime, endErr := time.Parse(time.RFC3339, windowEnd)
		snapshotTime, snapshotErr := time.Parse(time.RFC3339, snapshotAt)
		if startErr != nil || endErr != nil || snapshotErr != nil || !startTime.Before(endTime) || snapshotTime.IsZero() || snapshotTime.After(now) {
			writeError(w, http.StatusBadRequest, "내보내기 기간 또는 스냅샷이 올바르지 않습니다")
			return
		}
		windowStart, windowEnd, snapshotAt = startTime.UTC().Format(time.RFC3339Nano), endTime.UTC().Format(time.RFC3339Nano), snapshotTime.UTC().Format(time.RFC3339Nano)
	}
	actor, role := "", ""
	if identity, ok := claimsFromCtx(r.Context()); ok {
		actor, role = identity.Email, identity.Role
	}
	claims := usageExportClaims{
		OrganizationID: orgID, Range: rangeLabel, WindowStart: windowStart, WindowEnd: windowEnd, SnapshotAt: snapshotAt,
		Actor: actor, Role: role, UserID: userID, SessionID: sessionID, ProjectID: projectID, Purpose: "usage_csv",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "pccp-usage-export", IssuedAt: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.jwtSecret))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "내보내기 티켓을 만들 수 없습니다")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"download_url": "/api/exports/usage?ticket=" + token, "expires_at": claims.ExpiresAt.Time.Format(time.RFC3339)})
}

func (s *Server) handleUsageCSVTicketDownload(w http.ResponseWriter, r *http.Request) {
	claims := &usageExportClaims{}
	token, err := jwt.ParseWithClaims(r.URL.Query().Get("ticket"), claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(s.jwtSecret), nil
	}, jwt.WithIssuer("pccp-usage-export"))
	if err != nil || !token.Valid || claims.Purpose != "usage_csv" || claims.OrganizationID == "" {
		writeError(w, http.StatusUnauthorized, "내보내기 티켓이 올바르지 않거나 만료되었습니다")
		return
	}
	windowStart, startErr := time.Parse(time.RFC3339, claims.WindowStart)
	windowEnd, endErr := time.Parse(time.RFC3339, claims.WindowEnd)
	snapshotAt, snapshotErr := time.Parse(time.RFC3339, claims.SnapshotAt)
	if startErr != nil || endErr != nil || snapshotErr != nil || !windowStart.Before(windowEnd) || snapshotAt.IsZero() {
		writeError(w, http.StatusUnauthorized, "내보내기 티켓의 범위가 올바르지 않습니다")
		return
	}
	s.streamUsageCSV(w, claims.OrganizationID, usageFilter{
		Context: r.Context(), SnapshotAt: snapshotAt, UserID: claims.UserID, SessionID: claims.SessionID, ProjectID: claims.ProjectID,
	}, windowStart, windowEnd)
}

func csvSafe(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}

// --- 17: audit ---

// handleListAuditEventsExtended adds server-side filters + pagination.
func (s *Server) handleListAuditEventsExtended(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.AuditEvent{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	if v := r.URL.Query().Get("search"); v != "" {
		q = q.Where("event_type LIKE ? OR action LIKE ? OR resource_id LIKE ? OR details LIKE ?",
			"%"+v+"%", "%"+v+"%", "%"+v+"%", "%"+v+"%")
	}
	for key, col := range map[string]string{
		"type": "event_type", "resource": "resource_type",
		"result": "result", "action": "action",
	} {
		if v := r.URL.Query().Get(key); v != "" {
			q = q.Where(col+" = ?", v)
		}
	}
	// PAT-1503: actor filters by actor_id OR actor_type (shared with the UI).
	if v := r.URL.Query().Get("actor"); v != "" {
		q = q.Where("actor_id = ? OR actor_type = ?", v, v)
	}
	// PAT-1503: canonical category filter (same taxonomy as the Audit UI).
	if v := r.URL.Query().Get("category"); v != "" {
		like := auditCategoryLike(v)
		if len(like) > 0 {
			or := make([]string, len(like))
			args := make([]interface{}, len(like))
			for i, p := range like {
				or[i] = "event_type LIKE ?"
				args[i] = p + ".%"
			}
			q = q.Where("("+strings.Join(or, " OR ")+")", args...)
		} else {
			q = q.Where("event_type = ?", v) // exact unknown prefix still matches
		}
	}
	// PAT-1503: integrity/hold facet.
	switch r.URL.Query().Get("integrity") {
	case "hold":
		q = q.Where("legal_hold = ?", true)
	case "degraded":
		q = q.Where("event_digest = '' OR event_digest IS NULL")
	case "verified":
		q = q.Where("event_digest != '' AND event_digest IS NOT NULL")
	}
	if v := r.URL.Query().Get("from"); v != "" {
		q = q.Where("created_at >= ?", v)
	}
	if v := r.URL.Query().Get("to"); v != "" {
		q = q.Where("created_at <= ?", v)
	}
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		page, _ := strconv.Atoi(pageStr)
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		if size <= 0 {
			size = 50
		}
		if page <= 0 {
			page = 1
		}
		var total int64
		q.Count(&total)
		var events []models.AuditEvent
		q.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&events)
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": events, "total": total, "page": page, "size": size})
		return
	}
	var events []models.AuditEvent
	q.Order("created_at DESC").Limit(500).Find(&events)
	writeJSON(w, http.StatusOK, events)
}

// handleAuditHolds manages legal holds (web/17 C).
func (s *Server) handleAuditHolds(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	switch r.Method {
	case http.MethodGet:
		var holds []models.LegalHold
		s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Find(&holds)
		writeJSON(w, http.StatusOK, holds)
	case http.MethodPost:
		var req struct {
			ResourceType string `json:"resource_type"`
			ResourceID   string `json:"resource_id"`
			Reason       string `json:"reason"`
		}
		if err := decodeJSON(r, &req); err != nil || req.ResourceType == "" || req.ResourceID == "" {
			writeError(w, http.StatusBadRequest, "resource_type + resource_id + reason required")
			return
		}
		hold := models.LegalHold{
			Base:           models.Base{ID: models.GenerateID("hold")},
			OrganizationID: orgID,
			ResourceType:   req.ResourceType,
			ResourceID:     req.ResourceID,
			Reason:         req.Reason,
			PlacedBy:       getOperatorEmail(r),
			Status:         "active",
		}
		if err := s.db.Create(&hold).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.db.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.audit.hold_placed", ActorType: "admin",
			Action: "place_legal_hold", ResourceType: req.ResourceType, ResourceID: req.ResourceID,
			Details: fmt.Sprintf(`{"reason":"%s","hold_id":"%s"}`, req.Reason, hold.ID),
			Result:  "success", OccurredAt: time.Now().Format(time.RFC3339),
		})
		writeJSON(w, http.StatusCreated, hold)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleAuditHoldItem lifts a legal hold.
func (s *Server) handleAuditHoldItem(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	var req struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(r, &req)
	var hold models.LegalHold
	if err := s.db.First(&hold, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "hold not found")
		return
	}
	s.db.Model(&hold).Updates(map[string]interface{}{
		"status": "lifted", "lifted_at": time.Now().Format(time.RFC3339),
	})
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID, EventType: "cp.audit.hold_lifted", ActorType: "admin",
		Action: "lift_legal_hold", ResourceType: hold.ResourceType, ResourceID: hold.ResourceID,
		Details: fmt.Sprintf(`{"reason":"%s","hold_id":"%s"}`, req.Reason, id),
		Result:  "success", OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "lifted"})
}

// handleAuditEvidenceBundle assembles selected events into a
// downloadable evidence package (web/17 E).
func (s *Server) handleAuditEvidenceBundle(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids[] required")
		return
	}
	var events []models.AuditEvent
	s.db.Where("organization_id = ? AND id IN (?)", orgID, req.IDs).Find(&events)
	bundle := map[string]interface{}{
		"collected_at": time.Now().Format(time.RFC3339),
		"org_id":       orgID,
		"events":       events,
		"count":        len(events),
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=audit-evidence-bundle.json")
	writeJSON(w, http.StatusOK, bundle)
}

// forwardAuditToSIEM pushes unseen audit events to the org's configured
// SIEM webhook (web/17 E). Returns the number forwarded.
func (s *Server) forwardAuditToSIEM(orgID string) int {
	var webhook models.OrgSetting
	if err := s.db.Where("organization_id = ? AND key = ?", orgID, "audit.siem_webhook").
		First(&webhook).Error; err != nil || webhook.Value == "" {
		return 0
	}
	var secret models.OrgSetting
	s.db.Where("organization_id = ? AND key = ?", orgID, "audit.siem_secret").First(&secret)
	var cursor models.OrgSetting
	s.db.Where("organization_id = ? AND key = ?", orgID, "audit.siem_cursor").First(&cursor)
	// Cursor over the per-org monotonic ChainSeq — NOT the random UUID
	// id, which has no ordering and would silently skip events.
	lastSeq := int64(0)
	if cursor.Value != "" {
		lastSeq, _ = strconv.ParseInt(cursor.Value, 10, 64)
	}
	var events []models.AuditEvent
	s.db.Where("organization_id = ? AND chain_seq > ?", orgID, lastSeq).
		Order("chain_seq ASC").Limit(100).Find(&events)
	if len(events) == 0 {
		return 0
	}
	payload, _ := json.Marshal(map[string]interface{}{"source": "pccp", "events": events})
	mac := hmac.New(sha256.New, []byte(secret.Value))
	mac.Write(payload)
	req, err := http.NewRequest(http.MethodPost, webhook.Value, bytes.NewReader(payload))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PCCP-Signature", hex.EncodeToString(mac.Sum(nil)))
	// Bounded timeout: a hung SIEM endpoint must never stall the
	// server's background ticker (which also runs sweeps).
	siemClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := siemClient.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0
	}
	newCursor := strconv.FormatInt(events[len(events)-1].ChainSeq, 10)
	if cursor.ID != "" {
		s.db.Model(&cursor).Update("value", newCursor)
	} else {
		s.db.Create(&models.OrgSetting{
			Base:           models.Base{ID: models.GenerateID("os")},
			OrganizationID: orgID, Key: "audit.siem_cursor", Value: newCursor,
		})
	}
	return len(events)
}

// handleAuditSIEMConfig gets/sets the SIEM forwarding config.
func (s *Server) handleAuditSIEMConfig(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	switch r.Method {
	case http.MethodGet:
		var webhook, secret models.OrgSetting
		s.db.Where("organization_id = ? AND key = ?", orgID, "audit.siem_webhook").First(&webhook)
		s.db.Where("organization_id = ? AND key = ?", orgID, "audit.siem_secret").First(&secret)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"webhook": webhook.Value, "configured": webhook.Value != "",
			"secret_set": secret.Value != "",
		})
	case http.MethodPut:
		var req struct {
			Webhook string `json:"webhook"`
			Secret  string `json:"secret"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		upsertOrgSetting(s.db, orgID, "audit.siem_webhook", req.Webhook)
		if req.Secret != "" {
			upsertOrgSetting(s.db, orgID, "audit.siem_secret", req.Secret)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "configured"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func upsertOrgSetting(db *gorm.DB, orgID, key, value string) {
	var existing models.OrgSetting
	if err := db.Where("organization_id = ? AND key = ?", orgID, key).First(&existing).Error; err != nil {
		db.Create(&models.OrgSetting{Base: models.Base{ID: models.GenerateID("os")}, OrganizationID: orgID, Key: key, Value: value})
	} else {
		db.Model(&existing).Update("value", value)
	}
}

// --- 18: model infra ---

// handlePublishModelVerified verifies the package signature + manifest
// digest before publishing (web/18 A).
func (s *Server) handlePublishModelVerified(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var pkg models.ModelPackage
	if err := s.db.Where("id = ? OR package_id = ?", id, id).First(&pkg).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.registry.VerifyPackageIntegrity(&pkg); err != nil {
		writeError(w, http.StatusForbidden, "모델 패키지 검증 실패 · "+err.Error())
		return
	}
	_ = fmt.Sprintf // keep fmt
	if err := s.registry.PublishModelPackage(pkg.PackageID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.model.published", ActorType: "admin",
		Action: "publish_model", ResourceType: "model_package", ResourceID: id,
		Details: fmt.Sprintf(`{"verified":true,"manifest_digest":"%s"}`, pkg.ManifestDigest),
		Result:  "success", OccurredAt: time.Now().Format(time.RFC3339),
	})
	if s.modelPublishedHook != nil {
		go s.modelPublishedHook(pkg.PackageID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published", "verified": "true"})
}

// handleModelRecallImpact enumerates what a recall would affect (18 C).
func (s *Server) handleModelRecallImpact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var pkg models.ModelPackage
	if err := s.db.Where("id = ? OR package_id = ?", id, id).First(&pkg).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var endpoints int64
	s.db.Model(&models.InferenceEndpoint{}).Where("model_package_id = ?", pkg.PackageID).Count(&endpoints)
	var sessions int64
	s.db.Model(&models.Session{}).Where("model_class = ? OR model_class = ?", pkg.ModelID, pkg.PackageID).Count(&sessions)
	var usage int64
	s.db.Model(&models.UsageRecord{}).Where("model_package_id = ?", pkg.PackageID).Count(&usage)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"package_id": pkg.PackageID, "affected_endpoints": endpoints,
		"affected_sessions": sessions, "usage_records": usage,
		"state": pkg.State,
	})
}

// handleModelRingAssign sets the release ring (18 D).
func (s *Server) handleModelRingAssign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		ReleaseRing string `json:"release_ring"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ReleaseRing == "" {
		writeError(w, http.StatusBadRequest, "release_ring required")
		return
	}
	switch req.ReleaseRing {
	case "stable", "beta", "canary":
	default:
		writeError(w, http.StatusBadRequest, "release_ring must be stable|beta|canary")
		return
	}
	var pkg models.ModelPackage
	if err := s.db.Where("id = ? OR package_id = ?", id, id).First(&pkg).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.db.Model(&pkg).Update("release", req.ReleaseRing)
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.model.ring_assigned", ActorType: "admin",
		Action: "assign_ring", ResourceType: "model_package", ResourceID: id,
		Details: req.ReleaseRing, Result: "success", OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ring_assigned", "release": req.ReleaseRing})
}

// --- 19: code explorer ---

// handleCodeExplorerSpans lists provenance spans with server filters.
func (s *Server) handleCodeExplorerSpans(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.ProvenanceSpan{}).Where("organization_id = ?", orgID)
	if v := r.URL.Query().Get("repository"); v != "" {
		q = q.Where("repository_id = ?", v)
	}
	if v := r.URL.Query().Get("file"); v != "" {
		q = q.Where("file_path LIKE ?", "%"+v+"%")
	}
	if v := r.URL.Query().Get("attribution"); v != "" {
		q = q.Where("attribution_state = ?", v)
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if page > 0 && size > 0 {
		var total int64
		q.Count(&total)
		var spans []models.ProvenanceSpan
		q.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&spans)
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": spans, "total": total, "page": page, "size": size})
		return
	}
	var spans []models.ProvenanceSpan
	q.Order("created_at DESC").Limit(200).Find(&spans)
	writeJSON(w, http.StatusOK, spans)
}

// handleCodeExplorerAttribution aggregates spans into per-file
// attribution (web/19 B): AI_GENERATED, AI_THEN_HUMAN_EDITED,
// HUMAN_THEN_AI_ASSISTED, HUMAN_WRITTEN.
func (s *Server) handleCodeExplorerAttribution(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	repoID := r.URL.Query().Get("repository")
	q := s.db.Model(&models.ProvenanceSpan{}).Where("organization_id = ?", orgID)
	if repoID != "" {
		q = q.Where("repository_id = ?", repoID)
	}
	var spans []models.ProvenanceSpan
	// Chronological order matters: the AI→human vs human→AI state
	// machine below consumes spans in time order per file.
	q.Order("created_at ASC").Find(&spans)
	type fileAttr struct {
		FilePath   string  `json:"file_path"`
		State      string  `json:"state"`
		Confidence float64 `json:"confidence"`
		SpanCount  int     `json:"span_count"`
	}
	byFile := map[string]*fileAttr{}
	for _, sp := range spans {
		attr, ok := byFile[sp.FilePath]
		if !ok {
			attr = &fileAttr{FilePath: sp.FilePath, State: sp.AttributionState}
			byFile[sp.FilePath] = attr
		}
		attr.SpanCount++
		attr.Confidence += sp.Confidence
		switch sp.AttributionState {
		case "HUMAN_WRITTEN":
			// human edit after AI → AI_THEN_HUMAN_EDITED unless fully human
			if attr.State == "AI_GENERATED" || attr.State == "AI_THEN_HUMAN_EDITED" {
				attr.State = "AI_THEN_HUMAN_EDITED"
			} else if attr.State != "HUMAN_THEN_AI_ASSISTED" {
				attr.State = "HUMAN_WRITTEN"
			}
		case "AI_GENERATED":
			if attr.State == "HUMAN_WRITTEN" || attr.State == "HUMAN_THEN_AI_ASSISTED" {
				attr.State = "HUMAN_THEN_AI_ASSISTED"
			}
		}
	}
	out := make([]fileAttr, 0, len(byFile))
	for _, attr := range byFile {
		if attr.SpanCount > 0 {
			attr.Confidence = attr.Confidence / float64(attr.SpanCount)
		}
		out = append(out, *attr)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCodeExplorerBlast runs the change-impact analysis for a file
// (web/19 D).
func (s *Server) handleCodeExplorerBlast(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	repoID := r.URL.Query().Get("repository")
	filePath := r.URL.Query().Get("file")
	if repoID == "" || filePath == "" {
		writeError(w, http.StatusBadRequest, "repository + file required")
		return
	}
	// Derive touched symbols from the spans on this file.
	var spans []models.ProvenanceSpan
	s.db.Where("organization_id = ? AND repository_id = ? AND file_path = ?", orgID, repoID, filePath).
		Find(&spans)
	symbols := []string{}
	lang := ""
	for _, sp := range spans {
		if sp.SymbolName != "" {
			symbols = append(symbols, sp.SymbolName)
		}
		if lang == "" {
			lang = sp.SymbolLang
		}
	}
	graph, score, err := s.impact.AnalyzeChange(impact.AnalyzeRequest{
		OrganizationID: orgID, RepositoryID: repoID, FilePath: filePath,
		SymbolsChanged: symbols, Languages: []string{lang},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"graph": graph, "risk_score": score, "span_count": len(spans),
	})
}

var _ = (*registry.Service).VerifyPackageIntegrity
