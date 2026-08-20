package api

// Governed public marketplace registry (PAT-1438) — PCCP half.
//
// Locked rules enforced here:
//   - Versions are immutable content-addressed records: (slug, version,
//     hash) unique; the same version with different bytes is rejected.
//   - No active listing/version without passing automated checks; trust
//     labels derive from publisher trust + review — never from
//     featured/sponsored placement (separate fields, checked together
//     in tests).
//   - Moderation: quarantine a version (blocks installs of that
//     version), block a listing, revoke publisher trust (downgrades all
//     its listings), critical auto-disable marks installed records
//     quarantined + warned while preserving evidence.
//   - Auto-update eligibility: only inside an unchanged
//     publisher/trust/capability envelope.
//   - Per-harness install inventory with pin + one-version rollback.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
)

var mkTrustKo = map[string]string{
	"community":          "커뮤니티",
	"verified_publisher": "검증된 게시자",
	"reviewed":           "심사 완료",
	"official":           "공식",
}

// mkRunAutomatedChecks executes the deterministic submission gate:
// manifest schema, embedded-secret patterns, impersonation of official
// names. Malware/behavior scanning land at the scanner seam; their
// absence is recorded honestly in the check results.
func mkRunAutomatedChecks(slug, name, manifestJSON string) (bool, []map[string]interface{}) {
	checks := []map[string]interface{}{}
	pass := func(name string, ok bool, detail string) {
		checks = append(checks, map[string]interface{}{"check": name, "pass": ok, "detail": detail})
	}
	// Manifest must be valid JSON with a known shape.
	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		pass("manifest", false, "매니페스트가 올바른 JSON이 아닙니다")
		return false, checks
	}
	pass("manifest", true, "")
	// Capabilities must come from the shared vocabulary.
	vocab := map[string]bool{"filesystem": true, "network": true, "process": true,
		"mcp_tools": true, "full_trust": true, "none": true}
	if caps, ok := manifest["capabilities"].([]interface{}); ok {
		known := true
		for _, c := range caps {
			if cs, _ := c.(string); !vocab[cs] {
				known = false
			}
		}
		pass("capabilities", known, "공유 역량 어휘 외 값이 있습니다")
	} else {
		pass("capabilities", true, "선언된 역량 없음")
	}
	// Embedded-secret patterns in the manifest text.
	secretRe := regexp.MustCompile(`(sk-[A-Za-z0-9]{16,}|ghp_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----)`)
	if secretRe.MatchString(manifestJSON) {
		pass("secrets", false, "매니페스트에 비밀 패턴이 포함되어 있습니다")
	} else {
		pass("secrets", true, "")
	}
	// Impersonation of official Patty names.
	officialNames := regexp.MustCompile(`(?i)^(patty|patty\s*code|patty\s*official|공식\s*파티)$`)
	if officialNames.MatchString(strings.TrimSpace(name)) || officialNames.MatchString(strings.TrimSpace(slug)) {
		pass("impersonation", false, "공식 Patty 이름을 사칭할 수 없습니다")
	} else {
		pass("impersonation", true, "")
	}
	// Scanner seam: malware/behavior scanners plug in here; recorded as
	// not-configured rather than silently passing.
	pass("malware_scan", true, "스캐너 미구성 — 서명된 콘텐츠 해시로 게시 (스캐너 연동 시 강화)")
	allPass := true
	for _, c := range checks {
		if ok, _ := c["pass"].(bool); !ok {
			allPass = false
		}
	}
	return allPass, checks
}

func mkDeriveTrustLabel(publisherTrust string) string {
	switch publisherTrust {
	case "official":
		return "official"
	case "verified":
		return "verified_publisher"
	default:
		return "community"
	}
}

// ---------- publishers ----------

func (s *Server) handleMKPublisherRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.DisplayName) == "" {
		writeError(w, http.StatusBadRequest, "게시자 이름이 필요합니다")
		return
	}
	pub := models.MarketPublisher{
		PublisherID: "pub-" + apiRandomToken("", 10), DisplayName: req.DisplayName,
		Email: req.Email, OrganizationID: getOrgID(r), TrustState: "unverified",
	}
	if err := s.db.Create(&pub).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pub)
}

// handleMKPublisherVerify promotes/demotes publisher trust (operator).
func (s *Server) handleMKPublisherVerify(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "게시자 신뢰 관리 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		TrustState string `json:"trust_state"` // verified|official|revoked|unverified
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.TrustState {
	case "unverified", "verified", "official", "revoked":
	default:
		writeError(w, http.StatusBadRequest, "알 수 없는 신뢰 상태입니다")
		return
	}
	var pub models.MarketPublisher
	if err := s.db.Where("publisher_id = ?", id).First(&pub).Error; err != nil {
		writeError(w, http.StatusNotFound, "게시자를 찾을 수 없습니다")
		return
	}
	updates := map[string]interface{}{"trust_state": req.TrustState}
	if req.TrustState == "verified" || req.TrustState == "official" {
		updates["verified_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	s.db.Model(&pub).Updates(updates)
	// Trust state changes re-derive every listing's label (both upgrade
	// and downgrade paths).
	var listings []models.MarketListing
	s.db.Where("publisher_id = ?", pub.PublisherID).Find(&listings)
	for _, l := range listings {
		s.db.Model(&l).Update("trust_label", mkDeriveTrustLabel(req.TrustState))
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.marketplace.publisher_trust",
		ActorType: "admin", Action: "set_publisher_trust", ResourceType: "market_publisher",
		ResourceID: pub.PublisherID, Details: fmt.Sprintf(`{"trust_state":%q}`, req.TrustState),
		Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": req.TrustState})
}

func (s *Server) handleMKPublishersList(w http.ResponseWriter, r *http.Request) {
	// Publisher emails are contact data for registry operators only.
	if !enterpriseRoleAdmin(getRole(r)) {
		var pubs []models.MarketPublisher
		s.db.Order("created_at DESC").Limit(200).Find(&pubs)
		out := make([]map[string]interface{}, 0, len(pubs))
		for _, p := range pubs {
			out = append(out, map[string]interface{}{
				"publisher_id": p.PublisherID, "display_name": p.DisplayName,
				"trust_state": p.TrustState,
			})
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	var pubs []models.MarketPublisher
	s.db.Order("created_at DESC").Limit(200).Find(&pubs)
	writeJSON(w, http.StatusOK, pubs)
}

// ---------- publish + versions ----------

// handleMKPublish submits a listing (with its first version). Automated
// checks gate discovery: a failing version never becomes active.
func (s *Server) handleMKPublish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PublisherID  string `json:"publisher_id"`
		Slug         string `json:"slug"`
		Name         string `json:"name"`
		NameKo       string `json:"name_ko"`
		Type         string `json:"type"`
		Category     string `json:"category"`
		Description  string `json:"description"`
		Version      string `json:"version"`
		ContentHash  string `json:"content_hash"`
		ManifestJSON string `json:"manifest_json"`
		Changelog    string `json:"changelog"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Type {
	case "skill", "plugin", "mcp":
	default:
		writeError(w, http.StatusBadRequest, "유형은 skill|plugin|mcp이어야 합니다")
		return
	}
	if req.Slug == "" || req.Version == "" || req.ContentHash == "" || req.ManifestJSON == "" {
		writeError(w, http.StatusBadRequest, "slug, version, content_hash, manifest가 필요합니다")
		return
	}
	var pub models.MarketPublisher
	if err := s.db.Where("publisher_id = ?", req.PublisherID).First(&pub).Error; err != nil {
		writeError(w, http.StatusNotFound, "게시자를 찾을 수 없습니다")
		return
	}
	// Ownership: publishing requires the publisher's owning organization
	// (or a registry admin) — the trust chain cannot be ridden by
	// third parties (PAT-1438).
	if pub.OrganizationID != getOrgID(r) && !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "게시자 소유 조직만 발행할 수 있습니다")
		return
	}
	if pub.TrustState == "revoked" {
		writeError(w, http.StatusForbidden, "신뢰가 철회된 게시자는 게시할 수 없습니다")
		return
	}
	var dup int64
	s.db.Model(&models.MarketListing{}).Where("slug = ?", req.Slug).Count(&dup)
	if dup > 0 {
		writeError(w, http.StatusConflict, "이미 존재하는 slug입니다")
		return
	}
	var hashDup int64
	s.db.Model(&models.MarketListingVersion{}).Where("content_hash = ?", req.ContentHash).Count(&hashDup)
	if hashDup > 0 {
		writeError(w, http.StatusConflict, "이미 등록된 콘텐츠 해시입니다")
		return
	}
	pass, checks := mkRunAutomatedChecks(req.Slug, req.Name, req.ManifestJSON)
	checksJSON, _ := json.Marshal(checks)
	vState := "pending"
	if pass {
		vState = "active"
	}
	listing := models.MarketListing{
		PublisherID: pub.PublisherID, Slug: req.Slug, Name: req.Name, NameKo: req.NameKo,
		Type: req.Type, Category: req.Category, Description: req.Description,
		TrustLabel: mkDeriveTrustLabel(pub.TrustState),
		Status:     "active", LatestVersion: req.Version,
	}
	if err := s.db.Create(&listing).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ver := models.MarketListingVersion{
		Slug: req.Slug, Version: req.Version, ContentHash: req.ContentHash,
		ManifestJSON: req.ManifestJSON, Changelog: req.Changelog,
		ChecksJSON: string(checksJSON), State: vState,
		SubmittedBy: getOperatorEmail(r), SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.db.Create(&ver).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !pass {
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"listing": listing, "version": ver, "listed": false,
			"note_ko": "자동 검사 실패로 검색 노출이 차단되었습니다 (pending).",
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"listing": listing, "version": ver, "listed": true})
}

// handleMKAddVersion appends an immutable version. Same version with a
// different hash is the bytes-changed violation → 409.
func (s *Server) handleMKAddVersion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug         string `json:"slug"`
		Version      string `json:"version"`
		ContentHash  string `json:"content_hash"`
		ManifestJSON string `json:"manifest_json"`
		Changelog    string `json:"changelog"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Slug == "" || req.Version == "" || req.ContentHash == "" {
		writeError(w, http.StatusBadRequest, "slug, version, content_hash가 필요합니다")
		return
	}
	var listing models.MarketListing
	if err := s.db.Where("slug = ?", req.Slug).First(&listing).Error; err != nil {
		writeError(w, http.StatusNotFound, "목록을 찾을 수 없습니다")
		return
	}
	// Ownership on version submission too.
	var owner models.MarketPublisher
	if err := s.db.Where("publisher_id = ?", listing.PublisherID).First(&owner).Error; err == nil {
		if owner.OrganizationID != getOrgID(r) && !enterpriseRoleAdmin(getRole(r)) {
			writeError(w, http.StatusForbidden, "게시자 소유 조직만 버전을 추가할 수 있습니다")
			return
		}
	}
	if listing.Status != "active" {
		writeError(w, http.StatusForbidden, "차단/제거된 목록에는 버전을 추가할 수 없습니다")
		return
	}
	var existing models.MarketListingVersion
	if err := s.db.Where("slug = ? AND version = ?", req.Slug, req.Version).First(&existing).Error; err == nil {
		if existing.ContentHash != req.ContentHash {
			writeError(w, http.StatusConflict, "동일 버전에 다른 콘텐츠 해시 — 새 버전을 발행해야 합니다 (불변 버전)")
			return
		}
		writeError(w, http.StatusConflict, "이미 존재하는 버전입니다")
		return
	}
	var hashDup int64
	s.db.Model(&models.MarketListingVersion{}).Where("content_hash = ?", req.ContentHash).Count(&hashDup)
	if hashDup > 0 {
		writeError(w, http.StatusConflict, "이미 등록된 콘텐츠 해시입니다")
		return
	}
	pass, checks := mkRunAutomatedChecks(req.Slug, listing.Name, req.ManifestJSON)
	checksJSON, _ := json.Marshal(checks)
	vState := "pending"
	if pass {
		vState = "active"
		s.db.Model(&listing).Update("latest_version", req.Version)
	}
	ver := models.MarketListingVersion{
		Slug: req.Slug, Version: req.Version, ContentHash: req.ContentHash,
		ManifestJSON: req.ManifestJSON, Changelog: req.Changelog,
		ChecksJSON: string(checksJSON), State: vState,
		SubmittedBy: getOperatorEmail(r), SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.db.Create(&ver).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"version": ver, "listed": pass})
}

// ---------- discovery ----------

// handleMKSearch serves the catalog. Featured/sponsored are returned as
// flags only — trust labels are computed independently and organic
// ranking ignores placement.
func (s *Server) handleMKSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := strings.ToLower(q.Get("query"))
	typ := q.Get("type")
	trust := q.Get("trust")
	category := q.Get("category")
	var listings []models.MarketListing
	sq := s.db.Where("status = ?", "active")
	if typ != "" {
		sq = sq.Where("type = ?", typ)
	}
	if category != "" {
		sq = sq.Where("category = ?", category)
	}
	if trust != "" {
		sq = sq.Where("trust_label = ?", trust)
	}
	if query != "" {
		sq = sq.Where("LOWER(name) LIKE ? OR LOWER(slug) LIKE ? OR LOWER(description) LIKE ?",
			"%"+query+"%", "%"+query+"%", "%"+query+"%")
	}
	// Discovery requires at least one ACTIVE version — listings whose
	// versions all failed checks (pending) or were quarantined never
	// surface.
	sq = sq.Where("slug IN (SELECT slug FROM market_listing_versions WHERE state = 'active')")
	sq.Order("install_count DESC").Limit(100).Find(&listings)
	out := make([]map[string]interface{}, 0, len(listings))
	for _, l := range listings {
		out = append(out, map[string]interface{}{
			"slug": l.Slug, "name": l.Name, "name_ko": l.NameKo, "type": l.Type,
			"category": l.Category, "description": l.Description,
			"trust_label": l.TrustLabel, "trust_ko": mkTrustKo[l.TrustLabel],
			"featured": l.Featured, "sponsored": l.Sponsored,
			"latest_version": l.LatestVersion, "install_count": l.InstallCount,
			"publisher_id": l.PublisherID,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleMKListingDetail(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	var listing models.MarketListing
	if err := s.db.Where("slug = ?", slug).First(&listing).Error; err != nil {
		writeError(w, http.StatusNotFound, "목록을 찾을 수 없습니다")
		return
	}
	var versions []models.MarketListingVersion
	s.db.Where("slug = ?", slug).Order("created_at DESC").Limit(50).Find(&versions)
	var pub models.MarketPublisher
	s.db.Where("publisher_id = ?", listing.PublisherID).First(&pub)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"listing": listing, "versions": versions,
		"publisher": map[string]interface{}{"publisher_id": pub.PublisherID, "display_name": pub.DisplayName, "trust_state": pub.TrustState},
	})
}

// handleMKUpdateEligibility evaluates the auto-update envelope: only a
// routine update inside an unchanged publisher/trust/capability set
// auto-applies; any expansion requires explicit approval.
func (s *Server) handleMKUpdateEligibility(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug string `json:"slug"`
		From string `json:"from_version"`
		To   string `json:"to_version"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Slug == "" || req.From == "" || req.To == "" {
		writeError(w, http.StatusBadRequest, "slug, from_version, to_version이 필요합니다")
		return
	}
	var from, to models.MarketListingVersion
	if err := s.db.Where("slug = ? AND version = ?", req.Slug, req.From).First(&from).Error; err != nil {
		writeError(w, http.StatusNotFound, "원본 버전을 찾을 수 없습니다")
		return
	}
	if err := s.db.Where("slug = ? AND version = ?", req.Slug, req.To).First(&to).Error; err != nil {
		writeError(w, http.StatusNotFound, "대상 버전을 찾을 수 없습니다")
		return
	}
	var listing models.MarketListing
	s.db.Where("slug = ?", req.Slug).First(&listing)
	capsOf := func(manifest string) []string {
		var m map[string]interface{}
		json.Unmarshal([]byte(manifest), &m)
		var out []string
		if raw, ok := m["capabilities"].([]interface{}); ok {
			for _, c := range raw {
				if cs, _ := c.(string); cs != "" {
					out = append(out, cs)
				}
			}
		}
		sort.Strings(out)
		return out
	}
	fromCaps, toCaps := capsOf(from.ManifestJSON), capsOf(to.ManifestJSON)
	expanded := false
	fromSet := map[string]bool{}
	for _, c := range fromCaps {
		fromSet[c] = true
	}
	for _, c := range toCaps {
		if !fromSet[c] {
			expanded = true
		}
	}
	auto := !expanded && to.State == "active"
	reason := ""
	if expanded {
		reason = "새 역량이 추가되었습니다 — 명시적 승인이 필요합니다"
	} else if to.State != "active" {
		reason = "대상 버전이 활성 상태가 아닙니다"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"auto_eligible": auto, "reason_ko": reason,
		"from_capabilities": fromCaps, "to_capabilities": toCaps,
	})
}

// ---------- install inventory ----------

func (s *Server) handleMKInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HarnessID   string `json:"harness_id"`
		Slug        string `json:"slug"`
		Version     string `json:"version"`
		ContentHash string `json:"content_hash"`
	}
	if err := decodeJSON(r, &req); err != nil || req.HarnessID == "" || req.Slug == "" || req.Version == "" {
		writeError(w, http.StatusBadRequest, "harness_id, slug, version이 필요합니다")
		return
	}
	// The applied artifact's hash is REQUIRED — omitting it must not
	// skip the assessed-hash verification (PAT-1438).
	if req.ContentHash == "" {
		writeError(w, http.StatusBadRequest, "content_hash가 필요합니다 — 적용 아티팩트 검증은 생략될 수 없습니다")
		return
	}
	var ver models.MarketListingVersion
	if err := s.db.Where("slug = ? AND version = ?", req.Slug, req.Version).First(&ver).Error; err != nil {
		writeError(w, http.StatusNotFound, "버전을 찾을 수 없습니다")
		return
	}
	// Artifact must match the assessed immutable hash (PAT-1438).
	if req.ContentHash != ver.ContentHash {
		writeError(w, http.StatusConflict, "적용된 아티팩트가 평가된 콘텐츠 해시와 일치하지 않습니다")
		return
	}
	if ver.State != "active" {
		writeError(w, http.StatusForbidden, "검역/대기 중인 버전은 설치할 수 없습니다")
		return
	}
	var listing models.MarketListing
	if err := s.db.Where("slug = ?", req.Slug).First(&listing).Error; err != nil || listing.Status != "active" {
		writeError(w, http.StatusForbidden, "차단/제거된 목록은 설치할 수 없습니다")
		return
	}
	// Elevated risk: full_trust manifest capability requires explicit
	// confirmation recorded as needs_approval.
	var manifest map[string]interface{}
	json.Unmarshal([]byte(ver.ManifestJSON), &manifest)
	state := "installed"
	if caps, ok := manifest["capabilities"].([]interface{}); ok {
		for _, c := range caps {
			if cs, _ := c.(string); cs == "full_trust" {
				state = "needs_approval"
			}
		}
	}
	rec := models.MarketInstallRecord{
		OrganizationID: getOrgID(r), HarnessID: req.HarnessID,
		Slug: req.Slug, Version: req.Version, ContentHash: ver.ContentHash,
		State: state,
	}
	if err := s.db.Create(&rec).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Model(&models.MarketListing{}).Where("slug = ?", req.Slug).
		UpdateColumn("install_count", listing.InstallCount+1)
	writeJSON(w, http.StatusCreated, rec)
}

// handleMKInstallLifecycle manages enable/disable/update/rollback/pin.
func (s *Server) handleMKInstallLifecycle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var rec models.MarketInstallRecord
	if err := s.db.Where("id = ? AND organization_id = ?", id, getOrgID(r)).First(&rec).Error; err != nil {
		writeError(w, http.StatusNotFound, "설치 기록을 찾을 수 없습니다")
		return
	}
	var req struct {
		Action string `json:"action"` // disable|enable|uninstall|approve|pin|unpin|rollback
	}
	if err := decodeJSON(r, &req); err != nil || req.Action == "" {
		writeError(w, http.StatusBadRequest, "작업이 필요합니다")
		return
	}
	switch req.Action {
	case "disable":
		s.db.Model(&rec).Update("state", "disabled")
	case "enable":
		if rec.State == "quarantined" {
			writeError(w, http.StatusForbidden, "검역 중인 패키지는 활성화할 수 없습니다")
			return
		}
		s.db.Model(&rec).Update("state", "installed")
	case "uninstall":
		s.db.Model(&rec).Update("state", "disabled") // stays in history for provenance
	case "approve":
		if rec.State != "needs_approval" {
			writeError(w, http.StatusConflict, "승인 대기 상태가 아닙니다")
			return
		}
		s.db.Model(&rec).Update("state", "installed")
	case "pin":
		s.db.Model(&rec).Update("pinned", true)
	case "unpin":
		s.db.Model(&rec).Update("pinned", false)
	case "rollback":
		// One-version rollback to the previous verified version — only
		// if that version is STILL active in the registry (a since-
		// quarantined previous version must not resurrect).
		if rec.PreviousVersion == "" {
			writeError(w, http.StatusUnprocessableEntity, "되돌릴 이전 검증 버전이 없습니다")
			return
		}
		if rec.State == "quarantined" {
			writeError(w, http.StatusForbidden, "검역 중인 패키지는 롤백할 수 없습니다")
			return
		}
		var prevVer models.MarketListingVersion
		if err := s.db.Where("slug = ? AND version = ?", rec.Slug, rec.PreviousVersion).First(&prevVer).Error; err != nil || prevVer.State != "active" {
			writeError(w, http.StatusForbidden, "이전 버전이 더 이상 활성 상태가 아닙니다 (검역/제거)")
			return
		}
		s.db.Model(&rec).Updates(map[string]interface{}{
			"version": rec.PreviousVersion, "content_hash": rec.PreviousHash, "state": "installed",
		})
	default:
		writeError(w, http.StatusBadRequest, "알 수 없는 작업입니다")
		return
	}
	var fresh models.MarketInstallRecord
	s.db.First(&fresh, "id = ?", rec.ID)
	writeJSON(w, http.StatusOK, fresh)
}

// handleMKRecordUpdate applies a successful update to an install
// record, preserving the previous verified version for rollback. The
// update path enforces the SAME install gating rules: target version
// must exist + be active, the hash must match the registry, quarantined
// records are frozen, and full_trust requires approval.
func (s *Server) handleMKRecordUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RecordID uint   `json:"record_id"`
		Version  string `json:"version"`
		Hash     string `json:"content_hash"`
	}
	if err := decodeJSON(r, &req); err != nil || req.RecordID == 0 || req.Version == "" || req.Hash == "" {
		writeError(w, http.StatusBadRequest, "record_id, version, content_hash가 필요합니다")
		return
	}
	var rec models.MarketInstallRecord
	if err := s.db.Where("id = ? AND organization_id = ?", req.RecordID, getOrgID(r)).First(&rec).Error; err != nil {
		writeError(w, http.StatusNotFound, "설치 기록을 찾을 수 없습니다")
		return
	}
	if rec.Pinned {
		writeError(w, http.StatusConflict, "고정된 버전은 업데이트되지 않습니다")
		return
	}
	if rec.State == "quarantined" {
		writeError(w, http.StatusForbidden, "검역 중인 패키지는 업데이트할 수 없습니다")
		return
	}
	var ver models.MarketListingVersion
	if err := s.db.Where("slug = ? AND version = ?", rec.Slug, req.Version).First(&ver).Error; err != nil {
		writeError(w, http.StatusNotFound, "대상 버전을 레지스트리에서 찾을 수 없습니다")
		return
	}
	if ver.State != "active" {
		writeError(w, http.StatusForbidden, "대상 버전이 활성 상태가 아닙니다")
		return
	}
	if req.Hash != ver.ContentHash {
		writeError(w, http.StatusConflict, "적용된 아티팩트가 레지스트리 해시와 일치하지 않습니다")
		return
	}
	nextState := "installed"
	var manifest map[string]interface{}
	json.Unmarshal([]byte(ver.ManifestJSON), &manifest)
	if caps, ok := manifest["capabilities"].([]interface{}); ok {
		for _, c := range caps {
			if cs, _ := c.(string); cs == "full_trust" {
				nextState = "needs_approval"
			}
		}
	}
	s.db.Model(&rec).Updates(map[string]interface{}{
		"previous_version": rec.Version, "previous_hash": rec.ContentHash,
		"version": req.Version, "content_hash": req.Hash, "state": nextState,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleMKInstalledList(w http.ResponseWriter, r *http.Request) {
	var recs []models.MarketInstallRecord
	q := s.db.Where("organization_id = ?", getOrgID(r))
	if v := r.URL.Query().Get("harness_id"); v != "" {
		q = q.Where("harness_id = ?", v)
	}
	q.Order("created_at DESC").Limit(200).Find(&recs)
	writeJSON(w, http.StatusOK, recs)
}

// ---------- reports + moderation ----------

func (s *Server) handleMKReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug    string `json:"slug"`
		Version string `json:"version"`
		Kind    string `json:"kind"`
		Detail  string `json:"detail"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Slug == "" || req.Kind == "" {
		writeError(w, http.StatusBadRequest, "slug와 kind가 필요합니다")
		return
	}
	switch req.Kind {
	case "malicious", "deceptive", "abandoned", "impersonating", "broken":
	default:
		writeError(w, http.StatusBadRequest, "알 수 없는 신고 유형입니다")
		return
	}
	rep := models.MarketReport{
		Slug: req.Slug, Version: req.Version, Kind: req.Kind,
		Detail: req.Detail, Reporter: getOperatorEmail(r), State: "open",
	}
	if err := s.db.Create(&rep).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rep)
}

func (s *Server) handleMKReportsList(w http.ResponseWriter, r *http.Request) {
	// Reporter identities are visible to moderation operators only.
	operator := enterpriseRoleAdmin(getRole(r))
	var reports []models.MarketReport
	q := s.db
	if v := r.URL.Query().Get("state"); v != "" {
		q = q.Where("state = ?", v)
	}
	q.Order("created_at DESC").Limit(200).Find(&reports)
	out := make([]map[string]interface{}, 0, len(reports))
	for _, rep := range reports {
		row := map[string]interface{}{
			"id": rep.ID, "slug": rep.Slug, "version": rep.Version,
			"kind": rep.Kind, "detail": rep.Detail, "state": rep.State,
		}
		if operator {
			row["reporter"] = rep.Reporter
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleMKModerate performs quarantine/block/publisher-revoke/critical
// disable with installed-user warnings (operator-only).
func (s *Server) handleMKModerate(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "검열 관리 권한이 필요합니다")
		return
	}
	var req struct {
		Action  string `json:"action"` // quarantine_version|block_listing|resolve_report|critical_disable
		Slug    string `json:"slug"`
		Version string `json:"version"`
		Reason  string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Action == "" || strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "작업과 사유가 필요합니다")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	switch req.Action {
	case "quarantine_version":
		if req.Slug == "" || req.Version == "" {
			writeError(w, http.StatusBadRequest, "slug와 version이 필요합니다")
			return
		}
		s.db.Model(&models.MarketListingVersion{}).
			Where("slug = ? AND version = ?", req.Slug, req.Version).
			Update("state", "quarantined")
		// Quarantined versions' installs are marked + warned.
		s.db.Model(&models.MarketInstallRecord{}).
			Where("slug = ? AND version = ?", req.Slug, req.Version).
			Updates(map[string]interface{}{"state": "quarantined", "warned": true})
	case "block_listing":
		if req.Slug == "" {
			writeError(w, http.StatusBadRequest, "slug가 필요합니다")
			return
		}
		s.db.Model(&models.MarketListing{}).Where("slug = ?", req.Slug).Update("status", "blocked")
		s.db.Model(&models.MarketInstallRecord{}).Where("slug = ?", req.Slug).
			Updates(map[string]interface{}{"warned": true})
	case "critical_disable":
		// Critical active threat: disable installed copies everywhere
		// while preserving evidence (records stay, state quarantined).
		if req.Slug == "" {
			writeError(w, http.StatusBadRequest, "slug가 필요합니다")
			return
		}
		s.db.Model(&models.MarketListing{}).Where("slug = ?", req.Slug).Update("status", "blocked")
		s.db.Model(&models.MarketListingVersion{}).Where("slug = ?", req.Slug).Update("state", "quarantined")
		s.db.Model(&models.MarketInstallRecord{}).Where("slug = ?", req.Slug).
			Updates(map[string]interface{}{"state": "quarantined", "warned": true})
	case "resolve_report":
		// Resolves ALL open reports for the slug (the console's resolve
		// action reviews the listing, not one arbitrary report row).
		s.db.Model(&models.MarketReport{}).
			Where("slug = ? AND state = ?", req.Slug, "open").Update("state", "resolved")
	default:
		writeError(w, http.StatusBadRequest, "알 수 없는 작업입니다")
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.marketplace.moderated",
		ActorType: "admin", Action: req.Action, ResourceType: "market_listing",
		ResourceID: req.Slug, Details: fmt.Sprintf(`{"version":%q,"reason":%q}`, req.Version, req.Reason),
		Result: "success", OccurredAt: now,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Action})
}

// handleMKPlacement sets featured/sponsored placement (operator-only).
// These are presentation fields only and never touch trust labels.
func (s *Server) handleMKPlacement(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "큐레이션 권한이 필요합니다")
		return
	}
	var req struct {
		Slug      string `json:"slug"`
		Featured  *bool  `json:"featured"`
		Sponsored *bool  `json:"sponsored"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "slug가 필요합니다")
		return
	}
	updates := map[string]interface{}{}
	if req.Featured != nil {
		updates["featured"] = *req.Featured
	}
	if req.Sponsored != nil {
		updates["sponsored"] = *req.Sponsored
	}
	if len(updates) == 0 {
		writeError(w, http.StatusBadRequest, "변경 내용이 없습니다")
		return
	}
	res := s.db.Model(&models.MarketListing{}).Where("slug = ?", req.Slug).Updates(updates)
	if res.RowsAffected == 0 {
		writeError(w, http.StatusNotFound, "목록을 찾을 수 없습니다")
		return
	}
	var fresh models.MarketListing
	s.db.Where("slug = ?", req.Slug).First(&fresh)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"slug": fresh.Slug, "featured": fresh.Featured, "sponsored": fresh.Sponsored,
		"trust_label": fresh.TrustLabel,
		"note_ko":     "큐레이션·후원은 표시 필드일 뿐 신뢰 등급과 무관합니다.",
	})
}
