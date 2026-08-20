package api

// Governed public cloud schedules (PAT-1437).
//
// Locked rules enforced here:
//   - PCCP is the scheduling authority; occurrences run server-side on a
//     frozen task/context snapshot — later conversation messages never
//     silently alter a schedule (edits create a new revision).
//   - Occurrence idempotency key = (schedule, revision, intended time):
//     duplicate scheduler delivery cannot repeat an effect.
//   - At most one active run per schedule; overlapping pending
//     occurrences coalesce into the newest.
//   - Catch-up after downtime runs only the most recent missed
//     occurrence within 24 hours; older ones expire.
//   - Transient failures retry at most three times; policy/authorization/
//     malicious-use denials never retry.
//   - Quota/background-slot/risk admission happens before dispatch.
//   - A schedule can narrow but never expand a connection's scopes.
//   - The model sees capability metadata only — credential material
//     stays under the envelope seam and never enters API responses.
//   - Prohibited public-use classes are denied with recorded policy
//     evidence.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// Occurrence states (PAT-1437).
const (
	soPending     = "pending"
	soAdmitted    = "admitted"
	soRunning     = "running"
	soWaitingAuth = "waiting_for_authorization"
	soSucceeded   = "succeeded"
	soFailed      = "failed"
	soDenied      = "denied"
	soExpired     = "expired"
	soCancelled   = "cancelled"
	soCoalesced   = "coalesced"
)

// csDeniedClasses are the prohibited public-use classes. Matching is
// deterministic over the compiled task objective (PAT-1437).
var csDeniedPatterns = []struct {
	Class    string
	Patterns []string
}{
	{"credential_theft", []string{"비밀번호 탈취", "토큰 탈취", "credential theft", "extract api keys", "api 키 추출"}},
	{"phishing", []string{"피싱", "phishing", "사칭 메일"}},
	{"spam_bulk", []string{"대량 발송", "스팸", "bulk unsolicited", "mass dm"}},
	{"malware", []string{"멀웨어", "malware", "랜섬웨어", "ransomware", "키로거", "keylogger"}},
	{"surveillance", []string{"몰래 추적", "감시 도구", " covert tracking", "stalkerware"}},
	{"account_destruction", []string{"계정 삭제", "delete all accounts", "보안 설정 무력화"}},
	{"unauthorized_financial", []string{"무단 이체", "unauthorized transfer", "결제 승인 없이"}},
}

// csScreenObjective returns the denied class or "" (deterministic).
func csScreenObjective(objective string) string {
	low := strings.ToLower(objective)
	for _, d := range csDeniedPatterns {
		for _, p := range d.Patterns {
			if strings.Contains(low, strings.ToLower(p)) {
				return d.Class
			}
		}
	}
	return ""
}

// csNextOccurrence computes the next due time for a trigger in its
// timezone. Supported forms: once, cron with 5 fields (minute hour dom
// month dow) evaluated minute-quantized — DST handled by the tz loader.
func csNextOccurrence(trigger map[string]interface{}, after time.Time) (time.Time, error) {
	kind, _ := trigger["kind"].(string)
	tzName, _ := trigger["timezone"].(string)
	loc := time.UTC
	if tzName != "" {
		if l, err := time.LoadLocation(tzName); err == nil {
			loc = l
		}
	}
	switch kind {
	case "once":
		at, _ := trigger["at"].(string)
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return time.Time{}, fmt.Errorf("once 트리거에 유효한 at이 필요합니다")
		}
		return t.In(loc), nil
	case "cron":
		expr, _ := trigger["expr"].(string)
		return csNextCron(expr, after.In(loc))
	}
	return time.Time{}, fmt.Errorf("트리거 종류는 once|cron이어야 합니다")
}

// csNextCron evaluates a 5-field cron expression minute-quantized. It
// walks forward at most 366 days looking for the next match (skipped/
// repeated DST wall-clocks resolve through absolute zone math).
func csNextCron(expr string, after time.Time) (time.Time, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron 표현식은 5개 필드여야 합니다")
	}
	var min, hour, dom, mon, dow []int
	var err error
	if min, err = csCronField(fields[0], 0, 59); err != nil {
		return time.Time{}, err
	}
	if hour, err = csCronField(fields[1], 0, 23); err != nil {
		return time.Time{}, err
	}
	if dom, err = csCronField(fields[2], 1, 31); err != nil {
		return time.Time{}, err
	}
	if mon, err = csCronField(fields[3], 1, 12); err != nil {
		return time.Time{}, err
	}
	if dow, err = csCronField(fields[4], 0, 6); err != nil {
		return time.Time{}, err
	}
	minSet := toSet(min)
	hourSet := toSet(hour)
	domSet := toSet(dom)
	monSet := toSet(mon)
	dowSet := toSet(dow)
	// Start at the next whole minute strictly after `after`.
	t := after.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 527040; i++ { // 366 days of minutes
		if monSet[int(t.Month())] && domSet[t.Day()] && dowSet[int(t.Weekday())] && hourSet[t.Hour()] && minSet[t.Minute()] {
			return t, nil
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("다음 발생 시점을 찾을 수 없습니다")
}

func csCronField(field string, lo, hi int) ([]int, error) {
	parsePart := func(part string) (int, error) {
		// strconv.Atoi rejects trailing garbage ("5x") that Sscanf
		// silently accepted as 5 — cron fields must be exactly numeric.
		v, err := strconv.Atoi(part)
		if err != nil {
			return 0, fmt.Errorf("cron 필드에 숫자가 아닌 값: %q", part)
		}
		return v, nil
	}
	out := []int{}
	for _, part := range strings.Split(field, ",") {
		step := 1
		if at := strings.Index(part, "/"); at >= 0 {
			v, err := parsePart(part[at+1:])
			if err != nil || v <= 0 {
				return nil, fmt.Errorf("잘못된 cron 단계")
			}
			step = v
			part = part[:at]
		}
		start, end := lo, hi
		if part != "*" {
			if dash := strings.Index(part, "-"); dash >= 0 {
				s, err1 := parsePart(part[:dash])
				e, err2 := parsePart(part[dash+1:])
				if err1 != nil || err2 != nil {
					return nil, fmt.Errorf("잘못된 cron 범위")
				}
				start, end = s, e
			} else {
				v, err := parsePart(part)
				if err != nil {
					return nil, err
				}
				start, end = v, v
			}
		}
		if start < lo || end > hi || start > end {
			return nil, fmt.Errorf("cron 필드 범위 초과")
		}
		for v := start; v <= end; v += step {
			out = append(out, v)
		}
	}
	return out, nil
}

func toSet(vals []int) map[int]bool {
	m := map[int]bool{}
	for _, v := range vals {
		m[v] = true
	}
	return m
}

// ---------- schedules ----------

func (s *Server) handleCSCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskSpec                json.RawMessage `json:"task_spec"`
		ContextSnapshot         json.RawMessage `json:"context_snapshot"`
		Trigger                 json.RawMessage `json:"trigger"`
		Timezone                string          `json:"timezone"`
		CreatedFromConversation string          `json:"created_from_conversation"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var spec map[string]interface{}
	if err := json.Unmarshal(req.TaskSpec, &spec); err != nil {
		writeError(w, http.StatusBadRequest, "작업 명세 JSON이 필요합니다")
		return
	}
	objective, _ := spec["objective"].(string)
	if strings.TrimSpace(objective) == "" {
		writeError(w, http.StatusBadRequest, "작업 목표(objective)가 필요합니다")
		return
	}
	// Malicious-use screen BEFORE registration (deterministic).
	if class := csScreenObjective(objective); class != "" {
		models.CreateAuditEvent(s.db, &models.AuditEvent{
			OrganizationID: getOrgID(r), EventType: "cp.schedule.denied", ActorType: "system",
			Action: "deny_schedule", ResourceType: "cloud_schedule", ResourceID: "draft",
			Details: fmt.Sprintf(`{"class":"%s"}`, class), Result: "denied",
			OccurredAt: time.Now().UTC().Format(time.RFC3339),
		})
		writeError(w, http.StatusUnprocessableEntity, "금지된 용도로 분류되어 등록이 거부되었습니다: "+class)
		return
	}
	var trigger map[string]interface{}
	if err := json.Unmarshal(req.Trigger, &trigger); err != nil {
		writeError(w, http.StatusBadRequest, "트리거 JSON이 필요합니다")
		return
	}
	next, err := csNextOccurrence(trigger, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sched := models.CloudSchedule{
		OwnerUserID: csOwner(r), TaskSpecJSON: string(req.TaskSpec),
		ContextSnapshotJSON: string(req.ContextSnapshot), TriggerJSON: string(req.Trigger),
		State: "active", Revision: 1, NextOccurrenceAt: next.UTC().Format(time.RFC3339),
		Timezone: req.Timezone, CreatedFromConversation: req.CreatedFromConversation,
	}
	if sched.Timezone == "" {
		sched.Timezone = "Asia/Seoul"
	}
	if err := s.db.Create(&sched).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": sched.ID, "state": sched.State, "revision": sched.Revision,
		"next_occurrence_at": sched.NextOccurrenceAt,
		"ack_ko":             "일정을 등록했습니다. 등록 시점의 문맥 스냅샷이 고정되며 이후 대화 내용은 자동 반영되지 않습니다.",
	})
}

func csOwner(r *http.Request) string {
	if claims, ok := claimsFromCtx(r.Context()); ok && claims.Email != "" {
		return claims.Email
	}
	return getOperatorEmail(r)
}

func (s *Server) handleCSList(w http.ResponseWriter, r *http.Request) {
	var schedules []models.CloudSchedule
	s.db.Where("owner_user_id = ? AND state != ?", csOwner(r), "deleted").Order("created_at DESC").Find(&schedules)
	out := make([]map[string]interface{}, 0, len(schedules))
	for _, sc := range schedules {
		var occs []models.ScheduleOccurrence
		s.db.Where("schedule_id = ?", sc.ID).Order("intended_at DESC").Limit(10).Find(&occs)
		out = append(out, map[string]interface{}{
			"id": sc.ID, "state": sc.State, "revision": sc.Revision,
			"task_spec":          json.RawMessage(sc.TaskSpecJSON),
			"trigger":            json.RawMessage(sc.TriggerJSON),
			"next_occurrence_at": sc.NextOccurrenceAt, "timezone": sc.Timezone,
			"occurrences": occs,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCSMutate pauses/resumes/revokes/deletes; edit creates a new
// revision (frozen snapshot semantics).
func (s *Server) handleCSMutate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var sched models.CloudSchedule
	if err := s.db.Where("id = ? AND owner_user_id = ?", id, csOwner(r)).First(&sched).Error; err != nil {
		writeError(w, http.StatusNotFound, "일정을 찾을 수 없습니다")
		return
	}
	var req struct {
		Action   string          `json:"action"` // pause|resume|revoke|delete|edit
		TaskSpec json.RawMessage `json:"task_spec"`
		Trigger  json.RawMessage `json:"trigger"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Action == "" {
		writeError(w, http.StatusBadRequest, "작업이 필요합니다")
		return
	}
	switch req.Action {
	case "pause":
		s.db.Model(&sched).Update("state", "paused")
	case "resume":
		s.db.Model(&sched).Update("state", "active")
	case "revoke":
		s.db.Model(&sched).Update("state", "revoked")
		s.db.Model(&models.ScheduleOccurrence{}).
			Where("schedule_id = ? AND state IN ?", sched.ID, []string{soPending, soAdmitted}).
			Update("state", soCancelled)
	case "delete":
		s.db.Model(&sched).Update("state", "deleted")
		s.db.Model(&models.ScheduleOccurrence{}).
			Where("schedule_id = ? AND state IN ?", sched.ID, []string{soPending, soAdmitted}).
			Update("state", soCancelled)
	case "edit":
		// Edit = new immutable revision; the frozen context snapshot of
		// past occurrences is preserved.
		updates := map[string]interface{}{"revision": sched.Revision + 1}
		if len(req.TaskSpec) > 0 {
			var spec map[string]interface{}
			if err := json.Unmarshal(req.TaskSpec, &spec); err != nil {
				writeError(w, http.StatusBadRequest, "작업 명세 JSON이 올바르지 않습니다")
				return
			}
			if objective, _ := spec["objective"].(string); csScreenObjective(objective) != "" {
				writeError(w, http.StatusUnprocessableEntity, "금지된 용도로 분류되어 수정이 거부되었습니다")
				return
			}
			updates["task_spec_json"] = string(req.TaskSpec)
		}
		if len(req.Trigger) > 0 {
			var trigger map[string]interface{}
			if err := json.Unmarshal(req.Trigger, &trigger); err != nil {
				writeError(w, http.StatusBadRequest, "트리거 JSON이 올바르지 않습니다")
				return
			}
			next, err := csNextOccurrence(trigger, time.Now().UTC())
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			updates["trigger_json"] = string(req.Trigger)
			updates["next_occurrence_at"] = next.UTC().Format(time.RFC3339)
		}
		s.db.Model(&sched).Updates(updates)
	default:
		writeError(w, http.StatusBadRequest, "알 수 없는 작업입니다")
		return
	}
	var fresh models.CloudSchedule
	s.db.First(&fresh, "id = ?", sched.ID)
	writeJSON(w, http.StatusOK, fresh)
}

// ---------- dispatch sweep (idempotent, coalescing, catch-up ≤24h) ----------

// handleCSDispatchSweep admits due occurrences for active schedules.
// Operator-gated: dispatching drives every user's schedules.
func (s *Server) handleCSDispatchSweep(w http.ResponseWriter, r *http.Request) {
	if !inRolesAllowed(getRole(r)) && !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "디스패치 권한이 필요합니다")
		return
	}
	now := time.Now().UTC()
	// Retry pass first: pending occurrences with a due retry become
	// runnable again (the ≤3 bound was enforced at report time). The
	// parent schedule must still be active — a paused or
	// authorization-required schedule must not run retries.
	var retries []models.ScheduleOccurrence
	s.db.Where("state = ? AND next_retry_at != '' AND next_retry_at <= ?", soPending, now.Format(time.RFC3339)).
		Limit(100).Find(&retries)
	for i := range retries {
		occ := &retries[i]
		var parent models.CloudSchedule
		if err := s.db.First(&parent, "id = ?", occ.ScheduleID).Error; err != nil || parent.State != "active" {
			continue
		}
		var activeRuns int64
		s.db.Model(&models.ScheduleOccurrence{}).
			Where("schedule_id = ? AND state IN ?", occ.ScheduleID, []string{soAdmitted, soRunning}).Count(&activeRuns)
		if activeRuns == 0 {
			s.db.Model(&occ).Updates(map[string]interface{}{"state": soAdmitted, "next_retry_at": ""})
		}
	}
	var schedules []models.CloudSchedule
	s.db.Where("state = ?", "active").Find(&schedules)
	admitted, coalesced, expired := 0, 0, 0
	for i := range schedules {
		sc := &schedules[i]
		if sc.NextOccurrenceAt == "" {
			continue
		}
		next, err := time.Parse(time.RFC3339, sc.NextOccurrenceAt)
		if err != nil || now.Before(next) {
			continue
		}
		// Admit the due occurrence idempotently.
		key := fmt.Sprintf("sch-%d-rev-%d-at-%s", sc.ID, sc.Revision, next.UTC().Format(time.RFC3339))
		var dup int64
		s.db.Model(&models.ScheduleOccurrence{}).Where("idempotency_key = ?", key).Count(&dup)
		// One active run per schedule: pending occurrences coalesce
		// whenever an admitted/running occurrence exists (independent of
		// whether this sweep admitted it).
		var activeRuns int64
		s.db.Model(&models.ScheduleOccurrence{}).
			Where("schedule_id = ? AND state IN ?", sc.ID, []string{soAdmitted, soRunning}).Count(&activeRuns)
		if activeRuns > 0 {
			// Only overlaps inside the 24h catch-up window coalesce;
			// older missed occurrences belong to the expiry pass below.
			var pending []models.ScheduleOccurrence
			s.db.Where("schedule_id = ? AND state = ? AND intended_at >= ?", sc.ID, soPending,
				now.Add(-24*time.Hour).Format(time.RFC3339)).Find(&pending)
			for _, p := range pending {
				s.db.Model(&p).Update("state", soCoalesced)
				coalesced++
			}
		}
		if dup == 0 && activeRuns == 0 {
			occ := models.ScheduleOccurrence{
				ScheduleID: sc.ID, Revision: sc.Revision,
				IntendedAt: next.UTC().Format(time.RFC3339), IdempotencyKey: key,
				// Frozen snapshot copied at admission: later edits to the
				// schedule row never change what this run executes.
				TaskSpecJSON:        sc.TaskSpecJSON,
				ContextSnapshotJSON: sc.ContextSnapshotJSON,
				State:               soAdmitted, StartedAt: now.UTC().Format(time.RFC3339),
			}
			s.db.Create(&occ)
			admitted++
		}
		// Advance the schedule's next occurrence.
		var trigger map[string]interface{}
		if err := json.Unmarshal([]byte(sc.TriggerJSON), &trigger); err == nil {
			if nn, err := csNextOccurrence(trigger, next.Add(time.Minute)); err == nil {
				s.db.Model(&sc).Updates(map[string]interface{}{
					"next_occurrence_at": nn.UTC().Format(time.RFC3339),
					"last_occurrence_at": next.UTC().Format(time.RFC3339),
				})
			}
		}
	}
	// Catch-up window: expire pending occurrences older than 24h (only
	// the most recent missed one within 24h may run).
	cutoff := now.Add(-24 * time.Hour).Format(time.RFC3339)
	res := s.db.Model(&models.ScheduleOccurrence{}).
		Where("state = ? AND intended_at < ?", soPending, cutoff).
		Update("state", soExpired)
	expired += int(res.RowsAffected)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"admitted": admitted, "coalesced": coalesced, "expired_older_than_24h": expired,
	})
}

// handleCSReport records occurrence outcome transitions (the runner's
// report). Transient failures retry ≤3; policy/authorization denials
// never retry. Idempotent per state.
func (s *Server) handleCSReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OccurrenceID           uint   `json:"occurrence_id"`
		State                  string `json:"state"` // succeeded|failed|denied|waiting_for_authorization|running
		ResultSummaryKo        string `json:"result_summary_ko"`
		CostTokens             int    `json:"cost_tokens"`
		DenyReason             string `json:"deny_reason"`
		CredentialFingerprints string `json:"credential_fingerprints"`
	}
	if err := decodeJSON(r, &req); err != nil || req.OccurrenceID == 0 {
		writeError(w, http.StatusBadRequest, "occurrence_id와 state가 필요합니다")
		return
	}
	var occ models.ScheduleOccurrence
	if err := s.db.First(&occ, "id = ?", req.OccurrenceID).Error; err != nil {
		writeError(w, http.StatusNotFound, "발생을 찾을 수 없습니다")
		return
	}
	// Owner check: occurrences belong to the schedule's owner; another
	// user must never mutate someone else's run state.
	var parent models.CloudSchedule
	if err := s.db.First(&parent, "id = ?", occ.ScheduleID).Error; err != nil || parent.OwnerUserID != csOwner(r) {
		writeError(w, http.StatusForbidden, "본인 일정의 발생만 보고할 수 있습니다")
		return
	}
	switch req.State {
	case soSucceeded, soDenied, soCoalesced:
		// Terminal — no retry.
	case soFailed:
		// Transient retry at most three times, bounded backoff.
		if occ.Attempts >= 3 {
			req.State = soFailed // stays failed; no further retry scheduling
		} else {
			occ.Attempts++
			s.db.Model(&occ).Updates(map[string]interface{}{
				"attempts":      occ.Attempts,
				"next_retry_at": time.Now().UTC().Add(time.Duration(occ.Attempts) * 5 * time.Minute).Format(time.RFC3339),
				"state":         soPending, "result_summary_ko": req.ResultSummaryKo,
			})
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": "retry_scheduled", "attempts": occ.Attempts})
			return
		}
	case soWaitingAuth, soRunning:
		// Authorization pause: schedule enters authorization_required.
		if req.State == soWaitingAuth {
			s.db.Model(&models.CloudSchedule{}).Where("id = ?", occ.ScheduleID).
				Update("state", "authorization_required")
		}
	default:
		writeError(w, http.StatusBadRequest, "알 수 없는 상태입니다")
		return
	}
	updates := map[string]interface{}{"state": req.State}
	if req.ResultSummaryKo != "" {
		updates["result_summary_ko"] = req.ResultSummaryKo
	}
	if req.CostTokens > 0 {
		updates["cost_tokens"] = req.CostTokens
	}
	if req.DenyReason != "" {
		updates["deny_reason"] = req.DenyReason
	}
	if req.CredentialFingerprints != "" {
		updates["credential_fingerprints"] = req.CredentialFingerprints
	}
	if req.State == soSucceeded || req.State == soDenied {
		updates["finished_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	if req.State == soSucceeded {
		// A succeeded occurrence returns an authorization_required
		// schedule to active (reauthorization completed path is separate).
		s.db.Model(&models.CloudSchedule{}).Where("id = ? AND state = ?", occ.ScheduleID, "authorization_required").
			Update("state", "active")
	}
	s.db.Model(&occ).Updates(updates)
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": req.State})
}

// ---------- capabilities + delegation ----------

// handleCSCapabilitiesList returns the capability matrix the model may
// see: metadata and connection state ONLY — never tokens, refresh
// credentials, or provider OAuth mechanics (PAT-1437).
func (s *Server) handleCSCapabilitiesList(w http.ResponseWriter, r *http.Request) {
	var caps []models.AccountCapability
	s.db.Where("owner_user_id = ?", csOwner(r)).Find(&caps)
	out := make([]map[string]interface{}, 0, len(caps))
	for _, c := range caps {
		out = append(out, map[string]interface{}{
			"capability_id": c.CapabilityID, "kind": c.Kind, "display_ko": c.DisplayKo,
			"state": c.State, "cloud_executable": c.CloudExecutable, "version": c.Version,
			// No connection credentials, tokens, or authorization-server
			// internals in this response by construction.
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCSConnectFlow starts (or reflects) the PCCP-owned authorization
// flow for a capability. OAuth/browser interaction happens outside the
// transcript; completion updates the same account-level connection.
func (s *Server) handleCSConnectFlow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CapabilityID  string `json:"capability_id"`
		InitiatedFrom string `json:"initiated_from"` // harness | web
	}
	if err := decodeJSON(r, &req); err != nil || req.CapabilityID == "" {
		writeError(w, http.StatusBadRequest, "capability_id가 필요합니다")
		return
	}
	owner := csOwner(r)
	var cap models.AccountCapability
	if err := s.db.Where("owner_user_id = ? AND capability_id = ?", owner, req.CapabilityID).First(&cap).Error; err != nil {
		writeError(w, http.StatusNotFound, "역량을 찾을 수 없습니다")
		return
	}
	conn := models.CapabilityConnection{
		OwnerUserID: owner, CapabilityID: req.CapabilityID,
		State: "authorization_required", InitiatedFrom: req.InitiatedFrom,
	}
	s.db.Create(&conn)
	s.db.Model(&cap).Updates(map[string]interface{}{
		"state": "authorization_required", "connection_id": conn.ID,
	})
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":      "authorization_required",
		"connect_url": "/portal/integrations?capability=" + req.CapabilityID,
		"note_ko":     "연결 완료는 계정 수준 연결을 갱신합니다. 브라우저 인증은 대화 밖에서 진행됩니다.",
	})
}

// handleCSDelegation grants a schedule a narrowed subset of a
// connection's scopes (never an expansion), optionally with the
// disclosed standing authorization for consequential actions.
func (s *Server) handleCSDelegation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScheduleID    uint              `json:"schedule_id"`
		CapabilityID  string            `json:"capability_id"`
		Scopes        []string          `json:"scopes"`
		Consequential bool              `json:"consequential"`
		Disclosure    map[string]string `json:"disclosure"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ScheduleID == 0 || req.CapabilityID == "" {
		writeError(w, http.StatusBadRequest, "schedule_id와 capability_id가 필요합니다")
		return
	}
	owner := csOwner(r)
	var sched models.CloudSchedule
	if err := s.db.Where("id = ? AND owner_user_id = ?", req.ScheduleID, owner).First(&sched).Error; err != nil {
		writeError(w, http.StatusNotFound, "일정을 찾을 수 없습니다")
		return
	}
	var conn models.CapabilityConnection
	if err := s.db.Where("owner_user_id = ? AND capability_id = ? AND state = ?", owner, req.CapabilityID, "connected").First(&conn).Error; err != nil {
		writeError(w, http.StatusUnprocessableEntity, "연결된 역량만 위임할 수 있습니다. 먼저 연결을 완료하세요")
		return
	}
	var granted []string
	json.Unmarshal([]byte(conn.ScopesJSON), &granted)
	grantedSet := map[string]bool{}
	for _, g := range granted {
		grantedSet[g] = true
	}
	// Narrow-only: every requested scope must already be granted.
	for _, sc := range req.Scopes {
		if !grantedSet[sc] {
			writeError(w, http.StatusForbidden, fmt.Sprintf("위임은 연결 범위를 확장할 수 없습니다: %s", sc))
			return
		}
	}
	// Cross-service consequential standing authorization requires the
	// disclosure (source/destination/data class/operation) up front.
	if req.Consequential {
		for _, need := range []string{"source", "destination", "data_class", "operation"} {
			if strings.TrimSpace(req.Disclosure[need]) == "" {
				writeError(w, http.StatusBadRequest, "결과적 승인에는 출처 · 대상 · 데이터 등급 · 조작의 사전 공개가 필요합니다")
				return
			}
		}
	}
	raw, _ := json.Marshal(req.Scopes)
	disc, _ := json.Marshal(req.Disclosure)
	deleg := models.ScheduleDelegation{
		ScheduleID: req.ScheduleID, CapabilityID: req.CapabilityID,
		ScopesJSON: string(raw), Consequential: req.Consequential,
		DisclosureJSON: string(disc), GrantedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.db.Create(&deleg).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, deleg)
}
