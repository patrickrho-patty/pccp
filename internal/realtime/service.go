package realtime

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements real-time WebSocket/SSE for live updates (PRD §21, §14).
// Provides live session monitoring, presence, chat, and fleet status.
type Service struct {
	mu                       sync.RWMutex
	clients                  map[string]*Client // clientID → client
	orgEventLocks            map[string]*sync.Mutex
	nextOrgEventID           map[string]uint64
	history                  map[string][]sseFrame
	pollCursor               map[string]uint64
	transientCursor          map[string]transientCursor
	pollOnce                 sync.Once
	historyLimit             int
	heartbeatInterval        time.Duration
	lifecycleInterval        time.Duration
	db                       *gorm.DB
	busAEAD                  cipher.AEAD
	eventStoreReady          bool
	transientReady           bool
	transientQueue           chan models.RealtimeTransientEvent
	transientOnce            sync.Once
	transientCleanupInterval time.Duration
	grantCacheMu             sync.Mutex
	grantCache               map[string]grantCacheEntry
}

type transientCursor struct {
	at time.Time
	id string
}

type grantCacheEntry struct {
	active    bool
	checkedAt time.Time
}

// Client represents a connected WebSocket client.
type Client struct {
	ID                string          `json:"id"`
	UserID            string          `json:"user_id"`
	OrgID             string          `json:"org_id"`
	Conn              *websocket.Conn `json:"conn"`
	Send              chan []byte     `json:"send"`
	SSE               chan sseFrame   `json:"-"`
	Overflow          chan struct{}   `json:"-"`
	TranscriptVisible bool            `json:"-"`
	Subscriptions     map[string]bool `json:"subscriptions"` // event types subscribed to
}

// StreamGrant is the least-privilege authorization carried by a one-time SSE
// ticket. Actor and organization are persisted with the ticket and signed.
type StreamGrant struct {
	OrganizationID    string `json:"org_id"`
	ActorID           string `json:"actor_id"`
	ActorEmail        string `json:"actor_email,omitempty"`
	UserID            string `json:"user_id,omitempty"`
	LifecycleEpoch    uint64 `json:"lifecycle_epoch,omitempty"`
	TranscriptVisible bool   `json:"transcript_visible"`
}

type streamTicketClaims struct {
	OrganizationID string `json:"org_id"`
	ActorID        string `json:"actor_id"`
	ActorEmail     string `json:"actor_email,omitempty"`
	UserID         string `json:"user_id,omitempty"`
	LifecycleEpoch uint64 `json:"lifecycle_epoch,omitempty"`
	Transcript     bool   `json:"transcript"`
	Purpose        string `json:"purpose"`
	jwt.RegisteredClaims
}

// Event is a real-time event pushed to clients.
type Event struct {
	ID      uint64      `json:"id,omitempty"`
	Type    string      `json:"type"` // session.update, presence.update, chat.message, fleet.action, security.finding
	Payload interface{} `json:"payload"`
	Time    string      `json:"time"`
}

type sseFrame struct {
	ID   uint64
	Type string
	Data []byte
	Key  string
}

type orgSSEFrame struct {
	orgID string
	frame sseFrame
}

// SetSharedBusSecret enables encrypted, short-lived cross-process delivery
// for content-bearing events. The secret must be identical on relay and API
// replicas; empty disables the shared carrier rather than storing plaintext.
func (s *Service) SetSharedBusSecret(secret string) error {
	if strings.TrimSpace(secret) == "" {
		s.busAEAD = nil
		return nil
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	s.busAEAD = aead
	s.startTransientWriter()
	return nil
}

func (s *Service) startTransientWriter() {
	if s.db == nil || !s.transientReady || s.busAEAD == nil {
		return
	}
	s.transientOnce.Do(func() { go s.writeTransientEvents() })
}

// writeTransientEvents keeps token streaming independent from database
// latency. The carrier is deliberately bounded and short-lived: local clients
// already received the frame, while this queue only serves other replicas.
func (s *Service) writeTransientEvents() {
	cleanup := time.NewTicker(s.transientCleanupInterval)
	defer cleanup.Stop()
	batch := make([]models.RealtimeTransientEvent, 0, 64)
	for {
		select {
		case first := <-s.transientQueue:
			batch = append(batch[:0], first)
			for len(batch) < cap(batch) {
				select {
				case row := <-s.transientQueue:
					batch = append(batch, row)
				default:
					goto flush
				}
			}
		flush:
			_ = s.db.CreateInBatches(batch, len(batch)).Error
		case now := <-cleanup.C:
			_ = s.db.Unscoped().Where("expires_at <= ?", now.UTC()).Delete(&models.RealtimeTransientEvent{}).Error
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Production would verify origin
	},
}

// New creates a new real-time service.
func New(databases ...*gorm.DB) *Service {
	service := &Service{
		clients:                  make(map[string]*Client),
		orgEventLocks:            make(map[string]*sync.Mutex),
		nextOrgEventID:           make(map[string]uint64),
		history:                  make(map[string][]sseFrame),
		pollCursor:               make(map[string]uint64),
		transientCursor:          make(map[string]transientCursor),
		historyLimit:             512,
		heartbeatInterval:        15 * time.Second,
		lifecycleInterval:        time.Second,
		transientQueue:           make(chan models.RealtimeTransientEvent, 2048),
		transientCleanupInterval: 30 * time.Second,
		grantCache:               make(map[string]grantCacheEntry),
	}
	if len(databases) > 0 {
		service.db = databases[0]
		service.eventStoreReady = service.db.Migrator().HasTable(&models.RealtimeEvent{}) && service.db.Migrator().HasTable(&models.RealtimeSequence{})
		service.transientReady = service.db.Migrator().HasTable(&models.RealtimeTransientEvent{})
	}
	return service
}

// IssueSSETicket creates a short-lived, actor/org-bound ticket. The database
// row makes consumption atomic across control-plane replicas.
func (s *Service) IssueSSETicket(jwtSecret string, grant StreamGrant, ttl time.Duration) (string, time.Time, error) {
	if s.db == nil || strings.TrimSpace(grant.OrganizationID) == "" || strings.TrimSpace(grant.ActorID) == "" {
		return "", time.Time{}, errors.New("realtime: organization, actor, and database are required")
	}
	if ttl <= 0 || ttl > time.Minute {
		ttl = time.Minute
	}
	now := time.Now().UTC()
	expires := now.Add(ttl)
	if grant.UserID != "" && !s.streamGrantActive(grant) {
		return "", time.Time{}, errors.New("realtime: lifecycle subject is not active")
	}
	row := models.RealtimeStreamTicket{
		OrganizationID: grant.OrganizationID, ActorID: grant.ActorID, ActorEmail: grant.ActorEmail, UserID: grant.UserID,
		LifecycleEpoch: grant.LifecycleEpoch, Transcript: grant.TranscriptVisible, ExpiresAt: expires,
	}
	if err := s.db.Create(&row).Error; err != nil {
		return "", time.Time{}, fmt.Errorf("realtime: persist stream ticket: %w", err)
	}
	claims := streamTicketClaims{
		OrganizationID: grant.OrganizationID, ActorID: grant.ActorID, ActorEmail: grant.ActorEmail, UserID: grant.UserID,
		LifecycleEpoch: grant.LifecycleEpoch, Transcript: grant.TranscriptVisible, Purpose: "live-sse",
		RegisteredClaims: jwt.RegisteredClaims{ID: row.ID, Subject: grant.ActorID, Issuer: "pccp-live-sse", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expires)},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(jwtSecret))
	if err != nil {
		s.db.Unscoped().Delete(&row)
		return "", time.Time{}, fmt.Errorf("realtime: sign stream ticket: %w", err)
	}
	// Opportunistic bounded cleanup; no issued token is affected.
	s.db.Unscoped().Where("expires_at < ? OR consumed_at IS NOT NULL", now.Add(-time.Minute)).Delete(&models.RealtimeStreamTicket{})
	return token, expires, nil
}

func (s *Service) consumeSSETicket(jwtSecret, tokenString string) (StreamGrant, error) {
	if s.db == nil || strings.TrimSpace(tokenString) == "" {
		return StreamGrant{}, errors.New("realtime: invalid stream ticket")
	}
	claims := &streamTicketClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("realtime: unexpected signing method")
		}
		return []byte(jwtSecret), nil
	}, jwt.WithIssuer("pccp-live-sse"))
	if err != nil || !token.Valid || claims.Purpose != "live-sse" || claims.ID == "" || claims.Subject != claims.ActorID {
		return StreamGrant{}, errors.New("realtime: invalid stream ticket")
	}
	now := time.Now().UTC()
	result := s.db.Model(&models.RealtimeStreamTicket{}).
		Where("id = ? AND organization_id = ? AND actor_id = ? AND actor_email = ? AND user_id = ? AND lifecycle_epoch = ? AND transcript = ? AND consumed_at IS NULL AND expires_at > ?",
			claims.ID, claims.OrganizationID, claims.ActorID, claims.ActorEmail, claims.UserID, claims.LifecycleEpoch, claims.Transcript, now).
		Update("consumed_at", now)
	if result.Error != nil || result.RowsAffected != 1 {
		return StreamGrant{}, errors.New("realtime: expired or consumed stream ticket")
	}
	grant := StreamGrant{
		OrganizationID: claims.OrganizationID, ActorID: claims.ActorID, ActorEmail: claims.ActorEmail, UserID: claims.UserID,
		LifecycleEpoch: claims.LifecycleEpoch, TranscriptVisible: claims.Transcript,
	}
	if grant.UserID != "" && !s.streamGrantActive(grant) {
		return StreamGrant{}, errors.New("realtime: lifecycle subject is not active")
	}
	return grant, nil
}

func (s *Service) streamGrantActive(grant StreamGrant) bool {
	key := strings.Join([]string{grant.OrganizationID, grant.ActorEmail, grant.UserID, strconv.FormatUint(grant.LifecycleEpoch, 10), strconv.FormatBool(grant.TranscriptVisible)}, "\x00")
	now := time.Now()
	cacheTTL := s.lifecycleInterval
	if cacheTTL <= 0 || cacheTTL > time.Second {
		cacheTTL = time.Second
	}
	s.grantCacheMu.Lock()
	if cached, ok := s.grantCache[key]; ok && now.Sub(cached.checkedAt) < cacheTTL {
		s.grantCacheMu.Unlock()
		return cached.active
	}
	s.grantCacheMu.Unlock()
	active := s.validateStreamGrant(grant)
	s.grantCacheMu.Lock()
	if len(s.grantCache) >= 2048 {
		for cacheKey, cached := range s.grantCache {
			if now.Sub(cached.checkedAt) >= cacheTTL {
				delete(s.grantCache, cacheKey)
			}
		}
	}
	s.grantCache[key] = grantCacheEntry{active: active, checkedAt: now}
	s.grantCacheMu.Unlock()
	return active
}

func (s *Service) validateStreamGrant(grant StreamGrant) bool {
	if s.db == nil {
		return false
	}
	if grant.UserID != "" {
		var user models.User
		if err := s.db.Select("status", "lifecycle_epoch").
			Where("id = ? AND organization_id = ?", grant.UserID, grant.OrganizationID).
			First(&user).Error; err != nil {
			return false
		}
		if user.Status != models.UserStatusActive || user.LifecycleEpoch != grant.LifecycleEpoch {
			return false
		}
	}
	if strings.TrimSpace(grant.ActorEmail) == "" {
		return true
	}
	var operator struct {
		Role            string
		PermissionsJSON string
	}
	if err := s.db.Table("admin_credentials").Select("role, permissions_json").
		Where("organization_id = ? AND LOWER(email) = LOWER(?)", grant.OrganizationID, grant.ActorEmail).
		Take(&operator).Error; err != nil {
		return false
	}
	if !operatorHasPermission(operator.Role, operator.PermissionsJSON, "live:read") {
		return false
	}
	return !grant.TranscriptVisible || operatorHasPermission(operator.Role, operator.PermissionsJSON, "live:transcript")
}

func operatorHasPermission(role, encoded, permission string) bool {
	if permission != "live:transcript" && (role == "admin" || role == "owner" || role == "super_admin") {
		return true
	}
	var permissions []string
	if encoded != "" && json.Unmarshal([]byte(encoded), &permissions) != nil {
		return false
	}
	for _, grant := range permissions {
		if grant == permission || (strings.HasPrefix(permission, "live:") && grant == "live:*") {
			return true
		}
	}
	return false
}

// HandleWebSocket upgrades HTTP to WebSocket and manages the connection.
func (s *Service) HandleWebSocket(jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Authenticate via query parameter token
		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			tokenStr = r.Header.Get("Authorization")
			if len(tokenStr) > 7 {
				tokenStr = tokenStr[7:]
			}
		}

		claims := &jwt.RegisteredClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		client := &Client{
			ID:            fmt.Sprintf("ws_%d", time.Now().UnixNano()),
			Conn:          wsConn,
			Send:          make(chan []byte, 256),
			Subscriptions: make(map[string]bool),
		}

		s.mu.Lock()
		s.clients[client.ID] = client
		s.mu.Unlock()

		// Read pump
		go s.readPump(client)
		// Write pump
		go s.writePump(client)

		// Send welcome event
		client.Send <- mustMarshal(Event{
			Type:    "connection.established",
			Payload: map[string]string{"client_id": client.ID, "status": "연결됨 (connected)"},
			Time:    time.Now().Format(time.RFC3339),
		})
	}
}

// HandleSSE handles Server-Sent Events as an alternative to WebSocket.
func (s *Service) HandleSSE(jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		grant, err := s.consumeSSETicket(jwtSecret, r.URL.Query().Get("ticket"))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		orgID := grant.OrganizationID
		lastEventID := uint64(0)
		cursorProvided := false
		rawCursor := r.Header.Get("Last-Event-ID")
		if rawCursor == "" {
			rawCursor = r.URL.Query().Get("last_event_id")
		}
		if raw := rawCursor; raw != "" {
			cursorProvided = true
			lastEventID, err = strconv.ParseUint(raw, 10, 64)
			if err != nil {
				http.Error(w, "invalid Last-Event-ID", http.StatusBadRequest)
				return
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		clientID := fmt.Sprintf("sse_%d", time.Now().UnixNano())
		frames := make(chan sseFrame, 256)
		overflow := make(chan struct{}, 1)
		seenTransient := make(map[string]struct{}, 256)
		seenTransientOrder := make([]string, 0, 256)
		deliveredThrough := lastEventID
		if !cursorProvided {
			deliveredThrough = s.latestSequence(orgID)
		}

		s.mu.Lock()
		orgAlreadyConnected := false
		transcriptAlreadyConnected := false
		for _, existing := range s.clients {
			if existing.OrgID == orgID {
				orgAlreadyConnected = true
				transcriptAlreadyConnected = transcriptAlreadyConnected || existing.TranscriptVisible
			}
		}
		if !orgAlreadyConnected {
			s.pollCursor[orgID] = deliveredThrough
		}
		if grant.TranscriptVisible && !transcriptAlreadyConnected {
			s.transientCursor[orgID] = transientCursor{at: time.Now().UTC()}
		}
		s.clients[clientID] = &Client{
			ID: clientID, UserID: grant.ActorID, OrgID: orgID, TranscriptVisible: grant.TranscriptVisible, SSE: frames, Overflow: overflow,
		}
		s.mu.Unlock()
		s.ensureCrossReplicaPoller()
		var history []sseFrame
		if cursorProvided {
			history = s.replayHistory(orgID)
		}

		defer func() {
			s.mu.Lock()
			delete(s.clients, clientID)
			orgConnected := false
			transcriptConnected := false
			for _, existing := range s.clients {
				if existing.OrgID == orgID {
					orgConnected = true
					transcriptConnected = transcriptConnected || existing.TranscriptVisible
				}
			}
			if !orgConnected {
				delete(s.pollCursor, orgID)
			}
			if !transcriptConnected {
				delete(s.transientCursor, orgID)
			}
			s.mu.Unlock()
		}()

		// Register before replaying so broadcasts that arrive during replay
		// are queued and no reconnect gap is possible.
		fmt.Fprint(w, ": connected\n\n")
		if cursorProvided && ((len(history) == 0 && lastEventID > 0) || (len(history) > 0 && (lastEventID > history[len(history)-1].ID || (history[0].ID > 0 && lastEventID < history[0].ID-1)))) {
			fmt.Fprint(w, "event: replay.required\ndata: {\"reason\":\"cursor_expired\"}\n\n")
		}
		for _, frame := range history {
			if frame.ID > lastEventID {
				writeSSEFrame(w, frame)
			}
		}
		replayedThrough := deliveredThrough
		if len(history) > 0 && history[len(history)-1].ID > replayedThrough {
			replayedThrough = history[len(history)-1].ID
		}
		flusher.Flush()

		ctx := r.Context()
		heartbeat := time.NewTicker(s.heartbeatInterval)
		defer heartbeat.Stop()
		lifecyclePoll := time.NewTicker(s.lifecycleInterval)
		defer lifecyclePoll.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case frame := <-frames:
				if frame.Key != "" {
					if _, seen := seenTransient[frame.Key]; seen {
						continue
					}
					seenTransient[frame.Key] = struct{}{}
					seenTransientOrder = append(seenTransientOrder, frame.Key)
					if len(seenTransientOrder) > 2048 {
						delete(seenTransient, seenTransientOrder[0])
						seenTransientOrder = seenTransientOrder[1:]
					}
				}
				// Registration precedes the replay query to close the reconnect
				// gap. Suppress the resulting overlap by durable sequence.
				if frame.ID > 0 && frame.ID <= replayedThrough {
					continue
				}
				writeSSEFrame(w, frame)
				if frame.ID > replayedThrough {
					replayedThrough = frame.ID
				}
				flusher.Flush()
			case <-lifecyclePoll.C:
				if !s.streamGrantActive(grant) {
					return
				}
			case <-overflow:
				fmt.Fprint(w, "event: replay.required\ndata: {\"reason\":\"client_overflow\"}\n\n")
				flusher.Flush()
				return
			case <-heartbeat.C:
				// Named heartbeat (not a `:` comment) so clients can prove
				// the stream is alive even with no session traffic.
				fmt.Fprintf(w, "event: heartbeat\ndata: {\"ts\":%q}\n\n", time.Now().UTC().Format(time.RFC3339))
				flusher.Flush()
			}
		}
	}
}

// Broadcast sends an event to all connected clients.
func (s *Service) Broadcast(eventType string, payload interface{}) {
	event := Event{
		Type:    eventType,
		Payload: payload,
		Time:    time.Now().UTC().Format(time.RFC3339),
	}
	data := mustMarshal(event)

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, client := range s.clients {
		// SSE clients are organization-bound and therefore receive only
		// BroadcastToOrg events. An unscoped broadcast must never cross tenants.
		if client.SSE != nil {
			continue
		}
		select {
		case client.Send <- data:
		default:
			// Buffer full, skip
		}
	}
}

// BroadcastToOrg sends an event to clients in a specific organization.
func (s *Service) BroadcastToOrg(orgID string, eventType string, payload interface{}) {
	if strings.TrimSpace(orgID) == "" {
		return
	}
	// Only bounded operational metadata is replayed durably. Content-bearing
	// chat and token-chunk frames remain ephemeral and never enter this log.
	if eventType != "session.update" && eventType != "exchange.update" && eventType != "security.finding" {
		data := mustMarshal(Event{Type: eventType, Payload: payload, Time: time.Now().UTC().Format(time.RFC3339)})
		frame := sseFrame{Type: eventType, Data: data}
		if contentBearingEvent(eventType) {
			frame.Key = s.persistTransient(orgID, eventType, data)
		}
		s.deliverToOrgClients(orgID, eventType, data, frame)
		return
	}
	orgLock := s.orgEventLock(orgID)
	orgLock.Lock()
	event, data, persisted := s.persistEvent(orgID, eventType, payload, time.Now().UTC().Format(time.RFC3339))
	if !persisted {
		if s.eventStoreReady {
			// Persistence is the cursor contract. Never mint a durable-looking
			// process-local ID after a database failure; clients must refresh.
			warning := mustMarshal(Event{Type: "replay.required", Payload: map[string]string{"reason": "persistence_failed"}, Time: time.Now().UTC().Format(time.RFC3339)})
			s.deliverToOrgClients(orgID, "replay.required", warning, sseFrame{Type: "replay.required", Data: warning})
			data = mustMarshal(event)
		} else {
			s.mu.Lock()
			s.nextOrgEventID[orgID]++
			event.ID = s.nextOrgEventID[orgID]
			s.mu.Unlock()
			data = mustMarshal(event)
		}
	}
	orgLock.Unlock()
	frame := sseFrame{ID: event.ID, Type: eventType, Data: data}
	s.mu.Lock()
	if event.ID > s.nextOrgEventID[orgID] {
		s.nextOrgEventID[orgID] = event.ID
	}
	s.history[orgID] = append(s.history[orgID], frame)
	if excess := len(s.history[orgID]) - s.historyLimit; excess > 0 {
		s.history[orgID] = append([]sseFrame(nil), s.history[orgID][excess:]...)
	}
	s.mu.Unlock()
	s.deliverToOrgClients(orgID, eventType, data, frame)
}

// FanoutLocal delivers an event already owned by a durable shared spine. It
// intentionally does not append another replay row, preventing every API
// replica from duplicating the same source event.
func (s *Service) FanoutLocal(orgID, eventType string, payload interface{}) {
	if strings.TrimSpace(orgID) == "" {
		return
	}
	data := mustMarshal(Event{Type: eventType, Payload: payload, Time: time.Now().UTC().Format(time.RFC3339)})
	s.deliverToOrgClients(orgID, eventType, data, sseFrame{Type: eventType, Data: data})
}

// HasSSEClients lets background bridges stay idle when nobody can consume
// their frames. It intentionally exposes no tenant or user information.
func (s *Service) HasSSEClients() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, client := range s.clients {
		if client.SSE != nil {
			return true
		}
	}
	return false
}

// ActiveSSEOrganizations returns the tenant IDs that currently have at least
// one SSE consumer. Background bridges use this to avoid scanning and decoding
// events for tenants with no connected operator.
func (s *Service) ActiveSSEOrganizations() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[string]struct{})
	for _, client := range s.clients {
		if client.SSE != nil && client.OrgID != "" {
			seen[client.OrgID] = struct{}{}
		}
	}
	orgIDs := make([]string, 0, len(seen))
	for orgID := range seen {
		orgIDs = append(orgIDs, orgID)
	}
	return orgIDs
}

func (s *Service) orgEventLock(orgID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock := s.orgEventLocks[orgID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.orgEventLocks[orgID] = lock
	}
	return lock
}

func (s *Service) deliverToOrgClients(orgID, eventType string, data []byte, frame sseFrame) {
	s.mu.RLock()
	clients := make([]*Client, 0)
	for _, client := range s.clients {
		if client.OrgID == orgID && (!contentBearingEvent(eventType) || client.TranscriptVisible) {
			clients = append(clients, client)
		}
	}
	s.mu.RUnlock()
	for _, client := range clients {
		if client.SSE != nil {
			s.enqueueSSE(client, frame)
			continue
		}
		select {
		case client.Send <- data:
		default:
		}
	}
}

func contentBearingEvent(eventType string) bool {
	switch eventType {
	case "session.chunk", "chat.message", "comms.message":
		return true
	default:
		return false
	}
}

// ensureCrossReplicaPoller starts one database poller per service replica.
// Work is grouped by active organization, so adding browser tabs does not add
// duplicate database polling. Per-connection replay and transcript guards are
// still enforced when frames leave each client's queue.
func (s *Service) ensureCrossReplicaPoller() {
	if !s.eventStoreReady && !s.transientReady {
		return
	}
	s.pollOnce.Do(func() { go s.pollCrossReplica() })
}

func (s *Service) pollCrossReplica() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		type activeOrg struct {
			durableCursor uint64
			transcript    bool
			transient     transientCursor
		}
		active := make(map[string]activeOrg)
		s.mu.RLock()
		for _, client := range s.clients {
			if client.OrgID == "" || client.SSE == nil {
				continue
			}
			state := active[client.OrgID]
			state.durableCursor = s.pollCursor[client.OrgID]
			state.transcript = state.transcript || client.TranscriptVisible
			state.transient = s.transientCursor[client.OrgID]
			active[client.OrgID] = state
		}
		s.mu.RUnlock()

		durableCursors := make(map[string]uint64, len(active))
		transientCursors := make(map[string]transientCursor, len(active))
		for orgID, state := range active {
			durableCursors[orgID] = state.durableCursor
			if state.transcript {
				transientCursors[orgID] = state.transient
			}
		}
		if s.eventStoreReady {
			for _, item := range s.replayAfterMany(durableCursors) {
				s.deliverToOrgClients(item.orgID, item.frame.Type, item.frame.Data, item.frame)
				if item.frame.ID > durableCursors[item.orgID] {
					durableCursors[item.orgID] = item.frame.ID
				}
			}
		}
		transientFrames, advancedTransient := s.transientAfterMany(transientCursors)
		for _, item := range transientFrames {
			s.deliverToOrgClients(item.orgID, item.frame.Type, item.frame.Data, item.frame)
		}
		s.mu.Lock()
		for orgID, cursor := range durableCursors {
			if _, connected := s.pollCursor[orgID]; connected && cursor > s.pollCursor[orgID] {
				s.pollCursor[orgID] = cursor
			}
		}
		for orgID, cursor := range advancedTransient {
			if _, connected := s.transientCursor[orgID]; connected {
				s.transientCursor[orgID] = cursor
			}
		}
		s.mu.Unlock()
	}
}

func (s *Service) persistEvent(orgID, eventType string, payload interface{}, occurredAt string) (Event, []byte, bool) {
	event := Event{Type: eventType, Payload: payload, Time: occurredAt}
	if s.db == nil || !s.eventStoreReady {
		return event, nil, false
	}
	var sequence uint64
	var err error
	for attempt := 0; attempt < 10; attempt++ {
		sequence = 0
		err = s.db.Transaction(func(tx *gorm.DB) error {
			allocation := `INSERT INTO realtime_sequences (organization_id, sequence, created_at, updated_at)
			SELECT ?, COALESCE(MAX(sequence), 0) + 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
			FROM realtime_events WHERE organization_id = ?
			ON CONFLICT (organization_id) DO UPDATE SET
				sequence = realtime_sequences.sequence + 1,
				updated_at = CURRENT_TIMESTAMP
			RETURNING sequence`
			if err := tx.Raw(allocation, orgID, orgID).Scan(&sequence).Error; err != nil {
				return err
			}
			event.ID = sequence
			data := mustMarshal(event)
			row := models.RealtimeEvent{OrganizationID: orgID, Sequence: sequence, EventType: eventType, PayloadJSON: string(data), OccurredAt: occurredAt}
			return tx.Create(&row).Error
		})
		if err == nil {
			break
		}
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	if err != nil || sequence == 0 {
		return Event{Type: eventType, Payload: payload, Time: occurredAt}, nil, false
	}
	event.ID = sequence
	data := mustMarshal(event)
	if sequence > uint64(s.historyLimit) {
		_ = s.db.Unscoped().Where("organization_id = ? AND sequence <= ?", orgID, sequence-uint64(s.historyLimit)).Delete(&models.RealtimeEvent{}).Error
	}
	return event, data, true
}

func (s *Service) replayHistory(orgID string) []sseFrame {
	if s.db != nil && s.eventStoreReady {
		var records []models.RealtimeEvent
		if err := s.db.Where("organization_id = ?", orgID).Order("sequence DESC").Limit(s.historyLimit).Find(&records).Error; err == nil {
			frames := make([]sseFrame, 0, len(records))
			for i := len(records) - 1; i >= 0; i-- {
				record := records[i]
				frames = append(frames, sseFrame{ID: record.Sequence, Type: record.EventType, Data: []byte(record.PayloadJSON)})
			}
			s.mu.Lock()
			if len(records) > 0 && records[0].Sequence > s.nextOrgEventID[orgID] {
				s.nextOrgEventID[orgID] = records[0].Sequence
			}
			s.mu.Unlock()
			return frames
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]sseFrame(nil), s.history[orgID]...)
}

func (s *Service) latestSequence(orgID string) uint64 {
	if s.db == nil || !s.eventStoreReady {
		return 0
	}
	var maximum uint64
	if err := s.db.Model(&models.RealtimeEvent{}).Where("organization_id = ?", orgID).
		Select("COALESCE(MAX(sequence), 0)").Scan(&maximum).Error; err != nil {
		return 0
	}
	return maximum
}

func (s *Service) replayAfter(orgID string, sequence uint64, limit int) []sseFrame {
	if s.db == nil || !s.eventStoreReady {
		return nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var records []models.RealtimeEvent
	if err := s.db.Where("organization_id = ? AND sequence > ?", orgID, sequence).
		Order("sequence ASC").Limit(limit).Find(&records).Error; err != nil {
		return nil
	}
	frames := make([]sseFrame, 0, len(records))
	for _, record := range records {
		frames = append(frames, sseFrame{ID: record.Sequence, Type: record.EventType, Data: []byte(record.PayloadJSON)})
	}
	return frames
}

func (s *Service) replayAfterMany(cursors map[string]uint64) []orgSSEFrame {
	if s.db == nil || !s.eventStoreReady || len(cursors) == 0 {
		return nil
	}
	orgIDs := make([]string, 0, len(cursors))
	for orgID := range cursors {
		orgIDs = append(orgIDs, orgID)
	}
	result := make([]orgSSEFrame, 0)
	for start := 0; start < len(orgIDs); start += 50 {
		end := start + 50
		if end > len(orgIDs) {
			end = len(orgIDs)
		}
		var conditions *gorm.DB
		for _, orgID := range orgIDs[start:end] {
			if conditions == nil {
				conditions = s.db.Where("organization_id = ? AND sequence > ?", orgID, cursors[orgID])
			} else {
				conditions = conditions.Or("organization_id = ? AND sequence > ?", orgID, cursors[orgID])
			}
		}
		var records []models.RealtimeEvent
		if err := s.db.Where(conditions).Order("organization_id ASC, sequence ASC").Find(&records).Error; err != nil {
			continue
		}
		for _, record := range records {
			result = append(result, orgSSEFrame{orgID: record.OrganizationID, frame: sseFrame{ID: record.Sequence, Type: record.EventType, Data: []byte(record.PayloadJSON)}})
		}
	}
	return result
}

func (s *Service) persistTransient(orgID, eventType string, data []byte) string {
	if s.db == nil || s.busAEAD == nil || !s.transientReady {
		return ""
	}
	nonce := make([]byte, s.busAEAD.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return ""
	}
	aad := []byte(orgID + "\x00" + eventType)
	ciphertext := s.busAEAD.Seal(nil, nonce, data, aad)
	now := time.Now().UTC()
	row := models.RealtimeTransientEvent{
		Base:                models.Base{ID: uuid.NewString()},
		OrganizationID:      orgID,
		PublishedAtUnixNano: now.UnixNano(),
		EventType:           eventType,
		Ciphertext:          base64.RawStdEncoding.EncodeToString(ciphertext),
		Nonce:               base64.RawStdEncoding.EncodeToString(nonce),
		ExpiresAt:           now.Add(30 * time.Second),
	}
	select {
	case s.transientQueue <- row:
		return row.ID
	default:
		return ""
	}
}

func (s *Service) transientAfter(orgID string, after time.Time, afterID string, limit int) ([]sseFrame, time.Time, string) {
	if s.db == nil || s.busAEAD == nil || !s.transientReady {
		return nil, after, afterID
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []models.RealtimeTransientEvent
	afterNanos := after.UnixNano()
	if err := s.db.Where("organization_id = ? AND expires_at > ? AND (published_at_unix_nano > ? OR (published_at_unix_nano = ? AND id > ?))", orgID, time.Now().UTC(), afterNanos, afterNanos, afterID).
		Order("published_at_unix_nano ASC, id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, after, afterID
	}
	frames := make([]sseFrame, 0, len(rows))
	for _, row := range rows {
		publishedAt := time.Unix(0, row.PublishedAtUnixNano).UTC()
		if publishedAt.After(after) || (publishedAt.Equal(after) && row.ID > afterID) {
			after, afterID = publishedAt, row.ID
		}
		nonce, nonceErr := base64.RawStdEncoding.DecodeString(row.Nonce)
		ciphertext, cipherErr := base64.RawStdEncoding.DecodeString(row.Ciphertext)
		if nonceErr != nil || cipherErr != nil || len(nonce) != s.busAEAD.NonceSize() {
			continue
		}
		plaintext, err := s.busAEAD.Open(nil, nonce, ciphertext, []byte(orgID+"\x00"+row.EventType))
		if err != nil {
			continue
		}
		frames = append(frames, sseFrame{Type: row.EventType, Data: plaintext, Key: row.ID})
	}
	return frames, after, afterID
}

func (s *Service) transientAfterMany(cursors map[string]transientCursor) ([]orgSSEFrame, map[string]transientCursor) {
	advanced := make(map[string]transientCursor, len(cursors))
	for orgID, cursor := range cursors {
		advanced[orgID] = cursor
	}
	if s.db == nil || s.busAEAD == nil || !s.transientReady || len(cursors) == 0 {
		return nil, advanced
	}
	orgIDs := make([]string, 0, len(cursors))
	for orgID := range cursors {
		orgIDs = append(orgIDs, orgID)
	}
	result := make([]orgSSEFrame, 0)
	now := time.Now().UTC()
	for start := 0; start < len(orgIDs); start += 25 {
		end := start + 25
		if end > len(orgIDs) {
			end = len(orgIDs)
		}
		var conditions *gorm.DB
		for _, orgID := range orgIDs[start:end] {
			cursor := cursors[orgID]
			if conditions == nil {
				conditions = s.db.Where("organization_id = ? AND (published_at_unix_nano > ? OR (published_at_unix_nano = ? AND id > ?))", orgID, cursor.at.UnixNano(), cursor.at.UnixNano(), cursor.id)
			} else {
				conditions = conditions.Or("organization_id = ? AND (published_at_unix_nano > ? OR (published_at_unix_nano = ? AND id > ?))", orgID, cursor.at.UnixNano(), cursor.at.UnixNano(), cursor.id)
			}
		}
		var rows []models.RealtimeTransientEvent
		if err := s.db.Where("expires_at > ?", now).Where(conditions).
			Order("published_at_unix_nano ASC, id ASC").Limit((end - start) * 500).Find(&rows).Error; err != nil {
			continue
		}
		for _, row := range rows {
			publishedAt := time.Unix(0, row.PublishedAtUnixNano).UTC()
			cursor := advanced[row.OrganizationID]
			if publishedAt.After(cursor.at) || (publishedAt.Equal(cursor.at) && row.ID > cursor.id) {
				advanced[row.OrganizationID] = transientCursor{at: publishedAt, id: row.ID}
			}
			nonce, nonceErr := base64.RawStdEncoding.DecodeString(row.Nonce)
			ciphertext, cipherErr := base64.RawStdEncoding.DecodeString(row.Ciphertext)
			if nonceErr != nil || cipherErr != nil || len(nonce) != s.busAEAD.NonceSize() {
				continue
			}
			plaintext, err := s.busAEAD.Open(nil, nonce, ciphertext, []byte(row.OrganizationID+"\x00"+row.EventType))
			if err != nil {
				continue
			}
			result = append(result, orgSSEFrame{orgID: row.OrganizationID, frame: sseFrame{Type: row.EventType, Data: plaintext, Key: row.ID}})
		}
	}
	return result, advanced
}

func (s *Service) enqueueSSE(client *Client, frame sseFrame) {
	select {
	case client.SSE <- frame:
	default:
		select {
		case client.Overflow <- struct{}{}:
		default:
		}
	}
}

func writeSSEFrame(w http.ResponseWriter, frame sseFrame) {
	if frame.ID > 0 {
		fmt.Fprintf(w, "id: %d\n", frame.ID)
	}
	if frame.Type != "" && !strings.ContainsAny(frame.Type, "\r\n") {
		fmt.Fprintf(w, "event: %s\n", frame.Type)
	}
	fmt.Fprintf(w, "data: %s\n\n", frame.Data)
}

// NotifySessionUpdate pushes a session state update.
func (s *Service) NotifySessionUpdate(orgID, sessionID, status string) {
	s.BroadcastToOrg(orgID, "session.update", map[string]string{
		"session_id": sessionID,
		"status":     status,
	})
}

// NotifySessionScopeUpdate coalesces large lifecycle operations into one
// repair signal instead of synchronously publishing thousands of frames.
func (s *Service) NotifySessionScopeUpdate(orgID, status string, count int) {
	s.BroadcastToOrg(orgID, "session.scope_update", map[string]interface{}{
		"status": status,
		"count":  count,
	})
}

// NotifySecurityFinding pushes a security finding to admins.
func (s *Service) NotifySecurityFinding(orgID, findingID, severity, titleKo, status string) {
	s.BroadcastToOrg(orgID, "security.finding", map[string]string{
		"finding_id": findingID,
		"severity":   severity,
		"title_ko":   titleKo,
		"status":     status,
	})
}

// NotifyChatMessage pushes a new chat message.
func (s *Service) NotifyChatMessage(orgID, conversationID, senderName, content string) {
	s.BroadcastToOrg(orgID, "chat.message", map[string]string{
		"conversation_id": conversationID,
		"sender":          senderName,
		"content":         content,
	})
}

// NotifyExchangeEvent pushes a governed-exchange lifecycle event
// (open/decision/dlp/forward/complete) to the org's live consoles.
// Payload carries counts + statuses only — never token content
// (payload protection: admin visibility does not exempt P0).
func (s *Service) NotifyExchangeEvent(orgID, sessionID, exchangeID, state string, evidenceEvents int) {
	s.BroadcastToOrg(orgID, "exchange.update", map[string]any{
		"session_id":      sessionID,
		"exchange_id":     exchangeID,
		"state":           state,
		"evidence_events": evidenceEvents,
	})
}

// NotifyFleetAction pushes a fleet action result.
func (s *Service) NotifyFleetAction(orgID, action, harnessID string) {
	s.BroadcastToOrg(orgID, "fleet.action", map[string]string{
		"action":     action,
		"harness_id": harnessID,
	})
}

// ConnectedClients returns the count of connected clients.
func (s *Service) ConnectedClients() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// SetupRoutes registers the WebSocket/SSE routes on a chi router.
func (s *Service) SetupRoutes(r chi.Router, jwtSecret string) {
	r.Get("/ws", s.HandleWebSocket(jwtSecret).ServeHTTP)
	r.Get("/sse", s.HandleSSE(jwtSecret).ServeHTTP)
}

func (s *Service) readPump(client *Client) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, client.ID)
		s.mu.Unlock()
		client.Conn.Close()
	}()

	for {
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			break
		}

		// Parse subscription messages
		var msg struct {
			Action string   `json:"action"` // subscribe, unsubscribe
			Types  []string `json:"types"`
		}
		if json.Unmarshal(message, &msg) == nil {
			switch msg.Action {
			case "subscribe":
				for _, t := range msg.Types {
					client.Subscriptions[t] = true
				}
			case "unsubscribe":
				for _, t := range msg.Types {
					delete(client.Subscriptions, t)
				}
			}
		}
	}
}

func (s *Service) writePump(client *Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		client.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.Send:
			if !ok {
				client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			client.Conn.WriteMessage(websocket.TextMessage, message)
		case <-ticker.C:
			if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func mustMarshal(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}
