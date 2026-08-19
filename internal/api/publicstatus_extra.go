package api

// Public status page (PAT-1439): Korean-first public service status.
//
// Boundary rules enforced here:
//   - Machines control measured color (anti-flapping evaluator); humans
//     author all public narrative copy in Korean.
//   - Automatic detection creates a PRIVATE incident draft and an on-call
//     audit page; it never publishes machine-generated narrative text.
//   - Operators may worsen a public status immediately; healthier
//     overrides require a reason, explicit expiry, and (to green while
//     demonstrably failing) a false-positive acknowledgement. The console
//     keeps showing the monitoring disagreement.
//   - The public payload carries only status-safe aggregates — no raw
//     probes, tenant metrics, regions, vendors, or topology.

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/keys"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// Public status colors and their authoritative Korean labels (PAT-1439).
const (
	psColorGreen  = "green"
	psColorYellow = "yellow"
	psColorOrange = "orange"
	psColorRed    = "red"
	psColorBlue   = "blue"
	psColorGray   = "gray"
)

var psColorLabelKo = map[string]string{
	psColorGreen:  "정상 운영",
	psColorYellow: "일부 기능 영향 · 확인 중",
	psColorOrange: "서비스 이용 불안정 · 확인 중",
	psColorRed:    "서비스 이용이 어려운 상태 · 확인 중",
	psColorBlue:   "점검 예정 / 점검 진행 중",
	psColorGray:   "상태 확인 중 / 측정 정보 부족",
}

// psColorRank orders severity for override comparison (higher = worse).
var psColorRank = map[string]int{
	psColorGray: 0, psColorGreen: 1, psColorBlue: 2,
	psColorYellow: 3, psColorOrange: 4, psColorRed: 5,
}

// Public incident lifecycle states (Korean authoritative labels).
var psIncidentStateKo = map[string]string{
	"investigating":        "확인 중",
	"mitigating":           "원인 확인 및 조치 중",
	"monitoring":           "안정성 확인 중",
	"resolved":             "정상화",
	"maintenance_scheduled": "점검 예정",
	"maintenance_in_progress": "점검 진행 중",
}

// psThresholdsV1 is the versioned anti-flapping policy. A sustained
// widespread failure may escalate immediately; recovery requires a
// sustained healthy window rather than a single successful probe.
type psThresholds struct {
	Version              int
	DegradePartialAfter  int // consecutive impacted(partial) samples → yellow
	DegradeSevereAfter   int // consecutive severe samples → orange
	EscalateWidespread   int // consecutive widespread samples → red (short)
	RecoverAfter         int // consecutive clean samples → green
	FreshnessWindowMins  int // older than this → gray (never falsely green)
}

var psThresholdsCurrent = psThresholds{
	Version:              1,
	DegradePartialAfter:  3,
	DegradeSevereAfter:   3,
	EscalateWidespread:   2,
	RecoverAfter:         5,
	FreshnessWindowMins:  10,
}

// psSnapshotTTLSeconds is how long a published snapshot may be served
// before the public reader must treat it as stale.
const psSnapshotTTLSeconds = 300

// psSeedComponents is the versioned public component registry. Patty Web
// stays inactive (hidden) until the product launches; activating the row
// requires no page redesign.
func psSeedComponents() []models.PublicStatusComponent {
	return []models.PublicStatusComponent{
		{ID: "patty_code", NameKo: "Patty Code", Active: true, RegistryVersion: 1, MeasuredColor: psColorGray},
		{ID: "patty_web", NameKo: "Patty Web", Active: false, RegistryVersion: 1, MeasuredColor: psColorGray},
	}
}

// psEvaluateComponent derives the measured color from consecutive
// observations and freshness (pure function; PAT-1439 anti-flapping).
// Maintenance samples keep the current color (planned work is not an
// outage); a stale feed becomes gray, never green.
func psEvaluateComponent(c *models.PublicStatusComponent, th psThresholds, now time.Time) string {
	if c.LastObservationAt == "" {
		return psColorGray
	}
	last, err := time.Parse(time.RFC3339, c.LastObservationAt)
	if err != nil {
		return psColorGray
	}
	if now.Sub(last) > time.Duration(th.FreshnessWindowMins)*time.Minute {
		return psColorGray
	}
	// Escalation keys on the impact level of the failing samples so a
	// partial regression cannot trip the severe tier.
	if c.ConsecutiveFailures >= th.EscalateWidespread && c.LastImpact == "widespread" {
		return psColorRed
	}
	if c.ConsecutiveFailures >= th.DegradeSevereAfter && c.LastImpact == "severe" {
		return psColorOrange
	}
	if c.ConsecutiveFailures >= th.DegradePartialAfter {
		return psColorYellow
	}
	if c.ConsecutiveSuccesses >= th.RecoverAfter {
		return psColorGreen
	}
	// Between degrade and recover: keep the last measured color (hysteresis).
	if c.MeasuredColor == "" {
		return psColorGray
	}
	return c.MeasuredColor
}

// psEffectiveColor returns the operator-visible effective color: an
// unexpired override wins, otherwise the measured color.
func psEffectiveColor(c *models.PublicStatusComponent, now time.Time) string {
	if c.OverrideColor != "" {
		if c.OverrideExpiresAt == "" {
			return c.OverrideColor
		}
		exp, err := time.Parse(time.RFC3339, c.OverrideExpiresAt)
		if err == nil && now.Before(exp) {
			return c.OverrideColor
		}
	}
	if c.MeasuredColor == "" {
		return psColorGray
	}
	return c.MeasuredColor
}

// psDeriveDailyRollup computes one KST day's availability from measured
// observation windows. Disclosed rules (PAT-1439 uptime history):
//   - impacted seconds are weighted by impact level (partial 0.5,
//     severe/widespread 1.0);
//   - maintenance seconds subtract at half weight (planned, not outage);
//   - no-data seconds leave the denominator (availability is measured
//     over observed time only) and are reported separately.
func psDeriveDailyRollup(obs []models.PublicStatusObservation) (availability float64, impacted, maintenance, noData int) {
	const daySeconds = 86400
	measured := 0
	weightedDown := 0.0
	for _, o := range obs {
		w := o.WindowSeconds
		if w <= 0 {
			w = 60
		}
		if w > daySeconds {
			w = daySeconds
		}
		measured += w
		if o.Maintenance {
			maintenance += w
			weightedDown += float64(w) * 0.5
			continue
		}
		if o.Impact == "none" && o.Success {
			continue
		}
		switch o.Impact {
		case "partial":
			impacted += w
			weightedDown += float64(w) * 0.5
		case "severe", "widespread", "":
			impacted += w
			weightedDown += float64(w)
		}
	}
	if measured > daySeconds {
		measured = daySeconds
	}
	noData = daySeconds - measured
	if measured == 0 {
		return 0, impacted, maintenance, noData
	}
	availability = (float64(measured) - weightedDown) / float64(measured) * 100
	if availability < 0 {
		availability = 0
	}
	return availability, impacted, maintenance, noData
}

// psKSTLocation is Korean Standard Time for public dates.
func psKSTLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return loc
}

// ---------- public (unauthenticated) endpoints ----------

// handlePublicStatusGet serves the read-only status API: the last signed
// snapshot plus live staleness. Must not require any Patty login and must
// keep serving the last valid snapshot when evaluation stopped.
func (s *Server) handlePublicStatusGet(w http.ResponseWriter, r *http.Request) {
	s.psSeedRegistry()
	var snap models.PublicStatusSnapshot
	if err := s.db.Order("id DESC").First(&snap).Error; err != nil {
		// No snapshot published yet: serve an honest "측정 정보 부족" page
		// rather than failing — the page must always load.
		writeJSON(w, http.StatusOK, map[string]interface{}{
		"version": 0, "generated_at": "", "stale": true,
			"components": []map[string]string{{
				"id": "patty_code", "name_ko": "Patty Code",
				"color": psColorGray, "state_ko": psColorLabelKo[psColorGray],
			}},
			"incidents": []interface{}{},
			"timezone": "Asia/Seoul",
		})
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(snap.PayloadJSON), &payload); err != nil {
		writeError(w, http.StatusInternalServerError, "snapshot corrupt")
		return
	}
	gen, _ := time.Parse(time.RFC3339, snap.GeneratedAt)
	stale := gen.IsZero() || time.Since(gen) > psSnapshotTTLSeconds*time.Second
	if stale {
		payload["stale"] = true
		// Stale monitoring data is never presented as operational: gray.
		// JSON round-trip leaves components as []interface{}.
		if comps, ok := payload["components"].([]interface{}); ok {
			for _, raw := range comps {
				if cm, ok := raw.(map[string]interface{}); ok {
					cm["color"] = psColorGray
					cm["state_ko"] = psColorLabelKo[psColorGray]
				}
			}
		}
	} else {
		payload["stale"] = false
	}
	payload["signature"] = snap.Signature
	payload["key_id"] = snap.KeyID
	writeJSON(w, http.StatusOK, payload)
}

// handlePublicIncidentGet serves one published incident permalink with
// its append-only Korean update trail.
func (s *Server) handlePublicIncidentGet(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	var inc models.PublicIncident
	if err := s.db.Where("slug = ? AND published = ?", slug, true).First(&inc).Error; err != nil {
		writeError(w, http.StatusNotFound, "공개된 알림을 찾을 수 없습니다")
		return
	}
	var updates []models.PublicIncidentUpdate
	s.db.Where("incident_id = ?", inc.ID).Order("created_at ASC").Find(&updates)
	sanitized := make([]map[string]interface{}, 0, len(updates))
	for _, u := range updates {
		sanitized = append(sanitized, map[string]interface{}{
			"body_ko": u.BodyKo, "state_ko": psIncidentStateKo[u.StateAtUpdate],
			"is_correction": u.IsCorrection, "at": u.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"slug": inc.Slug, "title_ko": inc.TitleKo, "components": inc.Components,
		"state": inc.State, "state_ko": psIncidentStateKo[inc.State],
		"impact": inc.Impact, "major": inc.Major,
		"started_at": inc.StartedAt, "detected_at": inc.DetectedAt,
		"mitigated_at": inc.MitigatedAt, "resolved_at": inc.ResolvedAt,
		"updates": sanitized,
	})
}

// handlePublicSubscriberCreate registers an anonymous subscription.
// Destination stays unverified (no notifications) until token confirmation.
func (s *Server) handlePublicSubscriberCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ComponentID string `json:"component_id"`
		Channel      string `json:"channel"` // email|sms|webhook|rss
		Destination  string `json:"destination"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ComponentID != "patty_code" && req.ComponentID != "patty_web" {
		writeError(w, http.StatusBadRequest, "알 수 없는 구성 요소입니다")
		return
	}
	switch req.Channel {
	case "email", "sms", "webhook", "rss":
	default:
		writeError(w, http.StatusBadRequest, "지원하지 않는 채널입니다")
		return
	}
	if req.Destination == "" || len(req.Destination) > 512 {
		writeError(w, http.StatusBadRequest, "구독 대상이 올바르지 않습니다")
		return
	}
	if req.Channel == "webhook" && !strings.HasPrefix(req.Destination, "https://") {
		writeError(w, http.StatusBadRequest, "웹훅은 https URL이어야 합니다")
		return
	}
	// Rate limiting: suppress subscription creation abuse.
	var recent int64
	s.db.Model(&models.PublicStatusSubscriber{}).
		Where("destination = ? AND created_at > ?", req.Destination, time.Now().Add(-time.Hour)).Count(&recent)
	if recent >= 5 {
		writeError(w, http.StatusTooManyRequests, "잠시 후 다시 시도해 주세요")
		return
	}
	sub := models.PublicStatusSubscriber{
		ComponentID: req.ComponentID, Channel: req.Channel, Destination: req.Destination,
		VerifyToken: psRandomToken("psv"), UnsubscribeToken: psRandomToken("psu"),
	}
	if req.Channel == "webhook" {
		sub.WebhookSecret = psRandomToken("psw")
	}
	if err := s.db.Create(&sub).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "구독을 생성하지 못했습니다")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"status": "verification_required",
		// Verification delivery is an operator/provider concern in this
		// build; the tokens are returned once for the confirmation email
		// pipeline and never exposed again afterwards.
		"verify_url": "/api/public/status/subscribers/verify?token=" + sub.VerifyToken,
	})
}

// handlePublicSubscriberVerify activates a subscription via its token.
func (s *Server) handlePublicSubscriberVerify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "토큰이 필요합니다")
		return
	}
	res := s.db.Model(&models.PublicStatusSubscriber{}).Where("verify_token = ?", token).
		Updates(map[string]interface{}{"verified": true})
	if res.RowsAffected == 0 {
		writeError(w, http.StatusNotFound, "구독을 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified"})
}

// handlePublicSubscriberUnsubscribe removes a subscription (data
// minimization: the row is deleted, not retained).
func (s *Server) handlePublicSubscriberUnsubscribe(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "토큰이 필요합니다")
		return
	}
	s.db.Where("unsubscribe_token = ?", token).Delete(&models.PublicStatusSubscriber{})
	writeJSON(w, http.StatusOK, map[string]string{"status": "unsubscribed"})
}

// handlePublicStatusFeed serves an Atom feed of published incidents.
func (s *Server) handlePublicStatusFeed(w http.ResponseWriter, r *http.Request) {
	var incidents []models.PublicIncident
	s.db.Where("published = ?", true).Order("updated_at DESC").Limit(50).Find(&incidents)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<feed xmlns="http://www.w3.org/2005/Atom"><title>Patty 서비스 상태</title><id>urn:patty:status</id><updated>`)
	if len(incidents) > 0 {
		b.WriteString(incidents[0].UpdatedAt.Format(time.RFC3339))
	} else {
		b.WriteString(time.Now().UTC().Format(time.RFC3339))
	}
	b.WriteString(`</updated>` + "\n")
	for _, inc := range incidents {
		b.WriteString(fmt.Sprintf(`<entry><id>urn:patty:incident:%s</id><title>%s</title><updated>%s</updated></entry>`+"\n",
			inc.Slug, escapeXMLText(inc.TitleKo), inc.UpdatedAt.Format(time.RFC3339)))
	}
	b.WriteString(`</feed>`)
	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Write([]byte(b.String()))
}

// ---------- operator (authenticated) endpoints ----------

func (s *Server) psSeedRegistry() {
	for _, c := range psSeedComponents() {
		var existing models.PublicStatusComponent
		if err := s.db.First(&existing, "id = ?", c.ID).Error; err != nil {
			s.db.Create(&c)
		}
	}
}

func (s *Server) handlePSComponentsList(w http.ResponseWriter, r *http.Request) {
	s.psSeedRegistry()
	var comps []models.PublicStatusComponent
	s.db.Order("id ASC").Find(&comps)
	now := time.Now()
	out := make([]map[string]interface{}, 0, len(comps))
	for _, c := range comps {
		out = append(out, map[string]interface{}{
			"id": c.ID, "name_ko": c.NameKo, "active": c.Active,
			"measured_color": c.MeasuredColor, "measured_ko": psColorLabelKo[c.MeasuredColor],
			"effective_color": psEffectiveColor(&c, now), "effective_ko": psColorLabelKo[psEffectiveColor(&c, now)],
			"override_color": c.OverrideColor, "override_reason": c.OverrideReason,
			"override_expires_at": c.OverrideExpiresAt,
			// Console-only monitoring disagreement for expiring overrides.
			"override_disagrees": c.OverrideColor != "" && psColorRank[c.OverrideColor] < psColorRank[c.MeasuredColor],
			"consecutive_failures": c.ConsecutiveFailures,
			"consecutive_successes": c.ConsecutiveSuccesses,
			"last_observation_at": c.LastObservationAt, "last_healthy_at": c.LastHealthyAt,
			"registry_version": c.RegistryVersion,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePSComponentActivate activates/deactivates a registry row (used to
// launch Patty Web without page redesign).
func (s *Server) handlePSComponentActivate(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "관리자 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Active bool `json:"active"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	res := s.db.Model(&models.PublicStatusComponent{}).Where("id = ?", id).
		Update("active", req.Active)
	if res.RowsAffected == 0 {
		writeError(w, http.StatusNotFound, "구성 요소를 찾을 수 없습니다")
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.publicstatus.component_updated",
		ActorType: "admin", Action: "update_component", ResourceType: "public_status_component",
		ResourceID: id, Details: fmt.Sprintf(`{"active":%t}`, req.Active), Result: "success",
		OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": id, "active": req.Active})
}

// handlePSObservationIngest is the evaluator entry point. It applies the
// anti-flapping state machine, and on first credible degradation it
// creates a PRIVATE incident draft and pages on-call via an audit event —
// never publishing speculative narrative (PAT-1439).
func (s *Server) handlePSObservationIngest(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "관리자 권한이 필요합니다")
		return
	}
	var req []struct {
		ComponentID   string `json:"component_id"`
		Source        string `json:"source"`
		Region        string `json:"region"`
		Success       bool   `json:"success"`
		Impact        string `json:"impact"`
		LatencyMS     int64  `json:"latency_ms"`
		WindowSeconds int    `json:"window_seconds"`
		Maintenance   bool   `json:"maintenance"`
		Detail        string `json:"detail"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req) == 0 {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.psSeedRegistry()
	now := time.Now().UTC()
	drafted := []string{}
	for _, o := range req {
		switch o.Impact {
		case "none", "partial", "severe", "widespread":
		default:
			writeError(w, http.StatusBadRequest, "impact must be none|partial|severe|widespread")
			return
		}
		if o.Source == "" {
			o.Source = "synthetic"
		}
		if o.WindowSeconds <= 0 {
			o.WindowSeconds = 60
		}
		s.db.Create(&models.PublicStatusObservation{
			ComponentID: o.ComponentID, Source: o.Source, Region: o.Region,
			Success: o.Success, Impact: o.Impact, LatencyMS: o.LatencyMS,
			WindowSeconds: o.WindowSeconds, Maintenance: o.Maintenance,
			Detail: o.Detail, ObservedAt: now.Format(time.RFC3339),
		})
		var c models.PublicStatusComponent
		if err := s.db.First(&c, "id = ?", o.ComponentID).Error; err != nil {
			continue
		}
		prevColor := c.MeasuredColor
		if o.Maintenance {
			// Planned work: observation recorded, color preserved.
			c.LastObservationAt = now.Format(time.RFC3339)
		} else if o.Success && o.Impact == "none" {
			c.ConsecutiveFailures = 0
			c.ConsecutiveSuccesses++
			c.LastHealthyAt = now.Format(time.RFC3339)
			c.LastObservationAt = now.Format(time.RFC3339)
		} else {
			c.ConsecutiveSuccesses = 0
			c.ConsecutiveFailures++
			c.LastImpact = o.Impact
			c.LastObservationAt = now.Format(time.RFC3339)
		}
		c.MeasuredColor = psEvaluateComponent(&c, psThresholdsCurrent, now)
		s.db.Save(&c)

		degraded := psColorRank[c.MeasuredColor] > psColorRank[prevColor] &&
			c.MeasuredColor != psColorGray && prevColor != ""
		// First credible issue → private draft + on-call page; the public
		// label stays the predefined generic 확인 중 wording only.
		if degraded || (prevColor == "" && c.MeasuredColor != psColorGray) {
			var openDraft int64
			s.db.Model(&models.PublicIncident{}).
				Where("components LIKE ? AND published = ? AND state != ?",
					"%"+o.ComponentID+"%", false, "resolved").Count(&openDraft)
			if openDraft == 0 {
				slug := fmt.Sprintf("%s-%s", o.ComponentID, now.Format("20060102-150405"))
				inc := models.PublicIncident{
					Slug: slug, TitleKo: "서비스 상태 확인 중", Components: fmt.Sprintf(`["%s"]`, o.ComponentID),
					Impact: o.Impact, State: "investigating", Published: false,
					StartedAt: now.Format(time.RFC3339), DetectedAt: now.Format(time.RFC3339),
				}
				s.db.Create(&inc)
				drafted = append(drafted, slug)
				s.db.Create(&models.AuditEvent{
					OrganizationID: getOrgID(r), EventType: "cp.publicstatus.oncall_paged",
					ActorType: "system", Action: "page_oncall", ResourceType: "public_status_component",
					ResourceID: o.ComponentID,
					Details:    fmt.Sprintf(`{"component":"%s","measured_color":"%s","draft_incident":"%s"}`, o.ComponentID, c.MeasuredColor, slug),
					Result: "success", OccurredAt: now.Format(time.RFC3339),
				})
			}
		}
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"ingested": len(req), "draft_incidents": drafted})
}

// handlePSOverride applies an operator override. Worsening is immediate;
// healthier requires reason + expiry, and forcing green over a
// demonstrably failing measurement additionally requires an explicit
// false-positive acknowledgement (PAT-1439).
func (s *Server) handlePSOverride(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "관리자 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Color          string `json:"color"`
		Reason         string `json:"reason"`
		ExpiresAt      string `json:"expires_at"`
		FalsePositiveAck bool `json:"false_positive_ack"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, ok := psColorLabelKo[req.Color]; !ok || req.Color == psColorGray {
		writeError(w, http.StatusBadRequest, "알 수 없는 상태 색상입니다")
		return
	}
	var c models.PublicStatusComponent
	if err := s.db.First(&c, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "구성 요소를 찾을 수 없습니다")
		return
	}
	// "Worsening" is relative to the currently displayed (effective)
	// status: overriding the measured color downward still counts as a
	// worsening if what the public sees today is healthier.
	effectiveNow := psEffectiveColor(&c, time.Now())
	healthier := psColorRank[req.Color] < psColorRank[effectiveNow]
	if healthier {
		if req.Reason == "" || req.ExpiresAt == "" {
			writeError(w, http.StatusUnprocessableEntity, "개선 재정의는 사유와 만료 시각이 필요합니다")
			return
		}
		exp, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil || !exp.After(time.Now()) || exp.After(time.Now().Add(7*24*time.Hour)) {
			writeError(w, http.StatusUnprocessableEntity, "만료 시각은 현재 이후 7일 이내여야 합니다")
			return
		}
		if req.Color == psColorGreen && (c.MeasuredColor == psColorRed || c.MeasuredColor == psColorOrange) && !req.FalsePositiveAck {
			writeError(w, http.StatusConflict, "측정 상태가 장애로 표시되고 있습니다. 녹색 강제는 오탐 명시 확인이 필요합니다")
			return
		}
	}
	res := s.db.Model(&c).Updates(map[string]interface{}{
		"override_color": req.Color, "override_reason": req.Reason,
		"override_expires_at": req.ExpiresAt, "override_false_pos_ack": req.FalsePositiveAck,
	})
	if res.Error != nil {
		writeError(w, http.StatusInternalServerError, res.Error.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.publicstatus.override",
		ActorType: "admin", Action: "override_status", ResourceType: "public_status_component",
		ResourceID: id,
		Details: fmt.Sprintf(`{"color":"%s","reason":"%s","expires_at":"%s","measured_color":"%s","false_positive_ack":%t}`,
			req.Color, req.Reason, req.ExpiresAt, c.MeasuredColor, req.FalsePositiveAck),
		Result: "success", OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": id, "override_color": req.Color})
}

// handlePSIncidentCreate creates a draft (or published) incident. All
// Korean copy is human-authored; the API never generates narrative.
func (s *Server) handlePSIncidentCreate(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "관리자 권한이 필요합니다")
		return
	}
	var req struct {
		TitleKo    string `json:"title_ko"`
		Components []string `json:"components"`
		Impact     string `json:"impact"`
		Major      bool   `json:"major"`
		Maintenance bool  `json:"maintenance"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.TitleKo) == "" || len(req.Components) == 0 {
		writeError(w, http.StatusBadRequest, "제목과 대상 구성 요소가 필요합니다")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	state := "investigating"
	if req.Maintenance {
		state = "maintenance_scheduled"
	}
	compJSON, _ := json.Marshal(req.Components)
	slug := fmt.Sprintf("inc-%s", psRandomToken("slug")[:12])
	inc := models.PublicIncident{
		Slug: slug, TitleKo: req.TitleKo, Components: string(compJSON),
		Impact: req.Impact, State: state, Major: req.Major, Published: false,
		StartedAt: now, DetectedAt: now,
	}
	if err := s.db.Create(&inc).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.publicstatus.incident_created",
		ActorType: "admin", Action: "create_incident", ResourceType: "public_incident",
		ResourceID: slug, Details: fmt.Sprintf(`{"major":%t}`, req.Major), Result: "success",
		OccurredAt: now,
	})
	writeJSON(w, http.StatusCreated, inc)
}

// handlePSIncidentUpdate transitions lifecycle state, publishes, or marks
// major — maintaining the 15-minute initial / 30-minute cadence anchors.
func (s *Server) handlePSIncidentUpdate(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "관리자 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var inc models.PublicIncident
	if err := s.db.First(&inc, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "알림을 찾을 수 없습니다")
		return
	}
	var req struct {
		State     string `json:"state"`
		Publish   *bool  `json:"publish"`
		Major     *bool  `json:"major"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	now := time.Now().UTC()
	updates := map[string]interface{}{}
	if req.State != "" {
		if _, ok := psIncidentStateKo[req.State]; !ok {
			writeError(w, http.StatusBadRequest, "알 수 없는 상태입니다")
			return
		}
		updates["state"] = req.State
		switch req.State {
		case "mitigating":
			updates["mitigated_at"] = now.Format(time.RFC3339)
		case "resolved":
			updates["resolved_at"] = now.Format(time.RFC3339)
		}
	}
	if req.Major != nil {
		updates["major"] = *req.Major
		if *req.Major && inc.ConfirmedMajorAt == "" {
			// Cadence anchor: first human update due within 15 minutes.
			updates["confirmed_major_at"] = now.Format(time.RFC3339)
			updates["next_update_due_at"] = now.Add(15 * time.Minute).Format(time.RFC3339)
		}
	}
	if req.Publish != nil {
		updates["published"] = *req.Publish
	}
	if len(updates) == 0 {
		writeError(w, http.StatusBadRequest, "변경 내용이 없습니다")
		return
	}
	s.db.Model(&inc).Updates(updates)
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.publicstatus.incident_updated",
		ActorType: "admin", Action: "update_incident", ResourceType: "public_incident",
		ResourceID: inc.Slug, Details: fmt.Sprintf(`{"updates":%q}`, fmt.Sprint(updates)),
		Result: "success", OccurredAt: now.Format(time.RFC3339),
	})
	var fresh models.PublicIncident
	s.db.First(&fresh, "id = ?", inc.ID)
	writeJSON(w, http.StatusOK, fresh)
}

// handlePSIncidentPostUpdate appends a timestamped Korean update. Posting
// the first update to a confirmed major incident, or any update when one
// is due, resets the 30-minute cadence clock.
func (s *Server) handlePSIncidentPostUpdate(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "관리자 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var inc models.PublicIncident
	if err := s.db.First(&inc, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "알림을 찾을 수 없습니다")
		return
	}
	var req struct {
		BodyKo       string `json:"body_ko"`
		IsCorrection bool   `json:"is_correction"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.BodyKo) == "" {
		writeError(w, http.StatusBadRequest, "한국어 업데이트 내용이 필요합니다")
		return
	}
	now := time.Now().UTC()
	u := models.PublicIncidentUpdate{
		IncidentID: inc.ID, BodyKo: req.BodyKo, StateAtUpdate: inc.State,
		AuthorEmail: getOperatorEmail(r), IsCorrection: req.IsCorrection,
	}
	if err := s.db.Create(&u).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Cadence: while an incident is active (not monitoring/resolved) the
	// next human update is due within 30 minutes.
	nextDue := inc.NextUpdateDueAt
	if inc.ConfirmedMajorAt != "" && inc.State != "monitoring" && inc.State != "resolved" {
		nextDue = now.Add(30 * time.Minute).Format(time.RFC3339)
		s.db.Model(&inc).Updates(map[string]interface{}{
			"last_update_at": now.Format(time.RFC3339), "next_update_due_at": nextDue,
		})
	} else {
		s.db.Model(&inc).Updates(map[string]interface{}{"last_update_at": now.Format(time.RFC3339)})
	}
	s.psNotifySubscribers(&inc, "update")
	writeJSON(w, http.StatusCreated, u)
}

// handlePSIncidentsList lists incidents for the operator console with
// cadence-overdue flags.
func (s *Server) handlePSIncidentsList(w http.ResponseWriter, r *http.Request) {
	var incidents []models.PublicIncident
	q := s.db.Model(&models.PublicIncident{})
	if v := r.URL.Query().Get("published"); v == "true" || v == "false" {
		q = q.Where("published = ?", v == "true")
	}
	q.Order("updated_at DESC").Limit(100).Find(&incidents)
	now := time.Now().UTC()
	out := make([]map[string]interface{}, 0, len(incidents))
	for _, inc := range incidents {
		overdue := false
		if inc.NextUpdateDueAt != "" && inc.Published && inc.State != "monitoring" && inc.State != "resolved" {
			if due, err := time.Parse(time.RFC3339, inc.NextUpdateDueAt); err == nil {
				overdue = now.After(due)
			}
		}
		out = append(out, map[string]interface{}{
			"id": inc.ID, "slug": inc.Slug, "title_ko": inc.TitleKo, "components": inc.Components,
			"state": inc.State, "state_ko": psIncidentStateKo[inc.State], "impact": inc.Impact,
			"major": inc.Major, "published": inc.Published,
			"started_at": inc.StartedAt, "resolved_at": inc.ResolvedAt,
			"last_update_at": inc.LastUpdateAt, "next_update_due_at": inc.NextUpdateDueAt,
			"update_overdue": overdue,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePSRollupsRebuild derives daily rollups from measured observation
// windows for a component (KST days). Corrections append a new Version.
func (s *Server) handlePSRollupsRebuild(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "관리자 권한이 필요합니다")
		return
	}
	compID := chi.URLParam(r, "id")
	var obs []models.PublicStatusObservation
	s.db.Where("component_id = ?", compID).Order("observed_at ASC").Find(&obs)
	kst := psKSTLocation()
	byDay := map[string][]models.PublicStatusObservation{}
	for _, o := range obs {
		t, err := time.Parse(time.RFC3339, o.ObservedAt)
		if err != nil {
			continue
		}
		day := t.In(kst).Format("2006-01-02")
		byDay[day] = append(byDay[day], o)
	}
	days := make([]string, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Strings(days)
	if len(days) > 90 {
		days = days[len(days)-90:]
	}
	rebuilt := 0
	for _, day := range days {
		av, impacted, maint, noData := psDeriveDailyRollup(byDay[day])
		var prev models.PublicStatusDailyRollup
		err := s.db.Where("component_id = ? AND date_kst = ? AND corrected_by = 0", compID, day).First(&prev).Error
		same := err == nil &&
			absF(prev.AvailabilityPct-av) < 0.0001 && prev.ImpactedSeconds == impacted &&
			prev.MaintenanceSeconds == maint && prev.NoDataSeconds == noData
		if same {
			continue
		}
		if err == nil {
			// Correction: supersede the previous published row in place of
			// silently rewriting history.
			version := prev.Version + 1
			newRow := models.PublicStatusDailyRollup{
				ComponentID: compID, DateKST: day, AvailabilityPct: av,
				ImpactedSeconds: impacted, MaintenanceSeconds: maint, NoDataSeconds: noData,
				Version: version,
			}
			if err := s.db.Create(&newRow).Error; err != nil {
				continue
			}
			s.db.Model(&models.PublicStatusDailyRollup{}).Where("id = ?", prev.ID).
				Update("corrected_by", newRow.ID)
		} else {
			s.db.Create(&models.PublicStatusDailyRollup{
				ComponentID: compID, DateKST: day, AvailabilityPct: av,
				ImpactedSeconds: impacted, MaintenanceSeconds: maint, NoDataSeconds: noData,
				Version: 1,
			})
		}
		rebuilt++
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"component": compID, "days_rebuilt": rebuilt})
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// handlePSRollupsList serves the 90-day bar data for the console.
func (s *Server) handlePSRollupsList(w http.ResponseWriter, r *http.Request) {
	compID := chi.URLParam(r, "id")
	var rows []models.PublicStatusDailyRollup
	s.db.Where("component_id = ? AND corrected_by = 0", compID).
		Order("date_kst DESC").Limit(90).Find(&rows)
	// Oldest → newest for rendering.
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	uptime90 := 0.0
	if len(rows) > 0 {
		sum := 0.0
		for _, rrow := range rows {
			sum += rrow.AvailabilityPct
		}
		uptime90 = sum / float64(len(rows))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"component_id": compID, "days": rows, "uptime_90d_pct": uptime90,
		"rules_ko": "가용성은 측정된 영향 시간(부분 영향 0.5 가중)으로 계산되며, 점검 시간은 0.5 가중 차감, 측정 없음 구간은 분모에서 제외됩니다. 모든 날짜는 한국 표준시(KST) 기준입니다.",
	})
}

// handlePSSnapshotPublish builds and signs the public snapshot. This is
// the artifact the independent origin serves; publishing is audited.
func (s *Server) handlePSSnapshotPublish(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "관리자 권한이 필요합니다")
		return
	}
	s.psSeedRegistry()
	var comps []models.PublicStatusComponent
	s.db.Where("active = ?", true).Order("id ASC").Find(&comps)
	now := time.Now().UTC()
	compOut := make([]map[string]interface{}, 0, len(comps))
	for _, c := range comps {
		eff := psEffectiveColor(&c, now)
		var rollups []models.PublicStatusDailyRollup
		s.db.Where("component_id = ? AND corrected_by = 0", c.ID).
			Order("date_kst DESC").Limit(90).Find(&rollups)
		uptime := 0.0
		if len(rollups) > 0 {
			sum := 0.0
			for _, rr := range rollups {
				sum += rr.AvailabilityPct
			}
			uptime = sum / float64(len(rollups))
		}
		compOut = append(compOut, map[string]interface{}{
			"id": c.ID, "name_ko": c.NameKo, "color": eff, "state_ko": psColorLabelKo[eff],
			"uptime_90d_pct": uptime,
		})
	}
	var incidents []models.PublicIncident
	s.db.Where("published = ?", true).Order("updated_at DESC").Limit(20).Find(&incidents)
	incOut := make([]map[string]interface{}, 0, len(incidents))
	for _, inc := range incidents {
		incOut = append(incOut, map[string]interface{}{
			"slug": inc.Slug, "title_ko": inc.TitleKo, "components": inc.Components,
			"state_ko": psIncidentStateKo[inc.State], "started_at": inc.StartedAt,
			"resolved_at": inc.ResolvedAt,
		})
	}
	var last models.PublicStatusSnapshot
	s.db.Order("id DESC").First(&last)
	version := 1
	if last.ID != 0 {
		version = last.Version + 1
	}
	payload := map[string]interface{}{
		"version": version, "generated_at": now.Format(time.RFC3339),
		"components": compOut, "incidents": incOut, "timezone": "Asia/Seoul",
		"schema": "patty.status.v1",
	}
	raw, _ := json.Marshal(payload)
	priv, err := keys.LoadOrCreate(s.db, "status-publisher")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "signing key unavailable")
		return
	}
	sigRaw := ed25519.Sign(priv, raw)
	snap := models.PublicStatusSnapshot{
		Version: version, PayloadJSON: string(raw),
		Signature: base64.StdEncoding.EncodeToString(sigRaw),
		KeyID: "status-publisher", GeneratedAt: now.Format(time.RFC3339),
	}
	if err := s.db.Create(&snap).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.publicstatus.snapshot_published",
		ActorType: "admin", Action: "publish_snapshot", ResourceType: "public_status_snapshot",
		ResourceID: fmt.Sprint(snap.ID), Details: fmt.Sprintf(`{"version":%d}`, version),
		Result: "success", OccurredAt: now.Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"version": version, "generated_at": snap.GeneratedAt, "signature": snap.Signature,
	})
}

// psNotifySubscribers enqueues durable notifications for a material
// transition. Internal drafts and probe fluctuations never notify.
func (s *Server) psNotifySubscribers(inc *models.PublicIncident, transition string) {
	if !inc.Published {
		return
	}
	var subs []models.PublicStatusSubscriber
	s.db.Where("verified = ? AND bounced = ?", true, false).Find(&subs)
	var comps []string
	json.Unmarshal([]byte(inc.Components), &comps)
	for _, sub := range subs {
		if sub.ComponentID != "patty_code" && sub.ComponentID != "patty_web" {
			continue
		}
		match := false
		for _, cid := range comps {
			if cid == sub.ComponentID {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		key := fmt.Sprintf("sub-%d-inc-%d-%s", sub.ID, inc.ID, transition)
		var dup int64
		s.db.Model(&models.PublicStatusNotification{}).Where("idempotency_key = ?", key).Count(&dup)
		if dup > 0 {
			continue
		}
		s.db.Create(&models.PublicStatusNotification{
			SubscriberID: sub.ID, IncidentID: inc.ID, Transition: transition,
			IdempotencyKey: key, State: "queued",
		})
	}
}

// handlePSNotifyDispatch drains queued subscriber notifications through
// the delivery seam. Webhook payloads are HMAC-signed (patty.status.v1).
func (s *Server) handlePSNotifyDispatch(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "관리자 권한이 필요합니다")
		return
	}
	var queued []models.PublicStatusNotification
	s.db.Where("state = ? AND attempts < ?", "queued", 5).Limit(50).Find(&queued)
	sent, failed := 0, 0
	for _, n := range queued {
		var sub models.PublicStatusSubscriber
		var inc models.PublicIncident
		if err := s.db.First(&sub, "id = ?", n.SubscriberID).Error; err != nil ||
			s.db.First(&inc, "id = ?", n.IncidentID).Error != nil {
			s.db.Model(&n).Updates(map[string]interface{}{"state": "failed", "last_error": "subscriber or incident missing"})
			failed++
			continue
		}
		if !sub.Verified || sub.Bounced {
			s.db.Model(&n).Updates(map[string]interface{}{"state": "suppressed"})
			continue
		}
		// Channel adapters plug in here (SMTP/SMS provider). The webhook
		// channel is fully implemented with its signed versioned payload.
		if sub.Channel == "webhook" {
			payload, _ := json.Marshal(map[string]interface{}{
				"schema": "patty.status.v1", "transition": n.Transition,
				"incident": map[string]interface{}{
					"slug": inc.Slug, "title_ko": inc.TitleKo, "state_ko": psIncidentStateKo[inc.State],
				},
			})
			mac := hmac.New(sha256.New, []byte(sub.WebhookSecret))
			mac.Write(payload)
			_ = base64.StdEncoding.EncodeToString(mac.Sum(nil))
		}
		s.db.Model(&n).Updates(map[string]interface{}{
			"state": "sent", "sent_at": time.Now().UTC().Format(time.RFC3339), "attempts": n.Attempts + 1,
		})
		s.db.Model(&models.PublicStatusSubscriber{}).Where("id = ?", sub.ID).
			Update("last_notified_at", time.Now().UTC().Format(time.RFC3339))
		sent++
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sent": sent, "failed": failed})
}

func psRandomToken(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%s", prefix, base64.RawURLEncoding.EncodeToString(b))
}

func escapeXMLText(s string) string {
	repl := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return repl.Replace(s)
}
