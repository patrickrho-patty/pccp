package api

// Read-only SCM code-lineage observation (PAT-1453).
//
// Locked rules enforced here:
//   - Provider adapters (Patty Git / GitLab / GitHub+GHES) sit behind
//     one normalized contract; base URLs are provider-specific; there
//     are no provider-side mutation paths anywhere in this domain.
//   - Webhooks are signature-verified per provider convention,
//     replay-safe and idempotent via (provider, event/delivery ID)
//     unique constraints.
//   - Attribution requires a digest-verifiable match between the
//     repository commit patch and Patty's recorded ChangeSet digest;
//     timestamp/message/author/branch heuristics never bind.
//   - Git author and committer stay distinct facts.
//   - Lineage categories accumulate — a human edit to AI-created code
//     records a new category without rewriting the earlier one.
//   - Access revocation stops synchronization and reads immediately.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// Provider identifiers (PAT-1453).
const (
	scmProviderPattyGit = "patty_git"
	scmProviderGitLab   = "gitlab"
	scmProviderGitHub   = "github"
)

var scmProviderKo = map[string]string{
	scmProviderPattyGit: "Patty Git",
	scmProviderGitLab:   "GitLab",
	scmProviderGitHub:   "GitHub",
}

var scmLineageKo = map[string]string{
	"ai_created": "AI 작성", "human_created": "사람 작성",
	"human_modified_ai": "사람이 AI 코드 수정", "ai_modified_human": "AI가 사람 코드 수정",
	"mixed": "혼합", "imported_unverifiable": "가져옴/검증 불가",
}

// ---------- provider adapter boundary ----------

// scmAdapter normalizes one provider's webhook verification and event
// parsing. Adapter logic stays per-provider; the domain speaks only the
// normalized contract.
type scmAdapter interface {
	VerifyWebhook(secret string, header http.Header, body []byte) bool
	ParseEvent(body []byte) (providerEventID, deliveryID, eventType, actor, ref, sha string, err error)
}

type pattyGitAdapter struct{}
type gitLabAdapter struct{}
type gitHubAdapter struct{}

// Patty Git: x-patty-signature = hex(HMAC-SHA256(secret, body)).
func (pattyGitAdapter) VerifyWebhook(secret string, h http.Header, body []byte) bool {
	return scmVerifyHMACHex(secret, h.Get("X-Patty-Signature"), body)
}
func (pattyGitAdapter) ParseEvent(body []byte) (string, string, string, string, string, string, error) {
	var e struct {
		EventID string `json:"event_id"`
		Type    string `json:"type"`
		Actor   string `json:"actor"`
		Ref     string `json:"ref"`
		After   string `json:"after"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return "", "", "", "", "", "", err
	}
	return e.EventID, e.EventID, e.Type, e.Actor, e.Ref, e.After, nil
}

// GitLab: x-gitlab-token shared-secret comparison (constant-time).
func (gitLabAdapter) VerifyWebhook(secret string, h http.Header, body []byte) bool {
	tok := h.Get("X-Gitlab-Token")
	return secret != "" && tok != "" && hmac.Equal([]byte(tok), []byte(secret))
}
func (gitLabAdapter) ParseEvent(body []byte) (string, string, string, string, string, string, error) {
	var e struct {
		ObjectKind string `json:"object_kind"`
		EventUUID  string `json:"event_uuid"`
		Before     string `json:"before"`
		After      string `json:"after"`
		Ref        string `json:"ref"`
		User       struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return "", "", "", "", "", "", err
	}
	etype := map[string]string{"push": "push", "merge_request": "pr_opened", "pipeline": "check", "note": "review"}[e.ObjectKind]
	if e.ObjectKind == "push" && strings.HasPrefix(e.Before, "0000000") {
		etype = "branch_create"
	}
	return e.EventUUID, e.EventUUID, etype, e.User.Username, e.Ref, e.After, nil
}

// GitHub: x-hub-signature-256 = "sha256=" + hex(HMAC-SHA256).
func (gitHubAdapter) VerifyWebhook(secret string, h http.Header, body []byte) bool {
	sig := strings.TrimPrefix(h.Get("X-Hub-Signature-256"), "sha256=")
	return scmVerifyHMACHex(secret, sig, body)
}
func (gitHubAdapter) ParseEvent(body []byte) (string, string, string, string, string, string, error) {
	var e struct {
		DeliveryID string `json:"delivery_id"`
		After      string `json:"after"`
		Ref        string `json:"ref"`
		Sender     struct {
			Login string `json:"login"`
		} `json:"sender"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return "", "", "", "", "", "", err
	}
	// Only push-like deliveries (carrying a ref) are classified as push;
	// other event kinds stay neutral rather than mislabeled.
	if e.Ref == "" {
		return e.DeliveryID, e.DeliveryID, "observed", e.Sender.Login, "", "", nil
	}
	etype := "push"
	if e.After == "0000000000000000000000000000000000000000" {
		etype = "branch_delete"
	}
	return e.DeliveryID, e.DeliveryID, etype, e.Sender.Login, e.Ref, e.After, nil
}

func scmAdapterFor(provider string) scmAdapter {
	switch provider {
	case scmProviderGitLab:
		return gitLabAdapter{}
	case scmProviderGitHub:
		return gitHubAdapter{}
	default:
		return pattyGitAdapter{}
	}
}

func scmVerifyHMACHex(secret, hexSig string, body []byte) bool {
	if secret == "" || hexSig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(hexSig))
}

// ---------- connections ----------

func (s *Server) handleSCMConnectionCreate(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "관측 연결 관리 권한이 필요합니다")
		return
	}
	var req struct {
		Provider      string `json:"provider"`
		BaseURL       string `json:"base_url"`
		CredentialRef string `json:"credential_ref"`
		WebhookSecret string `json:"webhook_secret"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Provider {
	case scmProviderPattyGit, scmProviderGitLab, scmProviderGitHub:
	default:
		writeError(w, http.StatusBadRequest, "provider는 patty_git|gitlab|github이어야 합니다")
		return
	}
	if req.Provider != scmProviderPattyGit && !strings.HasPrefix(req.BaseURL, "https://") {
		writeError(w, http.StatusBadRequest, "gitlab/github 연결은 https base_url이 필요합니다")
		return
	}
	conn := models.SCMProviderConnection{
		OrganizationID: getOrgID(r), Provider: req.Provider, BaseURL: req.BaseURL,
		CredentialRef: req.CredentialRef, WebhookSecret: req.WebhookSecret, Health: "healthy",
	}
	if err := s.db.Create(&conn).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: conn.OrganizationID, EventType: "cp.lineage.connection_created",
		ActorType: "admin", Action: "create_scm_connection", ResourceType: "scm_provider_connection",
		ResourceID: fmt.Sprint(conn.ID), Details: fmt.Sprintf(`{"provider":"%s","base_url":"%s"}`, req.Provider, req.BaseURL),
		Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": conn.ID, "provider": conn.Provider, "base_url": conn.BaseURL,
		"webhook_url": "/api/scm/observation/webhooks/" + fmt.Sprint(conn.ID),
	})
}

func (s *Server) handleSCMConnectionsList(w http.ResponseWriter, r *http.Request) {
	var conns []models.SCMProviderConnection
	s.db.Where("organization_id = ?", getOrgID(r)).Find(&conns)
	out := make([]map[string]interface{}, 0, len(conns))
	for _, c := range conns {
		out = append(out, map[string]interface{}{
			"id": c.ID, "provider": c.Provider, "provider_ko": scmProviderKo[c.Provider],
			"base_url": c.BaseURL, "webhook_verified": c.WebhookVerified,
			"health": c.Health, "last_reconciliation": c.LastReconciliation,
			"known_gaps": c.KnownGaps, "sync_cursor": c.SyncCursor,
			// Credential never exposed.
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSCMConnectionRevoke immediately stops synchronization and reads.
func (s *Server) handleSCMConnectionRevoke(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "관측 연결 관리 권한이 필요합니다")
		return
	}
	id := chi.URLParam(r, "id")
	res := s.db.Model(&models.SCMProviderConnection{}).
		Where("id = ? AND organization_id = ?", id, getOrgID(r)).
		Update("health", "revoked")
	if res.RowsAffected == 0 {
		writeError(w, http.StatusNotFound, "연결을 찾을 수 없습니다")
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.lineage.connection_revoked", ActorType: "admin",
		Action: "revoke_scm_connection", ResourceType: "scm_provider_connection", ResourceID: id,
		Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// ---------- webhook ingestion ----------

// handleSCMObservationWebhook verifies, deduplicates, and normalizes one
// provider delivery (PAT-1453: replay-safe, idempotent).
func (s *Server) handleSCMObservationWebhook(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "connId")
	var conn models.SCMProviderConnection
	if err := s.db.Where("id = ?", connID).First(&conn).Error; err != nil {
		writeError(w, http.StatusNotFound, "연결을 찾을 수 없습니다")
		return
	}
	if conn.Health == "revoked" {
		writeError(w, http.StatusForbidden, "철회된 연결입니다")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "본문을 읽을 수 없습니다")
		return
	}
	adapter := scmAdapterFor(conn.Provider)
	if !adapter.VerifyWebhook(conn.WebhookSecret, r.Header, body) {
		writeError(w, http.StatusBadRequest, "웹훅 서명이 올바르지 않습니다")
		return
	}
	eventID, deliveryID, eventType, actor, ref, sha, err := adapter.ParseEvent(body)
	if err != nil || eventID == "" {
		writeError(w, http.StatusBadRequest, "이벤트를 해석할 수 없습니다")
		return
	}
	// Idempotency per connection: duplicate delivery/event → inert.
	// (Provider IDs are not globally unique across self-hosted
	// instances, so dedup must be connection-scoped.)
	var dup int64
	s.db.Model(&models.ObservedRepositoryEvent{}).
		Where("connection_id = ? AND (provider_event_id = ? OR provider_delivery_id = ?)", conn.ID, eventID, deliveryID).
		Count(&dup)
	if dup > 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate_ignored"})
		return
	}
	sum := sha256.Sum256(body)
	ev := models.ObservedRepositoryEvent{
		OrganizationID: conn.OrganizationID, ConnectionID: conn.ID, Provider: conn.Provider,
		ProviderEventID: eventID, ProviderDeliveryID: deliveryID,
		EventType: eventType, Actor: actor, Ref: ref, CommitSHA: sha,
		PayloadDigest: hex.EncodeToString(sum[:12]), IngestedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.db.Create(&ev).Error; err != nil {
		// Only unique-constraint races are duplicate deliveries; real
		// failures surface as errors instead of silent success.
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate_ignored"})
			return
		}
		writeError(w, http.StatusInternalServerError, "이벤트 저장 실패")
		return
	}
	// Force pushes are recorded as ref rewrites (evidence preserved).
	if strings.HasPrefix(ref, "refs/heads/") && sha == "" && eventType == "push" {
		s.db.Model(&ev).Update("event_type", "force_push")
	}
	s.db.Model(&conn).Update("last_reconciliation", time.Now().UTC().Format(time.RFC3339))
	writeJSON(w, http.StatusCreated, map[string]interface{}{"event_id": eventID, "type": eventType})
}

func (s *Server) handleSCMEventsList(w http.ResponseWriter, r *http.Request) {
	q := s.db.Where("organization_id = ?", getOrgID(r))
	if v := r.URL.Query().Get("connection_id"); v != "" {
		q = q.Where("connection_id = ?", v)
	}
	if v := r.URL.Query().Get("type"); v != "" {
		q = q.Where("event_type = ?", v)
	}
	var events []models.ObservedRepositoryEvent
	q.Order("created_at DESC").Limit(200).Find(&events)
	writeJSON(w, http.StatusOK, events)
}

// ---------- evidence-backed attribution ----------

// handleSCMBindAttribution binds a commit to Patty change evidence ONLY
// on a digest match (PAT-1453: heuristics never bind).
func (s *Server) handleSCMBindAttribution(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "귀속 관리 권한이 필요합니다")
		return
	}
	var req struct {
		ProviderRepoID string `json:"provider_repo_id"`
		CommitSHA      string `json:"commit_sha"`
		PatchDigest    string `json:"patch_digest"` // must equal a recorded ChangeSet.DiffDigest
		GitAuthor      string `json:"git_author"`
		GitCommitter   string `json:"git_committer"`
	}
	if err := decodeJSON(r, &req); err != nil || req.CommitSHA == "" || req.PatchDigest == "" {
		writeError(w, http.StatusBadRequest, "commit_sha와 patch_digest가 필요합니다")
		return
	}
	orgID := getOrgID(r)
	// Find the recorded change-set whose diff digest matches exactly.
	var cs models.ChangeSet
	err := s.db.Where("organization_id = ? AND diff_digest = ?", orgID, req.PatchDigest).First(&cs).Error
	// Even unmatched commits are ingested as imported/unverifiable —
	// evidence honesty, not silent drops.
	attr := models.CommitAttribution{
		OrganizationID: orgID, ProviderRepoID: req.ProviderRepoID, CommitSHA: req.CommitSHA,
		GitAuthor: req.GitAuthor, GitCommitter: req.GitCommitter,
		Lineage: "imported_unverifiable", DerivationVersion: 1,
		ObservedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err == nil && cs.DiffDigest == req.PatchDigest {
		attr.ChangeSetID = cs.ID
		attr.EvidenceDigest = cs.DiffDigest
		attr.SessionID = cs.SessionID
		attr.UserID = cs.UserID
		attr.HarnessID = cs.HarnessID
		attr.Authoritative = true
		// Category derives from the change-set's recorded attribution
		// state — never from message conventions or timing.
		attr.Lineage = map[string]string{
			"AI_GENERATED":           "ai_created",
			"HUMAN_WRITTEN":          "human_created",
			"AI_THEN_HUMAN_EDITED":   "human_modified_ai",
			"HUMAN_THEN_AI_ASSISTED": "ai_modified_human",
		}[cs.AttributionState]
		if attr.Lineage == "" {
			attr.Lineage = "mixed"
		}
	}
	if err := s.db.Create(&attr).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: orgID, EventType: "cp.lineage.attribution_bound", ActorType: "admin",
		Action: "bind_attribution", ResourceType: "commit_attribution", ResourceID: attr.CommitSHA,
		Details: fmt.Sprintf(`{"authoritative":%t,"lineage":"%s"}`, attr.Authoritative, attr.Lineage),
		Result:  "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"commit_sha": attr.CommitSHA, "lineage": attr.Lineage,
		"lineage_ko": scmLineageKo[attr.Lineage], "authoritative": attr.Authoritative,
	})
}

// handleSCMLineage renders the current-line lineage view: per commit/
// file, the evidence chain behind code that remains in a revision.
// Ambiguity is shown as mixed/imported — never guessed.
func (s *Server) handleSCMLineage(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	repoID := r.URL.Query().Get("repo")
	q := s.db.Where("organization_id = ?", orgID)
	if repoID != "" {
		q = q.Where("provider_repo_id = ?", repoID)
	}
	var attrs []models.CommitAttribution
	q.Order("observed_at DESC").Limit(200).Find(&attrs)
	// Per-file current-line view from commit-pinned spans, scoped to the
	// requested repository.
	fileFilter := r.URL.Query().Get("path")
	spans := []models.ProvenanceSpan{}
	if repoID != "" {
		sq := s.db.Where("organization_id = ? AND repository_id = ?", orgID, repoID)
		if fileFilter != "" {
			sq = sq.Where("file_path LIKE ?", "%"+fileFilter+"%")
		}
		sq.Limit(200).Find(&spans)
	}
	out := make([]map[string]interface{}, 0, len(attrs))
	for _, a := range attrs {
		row := map[string]interface{}{
			"commit_sha": a.CommitSHA, "lineage": a.Lineage,
			"lineage_ko":    scmLineageKo[a.Lineage],
			"authoritative": a.Authoritative,
			"git_author":    a.GitAuthor, "git_committer": a.GitCommitter,
			"author_distinct": a.GitAuthor != "" && a.GitCommitter != "" && a.GitAuthor != a.GitCommitter,
			"observed_at":     a.ObservedAt,
		}
		if a.Authoritative {
			row["changeset_id"] = a.ChangeSetID
			row["session_id"] = a.SessionID
			row["evidence_digest"] = a.EvidenceDigest
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"commits": out, "spans": spans,
		"legend": map[string]string{
			"ai_created": scmLineageKo["ai_created"], "human_created": scmLineageKo["human_created"],
			"human_modified_ai": scmLineageKo["human_modified_ai"], "ai_modified_human": scmLineageKo["ai_modified_human"],
			"mixed": scmLineageKo["mixed"], "imported_unverifiable": scmLineageKo["imported_unverifiable"],
		},
	})
}

// handleSCMReconcile marks connections stale when reconciliation lags —
// never implying completeness (PAT-1453).
func (s *Server) handleSCMReconcile(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "조정 권한이 필요합니다")
		return
	}
	staleBefore := time.Now().UTC().Add(-6 * time.Hour).Format(time.RFC3339)
	res := s.db.Model(&models.SCMProviderConnection{}).
		Where("organization_id = ? AND health = ? AND (last_reconciliation = '' OR last_reconciliation < ?)",
			getOrgID(r), "healthy", staleBefore).
		Update("health", "stale")
	writeJSON(w, http.StatusOK, map[string]interface{}{"marked_stale": res.RowsAffected})
}
