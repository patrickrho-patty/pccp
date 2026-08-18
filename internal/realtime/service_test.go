package realtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBroadcast(t *testing.T) {
	svc := New()
	svc.Broadcast("test.event", map[string]string{"msg": "hello"})
	// No clients connected, should not panic
}

func TestNotifySession(t *testing.T) {
	svc := New()
	svc.NotifySessionUpdate("org-1", "ses-1", "active")
	svc.NotifySecurityFinding("org-1", "finding-1", "high", "테스트 보안 발견", "open")
	svc.NotifyChatMessage("org-1", "conv-1", "김개발", "안녕하세요")
	svc.NotifyFleetAction("org-1", "quarantine", "hrn-1")
	// Should not panic with no clients
}

func TestConnectedClients(t *testing.T) {
	svc := New()
	if svc.ConnectedClients() != 0 {
		t.Fatal("expected 0 clients")
	}
}

// --- PAT-1496: SSE contract (auth, named events, org routing, cleanup) ---

func sseTestService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/realtime.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.RealtimeEvent{}, &models.RealtimeSequence{}, &models.RealtimeStreamTicket{}, &models.RealtimeTransientEvent{}, &models.User{}); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func TestSSEDisconnectsWhenManagedUserLifecycleChanges(t *testing.T) {
	svc := sseTestService(t)
	svc.lifecycleInterval = 10 * time.Millisecond
	user := models.User{AuditBase: models.AuditBase{OrganizationID: "org-lifecycle"}, Email: "live@corp.kr", Name: "Live", Status: models.UserStatusActive}
	if err := svc.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.IssueSSETicket("secret", StreamGrant{
		OrganizationID: user.OrganizationID, ActorID: user.ID, UserID: user.ID, LifecycleEpoch: user.LifecycleEpoch,
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+token, nil)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { svc.HandleSSE("secret")(rec, req); close(done) }()
	waitForSSEClient(t, svc)
	if err := svc.db.Model(&models.User{}).Where("id = ? AND organization_id = ?", user.ID, user.OrganizationID).
		Updates(map[string]interface{}{"status": models.UserStatusSuspended, "lifecycle_epoch": gorm.Expr("lifecycle_epoch + 1")}).Error; err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE stream remained connected after lifecycle revocation")
	}
	if clients := svc.ConnectedClients(); clients != 0 {
		t.Fatalf("revoked SSE client remains registered: %d", clients)
	}
}

func TestSSEDisconnectsWhenTranscriptGrantIsRevoked(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/permission-revocation.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.RealtimeEvent{}, &models.RealtimeSequence{}, &models.RealtimeStreamTicket{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE admin_credentials (
		email TEXT NOT NULL, organization_id TEXT NOT NULL, role TEXT NOT NULL, permissions_json TEXT NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO admin_credentials (email, organization_id, role, permissions_json) VALUES (?, ?, ?, ?)",
		"operator@corp.kr", "org-permission", "security_operator", `["live:read","live:transcript"]`).Error; err != nil {
		t.Fatal(err)
	}
	svc := New(db)
	svc.lifecycleInterval = 10 * time.Millisecond
	token, _, err := svc.IssueSSETicket("secret", StreamGrant{
		OrganizationID: "org-permission", ActorID: "operator@corp.kr", ActorEmail: "operator@corp.kr", TranscriptVisible: true,
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+token, nil)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { svc.HandleSSE("secret")(rec, req); close(done) }()
	waitForSSEClient(t, svc)
	if err := db.Exec("UPDATE admin_credentials SET permissions_json = ? WHERE organization_id = ? AND email = ?",
		`["live:read"]`, "org-permission", "operator@corp.kr").Error; err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE stream remained connected after transcript grant revocation")
	}
}

func sseTestToken(t *testing.T, svc *Service, secret, orgID string, transcript bool) string {
	t.Helper()
	token, _, err := svc.IssueSSETicket(secret, StreamGrant{OrganizationID: orgID, ActorID: "operator@example.test", TranscriptVisible: transcript}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestSSERejectsInvalidToken(t *testing.T) {
	svc := New()
	h := svc.HandleSSE("secret")
	req := httptest.NewRequest("GET", "/api/realtime/sse?token=bogus", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token → %d, want 401", rec.Code)
	}
}

func TestSSERejectsTokenWithoutOrganization(t *testing.T) {
	svc := sseTestService(t)
	if _, _, err := svc.IssueSSETicket("secret", StreamGrant{ActorID: "operator@example.test"}, time.Minute); err == nil {
		t.Fatal("ticket without organization must be rejected")
	}
}

func TestSSEDeliversNamedSessionEventsToOrg(t *testing.T) {
	svc := sseTestService(t)
	h := svc.HandleSSE("secret")
	tok := sseTestToken(t, svc, "secret", "org-1", false)

	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+tok, nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() { h(rec, req); close(done) }()

	// Wait for the handler to register the client.
	deadline := time.Now().Add(2 * time.Second)
	for svc.ConnectedClients() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if svc.ConnectedClients() == 0 {
		t.Fatal("sse client never connected")
	}

	// An org-scoped session update must reach the SSE client as a NAMED
	// event the browser can addEventListener('session.update') for.
	svc.NotifySessionUpdate("org-1", "ses-1", "paused")
	// Let the handler drain its channel before tearing down.
	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done
	got := rec.Body.String()
	if !strings.Contains(got, "event: session.update") {
		t.Fatalf("SSE frame missing 'event: session.update' line:\n%s", got)
	}
	if !strings.Contains(got, `"session_id":"ses-1"`) || !strings.Contains(got, `"status":"paused"`) {
		t.Fatalf("SSE frame missing payload:\n%s", got)
	}
	if !strings.Contains(got, "data: ") {
		t.Fatalf("SSE frame missing data line:\n%s", got)
	}
}

func TestSSEDoesNotDeliverOtherOrganizationEvents(t *testing.T) {
	svc := sseTestService(t)
	h := svc.HandleSSE("secret")
	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+sseTestToken(t, svc, "secret", "org-1", false), nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h(rec, req); close(done) }()
	waitForSSEClient(t, svc)

	svc.NotifySessionUpdate("org-2", "foreign", "active")
	svc.NotifySessionUpdate("org-1", "own", "paused")
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done
	got := rec.Body.String()
	if strings.Contains(got, "foreign") || !strings.Contains(got, `"session_id":"own"`) {
		t.Fatalf("org-scoped SSE stream leaked or missed an event:\n%s", got)
	}
}

func TestSSEReplaysEventsAfterLastEventID(t *testing.T) {
	svc := sseTestService(t)
	svc.NotifySessionUpdate("org-1", "first", "active")
	svc.NotifySessionUpdate("org-1", "second", "paused")

	h := svc.HandleSSE("secret")
	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+sseTestToken(t, svc, "secret", "org-1", false)+"&last_event_id=1", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h(rec, req); close(done) }()
	waitForSSEClient(t, svc)
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	got := rec.Body.String()
	if strings.Contains(got, `"session_id":"first"`) || !strings.Contains(got, `"session_id":"second"`) {
		t.Fatalf("replay cursor returned wrong events:\n%s", got)
	}
	if !strings.Contains(got, "id: 2\n") || !strings.Contains(got, "event: session.update") {
		t.Fatalf("replay frame lacks durable id/name:\n%s", got)
	}
}

func TestSSESignalsWhenReplayCursorFellOutOfHistory(t *testing.T) {
	svc := sseTestService(t)
	svc.historyLimit = 1
	svc.NotifySessionUpdate("org-1", "first", "active")
	svc.NotifySessionUpdate("org-1", "second", "paused")

	h := svc.HandleSSE("secret")
	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+sseTestToken(t, svc, "secret", "org-1", false)+"&last_event_id=0", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h(rec, req); close(done) }()
	waitForSSEClient(t, svc)
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done
	if got := rec.Body.String(); !strings.Contains(got, "event: replay.required") || !strings.Contains(got, `"session_id":"second"`) {
		t.Fatalf("expired cursor must signal a snapshot refresh and replay retained events:\n%s", got)
	}
}

func TestSSEReplaySurvivesServiceRestart(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/realtime.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.RealtimeEvent{}, &models.RealtimeSequence{}, &models.RealtimeStreamTicket{}, &models.RealtimeTransientEvent{}); err != nil {
		t.Fatal(err)
	}
	first := New(db)
	first.NotifySessionUpdate("org-1", "before-restart", "active")
	second := New(db)

	h := second.HandleSSE("secret")
	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+sseTestToken(t, second, "secret", "org-1", false)+"&last_event_id=0", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h(rec, req); close(done) }()
	waitForSSEClient(t, second)
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done
	if got := rec.Body.String(); !strings.Contains(got, `"session_id":"before-restart"`) || !strings.Contains(got, "id: 1") {
		t.Fatalf("durable replay after service restart missing:\n%s", got)
	}
}

func TestSSEReceivesDurableEventFromAnotherReplica(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/realtime.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.RealtimeEvent{}, &models.RealtimeSequence{}, &models.RealtimeStreamTicket{}, &models.RealtimeTransientEvent{}); err != nil {
		t.Fatal(err)
	}
	consumer, producer := New(db), New(db)
	consumer.heartbeatInterval = time.Hour
	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+sseTestToken(t, consumer, "secret", "org-1", false), nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { consumer.HandleSSE("secret")(rec, req); close(done) }()
	waitForSSEClient(t, consumer)
	producer.NotifySessionUpdate("org-1", "cross-replica", "paused")
	time.Sleep(650 * time.Millisecond)
	cancel()
	<-done
	if got := rec.Body.String(); !strings.Contains(got, `"session_id":"cross-replica"`) {
		t.Fatalf("cross-replica event missing:\n%s", got)
	}
}

func TestSSEReceivesEncryptedTranscriptFrameFromRelayProcess(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/transient.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.RealtimeEvent{}, &models.RealtimeSequence{}, &models.RealtimeStreamTicket{}, &models.RealtimeTransientEvent{}); err != nil {
		t.Fatal(err)
	}
	apiReplica, relayProcess := New(db), New(db)
	for _, service := range []*Service{apiReplica, relayProcess} {
		if err := service.SetSharedBusSecret("shared-control-plane-token"); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+sseTestToken(t, apiReplica, "secret", "org-1", true), nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { apiReplica.HandleSSE("secret")(rec, req); close(done) }()
	waitForSSEClient(t, apiReplica)
	relayProcess.BroadcastToOrg("org-1", "session.chunk", map[string]string{"session_id": "session-1", "text": "secret delta"})
	var transientCount int64
	deadline := time.Now().Add(time.Second)
	var countErr error
	for time.Now().Before(deadline) {
		countErr = db.Model(&models.RealtimeTransientEvent{}).Count(&transientCount).Error
		if countErr == nil && transientCount == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if countErr != nil || transientCount != 1 {
		t.Fatalf("transient carrier count=%d err=%v", transientCount, countErr)
	}
	if rows, _, _ := apiReplica.transientAfter("org-1", time.Now().UTC().Add(-time.Minute), "", 100); len(rows) != 1 {
		var carrier models.RealtimeTransientEvent
		_ = db.First(&carrier).Error
		t.Fatalf("transient carrier could not be decrypted: created=%s expires=%s nonce=%d cipher=%d", carrier.CreatedAt, carrier.ExpiresAt, len(carrier.Nonce), len(carrier.Ciphertext))
	}
	time.Sleep(400 * time.Millisecond)
	cancel()
	<-done
	if got := rec.Body.String(); !strings.Contains(got, "event: session.chunk") || !strings.Contains(got, "secret delta") {
		t.Fatalf("cross-process transcript frame missing:\n%s", got)
	}
	var row models.RealtimeTransientEvent
	if err := db.First(&row).Error; err != nil || strings.Contains(row.Ciphertext, "secret delta") {
		t.Fatalf("transient carrier was not encrypted: err=%v row=%+v", err, row)
	}
}

func TestSSEDeliversLocalTranscriptFrameOnlyOnce(t *testing.T) {
	svc := sseTestService(t)
	if err := svc.SetSharedBusSecret("shared-control-plane-token"); err != nil {
		t.Fatal(err)
	}
	svc.heartbeatInterval = time.Hour
	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+sseTestToken(t, svc, "secret", "org-1", true), nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { svc.HandleSSE("secret")(rec, req); close(done) }()
	waitForSSEClient(t, svc)

	svc.BroadcastToOrg("org-1", "session.chunk", map[string]string{"session_id": "session-1", "text": "one local delta"})
	time.Sleep(650 * time.Millisecond)
	cancel()
	<-done

	if got := strings.Count(rec.Body.String(), "one local delta"); got != 1 {
		t.Fatalf("local transcript delivery count=%d, want 1:\n%s", got, rec.Body.String())
	}
}

func TestSSEHeartbeatIsNamedAndFrequent(t *testing.T) {
	svc := sseTestService(t)
	svc.heartbeatInterval = 5 * time.Millisecond
	h := svc.HandleSSE("secret")
	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+sseTestToken(t, svc, "secret", "org-1", false), nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h(rec, req); close(done) }()
	waitForSSEClient(t, svc)
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
	if got := rec.Body.String(); !strings.Contains(got, "event: heartbeat") {
		t.Fatalf("missing named heartbeat:\n%s", got)
	}
}

func waitForSSEClient(t *testing.T, svc *Service) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for svc.ConnectedClients() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if svc.ConnectedClients() == 0 {
		t.Fatal("sse client never connected")
	}
}

func TestSSEDisconnectCleansClientRegistry(t *testing.T) {
	svc := sseTestService(t)
	h := svc.HandleSSE("secret")
	tok := sseTestToken(t, svc, "secret", "org-2", false)
	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+tok, nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h(rec, req); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for svc.ConnectedClients() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if n := svc.ConnectedClients(); n != 0 {
		t.Fatalf("client registry after disconnect = %d, want 0", n)
	}
}

func TestSSETicketIsShortLivedActorBoundAndOneTime(t *testing.T) {
	svc := sseTestService(t)
	token := sseTestToken(t, svc, "secret", "org-1", false)
	grant, err := svc.consumeSSETicket("secret", token)
	if err != nil || grant.OrganizationID != "org-1" || grant.ActorID != "operator@example.test" {
		t.Fatalf("first consume = %+v, %v", grant, err)
	}
	if _, err := svc.consumeSSETicket("secret", token); err == nil {
		t.Fatal("ticket reuse must be rejected")
	}
}

func TestSSENewConnectionDoesNotReplayHistoryWithoutCursor(t *testing.T) {
	svc := sseTestService(t)
	svc.NotifySessionUpdate("org-1", "historical", "active")
	h := svc.HandleSSE("secret")
	req := httptest.NewRequest("GET", "/api/realtime/sse?ticket="+sseTestToken(t, svc, "secret", "org-1", false), nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h(rec, req); close(done) }()
	waitForSSEClient(t, svc)
	cancel()
	<-done
	if strings.Contains(rec.Body.String(), "historical") {
		t.Fatalf("fresh connection replayed history:\n%s", rec.Body.String())
	}
}

func TestSSETranscriptFramesRequireExplicitVisibility(t *testing.T) {
	svc := New()
	hidden := &Client{ID: "hidden", OrgID: "org-1", SSE: make(chan sseFrame, 1), Overflow: make(chan struct{}, 1)}
	visible := &Client{ID: "visible", OrgID: "org-1", TranscriptVisible: true, SSE: make(chan sseFrame, 1), Overflow: make(chan struct{}, 1)}
	svc.clients[hidden.ID], svc.clients[visible.ID] = hidden, visible
	svc.BroadcastToOrg("org-1", "session.chunk", map[string]string{"text": "secret content"})
	select {
	case <-hidden.SSE:
		t.Fatal("metadata-only stream received transcript content")
	default:
	}
	select {
	case <-visible.SSE:
	default:
		t.Fatal("transcript-visible stream missed content")
	}
}

func TestExpiredTransientCarrierIsDeletedWithoutLaterTraffic(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/transient-expiry.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.RealtimeTransientEvent{}); err != nil {
		t.Fatal(err)
	}
	svc := New(db)
	svc.transientCleanupInterval = 10 * time.Millisecond
	if err := svc.SetSharedBusSecret("shared-control-plane-token"); err != nil {
		t.Fatal(err)
	}
	row := models.RealtimeTransientEvent{
		OrganizationID: "org-1", PublishedAtUnixNano: time.Now().Add(-time.Minute).UnixNano(),
		EventType: "session.chunk", Ciphertext: "expired", Nonce: "expired", ExpiresAt: time.Now().Add(-time.Second),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := db.Unscoped().Model(&models.RealtimeTransientEvent{}).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expired transient carrier was not physically deleted")
}

func TestDurableSequenceAllocationIsAtomicAcrossServiceInstances(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/sequence.db?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.RealtimeEvent{}, &models.RealtimeSequence{}); err != nil {
		t.Fatal(err)
	}
	services := []*Service{New(db), New(db), New(db), New(db)}
	const events = 40
	var wg sync.WaitGroup
	failures := make(chan int, events)
	for i := 0; i < events; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if _, _, ok := services[index%len(services)].persistEvent("org-atomic", "session.update", map[string]int{"index": index}, time.Now().UTC().Format(time.RFC3339Nano)); !ok {
				failures <- index
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	if len(failures) != 0 {
		t.Fatalf("atomic allocator dropped %d events", len(failures))
	}
	var rows []models.RealtimeEvent
	if err := db.Where("organization_id = ?", "org-atomic").Order("sequence ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != events {
		t.Fatalf("durable events=%d, want %d", len(rows), events)
	}
	for index, row := range rows {
		if row.Sequence != uint64(index+1) {
			t.Fatalf("sequence[%d]=%d, want %d", index, row.Sequence, index+1)
		}
	}
}
