package api

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sessionlifecycle"
	"gorm.io/gorm"
)

// sessions_lifecycle.go: web/02 Sessions plan — consolidated detail (UX6),
// per-exchange decision log (B2), replay/fork (B6), cost breakdown (B7),
// visibility indicator (B8), bulk lifecycle (UX7).

// handleGetSessionDetail consolidates the inspector's fetches into one
// endpoint (UX6): session + timeline + exchanges + provenance. Financial
// usage is served only by the role-gated canonical /sessions/{id}/usage route.
func (s *Server) handleGetSessionDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.Where("organization_id = ? AND (id = ? OR session_id = ?)", orgID, id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	const detailLimit = 100
	transcriptVisible := hasConsolePermission(r, permissionLiveTranscript)
	var actions []models.ActionEnvelope
	actionQuery := s.db.Where("organization_id = ? AND session_id = ?", orgID, sess.SessionID).Order("occurred_at DESC").Limit(detailLimit)
	if !transcriptVisible {
		actionQuery = actionQuery.Omit("action_payload", "cp_signature")
	}
	if err := actionQuery.Find(&actions).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var changeSets []models.ChangeSet
	if err := s.db.Where("organization_id = ? AND session_id = ?", orgID, sess.SessionID).Order("created_at DESC").Limit(detailLimit).Find(&changeSets).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var findings []models.SecurityFinding
	if err := s.db.Where("organization_id = ? AND session_id = ?", orgID, sess.SessionID).Order("occurred_at DESC").Limit(detailLimit).Find(&findings).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var exchanges []models.PromptExchange
	exchangeQuery := s.db.Where("session_id = ?", sess.SessionID).Order("created_at DESC").Limit(detailLimit)
	if !transcriptVisible {
		exchangeQuery = exchangeQuery.Omit("prompt_text", "response_text")
	}
	if err := exchangeQuery.Find(&exchanges).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !transcriptVisible {
		redactActionPayloads(actions)
		for i := range exchanges {
			exchanges[i].PromptText = ""
			exchanges[i].ResponseText = ""
		}
	}
	var spans []models.ProvenanceSpan
	if err := s.db.Where("organization_id = ? AND session_id = ?", orgID, sess.SessionID).Order("created_at DESC").Limit(detailLimit).Find(&spans).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	boundSessionEvidence(actions, changeSets, findings, exchanges, spans)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session":            sess,
		"actions":            actions,
		"change_sets":        changeSets,
		"findings":           findings,
		"exchanges":          exchanges,
		"spans":              spans,
		"transcript_visible": transcriptVisible,
		"timezone":           s.organizationTimezone(orgID),
		"limits":             map[string]int{"actions": detailLimit, "change_sets": detailLimit, "findings": detailLimit, "exchanges": detailLimit, "spans": detailLimit, "text_bytes": sessionEvidenceTextLimit},
	})
}

// handleGetSessionDecisions returns the per-exchange policy decision log
// (B2): every exchange with its verdict + epoch, ordered chronologically.
func (s *Server) handleGetSessionDecisions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.Where("organization_id = ? AND (id = ? OR session_id = ?)", orgID, id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var exchanges []models.PromptExchange
	if err := s.db.Select("exchange_id", "policy_epoch_id", "verdict_result", "model_package_id", "input_tokens", "output_tokens", "created_at").
		Where("session_id = ?", sess.SessionID).Order("created_at DESC").Limit(500).Find(&exchanges).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type decisionRow struct {
		ExchangeID     string `json:"exchange_id"`
		PolicyEpochID  string `json:"policy_epoch_id"`
		Verdict        string `json:"verdict"`
		ModelPackageID string `json:"model_package_id"`
		InputTokens    int    `json:"input_tokens"`
		OutputTokens   int    `json:"output_tokens"`
		At             string `json:"at"`
	}
	rows := make([]decisionRow, 0, len(exchanges))
	for _, ex := range exchanges {
		verdict := ex.VerdictResult
		if verdict == "" {
			verdict = "unrecorded"
		}
		rows = append(rows, decisionRow{
			ExchangeID: ex.ExchangeID, PolicyEpochID: ex.PolicyEpochID,
			Verdict: verdict, ModelPackageID: ex.ModelPackageID,
			InputTokens: ex.InputTokens, OutputTokens: ex.OutputTokens,
			At: ex.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sess.SessionID,
		"decisions":  rows,
	})
}

// handleGetSessionReplay reconstructs the session's governed event
// sequence for replay/fork (B6): spans + actions + change sets in one
// ordered timeline.
func (s *Server) handleGetSessionReplay(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.Where("organization_id = ? AND (id = ? OR session_id = ?)", orgID, id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	type timelineEvent struct {
		At      string      `json:"at"`
		Kind    string      `json:"kind"`
		Payload interface{} `json:"payload"`
	}
	var events []timelineEvent
	var spans []models.ProvenanceSpan
	if err := s.db.Where("organization_id = ? AND session_id = ?", orgID, sess.SessionID).Order("created_at DESC").Limit(100).Find(&spans).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, sp := range spans {
		events = append(events, timelineEvent{At: sp.CreatedAt.Format(time.RFC3339), Kind: "span", Payload: sp})
	}
	var actions []models.ActionEnvelope
	actionQuery := s.db.Where("organization_id = ? AND session_id = ?", orgID, sess.SessionID).Order("occurred_at DESC").Limit(100)
	if !hasConsolePermission(r, permissionLiveTranscript) {
		actionQuery = actionQuery.Omit("action_payload", "cp_signature")
	}
	if err := actionQuery.Find(&actions).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !hasConsolePermission(r, permissionLiveTranscript) {
		redactActionPayloads(actions)
	}
	for _, a := range actions {
		events = append(events, timelineEvent{At: a.OccurredAt, Kind: "action", Payload: a})
	}
	var changeSets []models.ChangeSet
	if err := s.db.Where("organization_id = ? AND session_id = ?", orgID, sess.SessionID).Order("created_at DESC").Limit(100).Find(&changeSets).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	boundSessionEvidence(actions, changeSets, nil, nil, spans)
	for _, cs := range changeSets {
		events = append(events, timelineEvent{At: cs.CreatedAt.Format(time.RFC3339), Kind: "changeset", Payload: cs})
	}
	sort.SliceStable(events, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339Nano, events[i].At)
		right, rightErr := time.Parse(time.RFC3339Nano, events[j].At)
		if leftErr == nil && rightErr == nil {
			return left.Before(right)
		}
		return events[i].At < events[j].At
	})
	truncated := len(events) > 1000
	if truncated {
		events = events[len(events)-1000:]
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session":     sess,
		"replayable":  true,
		"baseline_id": sess.BaselineID,
		"events":      events,
		"truncated":   truncated,
	})
}

func redactActionPayloads(actions []models.ActionEnvelope) {
	for i := range actions {
		actions[i].ActionPayload = ""
	}
}

const sessionEvidenceTextLimit = 16 << 10

func boundedSessionText(value string) string {
	if len(value) <= sessionEvidenceTextLimit {
		return value
	}
	return strings.ToValidUTF8(value[:sessionEvidenceTextLimit], "")
}

func boundSessionEvidence(actions []models.ActionEnvelope, changeSets []models.ChangeSet, findings []models.SecurityFinding, exchanges []models.PromptExchange, spans []models.ProvenanceSpan) {
	for i := range actions {
		actions[i].ActionPayload = boundedSessionText(actions[i].ActionPayload)
		actions[i].CPSignature = boundedSessionText(actions[i].CPSignature)
	}
	for i := range changeSets {
		changeSets[i].FilesChanged = boundedSessionText(changeSets[i].FilesChanged)
		changeSets[i].DiffSummary = boundedSessionText(changeSets[i].DiffSummary)
	}
	for i := range findings {
		findings[i].Description = boundedSessionText(findings[i].Description)
		findings[i].DescriptionKo = boundedSessionText(findings[i].DescriptionKo)
		findings[i].EvidenceJSON = boundedSessionText(findings[i].EvidenceJSON)
	}
	for i := range exchanges {
		exchanges[i].PromptText = boundedSessionText(exchanges[i].PromptText)
		exchanges[i].ResponseText = boundedSessionText(exchanges[i].ResponseText)
	}
	for i := range spans {
		spans[i].ContextRefsJSON = boundedSessionText(spans[i].ContextRefsJSON)
		spans[i].ToolCallRefsJSON = boundedSessionText(spans[i].ToolCallRefsJSON)
		spans[i].PolicyDecisionRefsJSON = boundedSessionText(spans[i].PolicyDecisionRefsJSON)
		spans[i].ParentSpanRefs = boundedSessionText(spans[i].ParentSpanRefs)
	}
}

// handleBulkSessions applies close/pause/terminate to many sessions (UX7).
func (s *Server) handleBulkSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsolePermission(w, r, permissionSessionsManage) {
		return
	}
	orgID := getOrgID(r)
	var req struct {
		IDs    []string `json:"ids"`
		Action string   `json:"action"` // close, pause, terminate
		Reason string   `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 || strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "ids[] + action + reason required")
		return
	}
	if len(req.IDs) > 500 {
		writeError(w, http.StatusBadRequest, "한 번에 최대 500개 · max 500 ids per bulk call")
		return
	}
	target := ""
	switch req.Action {
	case "close":
		target = "closed"
	case "pause":
		target = "paused"
	case "terminate":
		target = "terminated"
	default:
		writeError(w, http.StatusBadRequest, "action must be close|pause|terminate")
		return
	}
	actorID := getActorID(r)
	outcomes, err := s.sessionLifecycle.TransitionMany(orgID, req.IDs, target, "bulk_"+req.Action, req.Reason, actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var affected int64
	skipped := make([]string, 0, 4)
	cleanupFailed := make([]string, 0)
	for _, outcome := range outcomes {
		if outcome.Result == sessionlifecycle.ResultUpdated {
			affected++
			cleanupFailed = append(cleanupFailed, outcome.CleanupFailures...)
		} else {
			skipped = append(skipped, outcome.RequestedID)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"affected": affected, "skipped": skipped, "cleanup_failures": cleanupFailed, "outcomes": outcomes})
}

// handleGetSessionVisibility returns the admin's visibility level for the
// session (B8): derived from the operator's scope vs the session's org —
// org admins get level A (full transcript), others level C (metadata).
func (s *Server) handleGetSessionVisibility(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.Where("organization_id = ? AND (id = ? OR session_id = ?)", orgID, id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	level := "C"
	labels := map[string]string{"A": "전체 열람 (전체 대화/코드)", "B": "부분 열람", "C": "메타데이터만", "D": "접근 제한"}
	if sess.OrganizationID == getOrgID(r) && hasConsolePermission(r, permissionLiveTranscript) {
		level = "A"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sess.SessionID,
		"level":      level,
		"label":      labels[level],
	})
}

type liveSessionRow struct {
	ID             string            `json:"id"`
	SessionID      string            `json:"session_id"`
	Title          string            `json:"title"`
	Status         string            `json:"status"`
	IsLive         bool              `json:"is_live"`
	UserID         string            `json:"user_id,omitempty"`
	UserName       string            `json:"user_name,omitempty"`
	UserEmail      string            `json:"user_email,omitempty"`
	HarnessID      string            `json:"harness_id,omitempty"`
	HarnessName    string            `json:"harness_name,omitempty"`
	HarnessRisk    string            `json:"harness_risk,omitempty"`
	ModelClass     string            `json:"model_class,omitempty"`
	ModelPackageID string            `json:"model_package_id,omitempty"`
	LastActivityAt string            `json:"last_activity_at"`
	OpenedAt       string            `json:"opened_at"`
	Links          map[string]string `json:"links"`
}

// handleLiveSessions returns one bounded, org-scoped snapshot for the Live
// console. It replaces three unbounded polling calls and resolves links only
// for entities that still exist in the authenticated organization.
func (s *Server) handleLiveSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsolePermission(w, r, permissionLiveRead) {
		return
	}
	orgID := getOrgID(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	statuses := models.SessionNonTerminalStatuses()
	base := s.db.Model(&models.Session{}).Where("organization_id = ? AND status IN ?", orgID, statuses)
	if value := strings.TrimSpace(r.URL.Query().Get("user_id")); value != "" {
		base = base.Where("user_id = ?", value)
	}
	if value := strings.TrimSpace(r.URL.Query().Get("project_id")); value != "" {
		base = base.Where("project_id = ?", value)
	}
	if value := strings.TrimSpace(r.URL.Query().Get("model")); value != "" {
		base = base.Where("model_class = ? OR session_id IN (?)", value, s.db.Model(&models.PromptExchange{}).Select("session_id").Where("model_package_id = ?", value))
	}
	if value := strings.TrimSpace(r.URL.Query().Get("risk")); value != "" {
		base = base.Where("harness_id IN (?)", s.db.Model(&models.Harness{}).Select("harness_id").Where("organization_id = ? AND risk_state = ?", orgID, value))
	}
	var activeCount, inProgressCount int64
	if err := base.Session(&gorm.Session{}).Where("status = ?", "active").Count(&activeCount).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := base.Session(&gorm.Session{}).Count(&inProgressCount).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var sessions []models.Session
	if err := base.Session(&gorm.Session{}).
		Order("last_activity_at DESC, opened_at DESC").Limit(limit).Find(&sessions).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	userIDs, harnessIDs, sessionIDs := make([]string, 0, len(sessions)), make([]string, 0, len(sessions)), make([]string, 0, len(sessions))
	for _, sess := range sessions {
		userIDs = append(userIDs, sess.UserID)
		harnessIDs = append(harnessIDs, sess.HarnessID)
		sessionIDs = append(sessionIDs, sess.SessionID)
	}
	usersByID := map[string]models.User{}
	if len(userIDs) > 0 {
		var users []models.User
		if err := s.db.Where("organization_id = ? AND id IN ?", orgID, userIDs).Find(&users).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, user := range users {
			usersByID[user.ID] = user
		}
	}
	harnessesByID := map[string]models.Harness{}
	if len(harnessIDs) > 0 {
		var harnesses []models.Harness
		if err := s.db.Where("organization_id = ? AND harness_id IN ?", orgID, harnessIDs).Find(&harnesses).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, harness := range harnesses {
			harnessesByID[harness.HarnessID] = harness
		}
	}
	modelBySession := map[string]string{}
	if len(sessionIDs) > 0 {
		var exchanges []models.PromptExchange
		if err := s.db.Raw(`
			SELECT * FROM (
				SELECT prompt_exchanges.*,
					ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY created_at DESC, id DESC) AS live_rank
				FROM prompt_exchanges
				WHERE session_id IN ? AND deleted_at IS NULL
			) latest
			WHERE live_rank = 1`, sessionIDs).Scan(&exchanges).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, exchange := range exchanges {
			if _, seen := modelBySession[exchange.SessionID]; !seen && exchange.ModelPackageID != "" {
				modelBySession[exchange.SessionID] = exchange.ModelPackageID
			}
		}
	}
	packageIDs := make([]string, 0, len(modelBySession))
	for _, packageID := range modelBySession {
		packageIDs = append(packageIDs, packageID)
	}
	existingPackages := map[string]bool{}
	if len(packageIDs) > 0 {
		var packages []models.ModelPackage
		if err := s.db.Where("package_id IN ? OR id IN ?", packageIDs, packageIDs).Find(&packages).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, pkg := range packages {
			existingPackages[pkg.PackageID] = true
			existingPackages[pkg.ID] = true
		}
	}

	rows := make([]liveSessionRow, 0, len(sessions))
	for _, sess := range sessions {
		lastActivity := sess.LastActivityAt
		if lastActivity == "" {
			lastActivity = sess.OpenedAt
		}
		row := liveSessionRow{
			ID: sess.ID, SessionID: sess.SessionID, Title: sess.Title, Status: sess.Status,
			IsLive: models.SessionIsLive(sess.Status), UserID: sess.UserID, HarnessID: sess.HarnessID,
			ModelClass: sess.ModelClass, ModelPackageID: modelBySession[sess.SessionID],
			LastActivityAt: lastActivity, OpenedAt: sess.OpenedAt,
			Links: map[string]string{"session": "/sessions/" + url.PathEscape(sess.ID)},
		}
		if user, ok := usersByID[sess.UserID]; ok {
			row.UserName, row.UserEmail = user.NameKo, user.Email
			if row.UserName == "" {
				row.UserName = user.Name
			}
			row.Links["user"] = "/users/" + url.PathEscape(user.ID)
		}
		if harness, ok := harnessesByID[sess.HarnessID]; ok {
			row.HarnessName, row.HarnessRisk = harness.Name, harness.RiskState
			row.Links["harness"] = "/harnesses/" + url.PathEscape(harness.ID)
			row.Links["fleet"] = "/fleet?harness_id=" + url.QueryEscape(harness.HarnessID)
		}
		if row.ModelPackageID != "" && existingPackages[row.ModelPackageID] {
			row.Links["model"] = "/models/" + url.PathEscape(row.ModelPackageID)
		} else if row.ModelClass != "" {
			row.Links["model"] = "/models?class=" + url.QueryEscape(row.ModelClass)
		}
		rows = append(rows, row)
	}

	timezone := s.organizationTimezone(orgID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": rows, "active_count": activeCount, "in_progress_count": inProgressCount,
		"limit": limit, "truncated": inProgressCount > int64(len(rows)), "timezone": timezone,
		"server_time": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) organizationTimezone(orgID string) string {
	timezone := "Asia/Seoul"
	var setting models.OrgSetting
	if err := s.db.Where("organization_id = ? AND key = ?", orgID, "timezone").First(&setting).Error; err == nil && setting.Value != "" {
		if _, err := time.LoadLocation(setting.Value); err == nil {
			timezone = setting.Value
		}
	}
	return timezone
}
