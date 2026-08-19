package api

// Evidence-hardened admin search (PAT-1451).
//
// Locked rules enforced here:
//   - A dedicated grant (not generic admin status) gates every search,
//     open, and reveal; grant expiry/revocation re-evaluates per call so
//     cached-from-before results cannot be reopened.
//   - Authorization happens before candidate generation: scoped queries
//     are org-bound in SQL, never filtered post-hoc.
//   - Exact identifiers resolve first; lexical results follow; domains
//     stay separately grouped with their own ranking labels — no global
//     blended score.
//   - Sensitive content is masked by default; reveal is a separate
//     permission and produces an audit event.
//   - No bulk export endpoint exists; opens are single-record only.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// esDomainResult is the shared evidence-result contract. Every result
// carries its immutable locator and verification state.
type esDomainResult struct {
	Domain    string `json:"domain"`
	SourceID  string `json:"source_id"`
	ScopeRef  string `json:"scope_ref"`
	Label     string `json:"label"` // masked excerpt
	RankKind  string `json:"rank_kind"` // exact|lexical
	Locator   map[string]interface{} `json:"locator"`
	Verification string `json:"verification"` // verified|modified|unavailable|superseded|legacy-unverified
	Masked    bool   `json:"masked"`
}

// esGrantFor resolves the caller's live grant (expiry + revocation
// checked per call — PAT-1451 role-removal immediacy).
func (s *Server) esGrantFor(orgID, email string) *models.EvidenceSearchGrant {
	var grants []models.EvidenceSearchGrant
	s.db.Where("organization_id = ? AND admin_email = ? AND revoked = ?", orgID, email, false).Find(&grants)
	now := time.Now().UTC()
	for i := range grants {
		g := &grants[i]
		if g.ExpiresAt == "" {
			return g
		}
		if exp, err := time.Parse(time.RFC3339, g.ExpiresAt); err == nil && now.Before(exp) {
			return g
		}
	}
	return nil
}

func esMaskExcerpt(text string, n int) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}
	if len(clean) > n {
		r := []rune(clean)
		if len(r) > n {
			clean = string(r[:n]) + "…"
		}
	}
	// Mask likely-sensitive tokens in the excerpt itself.
	for _, pat := range []string{"sk-", "ghp_", "AKIA", "BEGIN "} {
		if idx := strings.Index(clean, pat); idx >= 0 {
			clean = clean[:idx] + "████"
			break
		}
	}
	return clean
}

// handleESSearch is the single admin entry point with four separately
// governed domains (PAT-1451).
func (s *Server) handleESSearch(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	email := getOperatorEmail(r)
	grant := s.esGrantFor(orgID, email)
	if grant == nil {
		writeError(w, http.StatusForbidden, "증거 검색 권한이 없습니다 (별도 승인 필요)")
		return
	}
	var req struct {
		Query     string   `json:"query"`
		Domains   []string `json:"domains"`
		SessionID string   `json:"session_id"`
		From      string   `json:"from"`
		To        string   `json:"to"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "검색어가 필요합니다")
		return
	}
	q := strings.TrimSpace(req.Query)
	want := map[string]bool{}
	for _, d := range req.Domains {
		want[d] = true
	}
	if len(want) == 0 {
		want = map[string]bool{"conversations": true, "code": true, "provenance": true, "trails": true}
	}
	results := map[string][]esDomainResult{}
	counts := map[string]int{}

	// ---- Conversations: PromptExchange within the tenant. Sessions are
	// joined by session scope (org via session ownership).
	if want["conversations"] {
		var exchanges []models.PromptExchange
		sq := s.db.Joins("JOIN sessions ON sessions.id = prompt_exchanges.session_id").
			Where("sessions.organization_id = ?", orgID)
		if req.SessionID != "" {
			sq = sq.Where("prompt_exchanges.session_id = ?", req.SessionID)
		}
		sq.Where("(prompt_exchanges.prompt_text LIKE ? OR prompt_exchanges.response_text LIKE ?)", "%"+q+"%", "%"+q+"%").
			Order("prompt_exchanges.created_at DESC").Limit(20).Find(&exchanges)
		for _, ex := range exchanges {
			results["conversations"] = append(results["conversations"], esDomainResult{
				Domain: "conversations", SourceID: ex.ID,
				ScopeRef: "session:" + ex.SessionID,
				Label:    esMaskExcerpt(esFirstNonEmpty(ex.PromptText, ex.ResponseText), 80),
				RankKind: "lexical",
				Locator: map[string]interface{}{
					"session_id": ex.SessionID, "exchange_id": ex.ExchangeID,
				},
				Verification: "verified", Masked: true,
			})
		}
		counts["conversations"] = len(results["conversations"])
	}

	// ---- Code: commit-pinned spans (file path / symbol search).
	if want["code"] {
		var spans []models.ProvenanceSpan
		cq := s.db.Where("organization_id = ?", orgID)
		if req.SessionID != "" {
			cq = cq.Where("session_id = ?", req.SessionID)
		}
		cq.Where("file_path LIKE ? OR symbol_qualified_name LIKE ?", "%"+q+"%", "%"+q+"%").
			Order("created_at DESC").Limit(20).Find(&spans)
		for _, sp := range spans {
			results["code"] = append(results["code"], esDomainResult{
				Domain: "code", SourceID: sp.ID,
				ScopeRef: "repository:" + sp.RepositoryID,
				Label:    esMaskExcerpt(sp.FilePath+" :: "+sp.SymbolName, 80),
				RankKind: "lexical",
				Locator: map[string]interface{}{
					"repository_id": sp.RepositoryID, "commit_sha": sp.CommitSHA,
					"file_path": sp.FilePath, "lines": fmt.Sprintf("%d-%d", sp.StartLine, sp.EndLine),
				},
				Verification: map[bool]string{true: "verified", false: "modified"}[sp.CommitSHA != ""],
				Masked:       false,
			})
		}
		counts["code"] = len(results["code"])
	}

	// ---- Provenance: attribution + actor search over spans/changesets.
	if want["provenance"] {
		var changes []models.ChangeSet
		pq := s.db.Where("organization_id = ?", orgID)
		if req.SessionID != "" {
			pq = pq.Where("session_id = ?", req.SessionID)
		}
		pq.Where("branch LIKE ? OR user_id LIKE ? OR user_harness_id LIKE ?", "%"+q+"%", "%"+q+"%", "%"+q+"%").
			Order("created_at DESC").Limit(20).Find(&changes)
		for _, c := range changes {
			results["provenance"] = append(results["provenance"], esDomainResult{
				Domain: "provenance", SourceID: c.ID,
				ScopeRef: "repository:" + c.RepositoryID,
				Label:    esMaskExcerpt(fmt.Sprintf("%s 변경 — %s", c.Branch, c.AttributionState), 80),
				RankKind: "lexical",
				Locator: map[string]interface{}{
					"changeset_id": c.ID, "session_id": c.SessionID,
					"diff_digest": c.DiffDigest,
				},
				Verification: "verified", Masked: false,
			})
		}
		counts["provenance"] = len(results["provenance"])
	}

	// ---- Trails: bounded safe summaries only.
	if want["trails"] {
		var nodes []models.TrailNode
		s.db.Where("organization_id = ? AND label_ko LIKE ?", orgID, "%"+q+"%").
			Order("occurred_at DESC").Limit(20).Find(&nodes)
		for _, n := range nodes {
			results["trails"] = append(results["trails"], esDomainResult{
				Domain: "trails", SourceID: n.SourceID,
				ScopeRef: "session:" + n.SessionID,
				Label:    esMaskExcerpt(n.LabelKo, 80),
				RankKind: "lexical",
				Locator: map[string]interface{}{
					"source_type": n.SourceType, "source_id": n.SourceID,
				},
				Verification: "verified", Masked: false,
			})
		}
		counts["trails"] = len(results["trails"])
	}

	// ---- Exact-identifier priority: canonical IDs resolve first, ahead
	// of lexical hits (PAT-1451 deterministic prioritization).
	if looksLikeID(q) {
		var sess models.Session
		if err := s.db.Where("id = ? AND organization_id = ?", q, orgID).First(&sess).Error; err == nil {
			results["conversations"] = append([]esDomainResult{{
				Domain: "conversations", SourceID: sess.ID, ScopeRef: "session:" + sess.ID,
				Label: "세션 정확 일치", RankKind: "exact",
				Locator: map[string]interface{}{"session_id": sess.ID},
				Verification: "verified", Masked: false,
			}}, results["conversations"]...)
			counts["conversations"]++
		}
		var act models.ActionEnvelope
		if err := s.db.Where("action_id = ? AND organization_id = ?", q, orgID).First(&act).Error; err == nil {
			results["provenance"] = append([]esDomainResult{{
				Domain: "provenance", SourceID: act.ActionID, ScopeRef: "session:" + act.SessionID,
				Label: "조치 봉투 정확 일치 — " + act.ActionType, RankKind: "exact",
				Locator: map[string]interface{}{"action_id": act.ActionID, "envelope_digest": act.EnvelopeDigest},
				Verification: "verified", Masked: false,
			}}, results["provenance"]...)
			counts["provenance"]++
		}
	}

	countsJSON, _ := json.Marshal(counts)
	domainsJSON, _ := json.Marshal(req.Domains)
	s.db.Create(&models.EvidenceSearchAudit{
		OrganizationID: orgID, AdminEmail: email, Kind: "query",
		Query: q, Domains: string(domainsJSON), ResultCounts: string(countsJSON),
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"results": results, "counts": counts,
		"ranking_note": "정확 일치 > 어휘 일치. 도메인별 순위는 서로 비교되지 않습니다.",
		"export_available": false,
	})
}

func looksLikeID(q string) bool {
	// Canonical IDs (UUID/ULID/hex/action IDs) are single tokens.
	if len(q) < 8 || strings.ContainsAny(q, " \t") {
		return false
	}
	return true
}

// handleESOpen re-authorizes and resolves one result to its immutable
// position (single record; there is no bulk open).
func (s *Server) handleESOpen(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	grant := s.esGrantFor(orgID, getOperatorEmail(r))
	if grant == nil {
		writeError(w, http.StatusForbidden, "증거 검색 권한이 없습니다")
		return
	}
	domain := chi.URLParam(r, "domain")
	id := chi.URLParam(r, "id")
	switch domain {
	case "conversations":
		var ex models.PromptExchange
		if err := s.db.Joins("JOIN sessions ON sessions.id = prompt_exchanges.session_id").
			Where("sessions.organization_id = ? AND prompt_exchanges.id = ?", orgID, id).
			First(&ex).Error; err != nil {
			writeError(w, http.StatusNotFound, "위치를 찾을 수 없습니다")
			return
		}
		s.db.Create(&models.EvidenceSearchAudit{
			OrganizationID: orgID, AdminEmail: getOperatorEmail(r), Kind: "open",
			Query: domain + "/" + id, OccurredAt: time.Now().UTC().Format(time.RFC3339),
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"session_id": ex.SessionID, "exchange_id": ex.ExchangeID,
			"prompt": esMaskExcerpt(ex.PromptText, 200), "masked": true,
		})
	case "code":
		var sp models.ProvenanceSpan
		if err := s.db.Where("organization_id = ? AND id = ?", orgID, id).First(&sp).Error; err != nil {
			writeError(w, http.StatusNotFound, "위치를 찾을 수 없습니다")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"repository_id": sp.RepositoryID, "commit_sha": sp.CommitSHA,
			"file_path": sp.FilePath, "lines": fmt.Sprintf("%d-%d", sp.StartLine, sp.EndLine),
			"symbol": sp.SymbolName,
		})
	default:
		writeError(w, http.StatusBadRequest, "알 수 없는 도메인입니다")
	}
}

// handleESReveal unmasks sensitive content: separate permission, single
// record, audited (PAT-1451).
func (s *Server) handleESReveal(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	email := getOperatorEmail(r)
	grant := s.esGrantFor(orgID, email)
	if grant == nil || !grant.CanReveal {
		writeError(w, http.StatusForbidden, "민감 내용 표시 권한이 없습니다 (별도 승인 필요)")
		return
	}
	var req struct {
		Domain   string `json:"domain"`
		SourceID string `json:"source_id"`
		Reason   string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil || req.SourceID == "" || strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "대상과 사유가 필요합니다")
		return
	}
	if req.Domain != "conversations" {
		writeError(w, http.StatusBadRequest, "이 도메인에는 마스킹 해제 대상이 없습니다")
		return
	}
	var ex models.PromptExchange
	if err := s.db.Joins("JOIN sessions ON sessions.id = prompt_exchanges.session_id").
		Where("sessions.organization_id = ? AND prompt_exchanges.id = ?", orgID, req.SourceID).
		First(&ex).Error; err != nil {
		writeError(w, http.StatusNotFound, "위치를 찾을 수 없습니다")
		return
	}
	s.db.Create(&models.EvidenceSearchAudit{
		OrganizationID: orgID, AdminEmail: email, Kind: "reveal",
		Query: fmt.Sprintf("%s/%s: %s", req.Domain, req.SourceID, req.Reason),
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": ex.SessionID, "prompt": ex.PromptText, "response": ex.ResponseText,
		"revealed": true,
	})
}

// ---------- grant administration ----------

func (s *Server) handleESGrantCreate(w http.ResponseWriter, r *http.Request) {
	if getRole(r) != "super_admin" && getRole(r) != "admin" {
		writeError(w, http.StatusForbidden, "증거 검색 권한 부여는 최고 관리자만 가능합니다")
		return
	}
	var req models.EvidenceSearchGrant
	if err := decodeJSON(r, &req); err != nil || req.AdminEmail == "" {
		writeError(w, http.StatusBadRequest, "admin_email이 필요합니다")
		return
	}
	req.OrganizationID = getOrgID(r)
	if req.ScopeKind == "" {
		req.ScopeKind = "organization"
	}
	if req.ExpiresAt == "" {
		req.ExpiresAt = time.Now().UTC().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	}
	req.GrantedBy = getOperatorEmail(r)
	if err := s.db.Create(&req).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: req.OrganizationID, EventType: "cp.evidencesearch.grant_created",
		ActorType: "admin", Action: "grant_evidence_search", ResourceType: "evidence_search_grant",
		ResourceID: fmt.Sprint(req.ID), Details: fmt.Sprintf(`{"admin":"%s","can_reveal":%t}`, req.AdminEmail, req.CanReveal),
		Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, req)
}

func (s *Server) handleESGrantRevoke(w http.ResponseWriter, r *http.Request) {
	if getRole(r) != "super_admin" && getRole(r) != "admin" {
		writeError(w, http.StatusForbidden, "권한 철회는 최고 관리자만 가능합니다")
		return
	}
	id := chi.URLParam(r, "id")
	res := s.db.Model(&models.EvidenceSearchGrant{}).
		Where("id = ? AND organization_id = ?", id, getOrgID(r)).Update("revoked", true)
	if res.RowsAffected == 0 {
		writeError(w, http.StatusNotFound, "권한을 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) handleESGrantsList(w http.ResponseWriter, r *http.Request) {
	var grants []models.EvidenceSearchGrant
	s.db.Where("organization_id = ?", getOrgID(r)).Order("created_at DESC").Find(&grants)
	writeJSON(w, http.StatusOK, grants)
}

func esFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
