package api

// Public terminal ads (PAT-1435) — PCCP half.
//
// Locked rules enforced here:
//   - Platform-operator boundary: only super_admin (patty_ops) may
//     mutate campaigns — ordinary tenant admins/owners cannot.
//   - Integer-safe accounting: spend = impressions × CPM / 1000 in
//     minor units, computed in a single transactional UPDATE whose
//     WHERE clause enforces active-window/ceiling/budget — concurrent
//     events can neither exceed the ceiling nor overspend.
//   - Measurement is anonymous and at-most-once: events carry no
//     identity fields; unique event_id dedups retries; campaigns and
//     creative revisions are validated against the live state.
//   - Catalog is versioned, bounded (≤50 campaigns), field-limited,
//     and ed25519-signed with expiry; only currently eligible
//     campaigns are included.
//   - Bilingual creative: English required; Korean optional. Locale
//     affects translation selection only — never eligibility/weight.

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/patrickrho-patty/pccp/internal/keys"
	"github.com/patrickrho-patty/pccp/internal/models"
)

const (
	adMaxCampaigns = 50
	adCatalogTTL   = 1 * time.Hour
)

// adOperatorOnly is the Patty platform-operator gate — deliberately
// stricter than enterpriseRoleAdmin: tenant admins/owners are excluded.
func adOperatorOnly(r *http.Request) bool {
	return getRole(r) == "super_admin"
}

// adEligibleAt checks lifecycle + schedule + accounting headroom
// (read-side; the authoritative check is the transactional UPDATE).
func adEligibleAt(c *models.AdCampaign, now time.Time) bool {
	if c.State != "active" {
		return false
	}
	if c.StartAt != "" {
		if st, err := time.Parse(time.RFC3339, c.StartAt); err == nil && now.Before(st) {
			return false
		}
	}
	if c.EndAt != "" {
		if et, err := time.Parse(time.RFC3339, c.EndAt); err == nil && now.After(et) {
			return false
		}
	}
	if c.ImpressionCeiling > 0 && c.ValidatedImpressions >= int64(c.ImpressionCeiling) {
		return false
	}
	if adSpendMinor(c.ValidatedImpressions, c.CpmMinor) >= c.BudgetMinor {
		return false
	}
	return true
}

// adSpendMinor is THE integer-safe CPM computation: floor(impressions ×
// CPM / 1000) in minor units. Single accounting path for all callers.
func adSpendMinor(impressions, cpmMinor int64) int64 {
	if impressions <= 0 || cpmMinor <= 0 {
		return 0
	}
	return impressions * cpmMinor / 1000
}

// adExpectedImpressions = min(ceiling, budget-derived capacity).
func adExpectedImpressions(ceiling int, budgetMinor, cpmMinor int64) int64 {
	if cpmMinor <= 0 {
		return 0
	}
	capacity := budgetMinor * 1000 / cpmMinor
	if ceiling > 0 && int64(ceiling) < capacity {
		return int64(ceiling)
	}
	return capacity
}

// adNormalizeRFC3339 parses and re-stores timestamps in UTC Z-form so
// lexicographic SQL comparisons in the transactional accounting gate
// are sound (PAT-1435 window correctness).
func adNormalizeRFC3339(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return "", fmt.Errorf("시각은 RFC3339 형식이어야 합니다: %q", v)
	}
	return t.UTC().Format(time.RFC3339), nil
}

// adFieldLimits mirror the model column limits exactly.
const (
	adHeadlineMax = 120
	adBodyMax     = 200
)

func adValidateCreative(headline, body, destURL string) error {
	if strings.TrimSpace(headline) == "" || strings.TrimSpace(body) == "" {
		return fmt.Errorf("영어 헤드라인과 본문은 필수입니다")
	}
	if len(headline) > adHeadlineMax || len(body) > adBodyMax {
		return fmt.Errorf("헤드라인은 120자, 본문은 200자 이내여야 합니다")
	}
	u, err := url.Parse(destURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("대상 URL은 https여야 합니다")
	}
	return nil
}

func adNormalizeDomain(destURL string) string {
	if u, err := url.Parse(destURL); err == nil && u.Host != "" {
		return strings.ToLower(u.Host)
	}
	return ""
}

// ---------- operator campaign CRUD ----------

// apiSignPayload signs raw bytes with the named service key — the one
// shared sign-and-encode path for snapshots/policies/catalogs.
func apiSignPayload(db *gorm.DB, service string, raw []byte) (string, error) {
	priv, err := keys.LoadOrCreate(db, service)
	if err != nil {
		return "", fmt.Errorf("서명 키를 사용할 수 없습니다")
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, raw)), nil
}

func (s *Server) handleADCampaignCreate(w http.ResponseWriter, r *http.Request) {
	if !adOperatorOnly(r) {
		writeError(w, http.StatusForbidden, "광고 캠페인 관리는 Patty 플랫폼 운영자 전용입니다")
		return
	}
	// Whitelisted DTO: accounting fields (impressions/clicks/spend) and
	// audit fields are never client-settable.
	var req struct {
		Advertiser        string `json:"advertiser"`
		Category          string `json:"category"`
		HeadlineEn        string `json:"headline_en"`
		BodyEn            string `json:"body_en"`
		HeadlineKo        string `json:"headline_ko"`
		BodyKo            string `json:"body_ko"`
		DestinationURL    string `json:"destination_url"`
		StartAt           string `json:"start_at"`
		EndAt             string `json:"end_at"`
		Weight            int    `json:"weight"`
		ImpressionCeiling int    `json:"impression_ceiling"`
		CpmMinor          int64  `json:"cpm_minor"`
		BudgetMinor       int64  `json:"budget_minor"`
		Currency          string `json:"currency"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := adValidateCreative(req.HeadlineEn, req.BodyEn, req.DestinationURL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.HeadlineKo) > adHeadlineMax || len(req.BodyKo) > adBodyMax {
		writeError(w, http.StatusBadRequest, "한국어 헤드라인은 120자, 본문은 200자 이내여야 합니다")
		return
	}
	startAt, err := adNormalizeRFC3339(req.StartAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	endAt, err := adNormalizeRFC3339(req.EndAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Advertiser == "" {
		writeError(w, http.StatusBadRequest, "광고주 표시명이 필요합니다")
		return
	}
	if req.Weight < 1 {
		writeError(w, http.StatusBadRequest, "표시 가중치는 양의 정수여야 합니다")
		return
	}
	if req.CpmMinor <= 0 || req.BudgetMinor <= 0 {
		writeError(w, http.StatusBadRequest, "CPM과 예산은 양의 정수(최소 통화 단위)여야 합니다")
		return
	}
	if req.Currency == "" {
		req.Currency = "KRW"
	}
	if req.ImpressionCeiling < 0 {
		req.ImpressionCeiling = 0
	}
	c := models.AdCampaign{
		Advertiser: req.Advertiser, Category: req.Category, State: "draft",
		HeadlineEn: req.HeadlineEn, BodyEn: req.BodyEn,
		HeadlineKo: req.HeadlineKo, BodyKo: req.BodyKo,
		DestinationURL: req.DestinationURL, DisplayDomain: adNormalizeDomain(req.DestinationURL),
		CreativeRevision: 1, StartAt: startAt, EndAt: endAt,
		Weight: req.Weight, ImpressionCeiling: req.ImpressionCeiling,
		CpmMinor: req.CpmMinor, BudgetMinor: req.BudgetMinor, Currency: req.Currency,
		CreatedBy: getOperatorEmail(r),
	}
	if err := s.db.Create(&c).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s.adCampaignView(&c))
}

func (s *Server) handleADCampaignUpdate(w http.ResponseWriter, r *http.Request) {
	if !adOperatorOnly(r) {
		writeError(w, http.StatusForbidden, "광고 캠페인 관리는 Patty 플랫폼 운영자 전용입니다")
		return
	}
	id := chi.URLParam(r, "id")
	var c models.AdCampaign
	if err := s.db.Where("id = ?", id).First(&c).Error; err != nil {
		writeError(w, http.StatusNotFound, "캠페인을 찾을 수 없습니다")
		return
	}
	var req struct {
		HeadlineEn        *string `json:"headline_en"`
		BodyEn            *string `json:"body_en"`
		HeadlineKo        *string `json:"headline_ko"`
		BodyKo            *string `json:"body_ko"`
		DestinationURL    *string `json:"destination_url"`
		Weight            *int    `json:"weight"`
		ImpressionCeiling *int    `json:"impression_ceiling"`
		CpmMinor          *int64  `json:"cpm_minor"`
		BudgetMinor       *int64  `json:"budget_minor"`
		StartAt           *string `json:"start_at"`
		EndAt             *string `json:"end_at"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updates := map[string]interface{}{}
	headline, body, dest := c.HeadlineEn, c.BodyEn, c.DestinationURL
	if req.HeadlineEn != nil {
		headline = *req.HeadlineEn
	}
	if req.BodyEn != nil {
		body = *req.BodyEn
	}
	if req.DestinationURL != nil {
		dest = *req.DestinationURL
		updates["destination_url"] = dest
		updates["display_domain"] = adNormalizeDomain(dest)
	}
	if err := adValidateCreative(headline, body, dest); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	creativeChanged := req.HeadlineEn != nil || req.BodyEn != nil ||
		req.HeadlineKo != nil || req.BodyKo != nil || req.DestinationURL != nil
	if creativeChanged {
		// Atomic increment — concurrent creative edits cannot collide on
		// the same revision (billing keys on revision identity).
		updates["creative_revision"] = gorm.Expr("creative_revision + 1")
	}
	if req.HeadlineEn != nil {
		updates["headline_en"] = *req.HeadlineEn
	}
	if req.BodyEn != nil {
		updates["body_en"] = *req.BodyEn
	}
	if req.HeadlineKo != nil {
		updates["headline_ko"] = *req.HeadlineKo
	}
	if req.BodyKo != nil {
		updates["body_ko"] = *req.BodyKo
	}
	if req.Weight != nil {
		if *req.Weight < 1 {
			writeError(w, http.StatusBadRequest, "표시 가중치는 양의 정수여야 합니다")
			return
		}
		updates["weight"] = *req.Weight
	}
	if req.ImpressionCeiling != nil {
		if *req.ImpressionCeiling < 0 {
			writeError(w, http.StatusBadRequest, "노출 상한은 0 이상이어야 합니다")
			return
		}
		updates["impression_ceiling"] = *req.ImpressionCeiling
	}
	if req.CpmMinor != nil {
		if *req.CpmMinor <= 0 {
			writeError(w, http.StatusBadRequest, "CPM은 양의 정수여야 합니다")
			return
		}
		updates["cpm_minor"] = *req.CpmMinor
	}
	if req.BudgetMinor != nil {
		if *req.BudgetMinor <= 0 {
			writeError(w, http.StatusBadRequest, "예산은 양의 정수여야 합니다")
			return
		}
		// Budget may never drop below already-spent — computed with the
		// EFFECTIVE CPM (a combined CPM-raise + budget-cut must not
		// leave spend > budget after the change).
		cpm := c.CpmMinor
		if req.CpmMinor != nil {
			cpm = *req.CpmMinor
		}
		if *req.BudgetMinor < adSpendMinor(c.ValidatedImpressions, cpm) {
			writeError(w, http.StatusUnprocessableEntity, "예산을 이미 지출된 금액 이하로 낮출 수 없습니다")
			return
		}
		updates["budget_minor"] = *req.BudgetMinor
	}
	if req.StartAt != nil {
		normalized, err := adNormalizeRFC3339(*req.StartAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		updates["start_at"] = normalized
	}
	if req.EndAt != nil {
		normalized, err := adNormalizeRFC3339(*req.EndAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		updates["end_at"] = normalized
	}
	if len(updates) == 0 {
		writeError(w, http.StatusBadRequest, "변경 내용이 없습니다")
		return
	}
	s.db.Model(&c).Updates(updates)
	var fresh models.AdCampaign
	s.db.First(&fresh, "id = ?", c.ID)
	writeJSON(w, http.StatusOK, s.adCampaignView(&fresh))
}

// handleADCampaignLifecycle activates/pauses/ends with destructive
// confirmation semantics.
func (s *Server) handleADCampaignLifecycle(w http.ResponseWriter, r *http.Request) {
	if !adOperatorOnly(r) {
		writeError(w, http.StatusForbidden, "광고 캠페인 관리는 Patty 플랫폼 운영자 전용입니다")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Action string `json:"action"` // activate|pause|end
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "작업과 사유가 필요합니다")
		return
	}
	var c models.AdCampaign
	if err := s.db.Where("id = ?", id).First(&c).Error; err != nil {
		writeError(w, http.StatusNotFound, "캠페인을 찾을 수 없습니다")
		return
	}
	next := ""
	switch req.Action {
	case "activate":
		if adSpendMinor(c.ValidatedImpressions, c.CpmMinor) >= c.BudgetMinor {
			writeError(w, http.StatusUnprocessableEntity, "예산이 소진되어 활성화할 수 없습니다")
			return
		}
		next = "active"
	case "pause":
		next = "paused"
	case "end":
		next = "ended"
	default:
		writeError(w, http.StatusBadRequest, "알 수 없는 작업입니다")
		return
	}
	s.db.Model(&c).Update("state", next)
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.ads.campaign_lifecycle",
		ActorType: "admin", Action: req.Action + "_campaign", ResourceType: "ad_campaign",
		ResourceID: fmt.Sprint(c.ID), Details: fmt.Sprintf(`{"reason":%q}`, req.Reason),
		Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	var fresh models.AdCampaign
	s.db.First(&fresh, "id = ?", c.ID)
	writeJSON(w, http.StatusOK, s.adCampaignView(&fresh))
}

func (s *Server) handleADCampaignsList(w http.ResponseWriter, r *http.Request) {
	if !adOperatorOnly(r) {
		writeError(w, http.StatusForbidden, "광고 캠페인 조회는 Patty 플랫폼 운영자 전용입니다")
		return
	}
	var campaigns []models.AdCampaign
	s.db.Order("created_at DESC").Limit(200).Find(&campaigns)
	out := make([]map[string]interface{}, 0, len(campaigns))
	for i := range campaigns {
		out = append(out, s.adCampaignView(&campaigns[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// adCampaignView computes the derived accounting fields with the one
// integer-safe accounting path.
func (s *Server) adCampaignView(c *models.AdCampaign) map[string]interface{} {
	spend := adSpendMinor(c.ValidatedImpressions, c.CpmMinor)
	if spend > c.BudgetMinor {
		spend = c.BudgetMinor
	}
	expected := adExpectedImpressions(c.ImpressionCeiling, c.BudgetMinor, c.CpmMinor)
	deliveryPct := 0.0
	if expected > 0 {
		deliveryPct = float64(c.ValidatedImpressions) / float64(expected) * 100
	}
	return map[string]interface{}{
		"id": c.ID, "advertiser": c.Advertiser, "category": c.Category, "state": c.State,
		"headline_en": c.HeadlineEn, "body_en": c.BodyEn, "headline_ko": c.HeadlineKo, "body_ko": c.BodyKo,
		"destination_url": c.DestinationURL, "display_domain": c.DisplayDomain,
		"creative_revision": c.CreativeRevision, "start_at": c.StartAt, "end_at": c.EndAt,
		"weight": c.Weight, "impression_ceiling": c.ImpressionCeiling,
		"cpm_minor": c.CpmMinor, "currency": c.Currency, "budget_minor": c.BudgetMinor,
		"expected_impressions":  expected,
		"validated_impressions": c.ValidatedImpressions, "clicks": c.Clicks,
		"spend_minor": spend, "remaining_budget_minor": c.BudgetMinor - spend,
		"delivery_pct": deliveryPct,
		"eligible":     adEligibleAt(c, time.Now().UTC()),
	}
}

// ---------- signed catalog (public, unauthenticated) ----------

// handleADCatalogGet serves the latest unexpired signed catalog (SSE
// headers allow simple caching; harness treats expiry as cache limit).
func (s *Server) handleADCatalogGet(w http.ResponseWriter, r *http.Request) {
	var snap models.AdCatalogSnapshot
	if err := s.db.Order("revision DESC").First(&snap).Error; err != nil {
		// No catalog published: empty (public harness shows no ad).
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"schema": "patty.ads.catalog.v1", "campaigns": []interface{}{},
		})
		return
	}
	if snap.ExpiresAt != "" {
		if exp, err := time.Parse(time.RFC3339, snap.ExpiresAt); err == nil && time.Now().UTC().After(exp) {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"schema": "patty.ads.catalog.v1", "campaigns": []interface{}{}, "expired": true,
			})
			return
		}
	}
	// Serve an envelope whose "catalog" field carries the SIGNED bytes
	// verbatim — a verifier checks the signature over exactly those
	// bytes, with no re-canonicalization round-trip.
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"catalog":` + snap.PayloadJSON + `,"signature":` + strconv.Quote(snap.Signature) + `,"key_id":` + strconv.Quote(snap.KeyID) + `}`))
}

// handleADCatalogPublish builds + signs the catalog from currently
// eligible campaigns (operator-only).
func (s *Server) handleADCatalogPublish(w http.ResponseWriter, r *http.Request) {
	if !adOperatorOnly(r) {
		writeError(w, http.StatusForbidden, "카탈로그 발행은 Patty 플랫폼 운영자 전용입니다")
		return
	}
	var campaigns []models.AdCampaign
	s.db.Where("state = ?", "active").Limit(adMaxCampaigns).Find(&campaigns)
	now := time.Now().UTC()
	entries := make([]map[string]interface{}, 0, len(campaigns))
	for i := range campaigns {
		c := &campaigns[i]
		if !adEligibleAt(c, now) {
			continue
		}
		entries = append(entries, map[string]interface{}{
			"campaign_id": c.ID, "creative_revision": c.CreativeRevision,
			"headline_en": c.HeadlineEn, "body_en": c.BodyEn,
			"headline_ko": c.HeadlineKo, "body_ko": c.BodyKo, // optional; harness falls back to EN
			"url": c.DestinationURL, "display_domain": c.DisplayDomain,
			"weight": c.Weight,
		})
		if len(entries) >= adMaxCampaigns {
			break
		}
	}
	var last models.AdCatalogSnapshot
	s.db.Order("revision DESC").First(&last)
	revision := 1
	if last.ID != 0 {
		revision = last.Revision + 1
	}
	payload := map[string]interface{}{
		"schema": "patty.ads.catalog.v1", "catalog_revision": revision,
		"issued_at":  now.Format(time.RFC3339),
		"expires_at": now.Add(adCatalogTTL).Format(time.RFC3339),
		"campaigns":  entries,
	}
	raw, _ := json.Marshal(payload)
	sig, err := apiSignPayload(s.db, "ad-catalog", raw)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	snap := models.AdCatalogSnapshot{
		Revision: revision, PayloadJSON: string(raw),
		Signature: sig,
		KeyID:     "ad-catalog", GeneratedAt: now.Format(time.RFC3339),
		ExpiresAt: now.Add(adCatalogTTL).Format(time.RFC3339),
	}
	if err := s.db.Create(&snap).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"revision": revision, "campaigns": len(entries), "expires_at": snap.ExpiresAt,
	})
}

// ---------- anonymous measurement ----------

// handleADEventIngest is the bounded anonymous path (NOT the event
// spine). Idempotent by event_id; validates campaign/revision/window.
func (s *Server) handleADEventIngest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EventID          string `json:"event_id"`
		CampaignID       uint   `json:"campaign_id"`
		CreativeRevision int    `json:"creative_revision"`
		Type             string `json:"type"` // impression|click
		Timestamp        string `json:"timestamp"`
		CatalogRevision  int    `json:"catalog_revision"`
	}
	if err := decodeJSON(r, &req); err != nil || req.EventID == "" || req.CampaignID == 0 {
		writeError(w, http.StatusBadRequest, "event_id와 campaign_id가 필요합니다")
		return
	}
	if req.Type != "impression" && req.Type != "click" {
		writeError(w, http.StatusBadRequest, "type은 impression|click이어야 합니다")
		return
	}
	if len(req.EventID) > 64 {
		writeError(w, http.StatusBadRequest, "event_id가 너무 깁니다")
		return
	}
	var c models.AdCampaign
	if err := s.db.Where("id = ?", req.CampaignID).First(&c).Error; err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "unknown_campaign"})
		return
	}
	// Revision mismatch (stale/unknown creative) → not counted, and NOT
	// persisted — only COUNTED events occupy rows (bounded anonymous
	// growth; dedup applies to counted events).
	if req.CreativeRevision != c.CreativeRevision {
		writeJSON(w, http.StatusOK, map[string]string{"status": "stale_revision"})
		return
	}
	ev := models.AdMeasurementEvent{
		EventID: req.EventID, CampaignID: req.CampaignID,
		CreativeRevision: req.CreativeRevision, Type: req.Type,
		Timestamp: req.Timestamp, CatalogRevision: req.CatalogRevision,
	}
	// Idempotency: duplicate event_id → acknowledged, not counted.
	if err := s.db.Create(&ev).Error; err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}
	now := time.Now().UTC()
	switch req.Type {
	case "impression":
		// THE transactional accounting gate: eligibility (active window +
		// ceiling + budget) is enforced inside the UPDATE with an atomic
		// increment; the budget term uses the POST-increment bound so
		// floor() overshoot can never exceed the budget. Rejected events
		// are not persisted (no unbounded anonymous table growth).
		res := s.db.Model(&models.AdCampaign{}).
			Where("id = ? AND state = ? AND (start_at = '' OR start_at <= ?) AND (end_at = '' OR end_at >= ?) AND (impression_ceiling = 0 OR validated_impressions < impression_ceiling) AND ((validated_impressions + 1) * cpm_minor / 1000) <= budget_minor",
				c.ID, "active", now.Format(time.RFC3339), now.Format(time.RFC3339)).
			UpdateColumn("validated_impressions", gorm.Expr("validated_impressions + 1"))
		if res.RowsAffected == 0 {
			// Rejected by the accounting gate: remove the event row so
			// only counted events persist (a later legitimate retry of
			// the same event_id can still count if eligibility returns).
			s.db.Unscoped().Delete(&models.AdMeasurementEvent{}, ev.ID)
			writeJSON(w, http.StatusOK, map[string]string{"status": "not_billable"})
			return
		}
	case "click":
		s.db.Model(&models.AdCampaign{}).Where("id = ?", c.ID).
			UpdateColumn("clicks", gorm.Expr("clicks + 1"))
	}
	s.db.Model(&ev).Update("counted", true)
	writeJSON(w, http.StatusOK, map[string]string{"status": "counted"})
}

// handleADClickRedirect forwards to the reviewed destination for
// ACTIVE campaigns only, with an atomic aggregate click increment (no
// cookies/identity params). Inactive/unknown campaigns 404.
func (s *Server) handleADClickRedirect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res := s.db.Model(&models.AdCampaign{}).
		Where("id = ? AND state = ?", id, "active").
		UpdateColumn("clicks", gorm.Expr("clicks + 1"))
	if res.RowsAffected == 0 {
		writeError(w, http.StatusNotFound, "캠페인을 찾을 수 없습니다")
		return
	}
	var c models.AdCampaign
	if err := s.db.Where("id = ?", id).First(&c).Error; err != nil {
		writeError(w, http.StatusNotFound, "캠페인을 찾을 수 없습니다")
		return
	}
	http.Redirect(w, r, c.DestinationURL, http.StatusFound)
}
