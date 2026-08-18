package sessionlifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Result string

const (
	ResultUpdated           Result = "updated"
	ResultNotFound          Result = "not_found"
	ResultInvalidTransition Result = "invalid_transition"
	ResultConflict          Result = "conflict"
	ResultFailed            Result = "failed"
)

type Request struct {
	OrganizationID string
	SessionRef     string
	Target         string
	Action         string
	Reason         string
	ActorID        string
	ActorType      string
	ForceTerminal  bool
	// Optional optimistic guards used by automatic lifecycle enforcement.
	// They prevent a stale sweep candidate from overwriting fresh activity or
	// a concurrently changed timeout policy.
	ExpectedLastActivityAt *string
	ExpectedIdleTTL        *int
}

type Outcome struct {
	RequestedID     string   `json:"requested_id"`
	SessionID       string   `json:"session_id,omitempty"`
	RecordID        string   `json:"record_id,omitempty"`
	From            string   `json:"from,omitempty"`
	To              string   `json:"to"`
	Result          Result   `json:"result"`
	CleanupFailures []string `json:"cleanup_failures,omitempty"`
	Error           string   `json:"error,omitempty"`
}

type Scope struct {
	OrganizationID string
	HarnessID      string
	UserID         string
	ProjectID      string
	SessionRefs    []string
	ForceTerminal  bool
	ActorType      string
}

type CleanupFunc func(orgID, sessionID string) []string
type NotifyFunc func(orgID, sessionID, status string)
type NotifyScopeFunc func(orgID, status string, count int)

type Service struct {
	db          *gorm.DB
	mu          sync.RWMutex
	cleanup     CleanupFunc
	notify      NotifyFunc
	notifyScope NotifyScopeFunc
}

func New(db *gorm.DB) *Service { return &Service{db: db} }

func (s *Service) SetCleanup(cleanup CleanupFunc) {
	s.mu.Lock()
	s.cleanup = cleanup
	s.mu.Unlock()
}

func (s *Service) SetNotifier(notify NotifyFunc) {
	s.mu.Lock()
	s.notify = notify
	s.mu.Unlock()
}

func (s *Service) SetScopeNotifier(notify NotifyScopeFunc) {
	s.mu.Lock()
	s.notifyScope = notify
	s.mu.Unlock()
}

func (s *Service) hooks() (CleanupFunc, NotifyFunc, NotifyScopeFunc) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cleanup, s.notify, s.notifyScope
}

func (s *Service) Transition(req Request) Outcome {
	out := Outcome{RequestedID: req.SessionRef, To: req.Target}
	if strings.TrimSpace(req.OrganizationID) == "" || strings.TrimSpace(req.SessionRef) == "" {
		out.Result = ResultNotFound
		return out
	}
	var sess models.Session
	err := s.db.Where("organization_id = ? AND (id = ? OR session_id = ?)", req.OrganizationID, req.SessionRef, req.SessionRef).First(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		out.Result = ResultNotFound
		return out
	}
	if err != nil {
		out.Result, out.Error = ResultFailed, err.Error()
		return out
	}
	return s.transitionLoaded(sess, req)
}

// TransitionMany resolves the requested identifiers in one org-scoped query,
// then emits one durable outcome per input identifier in the original order.
func (s *Service) TransitionMany(orgID string, refs []string, target, action, reason, actorID string) ([]Outcome, error) {
	if strings.TrimSpace(orgID) == "" {
		return nil, errors.New("organization_id is required")
	}
	var sessions []models.Session
	if err := s.db.Where("organization_id = ? AND (id IN ? OR session_id IN ?)", orgID, refs, refs).Find(&sessions).Error; err != nil {
		return nil, err
	}
	byRef := make(map[string]models.Session, len(sessions)*2)
	for _, sess := range sessions {
		byRef[sess.ID], byRef[sess.SessionID] = sess, sess
	}
	outcomes := make([]Outcome, 0, len(refs))
	for _, ref := range refs {
		req := Request{OrganizationID: orgID, SessionRef: ref, Target: target, Action: action, Reason: reason, ActorID: actorID, ActorType: "admin"}
		if sess, ok := byRef[ref]; ok {
			outcomes = append(outcomes, s.transitionLoaded(sess, req))
			continue
		}
		out := Outcome{RequestedID: ref, RecordID: ref, To: target, Result: ResultNotFound}
		if err := s.recordOutcome(s.db, req, out); err != nil {
			out.Result, out.Error = ResultFailed, "audit outcome: "+err.Error()
		}
		outcomes = append(outcomes, out)
	}
	return outcomes, nil
}

func (s *Service) transitionLoaded(sess models.Session, req Request) Outcome {
	return s.transitionLoadedMode(sess, req, true)
}

func (s *Service) transitionLoadedMode(sess models.Session, req Request, runHooks bool) Outcome {
	return s.transitionLoadedModeAtomic(sess, req, runHooks, true)
}

func (s *Service) transitionLoadedModeAtomic(sess models.Session, req Request, runHooks, wrapTransaction bool) Outcome {
	out := Outcome{RequestedID: req.SessionRef, SessionID: sess.SessionID, RecordID: sess.ID, From: sess.Status, To: req.Target}
	if !models.SessionTransitionAllowed(sess.Status, req.Target) && !(req.ForceTerminal && models.SessionIsTerminal(req.Target) && !models.SessionIsTerminal(sess.Status)) {
		out.Result = ResultInvalidTransition
		if err := s.recordOutcome(s.db, req, out); err != nil {
			out.Result, out.Error = ResultFailed, "audit outcome: "+err.Error()
		}
		return out
	}
	now := time.Now().UTC()
	updates := map[string]interface{}{"status": req.Target, "last_activity_at": now.Format(time.RFC3339)}
	if models.SessionIsTerminal(req.Target) {
		updates["closed_at"] = now.Format(time.RFC3339)
	}
	apply := func(tx *gorm.DB) error {
		query := tx.Model(&models.Session{}).
			Where("id = ? AND organization_id = ? AND status = ?", sess.ID, req.OrganizationID, sess.Status)
		if req.ExpectedLastActivityAt != nil {
			if *req.ExpectedLastActivityAt == "" {
				query = query.Where("last_activity_at IS NULL OR last_activity_at = ''")
			} else {
				query = query.Where("last_activity_at = ?", *req.ExpectedLastActivityAt)
			}
		}
		if req.ExpectedIdleTTL != nil {
			query = query.Where("idle_ttl = ?", *req.ExpectedIdleTTL)
		}
		result := query.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			out.Result = ResultConflict
		} else {
			out.Result = ResultUpdated
		}
		return s.recordOutcome(tx, req, out)
	}
	var err error
	if wrapTransaction {
		err = s.db.Transaction(apply)
	} else {
		err = apply(s.db)
	}
	if err != nil {
		out.Result, out.Error = ResultFailed, err.Error()
		return out
	}
	if out.Result != ResultUpdated {
		return out
	}
	if !runHooks {
		return out
	}
	s.finalizeOutcome(&out, req)
	return out
}

func (s *Service) finalizeOutcome(out *Outcome, req Request) {
	cleanup, notify, _ := s.hooks()
	if models.SessionIsTerminal(req.Target) && cleanup != nil {
		out.CleanupFailures = cleanup(req.OrganizationID, out.SessionID)
	}
	if notify != nil {
		notify(req.OrganizationID, out.SessionID, req.Target)
	}
	if len(out.CleanupFailures) > 0 {
		cleanupReq := req
		cleanupReq.Action = req.Action + "_cleanup_failed"
		cleanupOutcome := *out
		cleanupOutcome.Result = ResultFailed
		cleanupOutcome.Error = "one or more session sandboxes could not be destroyed"
		if err := s.recordOutcome(s.db, cleanupReq, cleanupOutcome); err != nil {
			out.Result = ResultFailed
			out.Error = "cleanup audit outcome: " + err.Error()
		}
	}
}

// TransitionScope walks an org-scoped set in bounded pages. Terminal rows are
// excluded up front, and each row still uses the same compare-and-swap path.
func (s *Service) TransitionScope(scope Scope, target, action, reason, actorID string) ([]Outcome, error) {
	return s.transitionScopeDB(s.db, scope, target, action, reason, actorID, true)
}

// TransitionScopeInTransaction persists lifecycle state and audit rows on the
// caller's transaction. FinalizeTransitions must be invoked after commit so
// sandbox cleanup and realtime notification never run for rolled-back state.
func (s *Service) TransitionScopeInTransaction(tx *gorm.DB, scope Scope, target, action, reason, actorID string) ([]Outcome, error) {
	return s.transitionScopeDB(tx, scope, target, action, reason, actorID, false)
}

func (s *Service) transitionScopeDB(db *gorm.DB, scope Scope, target, action, reason, actorID string, runHooks bool) ([]Outcome, error) {
	if strings.TrimSpace(scope.OrganizationID) == "" {
		return nil, errors.New("organization_id is required")
	}
	outcomes := make([]Outcome, 0)
	countQuery := scopedSessionQuery(db.Model(&models.Session{}), scope, target)
	var candidateCount int64
	if err := countQuery.Count(&candidateCount).Error; err != nil {
		return nil, err
	}
	if candidateCount > 5000 {
		return nil, fmt.Errorf("session lifecycle scope contains %d rows; maximum synchronous scope is 5000", candidateCount)
	}
	outcomes = make([]Outcome, 0, candidateCount)
	if !runHooks {
		return transitionScopeInTransactionBatches(db, scope, target, action, reason, actorID, candidateCount)
	}
	worker := &Service{db: db}
	if runHooks {
		worker.cleanup, worker.notify, worker.notifyScope = s.hooks()
	}
	cursor := ""
	for {
		var batch []models.Session
		query := scopedSessionQuery(db, scope, target)
		if cursor != "" {
			query = query.Where("id > ?", cursor)
		}
		if err := query.Order("id ASC").Limit(200).Find(&batch).Error; err != nil {
			return outcomes, err
		}
		if len(batch) == 0 {
			return outcomes, nil
		}
		for _, sess := range batch {
			request := Request{
				OrganizationID: scope.OrganizationID, SessionRef: sess.ID, Target: target,
				Action: action, Reason: reason, ActorID: actorID, ActorType: scope.ActorType, ForceTerminal: scope.ForceTerminal,
			}
			outcomes = append(outcomes, worker.transitionLoadedModeAtomic(sess, request, runHooks, runHooks))
		}
		cursor = batch[len(batch)-1].ID
	}
}

// transitionScopeInTransactionBatches relies on the caller's transaction and
// row locks, then updates each canonical source state in one statement and
// inserts its audit outcomes in batches. Project archive can therefore freeze
// thousands of sessions without one nested savepoint and two SQL calls per row.
func transitionScopeInTransactionBatches(db *gorm.DB, scope Scope, target, action, reason, actorID string, candidateCount int64) ([]Outcome, error) {
	outcomes := make([]Outcome, 0, candidateCount)
	cursor := ""
	for {
		var batch []models.Session
		query := scopedSessionQuery(db, scope, target)
		if cursor != "" {
			query = query.Where("id > ?", cursor)
		}
		if err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Order("id ASC").Limit(200).Find(&batch).Error; err != nil {
			return outcomes, err
		}
		if len(batch) == 0 {
			return outcomes, nil
		}
		now := time.Now().UTC()
		updates := map[string]interface{}{"status": target, "last_activity_at": now.Format(time.RFC3339)}
		if models.SessionIsTerminal(target) {
			updates["closed_at"] = now.Format(time.RFC3339)
		}
		bySource := make(map[string][]string)
		for _, sess := range batch {
			bySource[sess.Status] = append(bySource[sess.Status], sess.ID)
		}
		for source, ids := range bySource {
			result := db.Model(&models.Session{}).
				Where("organization_id = ? AND status = ? AND id IN ?", scope.OrganizationID, source, ids).
				Updates(updates)
			if result.Error != nil {
				return outcomes, result.Error
			}
			if result.RowsAffected != int64(len(ids)) {
				return outcomes, fmt.Errorf("session lifecycle batch conflict: updated %d of %d %s sessions", result.RowsAffected, len(ids), source)
			}
		}
		ids := make([]string, 0, len(batch))
		for _, sess := range batch {
			out := Outcome{RequestedID: sess.ID, SessionID: sess.SessionID, RecordID: sess.ID, From: sess.Status, To: target, Result: ResultUpdated}
			outcomes = append(outcomes, out)
			ids = append(ids, sess.ID)
		}
		details, err := json.Marshal(map[string]interface{}{
			"session_ids": ids, "count": len(ids), "to": target, "reason": reason,
		})
		if err != nil {
			return outcomes, err
		}
		actorType := scope.ActorType
		if actorType == "" {
			actorType = "admin"
		}
		if err := db.Create(&models.AuditEvent{
			OrganizationID: scope.OrganizationID, EventType: "cp.session." + action,
			ActorID: actorID, ActorType: actorType, Action: action,
			ResourceType: "session_scope", ResourceID: scopeResourceID(scope),
			Details: string(details), Result: "success", OccurredAt: now.Format(time.RFC3339Nano),
		}).Error; err != nil {
			return outcomes, err
		}
		cursor = batch[len(batch)-1].ID
	}
}

func scopedSessionQuery(db *gorm.DB, scope Scope, target string) *gorm.DB {
	query := db.Where("organization_id = ?", scope.OrganizationID)
	if scope.ForceTerminal && models.SessionIsTerminal(target) {
		// Forced security/offboarding transitions must fail closed for an
		// unknown future state too: everything except a canonical terminal
		// row is eligible for termination.
		query = query.Where("status NOT IN ?", models.SessionTerminalStatuses())
	} else {
		query = query.Where("status IN ?", models.SessionTransitionSources(target))
	}
	if scope.HarnessID != "" {
		query = query.Where("harness_id = ?", scope.HarnessID)
	}
	if scope.UserID != "" {
		query = query.Where("user_id = ?", scope.UserID)
	}
	if scope.ProjectID != "" {
		query = query.Where("project_id = ?", scope.ProjectID)
	}
	if len(scope.SessionRefs) > 0 {
		query = query.Where("id IN ? OR session_id IN ?", scope.SessionRefs, scope.SessionRefs)
	}
	return query
}

// FinalizeTransitions runs post-commit cleanup and notification for outcomes
// produced by TransitionScopeInTransaction.
func (s *Service) FinalizeTransitions(orgID string, outcomes []Outcome, target, action, reason, actorID, actorType string) ([]Outcome, error) {
	var firstErr error
	_, _, notifyScope := s.hooks()
	coalesced := len(outcomes) > 100 && notifyScope != nil
	for i := range outcomes {
		if outcomes[i].Result != ResultUpdated {
			continue
		}
		req := Request{OrganizationID: orgID, SessionRef: outcomes[i].RequestedID, Target: target, Action: action, Reason: reason, ActorID: actorID, ActorType: actorType}
		if coalesced {
			cleanup, _, _ := s.hooks()
			if models.SessionIsTerminal(target) && cleanup != nil {
				outcomes[i].CleanupFailures = cleanup(orgID, outcomes[i].SessionID)
				if len(outcomes[i].CleanupFailures) > 0 {
					outcomes[i].Result = ResultFailed
					outcomes[i].Error = "one or more session sandboxes could not be destroyed"
					cleanupReq := req
					cleanupReq.Action = action + "_cleanup_failed"
					if err := s.recordOutcome(s.db, cleanupReq, outcomes[i]); err != nil {
						outcomes[i].Error = "cleanup audit outcome: " + err.Error()
					}
				}
			}
		} else {
			s.finalizeOutcome(&outcomes[i], req)
		}
		if outcomes[i].Result == ResultFailed && firstErr == nil {
			firstErr = errors.New(outcomes[i].Error)
		}
	}
	if coalesced {
		notifyScope(orgID, target, len(outcomes))
	}
	return outcomes, firstErr
}

func scopeResourceID(scope Scope) string {
	switch {
	case scope.ProjectID != "":
		return scope.ProjectID
	case scope.HarnessID != "":
		return scope.HarnessID
	case scope.UserID != "":
		return scope.UserID
	default:
		return scope.OrganizationID
	}
}

func (s *Service) recordOutcome(db *gorm.DB, req Request, out Outcome) error {
	event := outcomeAudit(req, out)
	return db.Create(&event).Error
}

func outcomeAudit(req Request, out Outcome) models.AuditEvent {
	actorType := req.ActorType
	if actorType == "" {
		actorType = "admin"
	}
	details, _ := json.Marshal(map[string]interface{}{
		"session_id": out.SessionID, "from": out.From, "to": out.To, "reason": req.Reason,
		"result": out.Result, "cleanup_failures": out.CleanupFailures, "error": out.Error,
	})
	result := "success"
	if out.Result != ResultUpdated {
		result = "failure"
	}
	action := req.Action
	if action == "" {
		action = "session_transition"
	}
	return models.AuditEvent{
		OrganizationID: req.OrganizationID, EventType: "cp.session." + action,
		ActorID: req.ActorID, ActorType: actorType, Action: action,
		ResourceType: "session", ResourceID: out.RecordID, Details: string(details),
		Result: result, OccurredAt: time.Now().UTC().Format(time.RFC3339),
	}
}
