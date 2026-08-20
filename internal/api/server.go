package api

import (
	"bytes"
	stdctx "context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/patrickrho-patty/pccp/internal/config"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/patrickrho-patty/pccp/internal/audit"
	"github.com/patrickrho-patty/pccp/internal/communications"
	"github.com/patrickrho-patty/pccp/internal/context"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/events"
	"github.com/patrickrho-patty/pccp/internal/fleet"
	"github.com/patrickrho-patty/pccp/internal/gitscm"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/impact"
	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/korean"
	"github.com/patrickrho-patty/pccp/internal/leaderboard"
	"github.com/patrickrho-patty/pccp/internal/reference"
	"github.com/patrickrho-patty/pccp/internal/metering"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/policy"
	"github.com/patrickrho-patty/pccp/internal/provenance"
	"github.com/patrickrho-patty/pccp/internal/registry"
	"github.com/patrickrho-patty/pccp/internal/sandbox"
	"github.com/patrickrho-patty/pccp/internal/security"
	"github.com/patrickrho-patty/pccp/internal/sessionlifecycle"
	"github.com/patrickrho-patty/pccp/internal/workintel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	errUserEmailExists      = errors.New("user email already exists")
	errUserSeatLimit        = errors.New("user seat limit reached")
	errHarnessSeatLimit     = errors.New("harness seat limit reached")
	errBootstrapNotPristine = errors.New("bootstrap is available only before any organization or identity exists")
)

// Server is the Control Plane HTTP API server.
type Server struct {
	db               *gorm.DB
	identity         *identity.Service
	auth             *identity.AuthService
	registry         *registry.Service
	policy           *policy.Service
	provenance       *provenance.Service
	security         *security.Service
	comms            *communications.Service
	workintel        *workintel.Service
	leaderboardSV    *leaderboard.Service
	refSV            *reference.Service
	events           *events.Service
	gitscm           *gitscm.Service
	impact           *impact.Service
	fleet            *fleet.Service
	context          *context.Service
	sandbox          *sandbox.Service
	sessionLifecycle *sessionlifecycle.Service
	korean           *korean.Service
	jwtSecret        string
	router           *chi.Mux
	// keyProvider seals/opens alert-endpoint secret references. nil
	// by default so test fixtures without an HSM/KMS still build.
	// Production callers must inject via SetKeyProvider; write paths
	// fail closed when it is nil. PAT-1502 PR 2.
	keyProvider     keymgmt.KeyProvider
	alertHTTPClient security.HTTPDoer
	alertNow        func() time.Time
	// modelPublishedHook is invoked after a successful model publish
	// (Task 15 catalog push). Deployments link it to the relay's
	// OnModelPublished so connected sessions receive the delta.
	modelPublishedHook func(packageID string)
}

// SetModelPublishedHook installs the post-publish hook.
func (s *Server) SetModelPublishedHook(hook func(packageID string)) {
	s.modelPublishedHook = hook
}

// New creates a new API server.
func New(db *gorm.DB, jwtSecret string) (*Server, error) {
	idSvc, err := identity.New(db)
	if err != nil {
		return nil, fmt.Errorf("api: init identity: %w", err)
	}
	authSvc := identity.NewAuthService(db, jwtSecret)
	regSvc, err := registry.New(db)
	if err != nil {
		return nil, fmt.Errorf("api: init registry: %w", err)
	}
	polSvc, err := policy.New(db)
	if err != nil {
		return nil, fmt.Errorf("api: init policy: %w", err)
	}
	provSvc, err := provenance.New(db, "pccp-relay-1")
	secSvc := security.New(db)
	commsSvc := communications.New(db)
	wiSvc := workintel.New(db)
	lbSvc := leaderboard.New(db)
	refSvc := reference.New(db)
	evtSvc, _ := events.New(db)
	gitSvc := gitscm.New(db)
	impactSvc := impact.New(db)
	ctxSvc := context.New(db, secSvc)
	sandboxSvc := sandbox.New(db)
	lifecycleSvc := sessionlifecycle.New(db)
	fleetSvc := fleet.New(db, lifecycleSvc)
	koreanSvc := korean.New(db)
	if err != nil {
		return nil, fmt.Errorf("api: init provenance: %w", err)
	}

	s := &Server{
		db:               db,
		identity:         idSvc,
		auth:             authSvc,
		registry:         regSvc,
		policy:           polSvc,
		provenance:       provSvc,
		security:         secSvc,
		comms:            commsSvc,
		workintel:        wiSvc,
		leaderboardSV:    lbSvc,
		refSV:            refSvc,
		events:           evtSvc,
		gitscm:           gitSvc,
		impact:           impactSvc,
		fleet:            fleetSvc,
		context:          ctxSvc,
		sandbox:          sandboxSvc,
		sessionLifecycle: lifecycleSvc,
		korean:           koreanSvc,
		jwtSecret:        jwtSecret,
		alertHTTPClient:  secSvc.AlertHTTPClient(),
		alertNow:         time.Now,
	}
	idSvc.SetSessionLifecycle(lifecycleSvc)
	fleetSvc.SetHarnessRevoker(idSvc.RevokeHarnessByActor)
	if err := s.ext().Realtime.SetSharedBusSecret(os.Getenv("PCCP_CP_TOKEN")); err != nil {
		return nil, fmt.Errorf("api: configure realtime bus: %w", err)
	}
	s.ext().SSO.SetSessionLifecycle(lifecycleSvc)
	lifecycleSvc.SetCleanup(func(orgID, sessionID string) []string {
		failed := make([]string, 0)
		if err := s.ext().Secret.ExpireAllForSession(orgID, sessionID); err != nil {
			failed = append(failed, "scoped_credentials")
		}
		var records []models.SandboxRecord
		if err := db.Where("organization_id = ? AND session_id = ?", orgID, sessionID).Find(&records).Error; err != nil {
			return append(failed, "sandbox_lookup_failed")
		}
		for _, record := range records {
			if record.Status != "destroyed" {
				if _, err := sandboxSvc.DestroySandbox(record.ID); err != nil {
					failed = append(failed, record.ID)
				}
			}
		}
		return failed
	})
	lifecycleSvc.SetNotifier(s.ext().Realtime.NotifySessionUpdate)
	lifecycleSvc.SetScopeNotifier(s.ext().Realtime.NotifySessionScopeUpdate)
	s.setupRouter()
	return s, nil
}

// SetKeyProvider injects the envelope-encryption provider used by
// alert-endpoint write paths. PAT-1502 PR 2. The provider is invoked
// when sealing new targets (create/rotate) and when opening existing
// envelopes for delivery/test. When nil, write paths fail closed
// (503 service unavailable) so an unconfigured server cannot accept
// secret material.
func (s *Server) SetKeyProvider(provider keymgmt.KeyProvider) {
	s.keyProvider = provider
	s.security.SetAlertKeyProvider(provider)
	s.ext().SSO.SetKeyProvider(provider)
}

// SetAlertHTTPClient installs the same request seam for interactive tests and
// background delivery. Production uses the SSRF-safe client created by New.
func (s *Server) SetAlertHTTPClient(client security.HTTPDoer) {
	if client == nil {
		client = security.NewAlertHTTPClient(5 * time.Second)
	}
	s.alertHTTPClient = client
	s.security.SetAlertHTTPClient(client)
}

func (s *Server) StartAlertDeliveryWorker(ctx stdctx.Context) {
	s.security.StartAlertDeliveryWorker(ctx)
}

// KeyProvider returns the currently configured provider (nil-safe).
func (s *Server) KeyProvider() keymgmt.KeyProvider {
	return s.keyProvider
}

func (s *Server) setupRouter() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		ExposedHeaders:   []string{"Link", "X-Next-Cursor"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/health", s.handleHealth)
	// SRE component probes: REAL reachability checks for the relay +
	// PIA (TCP dial with timeout; unconfigured = honestly unknown).
	r.Get("/api/sre/probes", s.handleSREProbes)

	// OpenAI-compatible inference adapter (§38.5)
	r.Post("/v1/chat/completions", s.handleCompatChatCompletions)

	// Auth routes (no auth required)
	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/login", s.handleLogin)
		r.Post("/bootstrap", s.handleBootstrap)
		r.Post("/mfa/setup", s.handleMFASetup)
		r.Post("/mfa/verify", s.handleMFAVerify)
	})

	// Realtime SSE (no middleware — HandleSSE does its own JWT check via query param)
	r.Get("/api/realtime/sse", s.ext().Realtime.HandleSSE(s.jwtSecret))
	// Short-lived, usage-export-scoped tickets let the browser download
	// directly without buffering the entire CSV or exposing a console JWT.
	r.Get("/api/exports/usage", s.handleUsageCSVTicketDownload)

	// SCM webhook ingestion (repositories C1) — unauthenticated like
	// real SCM webhooks; each delivery is verified against the repo's
	// HMAC secret before any state is touched.
	r.Post("/webhooks/scm/{repoId}", s.handleScmWebhook)
	// SSO login endpoints (PUBLIC — they run BEFORE a console session
	// exists; identity verification is the IdP's signature/JWKS check,
	// and the callbacks mint the session themselves).
	{
		ext := s.ext()
		r.Route("/api/sso", func(r chi.Router) {
			r.Post("/saml/redirect", s.wrapSSOSAMLRedirect(ext))
			r.Post("/saml/callback", s.wrapSSOSAMLCallback(ext))
			r.Post("/session", s.wrapSSOSessionExchange(ext))
			r.Get("/oidc/auth-url", s.wrapSSOOIDCAuthURL(ext))
			r.Get("/oidc/callback", s.wrapSSOOIDCCallback(ext))
		})
		// SCIM has its own tenant-bound bearer authentication and must remain
		// outside console-JWT middleware so standards-compliant IdPs can call it.
		r.Handle("/scim/v2/*", s.wrapSSOSCIM(ext))
	}

	// Terminal ads (PAT-1435): public catalog + anonymous measurement +
	// click redirect are unauthenticated (public harness builds carry no
	// console JWT); mutation is platform-operator-gated in handlers.
	r.Route("/api/public/ads", func(r chi.Router) {
		r.Get("/catalog", s.handleADCatalogGet)
		r.Post("/events", s.handleADEventIngest)
		r.Get("/go/{id}", s.handleADClickRedirect)
	})

	// SCM lineage observation webhooks (PAT-1453): unauthenticated like
	// real provider webhooks — each delivery is signature-verified
	// against the connection's secret before any state is touched.
	r.Post("/api/scm/observation/webhooks/{connId}", s.handleSCMObservationWebhook)

	// Public status page (PAT-1439): unauthenticated read-only status
	// API + anonymous subscriptions. Must never require console auth and
	// keeps serving the last valid snapshot when evaluation stopped.
	r.Route("/api/public/status", func(r chi.Router) {
		r.Get("/", s.handlePublicStatusGet)
		r.Get("/incidents/{slug}", s.handlePublicIncidentGet)
		r.Get("/feed.atom", s.handlePublicStatusFeed)
		r.Post("/subscribers", s.handlePublicSubscriberCreate)
		r.Get("/subscribers/verify", s.handlePublicSubscriberVerify)
		r.Get("/subscribers/unsubscribe", s.handlePublicSubscriberUnsubscribe)
	})

	// Authenticated API routes
	r.Route("/api", func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Post("/realtime/ticket", s.handleRealtimeTicket)

		// Organizations
		r.Route("/organizations", func(r chi.Router) {
			r.Get("/", s.handleListOrganizations)
			r.Get("/seats", s.handleGetSeatUsage)
			r.Post("/", s.handleCreateOrganization)
			r.Get("/{id}", s.handleGetOrganization)
		})

		// Users
		r.Route("/users", func(r chi.Router) {
			r.Get("/", s.handleListUsers)
			r.Post("/", s.handleCreateUser)
			r.Post("/import", s.handleImportUsersCSV)
			r.Get("/{id}", s.handleGetUser)
			r.Put("/{id}", s.handleUpdateUser)
			r.Delete("/{id}", s.handleDeleteUser)
			r.Get("/{id}/audit", s.handleListUserAudit)
			r.Post("/{id}/enrollment-code", s.handleIssueEnrollmentCode)
			r.Get("/{id}/harnesses", s.handleListUserHarnesses)
			r.Post("/{id}/harnesses", s.handleGrantUserHarness)
			r.Delete("/{id}/harnesses/{harnessId}", s.handleRevokeUserHarness)
			r.Get("/{id}/usage", s.handleGetUserUsage)
			r.Post("/{id}/offboard", s.handleOffboardUser)
			r.Post("/{id}/suspend", s.handleSuspendUser)
			r.Post("/{id}/resume", s.handleResumeUser)
			r.Get("/{id}/entitlements", s.handleGetUserEntitlements)
			r.Put("/{id}/entitlements", s.handlePutUserEntitlements)
			r.Get("/{id}/sso-status", s.handleUserSSOStatus)
			r.Put("/{id}/contractor", s.handleContractorProfile)
		})

		// Developer entitlement roles (web/01 B5)
		r.Route("/roles", func(r chi.Router) {
			r.Get("/", s.handleListRoles)
		})

		// Business Units (Korean org hierarchy) — PRD §12.1
		r.Route("/business-units", func(r chi.Router) {
			r.Get("/", s.handleListBusinessUnits)
			r.Post("/", s.handleCreateBusinessUnit)
			r.Put("/{id}", s.handleUpdateBusinessUnit)
			r.Delete("/{id}", s.handleDeleteBusinessUnit)
		})

		// Harnesses
		r.Route("/harnesses", func(r chi.Router) {
			r.Get("/", s.handleListHarnesses)
			r.Post("/enroll", s.handleEnrollHarness)
			r.Post("/heartbeat", s.handleHarnessHeartbeat)
			r.Get("/{id}", s.handleGetHarness)
			r.Get("/{id}/detail", s.handleGetHarnessDetail)
			r.Post("/{id}/revoke", s.handleRevokeHarness)
			r.Post("/{id}/quarantine", s.handleQuarantineHarness)
			r.Post("/{id}/reactivate", s.handleReactivateHarness)
			r.Get("/{id}/audit", s.handleListHarnessAudit)
		})

		// Projects
		r.Route("/projects", func(r chi.Router) {
			r.Get("/", s.handleListProjects)
			r.Post("/", s.handleCreateProject)
			r.Get("/{id}", s.handleGetProject)
			r.Get("/{id}/detail", s.handleGetProjectDetail)
			r.Get("/{id}/members", s.handleListProjectMembers)
			r.Post("/{id}/members", s.handleAddProjectMember)
			r.Delete("/{id}/members/{userId}", s.handleRemoveProjectMember)
			r.Put("/{id}", s.handleUpdateProject)
			r.Delete("/{id}", s.handleDeleteProject)
			r.Post("/{id}/restore", s.handleRestoreProject)
			r.Get("/{id}/tool-allowlist", s.wrapProjectToolAllowlist(s.ext()))
			r.Put("/{id}/tool-allowlist", s.wrapProjectToolAllowlist(s.ext()))
			r.Get("/{id}/archive-impact", s.handleProjectArchiveImpact)
			r.Get("/{id}/usage", s.handleProjectUsage)
			r.Get("/{id}/change-requests", s.handleListProjectChangeRequests)
			r.Post("/{id}/policy-pack", s.handleBindProjectPolicyPack)
		})
		r.Post("/change-requests/{id}/decide", s.handleDecideChangeRequest)

		// Repositories
		r.Route("/repositories", func(r chi.Router) {
			r.Get("/", s.handleListRepositories)
			r.Post("/", s.handleRegisterRepository)
			r.Get("/{id}", s.handleGetRepository)
			r.Put("/{id}", s.handleUpdateRepository)
			r.Delete("/{id}", s.handleDeleteRepository)
			r.Post("/{id}/baselines", s.handleCreateBaseline)
			r.Get("/{id}/baselines", s.handleListRepoBaselines)
			r.Post("/{id}/sync", s.handleSyncRepository)
			r.Get("/{id}/tree", s.handleRepoTree)
			r.Get("/{id}/file", s.handleRepoFile)
			r.Get("/{id}/branches", s.handleListRepoBranches)
			r.Get("/{id}/webhook", s.handleGetRepoWebhook)
			r.Post("/{id}/webhook/rotate", s.handleRotateRepoWebhookSecret)
		})

		// Sessions
		r.Route("/sessions", func(r chi.Router) {
			r.Get("/", s.handleListSessions)
			r.Post("/", s.handleOpenSession)
			r.Get("/live", s.handleLiveSessions)
			r.Get("/{id}", s.handleGetSession)
			r.Post("/{id}/close", s.handleCloseSession)
			r.Post("/{id}/pause", s.handlePauseSession)
			r.Post("/{id}/resume", s.handleResumeSession)
			r.Get("/{id}/provenance", s.handleGetProvenance)
			r.Get("/{id}/provenance/receipts", s.handleProvenanceReceipts)
			r.Get("/{id}/provenance/export", s.handleProvenanceExport)
			r.Get("/{id}/usage", s.handleGetSessionUsage)
			r.Get("/{id}/timeline", s.handleGetSessionTimeline)
			r.Get("/{id}/exchanges", s.handleGetSessionExchanges)
			r.Get("/{id}/detail", s.handleGetSessionDetail)
			r.Get("/{id}/decisions", s.handleGetSessionDecisions)
			r.Get("/{id}/replay", s.handleGetSessionReplay)
			r.Get("/{id}/visibility", s.handleGetSessionVisibility)
			r.Post("/bulk", s.handleBulkSessions)
		})

		// Model registry
		r.Route("/models", func(r chi.Router) {
			r.Get("/", s.handleListModelPackages)
			r.Post("/", s.handleRegisterModelPackage)
			r.Get("/{id}", s.handleGetModelPackage)
			r.Post("/{id}/publish", s.handlePublishModelVerified)
			r.Post("/{id}/recall", s.handleRecallModel)
			r.Get("/{id}/recall-impact", s.handleModelRecallImpact)
			r.Put("/{id}/ring", s.handleModelRingAssign)
			r.Put("/{id}", s.handleUpdateModel)
		})

		// Endpoints
		r.Route("/endpoints", func(r chi.Router) {
			r.Get("/", s.handleListEndpoints)
			r.Post("/enroll", s.handleEnrollEndpoint)
			r.Post("/{id}/lease", s.handleIssueEndpointLease)
			r.Get("/{id}", s.handleGetEndpoint)
			r.Put("/{id}", s.handleUpdateEndpoint)
			r.Post("/{id}/drain", s.handleDrainEndpoint)
			r.Post("/{id}/attest", s.handleSubmitEndpointAttestation)
		})

		// Scheduler (fleet registry) — DARI scheduler §9
		r.Route("/scheduler", func(r chi.Router) {
			r.Get("/config-public-key", s.handleSchedulerConfigPublicKey)
			r.Post("/configs", s.handleSignWorkerConfig)
			r.Get("/revocations", s.handleSchedulerRevocations)
			r.Get("/workers", s.handleSchedulerWorkersProxy)
		})

		// Policy
		r.Route("/policy", func(r chi.Router) {
			r.Get("/epochs", s.handleListEpochs)
			r.Post("/epochs", s.handleCreateEpoch)
			r.Get("/epochs/{id}/diff", s.handleEpochDiff)
			r.Get("/epochs/{id}/acks", s.handleListEpochAcks)
			r.Post("/epochs/{id}/ack", s.handleAckEpoch)
			r.Post("/epochs/{id}/require-ack", s.handleRequireEpochAck)
			r.Get("/leases", s.handleListLeases)
			r.Post("/leases", s.handleIssueLease)
			r.Get("/rules", s.handleListPolicyRules)
			r.Post("/rules", s.handleCreatePolicyRule)
			r.Post("/rules/bulk", s.handleBulkPolicyRules)
			r.Post("/rules/{id}/approve", s.handleApprovePolicyRule)
			r.Post("/rules/{id}/reject", s.handleRejectPolicyRule)
			r.Delete("/rules/{id}", s.handleDeletePolicyRule)
			r.Get("/effective", s.handleEffectivePolicy)
			r.Get("/packs", s.handleListPolicyPacks)
			r.Post("/packs", s.handleCreatePolicyPack)
			r.Post("/packs/import", s.handleImportPolicyPack)
			r.Get("/packs/{id}/export", s.handleExportPolicyPack)
			r.Post("/packs/{id}/assign", s.handleAssignPolicyPack)
			r.Get("/templates", s.handleListPolicyTemplates)
			r.Post("/templates", s.handleSavePolicyTemplate)
			r.Delete("/templates/{id}", s.handleDeletePolicyTemplate)
			r.Get("/exceptions", s.handleListPolicyExceptions)
			r.Get("/exceptions/{id}", s.handleGetPolicyException)
			r.Post("/exceptions", s.handleCreatePolicyException)
			r.Post("/exceptions/{id}/decide", s.handleDecidePolicyException)
			r.Post("/exceptions/{id}/revoke", s.handleRevokePolicyException)
		})

		// Communications
		r.Route("/communications", func(r chi.Router) {
			r.Get("/conversations", s.handleListConversations)
			r.Post("/conversations", s.handleCreateConversation)
			r.Post("/conversations/dm", s.handleConversationDM)
			r.Get("/conversations/{id}/messages", s.handleListMessages)
			r.Post("/conversations/{id}/messages", s.handleSendMessageExtended)
			r.Put("/messages/{id}", s.handleMessageEdit)
			r.Delete("/messages/{id}", s.handleMessageDelete)
			r.Post("/messages/{id}/react", s.handleMessageReact)
			r.Post("/messages/{id}/read", s.handleMessageRead)
			r.Post("/messages/{id}/link", s.handleMessageLink)
			r.Get("/presence", s.handleGetPresence)
			r.Post("/presence", s.handleUpdatePresence)
			r.Post("/broadcasts", s.handleSendBroadcast)
			r.Post("/broadcasts/send", s.handleSendBroadcastGoverned)
			r.Get("/broadcasts", s.handleListBroadcasts)
			r.Get("/broadcasts/{id}/acks", s.handleBroadcastAcks)
			r.Post("/broadcasts/{id}/ack", s.handleBroadcastAck)
			r.Post("/file-transfers", s.handleCreateFileTransfer)
			r.Get("/file-transfers", s.handleListFileTransfers)
			r.Post("/file-transfers/{id}/content", s.handleFileTransferUpload)
			r.Get("/file-transfers/{id}/download", s.handleFileTransferDownload)
			r.Post("/file-transfers/{id}/transition", s.handleFileTransferTransition)
		})

		// Work Intelligence
		r.Route("/analytics", func(r chi.Router) {
			r.Get("/usage", s.handleGetUsageSummary)
			r.Get("/usage-extended", s.handleUsageSummaryExtended)
			r.Post("/usage-export-ticket", s.handleUsageCSVTicket)
			r.Get("/usage-breakdown", s.handleGetUsageBreakdown)
			r.Get("/engineering", s.handleGetEngineeringMetrics)
			r.Get("/security", s.handleGetSecurityMetrics)
			r.Get("/scorecard", s.handleGetScorecard)
			r.Get("/export", s.handleExportMetrics)
			r.Get("/cost", s.handleGetCostAnalysis)
		})

		// Security
		r.Post("/security/check", s.handleSecurityCheck)
		r.Get("/security/policy", s.handleGetSecurityPolicy)
		r.Put("/security/policy", s.handleUpdateSecurityPolicy)
		r.Get("/security/findings", s.handleSecurityFindings)
		r.Post("/security/findings/bulk", s.handleBulkSecurityFindings)
		r.Get("/security/findings/{id}", s.handleSecurityFindingDetail)
		r.Put("/security/findings/{id}", s.handleUpdateFinding)
		r.Post("/security/findings/{id}/suppress", s.handleSuppressFinding)
		r.Post("/security/findings/{id}/reopen", s.handleReopenFinding)
		r.Get("/security/rules", s.handleSecurityRules)
		r.Get("/security/rules/overrides", s.handleListRuleOverrides)
		r.Put("/security/rules/overrides", s.handlePutRuleOverride)
		r.Delete("/security/rules/overrides", s.handleDeleteRuleOverride)
		r.Post("/security/lockdown", s.handleSecurityLockdown)
		r.Post("/security/lockdown/release", s.handleSecurityLockdownRelease)
		r.Get("/security/lockdown-impact", s.handleSecurityLockdownImpact)
		r.Post("/security/scan-session", s.handleScanSession)
		r.Get("/security/alerts", s.handleListAlertEndpoints)
		r.Post("/security/alerts", s.handleCreateAlertEndpoint)
		r.Delete("/security/alerts/{id}", s.handleDeleteAlertEndpoint)
		r.Post("/security/alerts/{id}/test", s.handleTestAlertEndpoint)
		r.Post("/security/alerts/{id}/rotate", s.handleRotateAlertEndpoint)
		r.Post("/security/alerts/{id}/disable", s.handleDisableAlertEndpoint)
		r.Put("/security/alert-operators/permissions", s.handlePutAlertOperatorPermissions)
		r.Get("/security/lexicon", s.handleGetLexicon)
		r.Put("/security/lexicon", s.handleUpdateLexicon)

		// Fleet Operations
		r.Route("/fleet", func(r chi.Router) {
			r.Get("/inventory", s.handleFleetInventoryQuery)
			r.Get("/sessions/{id}/inspect", s.handleInspectSession)
			r.Post("/actions", s.handleFleetAction)
			r.Post("/actions/bulk", s.handleFleetBulkAction)
			r.Get("/actions", s.handleFleetActionHistory)
			r.Post("/freeze", s.handleFleetChangeFreeze)
			r.Post("/force-version", s.handleFleetForceVersion)
			r.Get("/harnesses/{id}/snapshot", s.handleFleetSnapshot)
			r.Get("/approvals", s.handleFleetApprovals)
			r.Get("/impact", s.handleFleetImpactPreview)
			r.Get("/status", s.handleFleetStatus)
		})

		// Terminal ad campaign operations (PAT-1435) — super_admin only.
		r.Route("/adcampaigns", func(r chi.Router) {
			r.Post("/", s.handleADCampaignCreate)
			r.Get("/", s.handleADCampaignsList)
			r.Put("/{id}", s.handleADCampaignUpdate)
			r.Post("/{id}/lifecycle", s.handleADCampaignLifecycle)
			r.Post("/catalog/publish", s.handleADCatalogPublish)
		})

		// Marketplace registry (PAT-1438)
		r.Route("/marketplace", func(r chi.Router) {
			r.Post("/publishers", s.handleMKPublisherRegister)
			r.Get("/publishers", s.handleMKPublishersList)
			r.Post("/publishers/{id}/trust", s.handleMKPublisherVerify)
			r.Post("/publish", s.handleMKPublish)
			r.Post("/versions", s.handleMKAddVersion)
			r.Get("/search", s.handleMKSearch)
			r.Get("/listings/{slug}", s.handleMKListingDetail)
			r.Post("/update-eligibility", s.handleMKUpdateEligibility)
			r.Post("/installs", s.handleMKInstall)
			r.Get("/installs", s.handleMKInstalledList)
			r.Post("/installs/{id}/lifecycle", s.handleMKInstallLifecycle)
			r.Post("/installs/update", s.handleMKRecordUpdate)
			r.Post("/reports", s.handleMKReport)
			r.Get("/reports", s.handleMKReportsList)
			r.Post("/moderate", s.handleMKModerate)
			r.Post("/placement", s.handleMKPlacement)
		})

		// Browser governance control plane (PAT-1448)
		r.Route("/browsergov", func(r chi.Router) {
			r.Get("/policy", s.handleBGPolicyGet)
			r.Put("/policy", s.handleBGPolicyPut)
			r.Get("/policy/explain", s.handleBGPolicyExplain)
			r.Get("/actions", s.handleBGTaxonomy)
			r.Post("/tasks", s.handleBGTaskCreate)
			r.Get("/tasks", s.handleBGTasksList)
			r.Post("/tasks/{id}/close", s.handleBGTaskClose)
			r.Get("/tasks/{id}/timeline", s.handleBGTimeline)
			r.Post("/approvals", s.handleBGApprovalRequest)
			r.Post("/approvals/{id}/decide", s.handleBGApprovalDecide)
			r.Post("/effects/gate", s.handleBGEffectGate)
			r.Post("/events", s.handleBGEventIngest)
		})

		// Public cloud schedules (PAT-1437) — public-build capability.
		r.Route("/schedules", func(r chi.Router) {
			r.Post("/", s.handleCSCreate)
			r.Get("/", s.handleCSList)
			r.Post("/{id}/mutate", s.handleCSMutate)
			r.Post("/dispatch-sweep", s.handleCSDispatchSweep)
			r.Post("/report", s.handleCSReport)
			r.Get("/capabilities", s.handleCSCapabilitiesList)
			r.Post("/capabilities/connect", s.handleCSConnectFlow)
			r.Post("/delegations", s.handleCSDelegation)
		})

		// Read-only SCM lineage observation (PAT-1453) — no provider-side mutations exist.
		r.Route("/scm/observation", func(r chi.Router) {
			r.Post("/connections", s.handleSCMConnectionCreate)
			r.Get("/connections", s.handleSCMConnectionsList)
			r.Post("/connections/{id}/revoke", s.handleSCMConnectionRevoke)
			r.Get("/events", s.handleSCMEventsList)
			r.Post("/attribution", s.handleSCMBindAttribution)
			r.Get("/lineage", s.handleSCMLineage)
			r.Post("/reconcile", s.handleSCMReconcile)
		})

		// Evidence-hardened admin search (PAT-1451) — no bulk export by design.
		r.Route("/evidence-search", func(r chi.Router) {
			r.Post("/query", s.handleESSearch)
			r.Get("/open/{domain}/{id}", s.handleESOpen)
			r.Post("/reveal", s.handleESReveal)
			r.Get("/grants", s.handleESGrantsList)
			r.Post("/grants", s.handleESGrantCreate)
			r.Post("/grants/{id}/revoke", s.handleESGrantRevoke)
		})

		// Trails causal graph (PAT-1450) — no export endpoints by design.
		r.Route("/trails", func(r chi.Router) {
			r.Get("/overview", s.handleTrailsOverview)
			r.Get("/graph", s.handleTrailsGraph)
			r.Get("/nodes/{sourceType}/{sourceId}", s.handleTrailsNodeDetail)
			r.Get("/nodes/{sourceType}/{sourceId}/neighbors", s.handleTrailsNeighbors)
			r.Post("/path", s.handleTrailsPath)
			r.Post("/rebuild", s.handleTrailsRebuild)
		})

		// Model distribution campaigns (PAT-1444)
		r.Route("/models/distribution", func(r chi.Router) {
			r.Post("/entitlements", s.handleMDEntitle)
			r.Post("/entitlements/{id}/revoke", s.handleMDEntitleRevoke)
			r.Get("/entitled", s.handleMDEntitledPackages)
			r.Post("/campaigns", s.handleMDCampaignCreate)
			r.Get("/campaigns", s.handleMDCampaignsList)
			r.Post("/campaigns/preview", s.handleMDCampaignPreview)
			r.Post("/campaigns/{id}/mutate", s.handleMDCampaignMutate)
			r.Post("/campaigns/{id}/rollback", s.handleMDCampaignRollback)
			r.Post("/campaigns/{id}/promote-gate", s.handleMDPromoteGate)
			r.Post("/campaigns/{id}/approve", s.handleMDApprove)
			r.Post("/agent/report", s.handleMDAgentReport)
			r.Post("/agent/lease", s.handleMDRequestLease)
			r.Post("/reconcile-sweep", s.handleMDReconcileSweep)
			r.Post("/recall", s.handleMDRecall)
			r.Get("/transfer/{token}", s.handleMDTransfer)
		})

		// Harness release campaigns (PAT-1449)
		r.Route("/release", func(r chi.Router) {
			r.Get("/catalog", s.handleHVReleasesList)
			r.Post("/catalog", s.handleHVReleaseRegister)
			r.Post("/catalog/{id}/revoke", s.handleHVReleaseRevoke)
			r.Post("/campaigns", s.handleHVCampaignCreate)
			r.Get("/campaigns", s.handleHVCampaignsList)
			r.Post("/campaigns/{id}/mutate", s.handleHVCampaignMutate)
			r.Post("/campaigns/preview", s.handleHVCampaignPreview)
			r.Post("/heartbeat-report", s.handleHVHeartbeatReport)
			r.Get("/fleet-states", s.handleHVFleetVersionStates)
			r.Post("/exceptions", s.handleHVExceptionCreate)
			r.Get("/exceptions", s.handleHVExceptionsList)
			r.Post("/exceptions/{id}/revoke", s.handleHVExceptionRevoke)
		})

		// Incident notifications (PAT-1454)
		r.Route("/incidentnotify", func(r chi.Router) {
			r.Get("/policy", s.handleINPolicyGet)
			r.Put("/policy", s.handleINPolicyPut)
			r.Get("/groups", s.handleINGroupsList)
			r.Post("/groups", s.handleINGroupUpsert)
			r.Get("/channels", s.handleINChannelsList)
			r.Post("/channels", s.handleINChannelUpsert)
			r.Post("/channels/{id}/verify", s.handleINChannelVerify)
			r.Post("/sources", s.handleINSourceIngest)
			r.Post("/dispatch", s.handleINDispatch)
			r.Post("/escalation-sweep", s.handleINEscalationSweep)
			r.Post("/ack", s.handleINAck)
			r.Get("/incidents", s.handleINIncidentsList)
			r.Post("/incidents/{id}/resolve", s.handleINIncidentResolve)
			r.Get("/jobs", s.handleINJobsList)
			r.Post("/test", s.handleINTest)
			r.Get("/health", s.handleINHealth)
		})

		// Public status page operator surface (PAT-1439)
		r.Route("/publicstatus", func(r chi.Router) {
			r.Get("/components", s.handlePSComponentsList)
			r.Put("/components/{id}/activate", s.handlePSComponentActivate)
			r.Post("/observations", s.handlePSObservationIngest)
			r.Post("/components/{id}/override", s.handlePSOverride)
			r.Get("/incidents", s.handlePSIncidentsList)
			r.Post("/incidents", s.handlePSIncidentCreate)
			r.Put("/incidents/{id}", s.handlePSIncidentUpdate)
			r.Post("/incidents/{id}/updates", s.handlePSIncidentPostUpdate)
			r.Post("/components/{id}/rollups/rebuild", s.handlePSRollupsRebuild)
			r.Get("/components/{id}/rollups", s.handlePSRollupsList)
			r.Post("/snapshot/publish", s.handlePSSnapshotPublish)
			r.Post("/notify/dispatch", s.handlePSNotifyDispatch)
		})

		// Git/SCM
		r.Route("/scm", func(r chi.Router) {
			r.Get("/heatmaps", s.handleRepositoryHeatmap)
			r.Post("/baselines", s.handleCreateBaselineSCM)
			r.Post("/branch-protection", s.handleSetBranchProtection)
		})

		// Provenance / Code Explorer
		r.Route("/provenance", func(r chi.Router) {
			r.Get("/repos/{repoId}", s.handleGetRepoProvenance)
			r.Get("/repos/{repoId}/changesets", s.handleGetRepoChangeSets)
			r.Get("/repos/{repoId}/spans", s.handleGetRepoSpans)
			r.Get("/repos/{repoId}/stats", s.handleGetRepoProvenanceStats)
			r.Get("/repos/{repoId}/code-span", s.handleCodeSpanLookup)
			r.Post("/changeset", s.handlePostChangeSet)
			r.Post("/span", s.handlePostProvenanceSpan)
		})

		// Impact Analysis
		r.Route("/impact", func(r chi.Router) {
			r.Post("/analyze", s.handleAnalyzeChange)
		})

		// Context Firewall
		r.Route("/context", func(r chi.Router) {
			r.Post("/evaluate", s.handleEvaluateContext)
		})

		// Sandbox
		r.Route("/sandboxes", func(r chi.Router) {
			r.Get("/", s.handleListSandboxes)
			r.Post("/", s.handleCreateSandbox)
			r.Post("/{id}/destroy", s.handleDestroySandbox)
			r.Post("/{id}/snapshot", s.handleForensicSnapshot)
			// GET image-allowlist lives in services.go: nested here it is
			// shadowed by the flat /sandboxes/{id} route registered there.
			r.Put("/image-allowlist", s.handleSandboxImageAllowlist)
		})

		// Events
		r.Route("/events", func(r chi.Router) {
			r.Get("/", s.handleQueryEvents)
			r.Post("/", s.handleEmitEvent)
		})

		// Enterprise Harness Features (§33)
		r.Route("/enterprise", func(r chi.Router) {
			r.Get("/features", s.handleListEnterpriseFeatures)
			r.Put("/features/{id}", s.handleUpdateEnterpriseFeature)
			r.Get("/violations", s.handleListEnterpriseViolations)
			r.Get("/violations/{id}", s.handleGetEnterpriseViolation)
			r.Put("/violations/{id}", s.handleResolveViolation)
			r.Post("/features/seed", s.handleSeedEnterpriseFeatures)
			// D2 change-control review queue: pending connector
			// submissions (governed action envelopes) + approve/reject
			// via signed relay directives.
			r.Get("/submissions", s.handleListChangeSubmissions)
			r.Post("/submissions/{id}/approve", s.handleReviewChangeSubmission)
			r.Post("/submissions/{id}/reject", s.handleReviewChangeSubmission)
		})

		// Managed skill governance (PAT-1456)
		r.Route("/skills", func(r chi.Router) {
			r.Get("/", s.handleAdminSkillInventory)
			r.Put("/assignments", s.handleAdminSkillAssignmentUpsert)
			r.Delete("/assignments/{id}", s.handleAdminSkillAssignmentDelete)
			r.Post("/epochs/deliver", s.handleAdminSkillEpochDeliver)
			r.Post("/report", s.handleHarnessSkillReport)
		})

		// Managed system-prompt additions (PAT-1455)
		r.Route("/prompts", func(r chi.Router) {
			r.Get("/", s.handleListSystemPrompts)
			r.Get("/effective", s.handleSystemPromptEffective)
			r.Put("/", s.handleSaveSystemPrompt)
			r.Get("/{id}/versions", s.handleListSystemPromptVersions)
			r.Post("/{id}/enabled", s.handleSetSystemPromptEnabled)
			r.Post("/{id}/restore/{version}", s.handleRestoreSystemPrompt)
			r.Post("/epochs/deliver", s.handleSystemPromptEpochDeliver)
		})

		// Evidence-backed leaderboard (PAT-1440)
		r.Route("/leaderboard", func(r chi.Router) {
			r.Get("/", s.handleLeaderboardList)
			r.Get("/rubrics", s.handleLeaderboardRubrics)
			r.Put("/rubrics", s.handleLeaderboardRubrics)
			r.Get("/periods", s.handleLeaderboardPeriods)
			r.Post("/periods", s.handleLeaderboardPeriods)
			r.Post("/periods/{id}/freeze", s.handleLeaderboardFreeze)
			r.Post("/periods/{id}/generate", s.handleLeaderboardGenerate)
			r.Put("/objectives", s.handleLeaderboardObjective)
			r.Post("/corrections", s.handleLeaderboardCorrection)
			r.Post("/reviews", s.handleLeaderboardReview)
			r.Get("/export", s.handleLeaderboardExport)
		})

		// Patty Reference retrieval (PAT-1404)
		r.Route("/reference", func(r chi.Router) {
			r.Get("/sources", s.handleReferenceSources)
			r.Post("/sources", s.handleReferenceSources)
			r.Delete("/sources/{id}", s.handleReferenceSourceDelete)
			r.Get("/resolve", s.handleReferenceResolve)
			r.Get("/search", s.handleReferenceSearch)
			r.Get("/versions", s.handleReferenceListVersions)
			r.Get("/packages", s.handleReferencePackages)
			r.Post("/packages", s.handleReferencePackages)
			r.Post("/packages/{id}/activate", s.handleReferencePackageActivate)
			r.Post("/packages/{id}/rollback", s.handleReferencePackageRollback)
			r.Get("/catalog", s.handleReferenceCatalog)
			r.Post("/catalog", s.handleReferenceCatalog)
		})

		// Audit
		r.Route("/audit", func(r chi.Router) {
			r.Get("/", s.handleListAuditEventsExtended)
			r.Get("/verify", s.handleVerifyAuditChain)
			r.Get("/holds", s.handleAuditHolds)
			r.Post("/holds", s.handleAuditHolds)
			r.Delete("/holds/{id}", s.handleAuditHoldItem)
			r.Post("/evidence-bundle", s.handleAuditEvidenceBundle)
			r.Get("/siem", s.handleAuditSIEMConfig)
			r.Put("/siem", s.handleAuditSIEMConfig)
			r.Get("/export", s.handleExportAuditEvents)
		})

		// Additional service routes
		s.setupAdditionalRoutes(r, s.ext())

		// Dashboard
		r.Get("/dashboard", s.handleDashboard)

		// Provenance search (web/20 A5)
		r.Get("/provenance/search", s.handleProvenanceSearch)

		// Code explorer (web/19)
		r.Route("/code-explorer", func(r chi.Router) {
			r.Get("/spans", s.handleCodeExplorerSpans)
			r.Get("/attribution", s.handleCodeExplorerAttribution)
			r.Get("/blast", s.handleCodeExplorerBlast)
		})

		// Unified search (00-cross-cutting A11) — one endpoint across
		// entities for the command palette + cross-entity actions.
		r.Get("/search", s.handleGlobalSearch)
	})

	// Serve the React frontend (static files with SPA fallback)
	r.Handle("/*", spaHandler("web/dist"))

	s.router = r
}

// spaHandler serves static files from the given directory with SPA fallback.
// For paths that don't match a real file (like /login, /dashboard),
// it serves index.html so React Router can handle client-side routing.
func spaHandler(distDir string) http.Handler {
	fs := http.FileServer(http.Dir(distDir))
	indexPath := distDir + "/index.html"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the file exists
		path := distDir + r.URL.Path
		if _, err := os.Stat(path); err == nil {
			// File exists, serve it normally
			fs.ServeHTTP(w, r)
			return
		}
		// File doesn't exist — check if it's an API route
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/health") {
			http.NotFound(w, r)
			return
		}
		// SPA fallback: serve index.html
		http.ServeFile(w, r, indexPath)
	})
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// ListenAndServe starts the API server.
// sessionSweepInterval is the web/02 A4 idle/TTL sweep cadence.
const sessionSweepInterval = time.Minute

var sessionSweepRunning atomic.Bool

// sweepSessions transitions sessions past their idle window to idle
// and auto-closes sessions past their TTL (web/02 A4 — the status
// machine's idle state was previously unreachable).
// bridgeSpineToRealtime polls the durable event spine for governed
// exchange + security events and forwards them to the realtime hub.
// 2s cadence: live enough for an activity feed, bounded DB load.
func (s *Server) bridgeSpineToRealtime() {
	cursors := make(map[string]int64)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		orgIDs := s.ext().Realtime.ActiveSSEOrganizations()
		if len(orgIDs) == 0 {
			continue
		}
		active := make(map[string]struct{}, len(orgIDs))
		for _, orgID := range orgIDs {
			active[orgID] = struct{}{}
			if _, initialized := cursors[orgID]; initialized {
				continue
			}
			// Start with a short committed overlap so an event between the
			// page snapshot and SSE registration cannot disappear. After this
			// initialization, monotonic per-tenant chain_seq is the cursor.
			var earliest models.AuditEvent
			if err := s.db.Where("organization_id = ? AND event_type LIKE ? AND created_at >= ?", orgID, "cp.exchange.%", time.Now().Add(-time.Minute)).
				Order("chain_seq ASC").First(&earliest).Error; err == nil {
				cursors[orgID] = earliest.ChainSeq - 1
				continue
			}
			var latest int64
			if err := s.db.Model(&models.AuditEvent{}).Where("organization_id = ?", orgID).Select("COALESCE(MAX(chain_seq), 0)").Scan(&latest).Error; err != nil {
				continue
			}
			cursors[orgID] = latest
		}
		for orgID := range cursors {
			if _, connected := active[orgID]; !connected {
				delete(cursors, orgID)
			}
		}
		for _, orgID := range orgIDs {
			// A per-tenant cap prevents one hot organization from consuming a
			// global page and starving every organization ordered after it.
			var events []models.AuditEvent
			if err := s.db.Where("organization_id = ? AND event_type LIKE ? AND chain_seq > ?", orgID, "cp.exchange.%", cursors[orgID]).
				Order("chain_seq ASC").Limit(250).Find(&events).Error; err != nil {
				continue
			}
			for _, e := range events {
				var details map[string]interface{}
				_ = json.Unmarshal([]byte(e.Details), &details)
				exchangeID, _ := details["exchange_id"].(string)
				s.ext().Realtime.FanoutLocal(e.OrganizationID, "exchange.update", map[string]interface{}{
					"session_id": e.ResourceID, "exchange_id": exchangeID,
					"state": strings.TrimPrefix(e.EventType, "cp.exchange."), "evidence_events": details["evidence_events"],
				})
				cursors[orgID] = e.ChainSeq
			}
		}
	}
}

func (s *Server) sweepSessions() {
	now := time.Now()
	if s.db.Dialector.Name() == "postgres" {
		_ = s.db.Connection(func(conn *gorm.DB) error {
			var acquired bool
			if err := conn.Raw("SELECT pg_try_advisory_lock(?)", int64(0x5043435053574545)).Scan(&acquired).Error; err != nil || !acquired {
				return err
			}
			defer conn.Exec("SELECT pg_advisory_unlock(?)", int64(0x5043435053574545))
			s.sweepSessionsAtDB(conn, now)
			return nil
		})
		return
	}
	if !sessionSweepRunning.CompareAndSwap(false, true) {
		return
	}
	defer sessionSweepRunning.Store(false)
	s.sweepSessionsAtDB(s.db, now)
}

func (s *Server) sweepSessionsAt(now time.Time) {
	s.sweepSessionsAtDB(s.db, now)
}

func (s *Server) sweepSessionsAtDB(db *gorm.DB, now time.Time) {
	deadline := time.Now().Add(5 * time.Second)
	for processed := 0; processed < 10000 && time.Now().Before(deadline); {
		var candidates []models.Session
		query := db.Where("status IN ?", models.SessionNonTerminalStatuses())
		switch db.Dialector.Name() {
		case "postgres":
			query = query.Where(`
				(session_ttl > 0 AND opened_at + make_interval(secs => session_ttl) <= ?)
				OR (status = 'active' AND idle_ttl > 0 AND COALESCE(last_activity_at, opened_at) + make_interval(secs => idle_ttl) <= ?)`, now, now)
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		case "sqlite":
			query = query.Where(`
				(session_ttl > 0 AND datetime(opened_at, '+' || session_ttl || ' seconds') <= datetime(?))
				OR (status = 'active' AND idle_ttl > 0 AND datetime(CASE WHEN last_activity_at IS NULL OR last_activity_at = '' THEN opened_at ELSE last_activity_at END, '+' || idle_ttl || ' seconds') <= datetime(?))`, now, now)
		default:
			query = query.Where("session_ttl > 0 OR (status = ? AND idle_ttl > 0)", "active")
		}
		if err := query.Order("id ASC").Limit(500).Find(&candidates).Error; err != nil || len(candidates) == 0 {
			return
		}
		for _, sess := range candidates {
			if time.Now().After(deadline) {
				return
			}
			opened, err := time.Parse(time.RFC3339, sess.OpenedAt)
			if err != nil {
				continue
			}
			target := ""
			if sess.SessionTTL > 0 && now.Sub(opened) > time.Duration(sess.SessionTTL)*time.Second {
				target = "closed"
			} else if sess.Status == "active" {
				last := opened
				if sess.LastActivityAt != "" {
					if t, err := time.Parse(time.RFC3339, sess.LastActivityAt); err == nil {
						last = t
					}
				}
				if sess.IdleTTL > 0 && now.Sub(last) > time.Duration(sess.IdleTTL)*time.Second {
					target = "idle"
				}
			}
			if target == "" {
				continue
			}
			action := "idle_timeout"
			if target == "closed" {
				action = "session_ttl_expired"
			}
			req := sessionlifecycle.Request{OrganizationID: sess.OrganizationID, SessionRef: sess.ID, Target: target, Action: action, Reason: "automatic lifecycle enforcement", ActorType: "system"}
			if target == "idle" {
				req.ExpectedLastActivityAt = &sess.LastActivityAt
				req.ExpectedIdleTTL = &sess.IdleTTL
			}
			s.sessionLifecycle.Transition(req)
		}
		processed += len(candidates)
		if len(candidates) < 500 {
			return
		}
	}
}

func (s *Server) ListenAndServe(addr string) error {
	go func() {
		ticker := time.NewTicker(sessionSweepInterval)
		defer ticker.Stop()
		for range ticker.C {
			s.sweepSessions()
			// Reopen findings whose suppression window expired
			// (security C1).
			if n := s.security.SweepSuppressions(); n > 0 {
				log.Printf("api: reopened %d expired suppressions", n)
			}
			// Auto-disable contractors past their contract window
			// (web/01 A5).
			if n := s.identity.SweepExpiredContractors(); n > 0 {
				log.Printf("api: suspended %d expired contractors", n)
			}
			// Continuous compliance re-assessment (web/08 C3): orgs
			// with a recent snapshot get re-assessed weekly so statuses
			// track the real system as features ship.
			if n := s.ext().Compliance.ContinuousReassess(7 * 24 * time.Hour); n > 0 {
				log.Printf("api: re-assessed %d compliance targets", n)
			}
			// Comms retention (web/13 C5): purge soft-deleted messages
			// and expired transfers with their stored content.
			if n := s.sweepCommsRetention(); n > 0 {
				log.Printf("api: comms retention purged %d items", n)
			}
			// SIEM forwarding (web/17 E): push unseen audit events to
			// each org's configured SIEM webhook.
			var siemOrgs []models.OrgSetting
			s.db.Where("key = 'audit.siem_webhook' AND value != ''").Find(&siemOrgs)
			for _, cfg := range siemOrgs {
				if n := s.forwardAuditToSIEM(cfg.OrganizationID); n > 0 {
					log.Printf("api: forwarded %d audit events to SIEM for %s", n, cfg.OrganizationID)
				}
			}
		}
	}()
	// Spine→hub bridge: the relay (a separate process) writes governed
	// exchange events to the durable spine (audit trail); this poller
	// fans them into the realtime hub so the admin SSE actually
	// carries LIVE exchange activity (web/21).
	go s.bridgeSpineToRealtime()
	log.Printf("api: listening on %s", addr)
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	return srv.ListenAndServe()
}

// --- Middleware ---

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow OPTIONS for CORS preflight
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}
		// Internal callers and test fixtures may attach claims using the
		// package-private context key after authenticating through their own
		// boundary. Network clients cannot manufacture this value. JWT requests
		// still take the path below and are always introspected against current
		// lifecycle state.
		if _, ok := claimsFromCtx(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		claims, err := s.auth.AuthMiddleware(authHeader)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		if err := s.auth.ValidateClaimsLifecycle(claims); err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}

		// Inject claims into context
		ctx := ctxWithClaims(r.Context(), claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v interface{}) error {
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}

func getOrgID(r *http.Request) string {
	if claims, ok := claimsFromCtx(r.Context()); ok {
		return claims.OrganizationID
	}
	return ""
}

func getRole(r *http.Request) string {
	if claims, ok := claimsFromCtx(r.Context()); ok {
		return claims.Role
	}
	return ""
}

// --- Handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Format(time.RFC3339),
		"service":   "pccp-control-plane",
		"version":   "0.1.0",
		"ca_pubkey": s.identity.CAPublicKeyHex(),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		MFACode  string `json:"mfa_code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = identity.NormalizeEmail(req.Email)
	if !throttleCheck(req.Email) {
		writeError(w, http.StatusTooManyRequests, "로그인 시도가 잠겼습니다 · too many attempts — try again in 15 minutes")
		return
	}
	var admin identity.AdminCredentials
	if err := s.db.Where("email = ?", req.Email).First(&admin).Error; err == nil && admin.MFAEnrolled {
		// MFA challenge (web/25 B): password + TOTP code. The TOTP
		// check is throttled too — an attacker holding the password
		// must not be free to grind the 10^6 code space at line speed.
		token, err := s.auth.Login(req.Email, req.Password)
		if err != nil {
			throttleRecordFailure(req.Email)
			writeError(w, http.StatusUnauthorized, "로그인 실패 / invalid credentials")
			return
		}
		if !verifyTOTPAcct(req.Email, admin.MFASecret, req.MFACode) {
			throttleRecordFailure(req.Email)
			writeError(w, http.StatusUnauthorized, "MFA 코드 필요 또는 불일치 · mfa code required or mismatch")
			return
		}
		throttleClear(req.Email)
		writeJSON(w, http.StatusOK, map[string]string{"token": token, "mfa": "verified"})
		return
	}
	token, err := s.auth.Login(req.Email, req.Password)
	if err != nil {
		throttleRecordFailure(req.Email)
		writeError(w, http.StatusUnauthorized, "로그인 실패 / invalid credentials")
		return
	}
	throttleClear(req.Email)
	writeJSON(w, http.StatusOK, map[string]string{
		"token": token,
	})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	configuredToken := strings.TrimSpace(os.Getenv("PCCP_BOOTSTRAP_TOKEN"))
	presentedToken := strings.TrimSpace(r.Header.Get("X-PCCP-Bootstrap-Token"))
	if configuredToken == "" {
		writeError(w, http.StatusServiceUnavailable, "bootstrap is not enabled")
		return
	}
	if presentedToken == "" || !hmac.Equal([]byte(presentedToken), []byte(configuredToken)) {
		writeError(w, http.StatusForbidden, "invalid bootstrap authorization")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var req struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		OrgName    string `json:"org_name"`
		Profile    string `json:"profile"`     // enterprise, public, sovereign (web/26 A)
		PolicyPack string `json:"policy_pack"` // CSAP, ISMS-P, ... (web/26 B)
		DemoData   bool   `json:"demo_data"`   // explicit opt-in; default false
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Profile == "" {
		req.Profile = "enterprise"
	}
	switch req.Profile {
	case "enterprise", "public", "sovereign":
	default:
		writeError(w, http.StatusBadRequest, "profile must be enterprise, public, or sovereign")
		return
	}

	var org models.Organization
	bootstrapErr := s.db.Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "pccp-initial-bootstrap").Error; err != nil {
				return err
			}
		}
		for _, model := range []interface{}{&models.Organization{}, &models.User{}, &identity.AdminCredentials{}} {
			var count int64
			if err := tx.Model(model).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return errBootstrapNotPristine
			}
		}
		org = models.Organization{
			Name: req.OrgName, NameKo: req.OrgName, Slug: "default", Profile: req.Profile,
			Type: req.Profile, Status: "active",
		}
		if err := tx.Create(&org).Error; err != nil {
			return err
		}
		if err := s.auth.BootstrapAdminWithDB(tx, req.Email, req.Password, org.ID); err != nil {
			return err
		}
		settings := []models.OrgSetting{{
			Base: models.Base{ID: models.GenerateID("os")}, OrganizationID: org.ID,
			Key: "bootstrap.profile", Value: org.Profile,
		}}
		if req.PolicyPack != "" {
			settings = append(settings, models.OrgSetting{
				Base: models.Base{ID: models.GenerateID("os")}, OrganizationID: org.ID,
				Key: "bootstrap.policy_pack", Value: req.PolicyPack,
			})
		}
		if err := tx.Create(&settings).Error; err != nil {
			return err
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: org.ID, EventType: "cp.auth.bootstrap", ActorType: "system",
			Action: "bootstrap", ResourceType: "organization", ResourceID: org.ID,
			Details: "initial deployment bootstrap", Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
		}).Error
	})
	if bootstrapErr != nil {
		if errors.Is(bootstrapErr, errBootstrapNotPristine) || errors.Is(bootstrapErr, identity.ErrAlreadyBootstrapped) {
			writeError(w, http.StatusConflict, "system is already bootstrapped")
			return
		}
		writeError(w, http.StatusInternalServerError, bootstrapErr.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"organization_id": org.ID,
		"profile":         org.Profile,
		"demo_data":       req.DemoData,
		"message":         "부트스트랩 완료",
	})
}

// Generic CRUD handlers
func (s *Server) handleGetSeatUsage(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)

	var org models.Organization
	if err := s.db.Where("id = ?", orgID).First(&org).Error; err != nil {
		writeError(w, http.StatusNotFound, "organization not found")
		return
	}

	var userCount int64
	s.db.Model(&models.User{}).Where("organization_id = ? AND status != ?", orgID, "offboarded").Count(&userCount)

	var harnessCount int64
	s.db.Model(&models.Harness{}).Where("organization_id = ? AND status NOT IN ?", orgID, []string{"revoked"}).Count(&harnessCount)

	var activeSessions int64
	s.db.Model(&models.Session{}).Where("organization_id = ? AND status = ?", orgID, "active").Count(&activeSessions)

	result := map[string]interface{}{
		"organization_id": org.ID,
		"plan_tier":       org.PlanTier,
		"user_seats": map[string]interface{}{
			"used":        userCount,
			"max":         org.MaxUserSeats,
			"available":   org.MaxUserSeats - int(userCount),
			"utilization": fmt.Sprintf("%.0f%%", float64(userCount)/float64(org.MaxUserSeats)*100),
		},
		"harness_seats": map[string]interface{}{
			"used":        harnessCount,
			"max":         org.MaxHarnessSeats,
			"available":   org.MaxHarnessSeats - int(harnessCount),
			"utilization": fmt.Sprintf("%.0f%%", float64(harnessCount)/float64(org.MaxHarnessSeats)*100),
		},
		"active_sessions":   activeSessions,
		"plan_renewal_date": org.PlanRenewalDate,
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleListOrganizations(w http.ResponseWriter, r *http.Request) {
	var orgs []models.Organization
	result := s.db.Where("id = ?", getOrgID(r)).Find(&orgs)
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, result.Error.Error())
		return
	}
	writeJSON(w, http.StatusOK, orgs)
}

func (s *Server) handleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		NameKo  string `json:"name_ko"`
		Slug    string `json:"slug"`
		Profile string `json:"profile"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Profile == "" {
		req.Profile = "enterprise"
	}
	org, err := s.identity.CreateOrganization(req.Name, req.NameKo, req.Slug, req.Profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, org)
}

func (s *Server) handleGetOrganization(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if id != orgID {
		writeError(w, http.StatusNotFound, "조직을 찾을 수 없습니다")
		return
	}
	var org models.Organization
	if err := s.db.First(&org, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "조직을 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, org)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.User{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}

	// Server-side pagination (when ?page= is provided, returns {data,total})
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		page, _ := strconv.Atoi(pageStr)
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		search := r.URL.Query().Get("search")
		if size == 0 {
			size = 25
		}
		if size < 1 || size > 100 {
			size = 100
		}
		if page == 0 {
			page = 1
		}
		if search != "" {
			q = q.Where("name LIKE ? OR email LIKE ? OR name_ko LIKE ? OR employee_id LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%", "%"+search+"%")
		}
		// Server-side filters (web/01 B3): business unit, status, role.
		if v := r.URL.Query().Get("business_unit"); v != "" {
			q = q.Where("business_unit_id = ?", v)
		}
		if v := r.URL.Query().Get("status"); v != "" {
			q = q.Where("status = ?", v)
		}
		if v := r.URL.Query().Get("role"); v != "" {
			q = q.Where("id IN (SELECT user_id FROM user_roles WHERE role_id = ?)", v)
		}
		if v := r.URL.Query().Get("auth_method"); v != "" {
			q = q.Where("auth_method = ?", v)
		}
		switch r.URL.Query().Get("sort") {
		case "name":
			q = q.Order("name ASC")
		case "name_ko":
			q = q.Order("name_ko ASC")
		case "last_login":
			q = q.Order("last_login_at DESC NULLS LAST")
		case "email":
			q = q.Order("email ASC")
		default:
			q = q.Order("created_at DESC")
		}
		var total int64
		if err := q.Count(&total).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "could not count users")
			return
		}
		var users []models.User
		if err := q.Offset((page - 1) * size).Limit(size).Find(&users).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "could not list users")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": s.decorateUsers(r, users), "total": total, "page": page, "size": size})
		return
	}

	// Full list (backward compatible — used by cross-page lookups)
	var users []models.User
	if err := q.Limit(1000).Find(&users).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "could not list users")
		return
	}
	writeJSON(w, http.StatusOK, s.decorateUsers(r, users))
}

// decorateUsers keeps list-level relationship summaries consistent with the
// user detail view. Harness membership is stored as a JSON array, so matching
// is done after decoding rather than with an unsafe substring query.
func (s *Server) decorateUsers(r *http.Request, users []models.User) []map[string]interface{} {
	if len(users) == 0 {
		return []map[string]interface{}{}
	}
	organizationSet := map[string]struct{}{}
	userSet := map[string]models.User{}
	for _, user := range users {
		organizationSet[user.OrganizationID] = struct{}{}
		userSet[user.ID] = user
	}
	organizationIDs := make([]string, 0, len(organizationSet))
	userIDs := make([]string, 0, len(userSet))
	for id := range organizationSet {
		organizationIDs = append(organizationIDs, id)
	}
	for id := range userSet {
		userIDs = append(userIDs, id)
	}

	roleIDsByUser := map[string][]string{}
	var assignments []models.UserRole
	if err := s.db.Where("organization_id IN ? AND user_id IN ?", organizationIDs, userIDs).Find(&assignments).Error; err == nil {
		for _, assignment := range assignments {
			roleIDsByUser[assignment.UserID] = append(roleIDsByUser[assignment.UserID], assignment.RoleID)
		}
	}

	deviceOwner := map[string]string{}
	deviceIDs := make([]string, 0)
	var devices []models.Device
	if err := s.db.Where("organization_id IN ? AND user_id IN ? AND status != ?", organizationIDs, userIDs, "revoked").Find(&devices).Error; err == nil {
		for _, device := range devices {
			deviceOwner[device.ID] = device.UserID
			deviceIDs = append(deviceIDs, device.ID)
		}
	}
	harnessesByUser := map[string]map[string]struct{}{}
	var harnesses []models.Harness
	bindings := make([]string, 0, len(userIDs)+1)
	bindingArgs := make([]interface{}, 0, len(userIDs)+1)
	if len(deviceIDs) > 0 {
		bindings = append(bindings, "device_id IN ?")
		bindingArgs = append(bindingArgs, deviceIDs)
	}
	for _, userID := range userIDs {
		bindings = append(bindings, "allowed_users LIKE ?")
		bindingArgs = append(bindingArgs, "%\""+userID+"\"%")
	}
	harnessQuery := s.db.Where("organization_id IN ? AND status != ?", organizationIDs, "revoked")
	if len(bindings) > 0 {
		harnessQuery = harnessQuery.Where("("+strings.Join(bindings, " OR ")+")", bindingArgs...)
	}
	if err := harnessQuery.Find(&harnesses).Error; err == nil {
		for _, harness := range harnesses {
			bound := parseAllowedUsers(harness.AllowedUsers)
			if owner := deviceOwner[harness.DeviceID]; owner != "" {
				bound = append(bound, owner)
			}
			for _, userID := range bound {
				user, present := userSet[userID]
				if !present || user.OrganizationID != harness.OrganizationID {
					continue
				}
				if harnessesByUser[userID] == nil {
					harnessesByUser[userID] = map[string]struct{}{}
				}
				harnessesByUser[userID][harness.ID] = struct{}{}
			}
		}
	}
	lastAdministrators, lastAdministratorErr := identity.LastOrganizationAdministratorIDs(s.db, organizationIDs)
	if lastAdministratorErr != nil {
		lastAdministrators = make(map[string]bool, len(userIDs))
		for _, userID := range userIDs {
			lastAdministrators[userID] = true
		}
	}

	out := make([]map[string]interface{}, 0, len(users))
	for _, user := range users {
		row := decorateUserForRequest(r, user, lastAdministrators[user.ID])
		roleIDs := roleIDsByUser[user.ID]
		if roleIDs == nil {
			roleIDs = []string{}
		}
		row["role_ids"] = roleIDs
		row["harness_count"] = len(harnessesByUser[user.ID])
		out = append(out, row)
	}
	return out
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !canAdministerUsers(getRole(r)) {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	var req struct {
		OrganizationID string `json:"organization_id"`
		Email          string `json:"email"`
		Name           string `json:"name"`
		NameKo         string `json:"name_ko"`
		AuthMethod     string `json:"auth_method"`
		Title          string `json:"title"`
		BusinessUnitID string `json:"business_unit_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AuthMethod == "" {
		req.AuthMethod = "local"
	}
	req.Email = identity.NormalizeEmail(req.Email)
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	orgID := getOrgID(r)
	if req.OrganizationID != "" && req.OrganizationID != orgID {
		writeError(w, http.StatusForbidden, "cannot create a user in another organization")
		return
	}
	var user *models.User
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var duplicate int64
		if err := tx.Model(&models.User{}).
			Where("organization_id = ? AND LOWER(email) = ?", orgID, req.Email).
			Count(&duplicate).Error; err != nil {
			return err
		}
		if duplicate > 0 {
			return errUserEmailExists
		}
		// Enforce user seat limit in the same transaction as creation.
		var org models.Organization
		if err := tx.First(&org, "id = ?", orgID).Error; err != nil {
			return err
		}
		if org.MaxUserSeats > 0 {
			var userCount int64
			if err := tx.Model(&models.User{}).Where("organization_id = ? AND status != 'offboarded'", orgID).Count(&userCount).Error; err != nil {
				return err
			}
			if userCount >= int64(org.MaxUserSeats) {
				return fmt.Errorf("%w:%d:%d", errUserSeatLimit, userCount, org.MaxUserSeats)
			}
		}
		created, err := s.identity.CreateUserWithDB(tx, orgID, req.Email, req.Name, req.NameKo, req.AuthMethod, "")
		if err != nil {
			return err
		}
		if err := tx.Model(created).Updates(map[string]interface{}{
			"title": req.Title, "business_unit_id": req.BusinessUnitID,
		}).Error; err != nil {
			return err
		}
		created.Title = req.Title
		created.BusinessUnitID = req.BusinessUnitID
		if err := tx.Model(&identity.AdminCredentials{}).
			Where("organization_id = ? AND email = ? AND (user_id = '' OR user_id IS NULL)", orgID, req.Email).
			Update("user_id", created.ID).Error; err != nil {
			return err
		}
		details, _ := json.Marshal(map[string]string{"email": req.Email, "name": req.Name})
		if err := tx.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.user.created", ActorType: "admin", Action: "create_user",
			ResourceType: "user", ResourceID: created.ID, Details: string(details), Result: "success",
			OccurredAt: time.Now().Format(time.RFC3339),
		}).Error; err != nil {
			return err
		}
		user = created
		return nil
	})
	if errors.Is(err, errUserEmailExists) {
		writeError(w, http.StatusConflict, "이미 등록된 이메일입니다 · Email already exists: "+req.Email)
		return
	}
	if errors.Is(err, errUserSeatLimit) {
		writeError(w, http.StatusPaymentRequired, "사용자 좌석 한도 초과 · User seat limit reached")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var user models.User
	if err := s.db.First(&user, "id = ? AND organization_id = ?", id, getOrgID(r)).Error; err != nil {
		writeError(w, http.StatusNotFound, "사용자를 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, s.decorateSingleUser(r, user))
}

func (s *Server) handleListHarnesses(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.Harness{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	// Entity filters (harnesses C4): status/risk/ring/user are
	// server-side so the UI never client-slices the full table.
	for _, key := range []string{"status", "risk_state", "build_channel"} {
		if v := r.URL.Query().Get(key); v != "" {
			q = q.Where(key+" = ?", v)
		}
	}
	if v := r.URL.Query().Get("user"); v != "" {
		q = q.Where("allowed_users LIKE ?", "%"+v+"%")
	}
	// Column sort (harnesses UX12): server-side ordering.
	sortBy := r.URL.Query().Get("sort")
	switch sortBy {
	case "binary_version":
		q = q.Order("binary_version DESC")
	case "risk_state":
		q = q.Order("risk_state DESC, created_at DESC")
	case "enrolled_at":
		q = q.Order("created_at DESC")
	case "":
		q = q.Order("created_at DESC")
	default:
		q = q.Order("created_at DESC")
	}
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		page, _ := strconv.Atoi(pageStr)
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		search := r.URL.Query().Get("search")
		if size == 0 {
			size = 25
		}
		if page == 0 {
			page = 1
		}
		if search != "" {
			q = q.Where("harness_id LIKE ? OR binary_version LIKE ?", "%"+search+"%", "%"+search+"%")
		}
		var total int64
		q.Count(&total)
		var harnesses []models.Harness
		q.Offset((page - 1) * size).Limit(size).Find(&harnesses)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": decorateHarnesses(harnesses), "total": total, "page": page, "size": size,
		})
		return
	}
	var harnesses []models.Harness
	q.Find(&harnesses)
	writeJSON(w, http.StatusOK, decorateHarnesses(harnesses))
}

// decorateHarnesses adds the stale flag (harnesses B2): enrolled/active
// harnesses whose last heartbeat is older than the heartbeat window are
// flagged stale for the UI and risk scoring.
func decorateHarnesses(harnesses []models.Harness) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(harnesses))
	now := time.Now()
	for _, h := range harnesses {
		row := map[string]interface{}{}
		b, _ := json.Marshal(h)
		json.Unmarshal(b, &row)
		row["stale"] = false
		if h.Status == "enrolled" || h.Status == "active" {
			if t, err := time.Parse(time.RFC3339, h.LastHeartbeat); err != nil {
				row["stale"] = true
			} else if now.Sub(t) > harnessStaleAfter {
				row["stale"] = true
			}
		}
		out = append(out, row)
	}
	return out
}

func (s *Server) handleEnrollHarness(w http.ResponseWriter, r *http.Request) {
	var req identity.EnrollHarnessRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.EnrollmentMode == "" {
		req.EnrollmentMode = "sso"
	}
	callerOrgID := getOrgID(r)
	if req.OrganizationID != "" && req.OrganizationID != callerOrgID {
		writeError(w, http.StatusForbidden, "cannot enroll a harness in another organization")
		return
	}
	req.OrganizationID = callerOrgID
	if req.OrganizationID == "" {
		writeError(w, http.StatusBadRequest, "organization_id is required — enroll from an organization-scoped operator session")
		return
	}
	// One-time enrollment code flow (harnesses B3): when the harness
	// self-enrolls with a code, validate + burn it instead of trusting
	// a raw admin paste.
	if strings.TrimSpace(req.EnrollmentCode) != "" {
		req.EnrollmentMode = "code"
	}
	// Forced-version floor (harnesses C2): vulnerable builds below the
	// org's minimum version are blocked at enrollment.
	if fv := s.ext().Korean.GetForcedHarnessVersion(req.OrganizationID); fv != nil {
		if korean.IsVersionBelowFloor(req.BinaryVersion, fv.MinVersion) {
			writeError(w, http.StatusForbidden, fmt.Sprintf(
				"하네스 버전이 최소 요구 버전 미만입니다 · Harness version %s below forced minimum %s (ring %s)",
				req.BinaryVersion, fv.MinVersion, fv.ReleaseRing))
			return
		}
	}
	var harness *models.Harness
	var cred *dari.PeerCredential
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var hOrg models.Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&hOrg, "id = ?", req.OrganizationID).Error; err != nil {
			return err
		}
		if hOrg.MaxHarnessSeats > 0 {
			var harnessCount int64
			if err := tx.Model(&models.Harness{}).Where("organization_id = ? AND status != 'revoked'", req.OrganizationID).Count(&harnessCount).Error; err != nil {
				return err
			}
			if harnessCount >= int64(hOrg.MaxHarnessSeats) {
				return fmt.Errorf("%w:%d:%d", errHarnessSeatLimit, harnessCount, hOrg.MaxHarnessSeats)
			}
		}
		if code := strings.TrimSpace(req.EnrollmentCode); code != "" {
			if err := s.consumeEnrollmentCodeWithDB(tx, req.OrganizationID, code, req.UserID, req.HarnessID); err != nil {
				return err
			}
		}
		var enrollErr error
		harness, cred, enrollErr = s.identity.EnrollHarnessWithDB(tx, req)
		if enrollErr != nil {
			return enrollErr
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: req.OrganizationID, EventType: "cp.harness.enrolled", ActorID: getActorID(r), ActorType: "admin",
			Action: "enroll_harness", ResourceType: "harness", ResourceID: harness.ID,
			Details: fmt.Sprintf("harness_id: %s, user_id: %s", req.HarnessID, req.UserID),
			Result:  "success", OccurredAt: time.Now().Format(time.RFC3339),
		}).Error
	})
	if err != nil {
		if errors.Is(err, errHarnessSeatLimit) {
			writeError(w, http.StatusPaymentRequired, "하네스 좌석 한도 초과 · Harness seat limit reached")
			return
		}
		if errors.Is(err, identity.ErrUserNotActive) || errors.Is(err, identity.ErrUserNotFound) {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"harness":    harness,
		"credential": cred,
	})
}

func (s *Server) handleGetHarness(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var harness models.Harness
	if err := s.db.Where("organization_id = ? AND (id = ? OR harness_id = ?)", getOrgID(r), id, id).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "하네스를 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, harness)
}

func (s *Server) handleRevokeHarness(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsolePermission(w, r, permissionSessionsManage) {
		return
	}
	id := chi.URLParam(r, "id")
	var harness models.Harness
	if err := s.db.Where("organization_id = ? AND (id = ? OR harness_id = ?)", getOrgID(r), id, id).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if r.Body != http.NoBody {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	err := s.fleet.PerformAction(fleet.ActionRequest{
		Context: r.Context(), OrganizationID: harness.OrganizationID, HarnessID: harness.HarnessID,
		Action: fleet.ActionRevokeCert, Reason: req.Reason, PerformedBy: getActorID(r),
	})
	if err != nil {
		var executionErr *fleet.ActionExecutionError
		if errors.As(err, &executionErr) && executionErr.LocalApplied {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"status": "revoked", "relay_propagated": executionErr.RelayDelivered, "error": err.Error()})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "revoked", "relay_propagated": true})
}

// harnessStaleAfter is the heartbeat window after which an
// enrolled/active harness is flagged stale (harnesses B2).
const harnessStaleAfter = 10 * time.Minute

// consumeEnrollmentCode validates and burns a one-time enrollment code
// (harnesses B3). Unused + unexpired codes are consumed by the
// enrolling harness ID; anything else fails closed.
func (s *Server) consumeEnrollmentCodeWithDB(tx *gorm.DB, orgID, code, userID, harnessID string) error {
	if userID == "" {
		return fmt.Errorf("enrollment code flow requires user_id")
	}
	if harnessID == "" {
		return fmt.Errorf("enrollment code flow requires harness_id")
	}
	var ec models.EnrollmentCode
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ? AND organization_id = ?", code, orgID).First(&ec).Error; err != nil {
		return fmt.Errorf("등록 코드가 유효하지 않습니다 · Invalid enrollment code")
	}
	if exp, err := time.Parse(time.RFC3339, ec.ExpiresAt); err == nil && time.Now().After(exp) {
		return fmt.Errorf("등록 코드가 만료되었습니다 · Enrollment code expired")
	}
	if ec.UserID != "" && ec.UserID != userID {
		return fmt.Errorf("등록 코드가 다른 사용자에게 발급되었습니다 · Code issued to a different user")
	}
	// Atomic single-use consume: the UPDATE itself is the gate, so two
	// concurrent redeems cannot both win the read-then-write window.
	res := tx.Model(&models.EnrollmentCode{}).
		Where("id = ? AND used = ?", ec.ID, false).
		Updates(map[string]interface{}{"used": true, "used_by": harnessID})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("등록 코드가 이미 사용되었습니다 · Enrollment code already used")
	}
	return nil
}

// handleHarnessHeartbeat receives a harness heartbeat over the control
// plane (harnesses B2): updates LastHeartbeat/LastAttestation and the
// device's live facts so stale detection + the fleet view reflect
// reality instead of enroll-time snapshots.
func (s *Server) handleHarnessHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID string `json:"organization_id"`
		HarnessID      string `json:"harness_id"`
		BinaryVersion  string `json:"binary_version,omitempty"`
		DeviceHostname string `json:"device_hostname,omitempty"`
		DeviceOS       string `json:"device_os,omitempty"`
		DeviceOSVer    string `json:"device_os_version,omitempty"`
		DeviceArch     string `json:"device_arch,omitempty"`
		IPAddress      string `json:"ip_address,omitempty"`
		Attestation    string `json:"attestation,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = getOrgID(r)
	}
	if req.HarnessID == "" {
		writeError(w, http.StatusBadRequest, "harness_id is required")
		return
	}
	var harness models.Harness
	if err := s.db.Where("harness_id = ? AND organization_id = ?", req.HarnessID, orgID).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "harness not found")
		return
	}
	now := time.Now().Format(time.RFC3339)
	updates := map[string]interface{}{"last_heartbeat": now}
	if req.Attestation != "" {
		updates["last_attestation"] = now
	}
	if req.BinaryVersion != "" {
		updates["binary_version"] = req.BinaryVersion
	}
	s.db.Model(&harness).Updates(updates)
	if harness.DeviceID != "" {
		devUpdates := map[string]interface{}{"last_seen": now}
		if req.DeviceHostname != "" {
			devUpdates["hostname"] = req.DeviceHostname
		}
		if req.DeviceOS != "" {
			devUpdates["os"] = req.DeviceOS
		}
		if req.DeviceOSVer != "" {
			devUpdates["os_version"] = req.DeviceOSVer
		}
		if req.DeviceArch != "" {
			devUpdates["arch"] = req.DeviceArch
		}
		if req.IPAddress != "" {
			devUpdates["ip_address"] = req.IPAddress
		}
		s.db.Model(&models.Device{}).Where("id = ?", harness.DeviceID).Updates(devUpdates)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "heartbeat_at": now})
}

// handleGetHarnessDetail returns the full vertical for the harness
// detail page (harnesses C1/C5): harness + device posture + decoded
// credential (issuer/validity/revocation) + allowed users + sessions +
// attestation + audit.
func (s *Server) handleGetHarnessDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var harness models.Harness
	if err := s.db.Where("organization_id = ? AND (id = ? OR harness_id = ?)", orgID, id, id).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "하네스를 찾을 수 없습니다")
		return
	}
	result := map[string]interface{}{"harness": harness}

	if harness.DeviceID != "" {
		var device models.Device
		if s.db.Where("organization_id = ? AND id = ?", orgID, harness.DeviceID).First(&device).Error == nil {
			result["device"] = device
		}
	}

	// Decode the stored PPC for credential metadata.
	if harness.CredentialJSON != "" {
		cred := map[string]interface{}{}
		if raw, err := hex.DecodeString(harness.CredentialJSON); err == nil {
			if sign1, derr := dari.DecodeCOSESign1(raw); derr == nil {
				if pc, perr := dari.DecodePeerCredential(sign1.Payload); perr == nil {
					now := time.Now().UnixMilli()
					cred["serial"] = pc.Serial
					cred["issuer"] = pc.Issuer
					cred["subject_peer_id"] = pc.SubjectPeerID
					cred["not_before"] = time.UnixMilli(pc.NotBefore).Format(time.RFC3339)
					cred["not_after"] = time.UnixMilli(pc.NotAfter).Format(time.RFC3339)
					cred["valid"] = now >= pc.NotBefore && now <= pc.NotAfter
					cred["build_channel"] = pc.BuildChannel
					cred["revocation_authority"] = pc.RevocationAuthority
					cred["revoked"] = false
					var rev models.CredentialRevocationRecord
					if s.db.First(&rev, "serial = ?", pc.Serial).Error == nil {
						cred["revoked"] = true
						cred["revoked_at"] = rev.RevokedAtRFC
						cred["revoked_reason"] = rev.Reason
					}
				}
			}
		}
		if len(cred) > 0 {
			result["credential"] = cred
		}
	}

	// Allowed users (harnesses UX1): resolve the JSON id array.
	var allowedUserIDs []string
	json.Unmarshal([]byte(harness.AllowedUsers), &allowedUserIDs)
	if len(allowedUserIDs) > 0 {
		var users []models.User
		s.db.Where("organization_id = ? AND id IN ?", orgID, allowedUserIDs).Find(&users)
		result["allowed_users"] = users
	}

	var sessions []models.Session
	s.db.Where("organization_id = ? AND harness_id = ?", orgID, harness.HarnessID).Order("created_at DESC").Find(&sessions)
	result["sessions"] = sessions

	// Attestation history: device posture + attestation audit events.
	var attEvents []models.AuditEvent
	s.db.Where("organization_id = ? AND resource_type = ? AND (resource_id = ? OR resource_id = ?)", orgID, "harness", harness.ID, harness.HarnessID).
		Where("action LIKE ?", "%attestation%").
		Order("occurred_at DESC").Limit(20).Find(&attEvents)
	result["attestation_events"] = attEvents

	var auditEvents []models.AuditEvent
	s.db.Where("organization_id = ? AND resource_id IN ?", orgID, []string{harness.ID, harness.HarnessID}).
		Order("occurred_at DESC").Limit(50).Find(&auditEvents)
	result["audit_events"] = auditEvents

	// Stale flag (B2) + version-floor status (C2).
	stale := false
	if harness.Status == "enrolled" || harness.Status == "active" {
		if t, err := time.Parse(time.RFC3339, harness.LastHeartbeat); err != nil {
			stale = true
		} else if time.Since(t) > harnessStaleAfter {
			stale = true
		}
	}
	result["stale"] = stale
	if fv := s.ext().Korean.GetForcedHarnessVersion(harness.OrganizationID); fv != nil {
		result["forced_version"] = fv
		result["version_blocked"] = korean.IsVersionBelowFloor(harness.BinaryVersion, fv.MinVersion)
	}

	writeJSON(w, http.StatusOK, result)
}

// pushRelayDirective delivers a control-plane lifecycle event to the
// live relay admin channel (harnesses C3 / security B1). Without
// PCCP_RELAY_ADMIN_URL configured the call reports honestly; DB-level
// enforcement (status gates) still applies on the next exchange.
func (s *Server) pushRelayDirective(commandType, orgID, harnessID, reason string, payload map[string]interface{}) error {
	return s.pushRelayDirectiveContext(stdctx.Background(), commandType, orgID, harnessID, reason, payload)
}

func (s *Server) pushRelayDirectiveContext(ctx stdctx.Context, commandType, orgID, harnessID, reason string, payload map[string]interface{}) error {
	return fleet.DeliverRelayDirective(fleet.RelayDirective{
		Context:        ctx,
		OrganizationID: orgID,
		HarnessID:      harnessID,
		CommandType:    commandType,
		Reason:         reason,
		IssuedBy:       "control-plane",
		Parameters:     payload,
	})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	q := s.db.Model(&models.Project{}).Where("organization_id = ?", orgID)
	// Server-side filters (projects B5): status + group affiliate.
	for _, key := range []string{"status", "group_affiliate"} {
		if v := r.URL.Query().Get(key); v != "" {
			q = q.Where(key+" = ?", v)
		}
	}
	// Card sort (projects UX10): server-side ordering.
	switch r.URL.Query().Get("sort") {
	case "name":
		q = q.Order("name_ko ASC, name ASC")
	case "sessions":
		q = q.Order("(SELECT COUNT(*) FROM sessions WHERE sessions.project_id = projects.id) DESC")
	case "created":
		q = q.Order("created_at DESC")
	default:
		q = q.Order("created_at DESC")
	}
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		page, _ := strconv.Atoi(pageStr)
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		search := r.URL.Query().Get("search")
		if size == 0 {
			size = 25
		}
		if page == 0 {
			page = 1
		}
		if search != "" {
			q = q.Where("name LIKE ? OR name_ko LIKE ? OR slug LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
		}
		var total int64
		q.Count(&total)
		var projects []models.Project
		q.Offset((page - 1) * size).Limit(size).Find(&projects)
		rows, err := s.decorateProjects(projects, orgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": rows, "total": total, "page": page, "size": size,
		})
		return
	}
	var projects []models.Project
	q.Find(&projects)
	rows, err := s.decorateProjects(projects, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// decorateProjects attaches per-project aggregates (repos, sessions,
// real membership count) so list cards render true numbers without
// client-side cross-page joins (projects B1/UX4).
func (s *Server) decorateProjects(projects []models.Project, orgID string) ([]projectListView, error) {
	out := make([]projectListView, 0, len(projects))
	if len(projects) == 0 {
		return out, nil
	}
	projectIDs := make([]string, 0, len(projects))
	identifiers := make([]string, 0)
	for _, proj := range projects {
		projectIDs = append(projectIDs, proj.ID)
		classes, _ := parseModelClasses(proj.AllowedModelClasses)
		identifiers = append(identifiers, classes...)
	}
	resolver, err := s.newAllowedModelResolver(orgID, identifiers)
	if err != nil {
		return nil, err
	}
	type countRow struct {
		ProjectID string
		Count     int64
	}
	memberCounts := map[string]int64{}
	repositoryCounts := map[string]int64{}
	type sessionCountRow struct {
		ProjectID   string
		Count       int64
		ActiveCount int64
	}
	sessionCounts := map[string]sessionCountRow{}
	var counts []countRow
	if err := s.db.Model(&models.ProjectMember{}).
		Select("project_id, COUNT(*) AS count").
		Where("organization_id = ? AND project_id IN ?", orgID, projectIDs).
		Group("project_id").Scan(&counts).Error; err != nil {
		return nil, err
	}
	for _, count := range counts {
		memberCounts[count.ProjectID] = count.Count
	}
	counts = nil
	if err := s.db.Model(&models.Repository{}).
		Select("project_id, COUNT(*) AS count").
		Where("organization_id = ? AND project_id IN ?", orgID, projectIDs).
		Group("project_id").Scan(&counts).Error; err != nil {
		return nil, err
	}
	for _, count := range counts {
		repositoryCounts[count.ProjectID] = count.Count
	}
	var sessions []sessionCountRow
	if err := s.db.Model(&models.Session{}).
		Select("project_id, COUNT(*) AS count, SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) AS active_count").
		Where("organization_id = ? AND project_id IN ?", orgID, projectIDs).
		Group("project_id").Scan(&sessions).Error; err != nil {
		return nil, err
	}
	for _, count := range sessions {
		sessionCounts[count.ProjectID] = count
	}
	for _, proj := range projects {
		out = append(out, projectListView{
			projectView:        projectViewRow(proj, resolver),
			MemberCount:        memberCounts[proj.ID],
			RepositoryCount:    repositoryCounts[proj.ID],
			SessionCount:       sessionCounts[proj.ID].Count,
			ActiveSessionCount: sessionCounts[proj.ID].ActiveCount,
		})
	}
	return out, nil
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID string          `json:"organization_id"`
		Name           string          `json:"name"`
		NameKo         string          `json:"name_ko"`
		Slug           string          `json:"slug"`
		AllowedModels  json.RawMessage `json:"allowed_models"`
		Description    string          `json:"description"`
		ProjectCode    string          `json:"project_code"`
		GroupAffiliate string          `json:"group_affiliate"`
		PolicyPackID   string          `json:"policy_pack_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	allowed := []string{defaultAllowedModelID}
	if len(req.AllowedModels) > 0 {
		var policyState string
		allowed, policyState = parseModelClasses(string(req.AllowedModels))
		if policyState == modelPolicyInvalid {
			writeError(w, http.StatusBadRequest, "allowed_models must be an array of non-empty model identifiers")
			return
		}
	}
	resolver, err := s.newAllowedModelResolver(orgID, allowed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if resolver.resolvesRestricted(allowed) {
		writeError(w, http.StatusForbidden, "allowed model is not available to this organization")
		return
	}
	if req.PolicyPackID != "" {
		var pack models.PolicyPack
		if err := s.db.First(&pack, "id = ? AND organization_id = ?", req.PolicyPackID, orgID).Error; err != nil {
			writeError(w, http.StatusNotFound, "policy pack not found")
			return
		}
	}
	proj, err := s.identity.CreateProject(orgID, req.Name, req.NameKo, req.Slug, allowed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A1/A3: description + Korean enterprise attrs + policy pack are
	// persisted (they were silently dropped before).
	updates := map[string]interface{}{}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.ProjectCode != "" {
		updates["project_code"] = req.ProjectCode
	}
	if req.GroupAffiliate != "" {
		updates["group_affiliate"] = req.GroupAffiliate
	}
	if req.PolicyPackID != "" {
		updates["policy_pack_id"] = req.PolicyPackID
	}
	if len(updates) > 0 {
		s.db.Model(proj).Updates(updates)
	}
	row, err := s.projectViewRow(*proj)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	var proj models.Project
	if err := s.db.First(&proj, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	row, err := s.projectViewRow(proj)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.Repository{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	// Server-side filters (repositories C4/UX10): project, sensitivity,
	// status.
	for _, key := range []string{"project_id", "sensitivity", "status"} {
		if v := r.URL.Query().Get(key); v != "" {
			q = q.Where(key+" = ?", v)
		}
	}
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		page, _ := strconv.Atoi(pageStr)
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		search := r.URL.Query().Get("search")
		if size == 0 {
			size = 25
		}
		if page == 0 {
			page = 1
		}
		if search != "" {
			q = q.Where("name LIKE ? OR clone_url LIKE ? OR scm_provider LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
		}
		var total int64
		q.Count(&total)
		var repos []models.Repository
		q.Offset((page - 1) * size).Limit(size).Find(&repos)
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": repos, "total": total, "page": page, "size": size})
		return
	}
	var repos []models.Repository
	q.Find(&repos)
	writeJSON(w, http.StatusOK, repos)
}

func (s *Server) handleRegisterRepository(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID string `json:"organization_id"`
		ProjectID      string `json:"project_id"`
		Name           string `json:"name"`
		FullName       string `json:"full_name"`
		DefaultBranch  string `json:"default_branch"`
		Sensitivity    string `json:"sensitivity"`
		CloneURL       string `json:"clone_url"`
		SCMProvider    string `json:"scm_provider"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = getOrgID(r)
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}
	if req.Sensitivity == "" {
		req.Sensitivity = "internal"
	}
	// Clone URL validation (repositories UX8): reject garbage before
	// it reaches the sync pipeline.
	if req.CloneURL != "" && !strings.HasPrefix(req.CloneURL, "http") && !strings.HasPrefix(req.CloneURL, "git@") && !strings.HasPrefix(req.CloneURL, "file://") && !strings.HasPrefix(req.CloneURL, "/") {
		writeError(w, http.StatusBadRequest, "clone_url must be an https/ssh/file URL")
		return
	}
	repo, err := s.identity.RegisterRepository(orgID, req.ProjectID, req.Name, req.FullName, req.DefaultBranch, req.Sensitivity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.CloneURL != "" || req.SCMProvider != "" {
		repo.CloneURL = req.CloneURL
		repo.SCMProvider = req.SCMProvider
		s.db.Save(repo)
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.repository.registered",
		ActorType:      "admin",
		Action:         "register_repository",
		ResourceType:   "repository",
		ResourceID:     repo.ID,
		Details:        fmt.Sprintf(`{"name":"%s","scm_provider":"%s"}`, req.Name, req.SCMProvider),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, repo)
}

func (s *Server) handleGetRepository(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var repo models.Repository
	if err := s.db.First(&repo, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "저장소를 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) handleCreateBaseline(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "id")
	var req struct {
		Branch        string `json:"branch"`
		CommitSHA     string `json:"commit_sha"`
		CommitMessage string `json:"commit_message"`
		AuthorName    string `json:"author_name"`
		AuthorEmail   string `json:"author_email"`
		CommittedAt   string `json:"committed_at"`
		TreeDigest    string `json:"tree_digest"`
		SessionID     string `json:"session_id"`
		OrgID         string `json:"org_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	baseline, err := s.identity.CreateBaseline(req.OrgID, repoID, req.Branch, req.CommitSHA,
		req.CommitMessage, req.AuthorName, req.AuthorEmail, req.CommittedAt, req.TreeDigest, req.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, baseline)
}

// handleListRepoBaselines lists the immutable task baselines recorded
// for a repository (repositories B1, §18.3).
func (s *Server) handleListRepoBaselines(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "id")
	var baselines []models.RepoBaseline
	s.db.Where("repository_id = ?", repoID).Order("created_at DESC").Find(&baselines)
	writeJSON(w, http.StatusOK, baselines)
}

// handleSyncRepository runs the SCM connector sync (repositories C1):
// clones the repo and records HEAD + sync status, feeding the file
// browser.
func (s *Server) handleSyncRepository(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Org-scoped lookup: a repo from another tenant is indistinguishable
	// from a missing one (no cross-tenant trigger or existence oracle).
	var repo models.Repository
	if err := s.db.Where("organization_id = ? AND id = ?", getOrgID(r), id).First(&repo).Error; err != nil {
		writeError(w, http.StatusNotFound, "저장소를 찾을 수 없습니다")
		return
	}
	if repo.CloneURL == "" {
		writeError(w, http.StatusBadRequest, "clone_url이 없습니다 · Repository has no clone URL")
		return
	}
	head, err := s.gitscm.SyncRepository(r.Context(), &repo)
	if err != nil {
		// A live sync holds the claim: 409, matching handleRepoTree.
		if strings.Contains(err.Error(), "already in progress") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "synced", "head": head, "sync_status": "synced",
	})
}

// handleRepoTree lists a directory in the synced clone (repositories
// C2 file browser).
func (s *Server) handleRepoTree(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	path := r.URL.Query().Get("path")
	entries, err := s.gitscm.ListTree(id, path)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleRepoFile returns file content from the synced clone
// (repositories C2 file browser).
func (s *Server) handleRepoFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	path := r.URL.Query().Get("path")
	content, err := s.gitscm.ReadFile(id, path)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"path": path, "content": string(content)})
}

// handleListRepoBranches lists the repo's governed branches with their
// protection rules (repositories A4/C3).
func (s *Server) handleListRepoBranches(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var repo models.Repository
	if s.db.First(&repo, "id = ?", id).Error != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var branches []models.Branch
	s.db.Where("repository_id = ?", id).Find(&branches)
	// Default branch exists implicitly even before the first rule row.
	found := false
	for _, b := range branches {
		if b.Name == repo.DefaultBranch {
			found = true
		}
	}
	if !found {
		branches = append([]models.Branch{{
			RepositoryID:    id,
			Name:            repo.DefaultBranch,
			ProtectionLevel: "standard",
			Status:          "active",
		}}, branches...)
	}
	writeJSON(w, http.StatusOK, branches)
}

// handleGetRepoWebhook returns the webhook ingest URL and a safe secret
// projection for the repository (repositories UX13). The signing secret is
// never returned by a normal read.
func (s *Server) handleGetRepoWebhook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var repo models.Repository
	if err := s.db.Where("id = ? AND organization_id = ?", id, getOrgID(r)).First(&repo).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	maskedSecret := ""
	if repo.WebhookSecret != "" {
		maskedSecret = "••••••••"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"url":           scheme + "://" + r.Host + "/webhooks/scm/" + id,
		"secret_set":    repo.WebhookSecret != "",
		"masked_secret": maskedSecret,
	})
}

// handleRotateRepoWebhookSecret issues a fresh one-time webhook secret to an
// organization administrator. The secret is intentionally omitted from audit
// evidence and from all normal read projections.
func (s *Server) handleRotateRepoWebhookSecret(w http.ResponseWriter, r *http.Request) {
	role := getRole(r)
	if role != "admin" && role != "owner" && role != "super_admin" {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	id := chi.URLParam(r, "id")
	var repo models.Repository
	if err := s.db.Where("id = ? AND organization_id = ?", id, getOrgID(r)).First(&repo).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.rotateRepoWebhookSecret(&repo, getOperatorEmail(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rotated", "secret": repo.WebhookSecret})
}

func (s *Server) rotateRepoWebhookSecret(repo *models.Repository, actor string) error {
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return err
	}
	repo.WebhookSecret = hex.EncodeToString(secretBytes)
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(repo).Error; err != nil {
			return err
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: repo.OrganizationID,
			EventType:      "repository.webhook_secret_rotated",
			ActorID:        actor,
			ActorType:      "admin",
			Action:         "repository.webhook_secret_rotated",
			ResourceType:   "repository",
			ResourceID:     repo.ID,
			Details:        `{"secret_rotated":true}`,
			Result:         "success",
			OccurredAt:     time.Now().UTC().Format(time.RFC3339),
		}).Error
	})
}

// handleScmWebhook ingests an SCM webhook delivery (repositories C1):
// HMAC-verified against the repo secret; records the event for
// provenance (§18.6) and refreshes sync state on push.
func (s *Server) handleScmWebhook(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "repoId")
	var repo models.Repository
	if err := s.db.First(&repo, "id = ?", repoID).Error; err != nil {
		writeError(w, http.StatusNotFound, "unknown repository")
		return
	}
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable body")
		return
	}
	// HMAC verification: X-PCCP-Signature = hex(sha256(secret, body)).
	sig := r.Header.Get("X-PCCP-Signature")
	if repo.WebhookSecret == "" || sig == "" {
		writeError(w, http.StatusUnauthorized, "missing webhook signature")
		return
	}
	mac := hmac.New(sha256.New, []byte(repo.WebhookSecret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		eventType = r.Header.Get("X-Gitlab-Event")
	}
	if eventType == "" {
		eventType = "push"
	}
	if err := s.gitscm.IngestWebhook(&repo, eventType, payload); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "received", "event": eventType})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.Session{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		page, _ := strconv.Atoi(pageStr)
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		search := r.URL.Query().Get("search")
		if size == 0 {
			size = 25
		}
		if page == 0 {
			page = 1
		}
		if search != "" {
			q = q.Where("title LIKE ? OR session_id LIKE ? OR harness_id LIKE ? OR user_id LIKE ? OR project_id LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%", "%"+search+"%", "%"+search+"%")
		}
		// Server-side filters (web/02 B4): status, model class, user,
		// project, time range.
		if v := r.URL.Query().Get("status"); v != "" {
			q = q.Where("status = ?", v)
		}
		if v := r.URL.Query().Get("model"); v != "" {
			q = q.Where("model_class = ?", v)
		}
		if v := r.URL.Query().Get("user"); v != "" {
			q = q.Where("user_id = ?", v)
		}
		if v := r.URL.Query().Get("harness_id"); v != "" {
			q = q.Where("harness_id = ?", v)
		}
		if v := r.URL.Query().Get("project"); v != "" {
			q = q.Where("project_id = ?", v)
		}
		if v := r.URL.Query().Get("repository"); v != "" {
			q = q.Where("repository_id = ?", v)
		}
		if v := r.URL.Query().Get("range"); v != "" {
			switch v {
			case "24h":
				q = q.Where("created_at >= ?", time.Now().Add(-24*time.Hour).Format(time.RFC3339))
			case "7d":
				q = q.Where("created_at >= ?", time.Now().Add(-7*24*time.Hour).Format(time.RFC3339))
			case "30d":
				q = q.Where("created_at >= ?", time.Now().Add(-30*24*time.Hour).Format(time.RFC3339))
			}
		}
		switch r.URL.Query().Get("sort") {
		case "duration":
			q = q.Order("created_at ASC")
		case "title":
			q = q.Order("title ASC")
		default:
			q = q.Order("created_at DESC")
		}
		var total int64
		if err := q.Count(&total).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var sessions []models.Session
		if err := q.Offset((page - 1) * size).Limit(size).Find(&sessions).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": sessions, "total": total, "page": page, "size": size})
		return
	}
	var sessions []models.Session
	if err := q.Order("created_at DESC").Limit(100).Find(&sessions).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleOpenSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID string `json:"organization_id"`
		HarnessID      string `json:"harness_id"`
		UserID         string `json:"user_id"`
		ProjectID      string `json:"project_id"`
		RepositoryID   string `json:"repository_id"`
		Branch         string `json:"branch"`
		BaselineID     string `json:"baseline_id"`
		Title          string `json:"title"`
		TaskPurpose    string `json:"task_purpose"`
		ModelClass     string `json:"model_class"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := getOrgID(r)
	if req.OrganizationID != "" && req.OrganizationID != orgID {
		writeError(w, http.StatusForbidden, "cannot open a session in another organization")
		return
	}
	if strings.TrimSpace(req.UserID) == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	claims, ok := claimsFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "authenticated user binding is required")
		return
	}
	if !canAdministerUsers(claims.Role) && (claims.Subject == "" || claims.Subject != req.UserID) {
		writeError(w, http.StatusForbidden, "session user must match the authenticated subject")
		return
	}
	if req.HarnessID == "" {
		req.HarnessID = "console:" + req.UserID
	} else if strings.HasPrefix(req.HarnessID, "console:") && req.HarnessID != "console:"+req.UserID {
		writeError(w, http.StatusForbidden, "console harness identity must match the session user")
		return
	}
	// Change-freeze enforcement (PRD §33.13) — block AI sessions on frozen repos.
	if req.RepositoryID != "" {
		if frozen, freeze, _ := s.ext().Korean.IsChangeFrozen(orgID, req.RepositoryID); frozen && freeze != nil {
			writeError(w, http.StatusForbidden, fmt.Sprintf("변경 중단 중 · Change freeze: %s", freeze.FreezeReasonKo))
			return
		}
	}
	// A2: repository sessions must anchor to an immutable baseline —
	// provenance binds to exact repo state (§18.3).
	if req.RepositoryID != "" && req.BaselineID == "" {
		writeError(w, http.StatusBadRequest, "저장소 세션은 베이스라인이 필요합니다 · repository sessions require a baseline (commit SHA + tree digest)")
		return
	}
	if req.BaselineID != "" && req.RepositoryID != "" {
		var baseline models.RepoBaseline
		if err := s.db.Where("id = ? AND repository_id = ? AND org_id = ?", req.BaselineID, req.RepositoryID, orgID).First(&baseline).Error; err != nil {
			writeError(w, http.StatusBadRequest, "베이스라인을 찾을 수 없습니다 · baseline not found for repository")
			return
		}
	}
	var sess *models.Session
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var organization models.Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", orgID).First(&organization).Error; err != nil {
			return errors.New("organization not found")
		}
		// Lock the lifecycle subject before validating grants. Project roster
		// mutations take the same lock, so membership cannot disappear between
		// this check and the session/lease/audit commit.
		if _, err := identity.LockActiveUser(tx, orgID, req.UserID); err != nil {
			return err
		}
		if !strings.HasPrefix(req.HarnessID, "console:") {
			if err := identity.ValidateActiveHarnessUserBinding(tx, orgID, req.HarnessID, req.UserID); err != nil {
				return err
			}
			if restriction, err := models.HarnessAdmissionRestriction(tx, orgID, req.HarnessID); err != nil {
				return fmt.Errorf("fleet desired state unavailable: %w", err)
			} else if restriction != nil {
				return fmt.Errorf("harness admission blocked by %s", restriction.Action)
			}
		}
		if req.RepositoryID != "" && req.ProjectID == "" {
			return errors.New("repository-scoped sessions require a project")
		}
		if req.ProjectID != "" {
			var project models.Project
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND organization_id = ?", req.ProjectID, orgID).First(&project).Error; err != nil {
				return errors.New("project not found in organization")
			}
			if project.Status == "archived" {
				return errors.New("project is archived")
			}
			lockdownActive, err := models.ActiveSecurityLockdown(tx, orgID, req.ProjectID)
			if err != nil {
				return err
			}
			if lockdownActive {
				return errors.New("security lockdown is active for the requested project")
			}
			var membershipCount int64
			if err := tx.Model(&models.ProjectMember{}).
				Where("organization_id = ? AND project_id = ? AND user_id = ?", orgID, req.ProjectID, req.UserID).
				Count(&membershipCount).Error; err != nil {
				return err
			}
			if membershipCount != 1 {
				return errors.New("user is not a member of the project")
			}
			if req.RepositoryID != "" {
				var repository models.Repository
				if err := tx.Where("id = ? AND organization_id = ? AND project_id = ?", req.RepositoryID, orgID, req.ProjectID).
					First(&repository).Error; err != nil {
					return errors.New("repository does not belong to the project")
				}
			}
		} else {
			lockdownActive, err := models.ActiveSecurityLockdown(tx, orgID, "")
			if err != nil {
				return err
			}
			if lockdownActive {
				return errors.New("organization security lockdown is active")
			}
		}
		for _, permission := range []string{"session:open", "inference:use"} {
			allowed, err := s.identity.EvaluateScopedEntitlementWithDB(tx, orgID, req.UserID, permission, req.ProjectID, req.RepositoryID)
			if err != nil {
				return err
			}
			if !allowed {
				return fmt.Errorf("%s entitlement is required for the requested scope", permission)
			}
		}
		var epoch models.PolicyEpoch
		if err := tx.Where("organization_id = ? AND status = ?", orgID, "active").Order("epoch_number DESC").First(&epoch).Error; err != nil {
			return fmt.Errorf("no active policy epoch: %w", err)
		}
		if epoch.RequiresAck && !s.policy.HasAcked(orgID, epoch.EpochID, req.UserID) {
			return fmt.Errorf("policy epoch requires acknowledgement")
		}
		created, err := s.identity.OpenSessionWithDB(tx, orgID, req.HarnessID, req.UserID, req.ProjectID,
			req.RepositoryID, req.Branch, req.BaselineID, req.Title, req.TaskPurpose, req.ModelClass)
		if err != nil {
			return err
		}
		var allowedModels []string
		_ = json.Unmarshal([]byte(epoch.AllowedModelsJSON), &allowedModels)
		lease, err := s.policy.IssueCapabilityLeaseWithDB(tx, policy.IssueLeaseRequest{
			OrganizationID: orgID, SubjectPeerID: req.HarnessID, UserID: req.UserID,
			SessionID: created.SessionID, PolicyEpochID: epoch.EpochID,
			AllowedModels: allowedModels, Validity: time.Duration(created.SessionTTL) * time.Second,
		})
		if err != nil {
			return err
		}
		created.PolicyEpochID = epoch.EpochID
		created.LeaseID = lease.LeaseID
		if err := tx.Model(&models.Session{}).Where("id = ? AND organization_id = ?", created.ID, orgID).
			Updates(map[string]interface{}{"policy_epoch_id": epoch.EpochID, "lease_id": lease.LeaseID}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.session.opened", ActorID: getActorID(r), ActorType: "admin",
			Action: "open_session", ResourceType: "session", ResourceID: created.ID,
			Details: fmt.Sprintf(`{"title":"%s","harness_id":"%s"}`, req.Title, req.HarnessID),
			Result:  "success", OccurredAt: time.Now().Format(time.RFC3339),
		}).Error; err != nil {
			return err
		}
		sess = created
		return nil
	})
	if err != nil {
		if errors.Is(err, identity.ErrUserNotActive) || errors.Is(err, identity.ErrUserNotFound) || errors.Is(err, identity.ErrHarnessUserBinding) ||
			strings.Contains(err.Error(), "policy") || strings.Contains(err.Error(), "project") || strings.Contains(err.Error(), "repository") ||
			strings.Contains(err.Error(), "entitlement") {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		writeError(w, http.StatusForbidden, "활성 정책 epoch 없음 · no active policy epoch — session refused")
		return
	}
	s.ext().Realtime.NotifySessionUpdate(orgID, sess.SessionID, "active")
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var sess models.Session
	if err := s.db.Where("organization_id = ? AND (id = ? OR session_id = ?)", getOrgID(r), id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "세션을 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	s.applySessionTransition(w, r, "closed", "close")
}

func (s *Server) handleCompatChatCompletions(w http.ResponseWriter, r *http.Request) {
	// BLOCKED: OpenAI-compatible model invocation on the Control Plane is not
	// permitted (DARI-only, PRD §10.11, §38.1). The Harness must use the DARI
	// Relay for all model inference.
	writeError(w, http.StatusGone, "OpenAI-compatible endpoint disabled — use DARI Relay for model invocation (§10.11, §38.1)")
	return
}

// handleCompatChatCompletionsLegacy contains the original OpenAI-compat proxy
// logic, retained for reference. It is NOT registered as a route — the endpoint
// is blocked per §10.11. Remove entirely once all callers have migrated to DARI.
func (s *Server) handleCompatChatCompletionsLegacy(w http.ResponseWriter, r *http.Request) {
	// OpenAI-compatible chat completions adapter
	// Proxies to PIA for inference
	var req map[string]interface{}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Get model from request
	modelName, _ := req["model"].(string)
	if modelName == "" {
		modelName = "default"
	}

	messages, _ := req["messages"].([]interface{})

	// Build a simple prompt from messages
	var promptParts []string
	for _, msg := range messages {
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		promptParts = append(promptParts, role+": "+content)
	}
	prompt := "You are a helpful coding assistant.\n\n" + strings.Join(promptParts, "\n") + "\n\nAssistant:"

	// Check if PIA URL is configured
	piaURL := os.Getenv("PCCP_PIA_URL")
	if piaURL == "" {
		// Return a mock response for dev/testing
		resp := map[string]interface{}{
			"id":      "chatcmpl-" + time.Now().Format("20060102150405"),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   modelName,
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "[PCCP] Inference adapter ready. Configure PCCP_PIA_URL for model serving. Prompt received: " + prompt[:min(len(prompt), 100)],
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     len(prompt) / 4,
				"completion_tokens": 20,
				"total_tokens":      len(prompt)/4 + 20,
			},
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Proxy to PIA
	maxTokens := 4096
	if mt, ok := req["max_tokens"].(float64); ok {
		maxTokens = int(mt)
	}

	piaReq := map[string]interface{}{
		"model":       os.Getenv("PCCP_VLLM_MODEL"),
		"prompt":      prompt,
		"max_tokens":  maxTokens,
		"temperature": 0.0,
		"stream":      false,
	}

	piaBody, _ := json.Marshal(piaReq)
	piaResp, err := http.Post(piaURL+"/v1/completions", "application/json", bytes.NewReader(piaBody))
	if err != nil {
		writeError(w, http.StatusBadGateway, "PIA unreachable: "+err.Error())
		return
	}
	defer piaResp.Body.Close()

	var piaResult map[string]interface{}
	json.NewDecoder(piaResp.Body).Decode(&piaResult)

	// Convert to OpenAI format
	choices, _ := piaResult["choices"].([]interface{})
	var respChoices []map[string]interface{}
	if len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if text, ok := choice["text"].(string); ok {
				respChoices = append(respChoices, map[string]interface{}{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": text,
					},
					"finish_reason": "stop",
				})
			}
		}
	}

	resp := map[string]interface{}{
		"id":      "chatcmpl-" + time.Now().Format("20060102150405"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   modelName,
		"choices": respChoices,
	}
	if usage, ok := piaResult["usage"].(map[string]interface{}); ok {
		resp["usage"] = usage
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetSessionExchanges(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.WithContext(r.Context()).Where("organization_id = ? AND (id = ? OR session_id = ?)", orgID, id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	// TODO(web audit): nothing writes models.PromptExchange today, so this
	// query returns an empty set. The required write location is
	// internal/relay/service.go GovernInference — the completion point of
	// every governed exchange (it has the exchange ID, model/endpoint,
	// token counts, verdict, and latency this table needs). The API server
	// is not in that path and must not fabricate rows; land the write in
	// the relay, then this endpoint serves them as-is.
	var exchanges []models.PromptExchange
	s.db.Where("session_id = ?", sess.SessionID).Order("created_at ASC").Find(&exchanges)
	writeJSON(w, http.StatusOK, exchanges)
}

func (s *Server) handleGetSessionTimeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.Where("organization_id = ? AND (id = ? OR session_id = ?)", orgID, id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	// Load actions (tool calls, commands, model requests)
	var actions []models.ActionEnvelope
	s.db.Where("organization_id = ? AND session_id = ?", orgID, sess.SessionID).Order("occurred_at DESC").Limit(100).Find(&actions)

	// Load change sets (code changes)
	var changeSets []models.ChangeSet
	s.db.Where("organization_id = ? AND session_id = ?", orgID, sess.SessionID).Order("created_at DESC").Find(&changeSets)

	// Load security findings
	var findings []models.SecurityFinding
	s.db.Where("organization_id = ? AND session_id = ?", orgID, sess.SessionID).Order("occurred_at DESC").Find(&findings)

	// Load approvals
	var approvals []models.Approval
	s.db.Where("organization_id = ? AND session_id = ?", orgID, sess.SessionID).Order("created_at DESC").Find(&approvals)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session":     sess,
		"actions":     actions,
		"change_sets": changeSets,
		"findings":    findings,
		"approvals":   approvals,
	})
}

func (s *Server) handleGetSessionUsage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if !requireUsagePermission(w, r, usageReadAction(r), "session", id) {
		return
	}
	var sess models.Session
	if err := s.db.Where("organization_id = ? AND (id = ? OR session_id = ?)", orgID, id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	days, since, until := usageWindowFromRequest(r, time.Now())
	report, err := s.buildUsageReport(orgID, usageFilterFromRequest(r, usageFilter{SessionID: sess.SessionID, Projection: usageProjectionLedger}), fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeUsageReportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleGetProvenance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.Where("organization_id = ? AND (id = ? OR session_id = ?)", orgID, id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	chain, err := s.provenance.GetProvenanceChain(sess.OrganizationID, sess.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, chain)
}

func (s *Server) handleListModelPackages(w http.ResponseWriter, r *http.Request) {
	var pkgs []models.ModelPackage
	s.db.Order("created_at DESC").Find(&pkgs)
	writeJSON(w, http.StatusOK, pkgs)
}

func (s *Server) handleRegisterModelPackage(w http.ResponseWriter, r *http.Request) {
	// Use a raw map to accept mixed types, then convert to model fields
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	pkg := models.ModelPackage{}
	// Simple string fields
	for field, jsonKey := range map[string]string{} {
		_ = field
		_ = jsonKey
	}
	if v, ok := raw["package_id"]; ok {
		json.Unmarshal(v, &pkg.PackageID)
	}
	if v, ok := raw["model_id"]; ok {
		json.Unmarshal(v, &pkg.ModelID)
	}
	if v, ok := raw["name"]; ok {
		json.Unmarshal(v, &pkg.Name)
	}
	if v, ok := raw["name_ko"]; ok {
		json.Unmarshal(v, &pkg.NameKo)
	}
	if v, ok := raw["family"]; ok {
		json.Unmarshal(v, &pkg.Family)
	}
	if v, ok := raw["version"]; ok {
		json.Unmarshal(v, &pkg.Version)
	}
	if v, ok := raw["release"]; ok {
		json.Unmarshal(v, &pkg.Release)
	}
	if v, ok := raw["weights_merkle_root"]; ok {
		json.Unmarshal(v, &pkg.WeightsMerkleRoot)
	}
	if v, ok := raw["tokenizer_digest"]; ok {
		json.Unmarshal(v, &pkg.TokenizerDigest)
	}
	if v, ok := raw["config_digest"]; ok {
		json.Unmarshal(v, &pkg.ConfigDigest)
	}
	if v, ok := raw["entitlement_class"]; ok {
		json.Unmarshal(v, &pkg.EntitlementClass)
	}
	if v, ok := raw["minimum_endpoint_assurance"]; ok {
		json.Unmarshal(v, &pkg.MinAssuranceLevel)
	}
	if v, ok := raw["state"]; ok {
		json.Unmarshal(v, &pkg.State)
	}
	if v, ok := raw["context_window"]; ok {
		json.Unmarshal(v, &pkg.ContextWindow)
	}
	// Array fields → store as JSON string
	if v, ok := raw["capabilities"]; ok {
		pkg.CapabilitiesJSON = string(v)
	}
	if v, ok := raw["weights_shards"]; ok {
		pkg.WeightsShardsJSON = string(v)
	}
	if v, ok := raw["adapters"]; ok {
		pkg.AdaptersJSON = string(v)
	}
	if v, ok := raw["serving_engines"]; ok {
		pkg.ServingEnginesJSON = string(v)
	}
	if v, ok := raw["allowed_data_classes"]; ok {
		pkg.AllowedDataClasses = string(v)
	}

	if err := s.registry.RegisterModelPackage(&pkg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pkg)
}

func (s *Server) handleGetModelPackage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var pkg models.ModelPackage
	if err := s.db.Where("id = ? OR package_id = ?", id, id).First(&pkg).Error; err != nil {
		writeError(w, http.StatusNotFound, "모델 패키지를 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, pkg)
}

func (s *Server) handlePublishModel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var pkg models.ModelPackage
	if err := s.db.Where("id = ? OR package_id = ?", id, id).First(&pkg).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.registry.PublishModelPackage(pkg.PackageID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r),
		EventType:      "cp.model.published",
		ActorType:      "admin",
		Action:         "publish_model",
		ResourceType:   "model_package",
		ResourceID:     id,
		Details:        "model package published",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	if s.modelPublishedHook != nil {
		go s.modelPublishedHook(pkg.PackageID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

func (s *Server) handleRecallModel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var pkg models.ModelPackage
	if err := s.db.Where("id = ? OR package_id = ?", id, id).First(&pkg).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.registry.RecallModelPackage(pkg.PackageID, "manual recall"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r),
		EventType:      "cp.model.recalled",
		ActorType:      "admin",
		Action:         "recall_model",
		ResourceType:   "model_package",
		ResourceID:     id,
		Details:        "model package recalled",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "recalled"})
}

func (s *Server) handleListEndpoints(w http.ResponseWriter, r *http.Request) {
	var endpoints []models.InferenceEndpoint
	q := s.db
	if orgID := getOrgID(r); orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Order("created_at DESC").Find(&endpoints)
	writeJSON(w, http.StatusOK, s.decorateEndpoints(endpoints))
}

// decorateEndpoints exposes the operational fields rendered in the customer
// console while preserving the endpoint record as the source of truth.
func (s *Server) decorateEndpoints(endpoints []models.InferenceEndpoint) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(endpoints))
	for _, endpoint := range endpoints {
		row := map[string]interface{}{}
		b, _ := json.Marshal(endpoint)
		_ = json.Unmarshal(b, &row)
		row["address"] = endpoint.ServingURL
		row["engine"] = endpoint.ServingEngine
		lastAttestation := endpoint.LastAttestation
		if lastAttestation == "" {
			var attestation models.EndpointAttestation
			if s.db.Where("endpoint_id = ?", endpoint.EndpointID).Order("timestamp DESC").First(&attestation).Error == nil {
				lastAttestation = attestation.Timestamp
			}
		}
		row["last_attestation_at"] = lastAttestation
		var leaseCount int64
		s.db.Model(&models.EndpointLease{}).Where("endpoint_id = ? AND status = ?", endpoint.EndpointID, "active").Count(&leaseCount)
		row["lease_count"] = leaseCount
		out = append(out, row)
	}
	return out
}

func (s *Server) handleEnrollEndpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID   string `json:"organization_id"`
		PIAPeerID        string `json:"pia_peer_id"`
		ModelPackageID   string `json:"model_package_id"`
		ServingEngine    string `json:"serving_engine"`
		ServingEngineVer string `json:"serving_engine_version"`
		PublicKeyHex     string `json:"public_key_hex"`
		NodeIdentity     string `json:"node_identity"`
		AssuranceLevel   string `json:"assurance_level"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AssuranceLevel == "" {
		req.AssuranceLevel = "L1"
	}
	if req.ServingEngine == "" {
		req.ServingEngine = "vllm"
	}
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = getOrgID(r)
	}
	endpoint, err := s.registry.EnrollEndpoint(orgID, req.PIAPeerID, req.ModelPackageID,
		req.ServingEngine, req.ServingEngineVer, req.PublicKeyHex, req.NodeIdentity, req.AssuranceLevel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.endpoint.enrolled",
		ActorType:      "admin",
		Action:         "enroll_endpoint",
		ResourceType:   "endpoint",
		ResourceID:     endpoint.EndpointID,
		Details:        fmt.Sprintf("PIA: %s, model: %s, assurance: %s", req.PIAPeerID, req.ModelPackageID, req.AssuranceLevel),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, endpoint)
}

func (s *Server) handleGetEndpoint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var ep models.InferenceEndpoint
	if err := s.db.Where("(id = ? OR endpoint_id = ?) AND organization_id = ?", id, id, getOrgID(r)).First(&ep).Error; err != nil {
		writeError(w, http.StatusNotFound, "엔드포인트를 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, s.decorateEndpoints([]models.InferenceEndpoint{ep})[0])
}

func (s *Server) handleIssueEndpointLease(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var ep models.InferenceEndpoint
	if err := s.db.Where("id = ? OR endpoint_id = ?", id, id).First(&ep).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		ValidityHours int `json:"validity_hours"`
	}
	if r.Body != http.NoBody {
		decodeJSON(r, &req)
	}
	if req.ValidityHours == 0 {
		req.ValidityHours = 1
	}
	lease, err := s.registry.IssueEndpointLease(ep.OrganizationID, ep.EndpointID,
		time.Duration(req.ValidityHours)*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, lease)
}

func (s *Server) handleListEpochs(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var epochs []models.PolicyEpoch
	q := s.db.Model(&models.PolicyEpoch{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Order("epoch_number DESC").Find(&epochs)
	writeJSON(w, http.StatusOK, epochs)
}

// handleCreateEpoch creates a multi-domain epoch (policy A2): allowed
// models + tool/DLP/network/SCM/session configs all land in one
// coherent, digested epoch.
func (s *Server) handleCreateEpoch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID string                 `json:"organization_id"`
		AllowedModels  []string               `json:"allowed_models"`
		ToolPolicy     map[string]interface{} `json:"tool_policy"`
		DLPPolicy      map[string]interface{} `json:"dlp_policy"`
		NetworkPolicy  map[string]interface{} `json:"network_policy"`
		SCMPolicy      map[string]interface{} `json:"scm_policy"`
		SessionPolicy  map[string]interface{} `json:"session_policy"`
		TransitionMode string                 `json:"transition_mode"`
		RequiresAck    bool                   `json:"requires_ack"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = getOrgID(r)
	}
	epoch, err := s.policy.CreatePolicyEpochFull(policy.EpochRequest{
		OrganizationID: orgID,
		AllowedModels:  req.AllowedModels,
		ToolPolicy:     req.ToolPolicy,
		DLPPolicy:      req.DLPPolicy,
		NetworkPolicy:  req.NetworkPolicy,
		SCMPolicy:      req.SCMPolicy,
		SessionPolicy:  req.SessionPolicy,
		TransitionMode: req.TransitionMode,
		RequiresAck:    req.RequiresAck,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.policy.epoch_created",
		ActorType:      "admin",
		Action:         "create_policy_epoch",
		ResourceType:   "policy_epoch",
		ResourceID:     epoch.ID,
		Details:        fmt.Sprintf(`{"epoch_id":"%s","transition":"%s","requires_ack":%v}`, epoch.EpochID, epoch.TransitionMode, epoch.RequiresAck),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, epoch)
}

func (s *Server) handleListLeases(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var leases []models.CapabilityLease
	q := s.db.Model(&models.CapabilityLease{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Order("created_at DESC").Find(&leases)
	writeJSON(w, http.StatusOK, leases)
}

func (s *Server) handleIssueLease(w http.ResponseWriter, r *http.Request) {
	if !canAdministerUsers(getRole(r)) {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	var req policy.IssueLeaseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Validity == 0 {
		req.Validity = 1 * time.Hour
	}
	orgID := getOrgID(r)
	if req.OrganizationID != "" && req.OrganizationID != orgID {
		writeError(w, http.StatusForbidden, "cannot issue a lease in another organization")
		return
	}
	req.OrganizationID = orgID
	if req.UserID == "" || req.SessionID == "" || req.SubjectPeerID == "" || req.PolicyEpochID == "" {
		writeError(w, http.StatusBadRequest, "user_id, session_id, subject_peer_id, and policy_epoch_id are required")
		return
	}
	var session models.Session
	if err := s.db.Where("organization_id = ? AND session_id = ? AND user_id = ? AND harness_id = ? AND status = ?",
		orgID, req.SessionID, req.UserID, req.SubjectPeerID, "active").First(&session).Error; err != nil {
		writeError(w, http.StatusForbidden, "active session identity binding not found")
		return
	}
	lease, err := s.policy.IssueCapabilityLease(req)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, lease)
}

// handleVerifyAuditChain verifies the org's audit-event hash chain and
// reports the first break (web/17 B — tamper-evidence for the trail).
func (s *Server) handleVerifyAuditChain(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org")
	if orgID == "" {
		// Default to the caller's org — the literal "default" never
		// matched a real organization and verified zero events.
		orgID = getOrgID(r)
	}
	report, err := audit.VerifyChain(s.db, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "verification failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.AuditEvent{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		page, _ := strconv.Atoi(pageStr)
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		search := r.URL.Query().Get("search")
		eventType := r.URL.Query().Get("type")
		result := r.URL.Query().Get("result")
		category := r.URL.Query().Get("category")
		actor := r.URL.Query().Get("actor")
		integrity := r.URL.Query().Get("integrity")
		if size == 0 {
			size = 50
		}
		if page == 0 {
			page = 1
		}
		if search != "" {
			q = q.Where("action LIKE ? OR event_type LIKE ? OR resource_id LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
		}
		if eventType != "" {
			q = q.Where("event_type = ?", eventType)
		}
		if result != "" {
			q = q.Where("result = ?", result)
		}
		// PAT-1503: canonical category filter (same taxonomy as the Audit UI)
		// and actor/integrity facets, all server-side + URL-encodable.
		if category != "" {
			like := auditCategoryLike(category)
			if len(like) > 0 {
				or := make([]string, len(like))
				args := make([]interface{}, len(like))
				for i, p := range like {
					or[i] = "event_type LIKE ?"
					args[i] = p + ".%"
				}
				q = q.Where("("+strings.Join(or, " OR ")+")", args...)
			} else {
				q = q.Where("event_type = ?", category) // exact unknown prefix still matches
			}
		}
		if actor != "" {
			q = q.Where("actor_id = ? OR actor_type = ?", actor, actor)
		}
		switch integrity {
		case "hold":
			q = q.Where("legal_hold = ?", true)
		case "degraded":
			q = q.Where("event_digest = '' OR event_digest IS NULL")
		case "verified":
			q = q.Where("event_digest != '' AND event_digest IS NOT NULL")
		}
		var total int64
		q.Count(&total)
		var events []models.AuditEvent
		q.Order("occurred_at DESC").Offset((page - 1) * size).Limit(size).Find(&events)
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": events, "total": total, "page": page, "size": size})
		return
	}
	var events []models.AuditEvent
	q.Order("occurred_at DESC").Limit(200).Find(&events)
	writeJSON(w, http.StatusOK, events)
}

// auditCategoryLike returns the event_type prefixes for a canonical audit
// category, matching the taxonomy the Audit UI renders (web/src/evidenceView.ts
// AUDIT_CATEGORIES). Keeping both in lockstep means the faceted filter and the
// displayed labels always agree.
func auditCategoryLike(category string) []string {
	switch category {
	case "session":
		return []string{"cp.session", "session", "exchange"}
	case "user":
		return []string{"cp.user", "user"}
	case "harness":
		return []string{"cp.fleet", "fleet", "cp.harness", "harness"}
	case "model":
		return []string{"cp.model", "model"}
	case "policy":
		return []string{"cp.policy", "policy"}
	case "security":
		return []string{"cp.security", "security", "enterprise.feature"}
	case "compliance":
		return []string{"cp.compliance", "compliance"}
	case "tool":
		return []string{"cp.tool", "tool"}
	case "sandbox":
		return []string{"cp.sandbox", "sandbox"}
	case "hold":
		return []string{"cp.hold", "cp.retention", "hold"}
	default:
		return nil
	}
}

// handleExportAuditEvents streams the org's audit trail as CSV (up to
// 10k events) with the same filters as the paginated list endpoint.
func (s *Server) handleExportAuditEvents(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.AuditEvent{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	if search := r.URL.Query().Get("search"); search != "" {
		q = q.Where("action LIKE ? OR event_type LIKE ? OR resource_id LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	if eventType := r.URL.Query().Get("type"); eventType != "" {
		q = q.Where("event_type = ?", eventType)
	}
	if result := r.URL.Query().Get("result"); result != "" {
		q = q.Where("result = ?", result)
	}
	var events []models.AuditEvent
	q.Order("occurred_at DESC").Limit(10000).Find(&events)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit_export_`+time.Now().Format("20060102_150405")+`.csv"`)
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"occurred_at", "event_type", "actor_type", "actor_id", "action", "resource_type", "resource_id", "result", "details"}); err != nil {
		return
	}
	for _, e := range events {
		if err := cw.Write([]string{e.OccurredAt, e.EventType, e.ActorType, e.ActorID, e.Action, e.ResourceType, e.ResourceID, e.Result, e.Details}); err != nil {
			return
		}
	}
	cw.Flush()
}

// --- Communications Handlers ---

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.Conversation{}).Where("organization_id = ?", orgID)
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		page, _ := strconv.Atoi(pageStr)
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		if size == 0 {
			size = 25
		}
		if page == 0 {
			page = 1
		}
		var total int64
		q.Count(&total)
		var convs []models.Conversation
		q.Order("last_message_at DESC").Offset((page - 1) * size).Limit(size).Find(&convs)
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": convs, "total": total, "page": page, "size": size})
		return
	}
	var convs []models.Conversation
	q.Order("last_message_at DESC").Find(&convs)
	writeJSON(w, http.StatusOK, convs)
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type         string   `json:"type"`
		Title        string   `json:"title"`
		Participants []string `json:"participants"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := getOrgID(r)
	conv, err := s.comms.CreateConversation(orgID, req.Type, req.Title, req.Participants)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.comms.conversation_created",
		ActorType:      "admin",
		Action:         "create_conversation",
		ResourceType:   "conversation",
		ResourceID:     conv.ID,
		Details:        fmt.Sprintf("type: %s", req.Type),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, conv)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	convID := chi.URLParam(r, "id")
	messages, err := s.comms.ListMessages(getOrgID(r), convID, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// C2: privacy-aware separation — viewer-role operators get
	// metadata only (content + context links redacted); elevated roles
	// read full content.
	if getRole(r) == "viewer" {
		for i := range messages {
			messages[i].Content = "[redacted]"
			messages[i].ContentEncrypted = "[redacted]"
			messages[i].LinkedSessionID = ""
			messages[i].LinkedExchangeID = ""
		}
	}
	writeJSON(w, http.StatusOK, messages)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	convID := chi.URLParam(r, "id")
	var req struct {
		SenderID   string `json:"sender_id"`
		SenderType string `json:"sender_type"`
		Content    string `json:"content"`
		ParentID   string `json:"parent_id,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SenderType == "" {
		req.SenderType = "user"
	}
	msg, err := s.comms.SendMessage(getOrgID(r), convID, req.SenderID, req.SenderType, "text", req.Content, req.ParentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.commsBroadcast(getOrgID(r), "comms.message", map[string]interface{}{
		"conversation_id": convID, "message": msg,
	})
	writeJSON(w, http.StatusCreated, msg)
}

func (s *Server) handleGetPresence(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	presences, err := s.comms.GetPresence(orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, presences)
}

func (s *Server) handleUpdatePresence(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID    string `json:"user_id"`
		Status    string `json:"status"`
		Activity  string `json:"activity"`
		HarnessID string `json:"harness_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := getOrgID(r)
	if err := s.comms.UpdatePresence(orgID, req.UserID, req.Status, req.Activity, req.HarnessID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// deliverBroadcastToRelay pushes a broadcast to the org's live harness
// sessions via the relay admin channel (POST {relay_admin_url}/v1/broadcasts,
// which fans out over live DARI sessions). The env is read per call;
// when unset or unreachable it returns 0 and the broadcast stays
// DB-recorded only.
func deliverBroadcastToRelay(orgID, severity, body string) int {
	base := strings.TrimSuffix(config.RelayAdminURL(), "/")
	token := strings.TrimSpace(config.LoadRelayFromEnv().ControlPlaneToken)
	if base == "" || token == "" {
		return 0
	}
	payload, _ := json.Marshal(map[string]string{
		"org_id":   orgID,
		"severity": severity,
		"body":     body,
	})
	req, err := http.NewRequest(http.MethodPost, base+"/v1/broadcasts", bytes.NewReader(payload))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0
	}
	var out struct {
		Delivered int `json:"delivered"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return 0
	}
	return out.Delivered
}

func (s *Server) handleSendBroadcast(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Severity    string `json:"severity"`
		Title       string `json:"title"`
		TitleKo     string `json:"title_ko"`
		Body        string `json:"body"`
		BodyKo      string `json:"body_ko"`
		TargetType  string `json:"target_type"`
		TargetID    string `json:"target_id"`
		RequiresAck bool   `json:"requires_ack"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := getOrgID(r)
	if req.TargetType == "" {
		req.TargetType = "all"
	}
	if req.Severity == "" {
		req.Severity = "info"
	}
	bc, err := s.comms.SendBroadcast(orgID, req.Severity, req.Title, req.TitleKo, req.Body, req.BodyKo, req.TargetType, req.TargetID, req.RequiresAck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Persist first, then attempt live delivery; the audit event records
	// how many live sessions actually received it.
	delivered := deliverBroadcastToRelay(orgID, req.Severity, req.Body)
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r),
		EventType:      "cp.comms.broadcast_sent",
		ActorType:      "admin",
		Action:         "send_broadcast",
		ResourceType:   "broadcast",
		ResourceID:     bc.ID,
		Details: fmt.Sprintf("severity: %s, title: %s, live deliveries: %d",
			req.Severity, req.Title, delivered),
		Result:     "success",
		OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, bc)
}

// --- Work Intelligence Handlers ---

func (s *Server) handleGetUsageSummary(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if !requireUsagePermission(w, r, usageReadAction(r), "organization", orgID) {
		return
	}
	days, since, until := usageWindowFromRequest(r, time.Now())
	report, err := s.buildUsageReport(orgID, usageFilterFromRequest(r, usageFilter{}), fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeUsageReportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleGetEngineeringMetrics(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	userID := r.URL.Query().Get("user_id")
	metrics, err := s.workintel.GetEngineeringMetrics(orgID, userID, 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) handleGetSecurityMetrics(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	metrics, err := s.workintel.GetSecurityMetrics(orgID, 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) handleGetScorecard(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	userID := r.URL.Query().Get("user_id")
	period := r.URL.Query().Get("period")
	if period == "" {
		period = time.Now().Format("2006-01")
	}
	scorecard, err := s.workintel.GenerateScorecard(orgID, userID, period)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, scorecard)
}

func (s *Server) handleExportMetrics(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if !requireUsagePermission(w, r, UsageActionExport, "organization", orgID) {
		return
	}
	days, since, until := usageWindowFromRequest(r, time.Now())
	usage, err := s.buildUsageReport(orgID, usageFilterFromRequest(r, usageFilter{}), fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeUsageReportError(w, err)
		return
	}
	securityMetrics, err := s.workintel.GetSecurityMetrics(orgID, days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"organization_id": orgID,
		"period_days":     days,
		"generated_at":    usage.SnapshotAt,
		"usage":           usage,
		"security":        securityMetrics,
	})
}

// modelCostRow is one model's server-computed cost line. PAT-1501:
// the row is a by-unit breakdown — no cross-unit aggregation.
type modelCostRow struct {
	ModelPackageID     string  `json:"model_package_id"`
	ModelName          string  `json:"model_name,omitempty"`
	TokensIn           int64   `json:"tokens_in,string"`
	TokensOut          int64   `json:"tokens_out,string"`
	TokensUnit         string  `json:"tokens_unit"` // always "tokens"
	CostKRW            float64 `json:"cost_krw"`
	ExpectedCostMicros int64   `json:"expected_cost_micros,string"`
	CostUnit           string  `json:"cost_unit"` // always "krw"
	RecordedCostMicros int64   `json:"recorded_cost_micros,string"`
	RecordedCurrency   string  `json:"recorded_currency,omitempty"`
	DifferenceMicros   int64   `json:"difference_micros,string"`
	Priced             bool    `json:"priced"` // false when the package has no unit price configured
	Reconciled         bool    `json:"reconciled"`
}

// handleGetCostAnalysis computes per-model cost for the org server-side:
// UsageRecord token sums × the ModelPackage's KRW-per-1K price fields.
// Packages without a configured price report priced=false ("단가 미설정")
// — never a fabricated number. PAT-1501: tokens and KRW are reported
// as separate unit-typed rows; the UI must not sum them.
func (s *Server) handleGetCostAnalysis(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if !requireUsagePermission(w, r, UsageActionSummaryRead, "organization", orgID) {
		return
	}
	days, since, until := usageWindowFromRequest(r, time.Now())
	usageReport, err := s.buildUsageReport(orgID, usageFilterFromRequest(r, usageFilter{Projection: usageProjectionModel}), fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeUsageReportError(w, err)
		return
	}

	packageIDs := make([]string, 0, len(usageReport.ModelTotals))
	for packageID := range usageReport.ModelTotals {
		packageIDs = append(packageIDs, packageID)
	}
	sort.Strings(packageIDs)
	var pkgs []models.ModelPackage
	if len(packageIDs) > 0 {
		if err := s.db.Where("package_id IN ? OR id IN ?", packageIDs, packageIDs).Find(&pkgs).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	pkgBy := make(map[string]models.ModelPackage, len(pkgs)*2)
	for _, p := range pkgs {
		pkgBy[p.PackageID] = p
		pkgBy[p.ID] = p
	}

	rowsOut := make([]modelCostRow, 0, len(packageIDs))
	anyPriced := false
	for _, pid := range packageIDs {
		total := usageReport.ModelTotals[pid]
		mc := modelCostRow{ModelPackageID: pid, TokensIn: total.InputTokens, TokensOut: total.OutputTokens, TokensUnit: UnitTokens, CostUnit: UnitKRW}
		if p, ok := pkgBy[pid]; ok {
			mc.ModelName = p.Name
			inputRate, inputConfigured, inputErr := metering.ResolveKRWPriceMicrosPer1K(p.PriceInputMicrosPer1K, p.PriceInputConfigured, p.PriceInputPer1K)
			outputRate, outputConfigured, outputErr := metering.ResolveKRWPriceMicrosPer1K(p.PriceOutputMicrosPer1K, p.PriceOutputConfigured, p.PriceOutputPer1K)
			if inputErr != nil || outputErr != nil {
				writeError(w, http.StatusInternalServerError, "model price is invalid")
				return
			}
			if inputConfigured && outputConfigured {
				inputCost, inputErr := metering.TokenCostMicros(mc.TokensIn, inputRate)
				outputCost, outputErr := metering.TokenCostMicros(mc.TokensOut, outputRate)
				if inputErr != nil || outputErr != nil || inputCost > math.MaxInt64-outputCost {
					writeError(w, http.StatusInternalServerError, "model cost overflows supported range")
					return
				}
				mc.Priced = true
				mc.ExpectedCostMicros = inputCost + outputCost
				mc.CostKRW = float64(mc.ExpectedCostMicros) / 1_000_000
			}
		}
		if len(total.CostByCurrency) == 1 {
			for currency, amount := range total.CostByCurrency {
				mc.RecordedCurrency = currency
				mc.RecordedCostMicros = amount
			}
		}
		mc.DifferenceMicros = mc.RecordedCostMicros - mc.ExpectedCostMicros
		mc.Reconciled = mc.Priced && total.PricingState == MeterStateRecorded && len(total.CostByCurrency) == 1 && strings.EqualFold(mc.RecordedCurrency, "KRW") && mc.DifferenceMicros == 0
		if mc.Priced {
			anyPriced = true
		}
		rowsOut = append(rowsOut, mc)
	}

	// Catalog-derived rows are estimates used only for reconciliation. The
	// authoritative total is always the recorded canonical ledger total.
	totalUsage := Usage{Unit: UnitCurrencyMicro, Currency: usageReport.DisplayCurrency, WindowStart: since, WindowEnd: until, Reconciled: usageReport.Reconciled, State: usageReport.DisplayTotal.State, Reason: usageReport.DisplayTotal.Reason, ReasonCode: usageReport.DisplayTotal.ReasonCode}
	if usageReport.DisplayTotal.State == MeterStateRecorded || usageReport.DisplayTotal.State == MeterStateZero {
		totalUsage.Quantity = usageReport.DisplayTotal.AmountMicros
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"days":             days,
		"window_start":     since,
		"window_end":       until,
		"models":           rowsOut,
		"total":            totalUsage,
		"total_amount":     usageReport.DisplayTotal,
		"any_priced":       anyPriced,
		"estimate_only":    true,
		"display_currency": usageReport.DisplayCurrency,
		"usage_report":     usageReport,
	})
}

// handleGetUsageBreakdown returns the org's metered consumption
// bucketed by unit. Cross-unit aggregation is impossible at the
// response type level — the UI receives one Usage row per unit.
// PAT-1501.
func (s *Server) handleGetUsageBreakdown(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if !requireUsagePermission(w, r, usageReadAction(r), "organization", orgID) {
		return
	}
	days, since, until := usageWindowFromRequest(r, time.Now())
	report, err := s.buildUsageReport(orgID, usageFilterFromRequest(r, usageFilter{}), fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeUsageReportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func usageWindowFromRequest(r *http.Request, now time.Time) (days int, since, until string) {
	now = now.UTC()
	days = 30
	switch r.URL.Query().Get("range") {
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	case "365d":
		days = 365
	default:
		if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && d > 0 && d <= 365 {
			days = d
		}
	}
	since = now.AddDate(0, 0, -days).Format(time.RFC3339Nano)
	until = now.Format(time.RFC3339Nano)
	return days, since, until
}

// --- Fleet Handlers ---

func (s *Server) handleInspectSession(w http.ResponseWriter, r *http.Request) {
	// Compatibility route: the canonical inspector owns bounds, tenant scope,
	// transcript redaction, and response semantics.
	s.handleGetSessionDetail(w, r)
}

func (s *Server) handleFleetAction(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsolePermission(w, r, permissionSessionsManage) {
		return
	}
	var req fleet.ActionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.OrganizationID = getOrgID(r)
	req.PerformedBy = getActorID(r)
	req.Context = r.Context()
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	if !fleet.IsHarnessScopedAction(req.Action) {
		writeError(w, http.StatusBadRequest, "unsupported harness-scoped action; use the security lockdown workflow for organization containment")
		return
	}
	if err := s.fleet.PerformAction(req); err != nil {
		var executionErr *fleet.ActionExecutionError
		if errors.As(err, &executionErr) && (executionErr.LocalApplied || executionErr.RelayDelivered) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"status": "partially_applied", "error": err.Error(),
				"local_state_applied": executionErr.LocalApplied,
				"relay_delivered":     executionErr.RelayDelivered,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Action == fleet.ActionClearDesiredState {
		target, _ := req.Parameters["action"].(string)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "desired_state_released", "action": target,
			"manual_recovery_required": true,
			"remaining_effects":        "existing paused sessions, revoked leases, or isolated sandboxes retain their current state and require their explicit recovery workflow",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "executed"})
}

// --- Git/SCM Handlers ---

func (s *Server) handleRepositoryHeatmap(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	// `repository` scopes the response to that repo's single row so the
	// repository detail page does not fetch the org-wide map and filter
	// client-side (E1).
	heatmap, err := s.gitscm.GetRepositoryHeatmap(orgID, r.URL.Query().Get("repository"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, heatmap)
}

func (s *Server) handleCreateBaselineSCM(w http.ResponseWriter, r *http.Request) {
	var req gitscm.BaselineRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	baseline, err := s.gitscm.CreateBaseline(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, baseline)
}

func (s *Server) handleSetBranchProtection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepositoryID     string `json:"repository_id"`
		Branch           string `json:"branch"`
		Level            string `json:"level"`
		RequiresApproval bool   `json:"requires_approval"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.gitscm.SetBranchProtection(req.RepositoryID, req.Branch, gitscm.BranchProtection(req.Level), req.RequiresApproval); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// --- Impact Handlers ---

func (s *Server) handleAnalyzeChange(w http.ResponseWriter, r *http.Request) {
	var req impact.AnalyzeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	graph, score, err := s.impact.AnalyzeChange(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"impact_graph": graph,
		"risk_score":   score,
	})
}

// --- Context Handlers ---

func (s *Server) handleEvaluateContext(w http.ResponseWriter, r *http.Request) {
	var manifest context.ContextManifest
	if err := decodeJSON(r, &manifest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := getOrgID(r)
	decisions := s.context.EvaluateManifest(orgID, &manifest)
	writeJSON(w, http.StatusOK, decisions)
}

// --- Sandbox Handlers ---

func (s *Server) handleListSandboxes(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	sandboxes, err := s.sandbox.ListSandboxes(orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sandboxes)
}

func (s *Server) handleCreateSandbox(w http.ResponseWriter, r *http.Request) {
	var req sandbox.CreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Org attribution from the authenticated context only.
	req.OrganizationID = getOrgID(r)
	sb, err := s.sandbox.CreateSandbox(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r),
		EventType:      "cp.sandbox.created",
		ActorType:      "admin",
		Action:         "create_sandbox",
		ResourceType:   "sandbox",
		ResourceID:     sb.ID,
		Details:        "sandbox created",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, sb)
}

func (s *Server) handleDestroySandbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)

	// Org scoping is the tenant boundary: a sandbox outside the caller's
	// org is indistinguishable from a missing one.
	var rec models.SandboxRecord
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, id).First(&rec).Error; err != nil {
		writeError(w, http.StatusNotFound, "샌드박스를 찾을 수 없습니다")
		return
	}

	sb, err := s.sandbox.DestroySandbox(id)
	if err != nil {
		var inv *sandbox.InvalidTransitionError
		if errors.As(err, &inv) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Audit only a real transition: the idempotent re-destroy path returns
	// the record without fresh destruction evidence (DestroyEvidence is
	// minted only when this call actually destroyed the sandbox).
	if sb.DestroyEvidence != "" {
		s.db.Create(&models.AuditEvent{
			OrganizationID: getOrgID(r),
			EventType:      "cp.sandbox.destroyed",
			ActorType:      "admin",
			Action:         "destroy_sandbox",
			ResourceType:   "sandbox",
			ResourceID:     id,
			Details:        "sandbox destroyed",
			Result:         "success",
			OccurredAt:     time.Now().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, sb)
}

func (s *Server) handleForensicSnapshot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)

	// Org scoping is the tenant boundary: a sandbox outside the caller's
	// org is indistinguishable from a missing one.
	var rec models.SandboxRecord
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, id).First(&rec).Error; err != nil {
		writeError(w, http.StatusNotFound, "샌드박스를 찾을 수 없습니다")
		return
	}

	snapshotID, err := s.sandbox.ForensicSnapshot(id)
	if err != nil {
		var inv *sandbox.InvalidTransitionError
		if errors.As(err, &inv) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"snapshot_id": snapshotID})
}

// --- Events Handlers ---

func (s *Server) handleQueryEvents(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	filter := events.QueryFilter{
		OrganizationID: orgID,
		EventType:      r.URL.Query().Get("type"),
		SessionID:      r.URL.Query().Get("session_id"),
		UserID:         r.URL.Query().Get("user_id"),
		Limit:          100,
	}
	evts, err := s.events.Query(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, evts)
}

func (s *Server) handleEmitEvent(w http.ResponseWriter, r *http.Request) {
	var req events.EmitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	envelope, err := s.events.Emit(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, envelope)
}

// --- Generic CRUD Handlers ---

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !canAdministerUsers(getRole(r)) {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	var user models.User
	if err := s.db.First(&user, "id = ? AND organization_id = ?", id, getOrgID(r)).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if user.Status == models.UserStatusOffboarded {
		writeError(w, http.StatusConflict, "offboarded users are read-only")
		return
	}
	var updates struct {
		Name           *string `json:"name,omitempty"`
		NameKo         *string `json:"name_ko,omitempty"`
		Email          *string `json:"email,omitempty"`
		Title          *string `json:"title,omitempty"`
		Status         *string `json:"status,omitempty"`
		AuthMethod     *string `json:"auth_method,omitempty"`
		Locale         *string `json:"locale,omitempty"`
		Timezone       *string `json:"timezone,omitempty"`
		BusinessUnitID *string `json:"business_unit_id,omitempty"`
		EmployeeID     *string `json:"employee_id,omitempty"`
		ContractorInfo *string `json:"contractor_info,omitempty"`
		MFAEnrolled    *bool   `json:"mfa_enrolled,omitempty"`
		Reason         *string `json:"reason,omitempty"`
	}
	if err := decodeJSON(r, &updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Lifecycle status moves exclusively through the dedicated endpoints
	// (PAT-1489) — a generic profile edit must never bypass the state
	// machine. A same-value status is a no-op and stays allowed (it is
	// simply not written).
	if updates.Status != nil && *updates.Status != user.Status {
		writeError(w, http.StatusConflict, "lifecycle status changes require the dedicated /suspend, /resume, or /offboard endpoints")
		return
	}
	if updates.AuthMethod != nil {
		writeError(w, http.StatusConflict, "authentication method changes require a dedicated identity relink workflow")
		return
	}
	// Column-scoped update: writing only the requested editable fields (never
	// Status) prevents a stale full-struct Save from reverting a lifecycle
	// transition that committed between the First read and this write.
	cols := map[string]interface{}{}
	if updates.Name != nil {
		cols["name"] = *updates.Name
	}
	if updates.NameKo != nil {
		cols["name_ko"] = *updates.NameKo
	}
	if updates.Email != nil {
		normalized := identity.NormalizeEmail(*updates.Email)
		updates.Email = &normalized
		cols["email"] = normalized
	}
	if updates.Title != nil {
		cols["title"] = *updates.Title
	}
	if updates.Locale != nil {
		cols["locale"] = *updates.Locale
	}
	if updates.Timezone != nil {
		cols["timezone"] = *updates.Timezone
	}
	if updates.BusinessUnitID != nil {
		cols["business_unit_id"] = *updates.BusinessUnitID
	}
	if updates.EmployeeID != nil {
		cols["employee_id"] = *updates.EmployeeID
	}
	if updates.ContractorInfo != nil {
		// Validate the structured contractor record (web/01 A5).
		var profile identity.ContractorProfile
		if err := json.Unmarshal([]byte(*updates.ContractorInfo), &profile); err != nil {
			writeError(w, http.StatusBadRequest, "contractor_info must be a valid ContractorProfile JSON")
			return
		}
		if profile.ContractStart != "" && profile.ContractEnd != "" && profile.ContractEnd < profile.ContractStart {
			writeError(w, http.StatusBadRequest, "contract_end precedes contract_start")
			return
		}
		cols["contractor_info"] = *updates.ContractorInfo
	}
	if updates.MFAEnrolled != nil {
		cols["mfa_enrolled"] = *updates.MFAEnrolled
	}
	reason := ""
	if updates.Reason != nil {
		reason = *updates.Reason
	}
	updateDetails, _ := json.Marshal(map[string]string{"reason": reason})
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := identity.LockMutableUser(tx, getOrgID(r), id); err != nil {
			return err
		}
		if updates.Email != nil && user.Email != *updates.Email {
			if err := tx.Model(&identity.AdminCredentials{}).
				Where("organization_id = ? AND (user_id = ? OR (user_id = '' AND email = ?))", getOrgID(r), id, user.Email).
				Updates(map[string]interface{}{"email": *updates.Email, "user_id": id}).Error; err != nil {
				return err
			}
		}
		if len(cols) > 0 {
			if err := tx.Model(&models.User{}).
				Where("id = ? AND organization_id = ?", id, getOrgID(r)).
				Updates(cols).Error; err != nil {
				return err
			}
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: getOrgID(r),
			EventType:      "cp.user.updated",
			ActorID:        getActorID(r),
			ActorType:      "admin",
			Action:         "update_user",
			ResourceType:   "user",
			ResourceID:     id,
			Details:        string(updateDetails),
			Result:         "success",
			OccurredAt:     time.Now().Format(time.RFC3339),
		}).Error
	})
	if err != nil {
		if errors.Is(err, identity.ErrUserReadOnly) {
			writeError(w, http.StatusConflict, "offboarded users are read-only")
		} else {
			writeError(w, http.StatusInternalServerError, "user update failed")
		}
		return
	}
	if err := s.db.First(&user, "id = ? AND organization_id = ?", id, getOrgID(r)).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "user reload failed")
		return
	}
	writeJSON(w, http.StatusOK, s.decorateSingleUser(r, user))
}

// handleDeleteUser historically reimplemented offboarding without the
// lifecycle guards. It now delegates to the canonical offboard endpoint
// (PAT-1489) so DELETE cannot bypass the state machine: same org scoping,
// reason requirement, RBAC/self-action guards, and transition validation.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	s.handleOffboardUser(w, r)
}

// --- Business Units (Korean enterprise hierarchy, PRD §12.1) ---

func (s *Server) handleListBusinessUnits(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var bus []models.BusinessUnit
	q := s.db.Model(&models.BusinessUnit{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Order("level, name").Find(&bus)
	writeJSON(w, http.StatusOK, bus)
}

func (s *Server) handleCreateBusinessUnit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID string `json:"organization_id"`
		ParentUnitID   string `json:"parent_unit_id"`
		Name           string `json:"name"`
		NameKo         string `json:"name_ko"`
		Type           string `json:"type"`
		Level          int    `json:"level"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Type == "" {
		req.Type = "department"
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = getOrgID(r)
	}
	bu := &models.BusinessUnit{OrganizationID: orgID, ParentUnitID: req.ParentUnitID, Name: req.Name, NameKo: req.NameKo, Type: req.Type, Level: req.Level}
	if err := s.db.Create(bu).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, bu)
}

func (s *Server) handleUpdateBusinessUnit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var bu models.BusinessUnit
	if err := s.db.First(&bu, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var updates struct {
		Name         *string `json:"name,omitempty"`
		NameKo       *string `json:"name_ko,omitempty"`
		Type         *string `json:"type,omitempty"`
		ParentUnitID *string `json:"parent_unit_id,omitempty"`
	}
	decodeJSON(r, &updates)
	if updates.Name != nil {
		bu.Name = *updates.Name
	}
	if updates.NameKo != nil {
		bu.NameKo = *updates.NameKo
	}
	if updates.Type != nil {
		bu.Type = *updates.Type
	}
	if updates.ParentUnitID != nil {
		bu.ParentUnitID = *updates.ParentUnitID
	}
	s.db.Save(&bu)
	writeJSON(w, http.StatusOK, bu)
}

func (s *Server) handleDeleteBusinessUnit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.db.Delete(&models.BusinessUnit{}, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListUserAudit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var events []models.AuditEvent
	s.db.Where("organization_id = ? AND (resource_id = ? OR actor_id = ?)", orgID, id, id).
		Order("occurred_at DESC").Limit(50).Find(&events)
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleListHarnessAudit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var events []models.AuditEvent
	s.db.Where("organization_id = ? AND resource_id = ?", orgID, id).
		Order("occurred_at DESC").Limit(50).Find(&events)
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleIssueEnrollmentCode(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if _, ok := s.requireMutableUser(w, r, userID); !ok {
		return
	}
	var code string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var issueErr error
		code, issueErr = s.identity.GenerateEnrollmentCodeWithDB(tx, orgID, userID, 24*time.Hour)
		if issueErr != nil {
			return issueErr
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.user.enrollment_code", ActorID: getActorID(r), ActorType: "admin",
			Action: "issue_enrollment_code", ResourceType: "user", ResourceID: userID,
			Details: "enrollment code issued (24h validity)", Result: "success", OccurredAt: time.Now().Format(time.RFC3339),
		}).Error
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"code": code, "enrollment_code": code, "expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339)})
}

// --- Policy Rules (governance rules authored in the Policy console, PRD §13) ---

func (s *Server) handleListPolicyRules(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var rules []models.PolicyRule
	q := s.db.Model(&models.PolicyRule{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Order("domain, created_at DESC").Find(&rules)
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) handleCreatePolicyRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID             string          `json:"id"`
		OrganizationID string          `json:"organization_id"`
		Domain         string          `json:"domain"`
		TemplateID     string          `json:"template_id"`
		Name           string          `json:"name"`
		NameEn         string          `json:"nameEn"`
		Description    string          `json:"desc"`
		Scope          string          `json:"scope"`
		ScopeName      string          `json:"scopeName"`
		Enabled        bool            `json:"enabled"`
		Config         json.RawMessage `json:"config"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = getOrgID(r)
	}
	if req.Domain == "" {
		writeError(w, http.StatusBadRequest, "domain is required")
		return
	}
	// Upsert: if an existing rule ID is provided, update it. Toggling
	// an APPROVED rule rebuilds the org epoch so enforcement matches.
	if req.ID != "" {
		var existing models.PolicyRule
		if s.db.First(&existing, "id = ? AND organization_id = ?", req.ID, orgID).Error == nil {
			wasEnforced := existing.Status == "approved" && existing.Enabled
			existing.Enabled = req.Enabled
			if req.Scope != "" {
				existing.Scope = req.Scope
			}
			if req.ScopeName != "" {
				existing.ScopeName = req.ScopeName
			}
			if req.Name != "" {
				existing.Name = req.Name
			}
			if req.NameEn != "" {
				existing.NameEn = req.NameEn
			}
			if len(req.Config) > 0 {
				existing.ConfigJSON = string(req.Config)
			}
			s.db.Save(&existing)
			if wasEnforced || (existing.Status == "approved" && existing.Enabled) {
				s.policy.RebuildEpochFromRules(orgID, "immediate", false)
			}
			writeJSON(w, http.StatusOK, existing)
			return
		}
	}
	rule := &models.PolicyRule{
		OrganizationID: orgID, Domain: req.Domain, TemplateID: req.TemplateID,
		Name: req.Name, NameEn: req.NameEn, Description: req.Description,
		Scope: req.Scope, ScopeName: req.ScopeName, Enabled: req.Enabled,
		ConfigJSON: string(req.Config),
		// New rules start as drafts (policy C1, §46.2): approval is
		// the publish step, not the create click.
		Status: "draft",
	}
	if rule.Scope == "" {
		rule.Scope = "org"
	}
	if err := s.db.Create(rule).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Conflict detection (policy C4): overlapping approved rules in
	// the same domain + scope are surfaced for the author.
	conflicts, _ := s.policy.RuleConflicts(orgID, rule.Domain, rule.Scope, rule.ScopeName, rule.ID)
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.policy.rule_created",
		ActorType:      "admin",
		Action:         "create_policy_rule",
		ResourceType:   "policy_rule",
		ResourceID:     rule.ID,
		Details:        fmt.Sprintf(`{"domain":"%s","name":"%s","enabled":%v,"status":"draft"}`, rule.Domain, rule.Name, rule.Enabled),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{"rule": rule, "conflicts": conflicts})
}

func (s *Server) handleDeletePolicyRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var rule models.PolicyRule
	wasEnforced := s.db.First(&rule, "id = ? AND organization_id = ?", id, orgID).Error == nil &&
		rule.Status == "approved" && rule.Enabled
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).Delete(&models.PolicyRule{}).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if wasEnforced {
		s.policy.RebuildEpochFromRules(orgID, "immediate", false)
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.policy.rule_deleted",
		ActorType:      "admin",
		Action:         "delete_policy_rule",
		ResourceType:   "policy_rule",
		ResourceID:     id,
		Details:        "policy rule deleted",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	var proj models.Project
	if err := s.db.First(&proj, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var updates struct {
		Name           *string         `json:"name,omitempty"`
		NameKo         *string         `json:"name_ko,omitempty"`
		Description    *string         `json:"description,omitempty"`
		Status         *string         `json:"status,omitempty"`
		AllowedModels  json.RawMessage `json:"allowed_models,omitempty"`
		ProjectCode    *string         `json:"project_code,omitempty"`
		GroupAffiliate *string         `json:"group_affiliate,omitempty"`
		PolicyPackID   *string         `json:"policy_pack_id,omitempty"`
	}
	if err := decodeJSON(r, &updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if updates.Status != nil {
		writeError(w, http.StatusBadRequest, "project status must be changed through the archive or restore action")
		return
	}
	if updates.Name != nil {
		proj.Name = *updates.Name
	}
	if updates.NameKo != nil {
		proj.NameKo = *updates.NameKo
	}
	if updates.Description != nil {
		proj.Description = *updates.Description
	}
	// Explicit empty array clears the allowance (제한 없음); absent key
	// leaves it unchanged (PAT-1491).
	if len(updates.AllowedModels) > 0 {
		allowed, policyState := parseModelClasses(string(updates.AllowedModels))
		if policyState == modelPolicyInvalid {
			writeError(w, http.StatusBadRequest, "allowed_models must be an array of non-empty model identifiers")
			return
		}
		resolver, err := s.newAllowedModelResolver(orgID, allowed)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if resolver.resolvesRestricted(allowed) {
			writeError(w, http.StatusForbidden, "allowed model is not available to this organization")
			return
		}
		b, _ := json.Marshal(allowed)
		proj.AllowedModelClasses = string(b)
	}
	if updates.ProjectCode != nil {
		proj.ProjectCode = *updates.ProjectCode
	}
	if updates.GroupAffiliate != nil {
		proj.GroupAffiliate = *updates.GroupAffiliate
	}
	if updates.PolicyPackID != nil {
		if *updates.PolicyPackID != "" {
			var pack models.PolicyPack
			if err := s.db.First(&pack, "id = ? AND organization_id = ?", *updates.PolicyPackID, orgID).Error; err != nil {
				writeError(w, http.StatusNotFound, "policy pack not found")
				return
			}
		}
		proj.PolicyPackID = *updates.PolicyPackID
	}
	if err := s.db.Save(&proj).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r),
		EventType:      "cp.project.updated",
		ActorType:      "admin",
		Action:         "update_project",
		ResourceType:   "project",
		ResourceID:     proj.ID,
		Details:        "project fields updated",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	row, err := s.projectViewRow(proj)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if !canAdministerUsers(getRole(r)) {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var impact map[string]interface{}
	var sessionOutcomes []sessionlifecycle.Outcome
	actorID := getActorID(r)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.Project{}).Where("id = ? AND organization_id = ?", id, orgID).Update("status", "archived")
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		var err error
		impact, err = projectArchiveImpactDB(tx, orgID, id)
		if err != nil {
			return err
		}
		sessionOutcomes, err = s.sessionLifecycle.TransitionScopeInTransaction(
			tx,
			sessionlifecycle.Scope{OrganizationID: orgID, ProjectID: id, ActorType: "admin"},
			"paused", "project_archived", "project archived", actorID,
		)
		if err != nil {
			return err
		}
		for _, outcome := range sessionOutcomes {
			if outcome.Result != sessionlifecycle.ResultUpdated {
				return fmt.Errorf("project archive: session %s transition %s", outcome.RequestedID, outcome.Result)
			}
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: orgID,
			EventType:      "cp.project.archived",
			ActorID:        actorID,
			ActorType:      "admin",
			Action:         "archive_project",
			ResourceType:   "project",
			ResourceID:     id,
			Details:        "project archived",
			Result:         "success",
			OccurredAt:     time.Now().Format(time.RFC3339),
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.sessionLifecycle.FinalizeTransitions(orgID, sessionOutcomes, "paused", "project_archived", "project archived", actorID, "admin"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "archived", "impact": impact})
}

// projectArchiveImpact counts what an archive affects (projects UX14):
// active sessions that will be frozen, attached repositories, members.
func projectArchiveImpactDB(db *gorm.DB, orgID, projectID string) (map[string]interface{}, error) {
	var projectCount, activeSessions, inProgressSessions, repos, members int64
	if err := db.Model(&models.Project{}).Where("id = ? AND organization_id = ?", projectID, orgID).Count(&projectCount).Error; err != nil {
		return nil, err
	}
	if projectCount != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	queries := []struct {
		value *int64
		query *gorm.DB
	}{
		{&activeSessions, db.Model(&models.Session{}).Where("organization_id = ? AND project_id = ? AND status = ?", orgID, projectID, "active")},
		{&inProgressSessions, db.Model(&models.Session{}).Where("organization_id = ? AND project_id = ? AND status IN ?", orgID, projectID, models.SessionNonTerminalStatuses())},
		{&repos, db.Model(&models.Repository{}).Where("organization_id = ? AND project_id = ? AND status = ?", orgID, projectID, "active")},
		{&members, db.Model(&models.ProjectMember{}).Where("organization_id = ? AND project_id = ?", orgID, projectID)},
	}
	for _, item := range queries {
		if err := item.query.Count(item.value).Error; err != nil {
			return nil, err
		}
	}
	return map[string]interface{}{
		"active_sessions":      activeSessions,
		"in_progress_sessions": inProgressSessions,
		"repositories":         repos,
		"members":              members,
	}, nil
}

// handleProjectArchiveImpact previews the archive blast radius before
// the operator confirms (projects UX14).
func (s *Server) handleProjectArchiveImpact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	impact, err := projectArchiveImpactDB(s.db, getOrgID(r), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, impact)
}

// handleRestoreProject un-archives a project (projects B4).
func (s *Server) handleRestoreProject(w http.ResponseWriter, r *http.Request) {
	if !canAdministerUsers(getRole(r)) {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.Project{}).Where("id = ? AND organization_id = ?", id, orgID).Update("status", "active")
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: orgID,
			EventType:      "cp.project.restored",
			ActorID:        getActorID(r),
			ActorType:      "admin",
			Action:         "restore_project",
			ResourceType:   "project",
			ResourceID:     id,
			Details:        "project restored",
			Result:         "success",
			OccurredAt:     time.Now().Format(time.RFC3339),
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// handleProjectUsage rolls up the project cost center (projects B6,
// §29.12): sessions, token usage, and recorded cost across all of the
// project's sessions.
func (s *Server) handleProjectUsage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if !requireUsagePermission(w, r, usageReadAction(r), "project", id) {
		return
	}
	var proj models.Project
	if err := s.db.WithContext(r.Context()).Where("id = ? AND organization_id = ?", id, orgID).First(&proj).Error; err != nil {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	var sessionCount int64
	if r.URL.Query().Get("cursor") == "" {
		if err := s.db.WithContext(r.Context()).Model(&models.Session{}).Where("organization_id = ? AND project_id = ?", orgID, id).Count(&sessionCount).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	days, since, until := usageWindowFromRequest(r, time.Now())
	report, err := s.buildUsageReport(orgID, usageFilterFromRequest(r, usageFilter{ProjectID: id, Projection: usageProjectionLedger}), fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeUsageReportError(w, err)
		return
	}
	report.SessionCount = int(sessionCount)
	writeJSON(w, http.StatusOK, report)
}

// handleListProjectMembers returns the real roster with user info
// (projects B1) — no more session-derived guessing.
func (s *Server) handleListProjectMembers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var project models.Project
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&project).Error; err != nil {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	var members []models.ProjectMember
	s.db.Where("organization_id = ? AND project_id = ?", orgID, id).Order("created_at DESC").Find(&members)
	type memberRow struct {
		models.ProjectMember
		User *models.User `json:"user,omitempty"`
	}
	userIDs := make([]string, 0, len(members))
	for _, member := range members {
		userIDs = append(userIDs, member.UserID)
	}
	var users []models.User
	if len(userIDs) > 0 {
		s.db.Where("organization_id = ? AND id IN ?", orgID, userIDs).Find(&users)
	}
	usersByID := make(map[string]*models.User, len(users))
	for i := range users {
		usersByID[users[i].ID] = &users[i]
	}
	out := make([]memberRow, 0, len(members))
	for _, m := range members {
		row := memberRow{ProjectMember: m, User: usersByID[m.UserID]}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAddProjectMember assigns a role to a user on the project
// (projects B1).
func (s *Server) handleAddProjectMember(w http.ResponseWriter, r *http.Request) {
	if !canAdministerUsers(getRole(r)) {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := decodeJSON(r, &req); err != nil || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}
	switch req.Role {
	case "owner", "admin", "maintainer", "member", "viewer":
	default:
		writeError(w, http.StatusBadRequest, "invalid project member role")
		return
	}
	if _, ok := s.requireMutableUser(w, r, req.UserID); !ok {
		return
	}
	var proj models.Project
	if s.db.First(&proj, "id = ? AND organization_id = ?", id, orgID).Error != nil {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	member := models.ProjectMember{OrganizationID: orgID, ProjectID: id, UserID: req.UserID, Role: req.Role, GrantedBy: getActorID(r)}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := identity.LockMutableUser(tx, orgID, req.UserID); err != nil {
			return err
		}
		if err := tx.Where("organization_id = ? AND project_id = ? AND user_id = ?", orgID, id, req.UserID).
			Assign(models.ProjectMember{Role: req.Role, GrantedBy: getActorID(r)}).FirstOrCreate(&member).Error; err != nil {
			return err
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.project.member_added", ActorID: getActorID(r), ActorType: "admin",
			Action: "add_project_member", ResourceType: "project", ResourceID: id,
			Details: fmt.Sprintf(`{"user_id":"%s","role":"%s"}`, req.UserID, req.Role),
			Result:  "success", OccurredAt: time.Now().Format(time.RFC3339),
		}).Error
	})
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "사용자를 찾을 수 없습니다")
		} else if errors.Is(err, identity.ErrUserReadOnly) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, member)
}

// handleRemoveProjectMember removes a roster entry (projects B1).
func (s *Server) handleRemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	if !canAdministerUsers(getRole(r)) {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	id := chi.URLParam(r, "id")
	userID := chi.URLParam(r, "userId")
	orgID := getOrgID(r)
	if _, ok := s.requireMutableUser(w, r, userID); !ok {
		return
	}
	var project models.Project
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&project).Error; err != nil {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := identity.LockMutableUser(tx, orgID, userID); err != nil {
			return err
		}
		if err := tx.Where("organization_id = ? AND project_id = ? AND user_id = ?", orgID, id, userID).Delete(&models.ProjectMember{}).Error; err != nil {
			return err
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.project.member_removed", ActorID: getActorID(r), ActorType: "admin",
			Action: "remove_project_member", ResourceType: "project", ResourceID: id,
			Details: fmt.Sprintf(`{"user_id":"%s"}`, userID), Result: "success", OccurredAt: time.Now().Format(time.RFC3339),
		}).Error
	})
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "사용자를 찾을 수 없습니다")
		} else if errors.Is(err, identity.ErrUserReadOnly) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// handleBindProjectPolicyPack binds a versioned policy pack to the
// project (projects B2) — surfaced as the project's effective policy.
func (s *Server) handleBindProjectPolicyPack(w http.ResponseWriter, r *http.Request) {
	if !canAdministerUsers(getRole(r)) {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		PolicyPackID string `json:"policy_pack_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if req.PolicyPackID != "" {
			var count int64
			if err := tx.Model(&models.PolicyPack{}).Where("id = ? AND organization_id = ?", req.PolicyPackID, orgID).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		updated := tx.Model(&models.Project{}).Where("id = ? AND organization_id = ?", id, orgID).Update("policy_pack_id", req.PolicyPackID)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		details, err := json.Marshal(map[string]string{"policy_pack_id": req.PolicyPackID})
		if err != nil {
			return err
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.project.policy_pack_bound",
			ActorID: getActorID(r), ActorType: "admin", Action: "bind_project_policy_pack",
			ResourceType: "project", ResourceID: id, Details: string(details), Result: "success",
			OccurredAt: time.Now().Format(time.RFC3339),
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "project or policy pack not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "bound", "policy_pack_id": req.PolicyPackID})
}

// handleGetProjectDetail assembles the project detail page (projects
// B3): repos, real membership roster, sessions, policy binding, usage,
// change-control queue, and audit.
func (s *Server) handleGetProjectDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	var proj models.Project
	if err := s.db.First(&proj, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	projectRow, err := s.projectViewRow(proj)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	result := map[string]interface{}{"project": projectRow}

	var repos []models.Repository
	s.db.Where("organization_id = ? AND project_id = ?", orgID, id).Find(&repos)
	result["repositories"] = repos

	var members []models.ProjectMember
	s.db.Where("organization_id = ? AND project_id = ?", orgID, id).Find(&members)
	type memberRow struct {
		models.ProjectMember
		User *models.User `json:"user,omitempty"`
	}
	rows := make([]memberRow, 0, len(members))
	userIDs := make([]string, 0, len(members))
	for _, member := range members {
		userIDs = append(userIDs, member.UserID)
	}
	usersByID := make(map[string]models.User, len(userIDs))
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
	for _, m := range members {
		row := memberRow{ProjectMember: m}
		if user, ok := usersByID[m.UserID]; ok {
			row.User = &user
		}
		rows = append(rows, row)
	}
	result["members"] = rows

	var sessions []models.Session
	s.db.Where("organization_id = ? AND project_id = ?", orgID, id).Order("created_at DESC").Limit(50).Find(&sessions)
	result["sessions"] = sessions

	if proj.PolicyPackID != "" {
		var pack models.PolicyPack
		if s.db.First(&pack, "id = ? AND organization_id = ?", proj.PolicyPackID, orgID).Error == nil {
			result["policy_pack"] = pack
		}
	}

	var changes []models.ChangeRequest
	s.db.Where("organization_id = ? AND project_id = ?", orgID, id).Order("created_at DESC").Find(&changes)
	result["change_requests"] = changes

	var auditEvents []models.AuditEvent
	s.db.Where("organization_id = ? AND resource_id = ?", orgID, id).Order("occurred_at DESC").Limit(50).Find(&auditEvents)
	result["audit_events"] = auditEvents

	writeJSON(w, http.StatusOK, result)
}

// handleListProjectChangeRequests lists the project's AI change-control
// queue (projects B7).
func (s *Server) handleListProjectChangeRequests(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var changes []models.ChangeRequest
	s.db.Where("project_id = ?", id).Order("created_at DESC").Find(&changes)
	writeJSON(w, http.StatusOK, changes)
}

// handleDecideChangeRequest approves or denies a queued high-risk
// change (projects B7).
func (s *Server) handleDecideChangeRequest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var cr models.ChangeRequest
	if s.db.First(&cr, "id = ?", id).Error != nil {
		writeError(w, http.StatusNotFound, "change request not found")
		return
	}
	if cr.Status != "pending" {
		writeError(w, http.StatusConflict, "change request already decided")
		return
	}
	status := "approved"
	if !req.Approve {
		status = "denied"
	}
	cr.Status = status
	cr.DecidedBy = orgID
	cr.DecisionReason = req.Reason
	cr.DecidedAt = time.Now().Format(time.RFC3339)
	s.db.Save(&cr)
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.project.change_request_decided",
		ActorType:      "admin",
		Action:         "decide_change_request",
		ResourceType:   "change_request",
		ResourceID:     id,
		Details:        fmt.Sprintf(`{"status":"%s","reason":"%s"}`, status, req.Reason),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, cr)
}

func (s *Server) handleUpdateRepository(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var repo models.Repository
	if err := s.db.First(&repo, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var updates struct {
		Name          *string `json:"name,omitempty"`
		ProjectID     *string `json:"project_id,omitempty"`
		CloneURL      *string `json:"clone_url,omitempty"`
		SCMProvider   *string `json:"scm_provider,omitempty"`
		DefaultBranch *string `json:"default_branch,omitempty"`
		Sensitivity   *string `json:"sensitivity,omitempty"`
		Status        *string `json:"status,omitempty"`
	}
	decodeJSON(r, &updates)
	if updates.Name != nil {
		repo.Name = *updates.Name
	}
	if updates.ProjectID != nil {
		repo.ProjectID = *updates.ProjectID
	}
	if updates.CloneURL != nil {
		repo.CloneURL = *updates.CloneURL
	}
	if updates.SCMProvider != nil {
		repo.SCMProvider = *updates.SCMProvider
	}
	if updates.DefaultBranch != nil {
		repo.DefaultBranch = *updates.DefaultBranch
	}
	if updates.Sensitivity != nil {
		repo.Sensitivity = *updates.Sensitivity
	}
	if updates.Status != nil {
		repo.Status = *updates.Status
	}
	s.db.Save(&repo)
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) handleDeleteRepository(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.db.Model(&models.Repository{}).Where("id = ?", id).Update("status", "unregistered")
	writeJSON(w, http.StatusOK, map[string]string{"status": "unregistered"})
}

// applySessionTransition performs one admin session lifecycle move with
// org-scoped lookup, canonical transition validation (409), and an SSE
// broadcast so Live surfaces hear it without polling (PAT-1496).
func (s *Server) applySessionTransition(w http.ResponseWriter, r *http.Request, to, auditAction string) {
	if !s.requireConsolePermission(w, r, permissionSessionsManage) {
		return
	}
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	actorID := getActorID(r)
	outcome := s.sessionLifecycle.Transition(sessionlifecycle.Request{OrganizationID: orgID, SessionRef: id, Target: to, Action: auditAction, Reason: "operator requested lifecycle transition", ActorID: actorID})
	if outcome.Result == sessionlifecycle.ResultNotFound {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if outcome.Result == sessionlifecycle.ResultConflict {
		writeError(w, http.StatusConflict, "session changed concurrently")
		return
	}
	if outcome.Result == sessionlifecycle.ResultInvalidTransition {
		writeError(w, http.StatusConflict, fmt.Sprintf("invalid session transition: %s → %s", outcome.From, to))
		return
	}
	if outcome.Result != sessionlifecycle.ResultUpdated {
		writeError(w, http.StatusInternalServerError, outcome.Error)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": to, "cleanup_failures": outcome.CleanupFailures})
}

func (s *Server) handlePauseSession(w http.ResponseWriter, r *http.Request) {
	s.applySessionTransition(w, r, "paused", "pause_session")
}

func (s *Server) handleResumeSession(w http.ResponseWriter, r *http.Request) {
	s.applySessionTransition(w, r, "active", "resume_session")
}

func (s *Server) handleUpdateModel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var pkg models.ModelPackage
	if err := s.db.Where("id = ? OR package_id = ?", id, id).First(&pkg).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var updates struct {
		Name             *string      `json:"name,omitempty"`
		NameKo           *string      `json:"name_ko,omitempty"`
		Description      *string      `json:"description,omitempty"`
		PriceInputPer1K  *json.Number `json:"price_input_per_1k,omitempty"`
		PriceOutputPer1K *json.Number `json:"price_output_per_1k,omitempty"`
		PriceVersion     *string      `json:"price_version,omitempty"`
		PriceSource      *string      `json:"price_source,omitempty"`
	}
	if err := decodeJSON(r, &updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if updates.PriceVersion != nil && strings.TrimSpace(*updates.PriceVersion) == "" {
		writeError(w, http.StatusBadRequest, "price_version must not be empty")
		return
	}
	if updates.PriceSource != nil && strings.TrimSpace(*updates.PriceSource) == "" {
		writeError(w, http.StatusBadRequest, "price_source must not be empty")
		return
	}
	inputMicros, outputMicros := pkg.PriceInputMicrosPer1K, pkg.PriceOutputMicrosPer1K
	inputLegacy, outputLegacy := pkg.PriceInputPer1K, pkg.PriceOutputPer1K
	if updates.PriceInputPer1K != nil {
		var err error
		inputMicros, err = metering.ParseKRWPriceMicrosPer1K(updates.PriceInputPer1K.String())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		inputLegacy, _ = strconv.ParseFloat(updates.PriceInputPer1K.String(), 64)
	}
	if updates.PriceOutputPer1K != nil {
		var err error
		outputMicros, err = metering.ParseKRWPriceMicrosPer1K(updates.PriceOutputPer1K.String())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		outputLegacy, _ = strconv.ParseFloat(updates.PriceOutputPer1K.String(), 64)
	}
	priceChanged := (updates.PriceInputPer1K != nil && (!pkg.PriceInputConfigured || inputMicros != pkg.PriceInputMicrosPer1K)) ||
		(updates.PriceOutputPer1K != nil && (!pkg.PriceOutputConfigured || outputMicros != pkg.PriceOutputMicrosPer1K))
	if priceChanged && updates.PriceVersion != nil && strings.TrimSpace(*updates.PriceVersion) == strings.TrimSpace(pkg.PriceVersion) {
		writeError(w, http.StatusBadRequest, "a changed price requires a new price_version")
		return
	}
	if updates.Name != nil {
		pkg.Name = *updates.Name
	}
	if updates.NameKo != nil {
		pkg.NameKo = *updates.NameKo
	}
	if updates.PriceInputPer1K != nil {
		pkg.PriceInputPer1K = inputLegacy
		pkg.PriceInputMicrosPer1K = inputMicros
		pkg.PriceInputConfigured = true
	}
	if updates.PriceOutputPer1K != nil {
		pkg.PriceOutputPer1K = outputLegacy
		pkg.PriceOutputMicrosPer1K = outputMicros
		pkg.PriceOutputConfigured = true
	}
	if updates.PriceVersion != nil {
		pkg.PriceVersion = strings.TrimSpace(*updates.PriceVersion)
	} else if priceChanged {
		pkg.PriceVersion = "catalog-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	if updates.PriceSource != nil {
		pkg.PriceSource = strings.TrimSpace(*updates.PriceSource)
	} else if priceChanged && pkg.PriceSource == "" {
		pkg.PriceSource = "pccp.model_catalog"
	}
	if err := s.db.Save(&pkg).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pkg)
}

func (s *Server) handleQuarantineHarness(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsolePermission(w, r, permissionSessionsManage) {
		return
	}
	id := chi.URLParam(r, "id")
	var harness models.Harness
	orgID := getOrgID(r)
	if err := s.db.Where("organization_id = ? AND (id = ? OR harness_id = ?)", orgID, id, id).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.fleet.PerformAction(fleet.ActionRequest{
		OrganizationID: orgID,
		HarnessID:      harness.HarnessID,
		Action:         fleet.ActionQuarantine,
		Reason:         "quarantined via control plane",
		PerformedBy:    getActorID(r),
	}); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "quarantined", "relay_propagated": true})
}

func (s *Server) handleReactivateHarness(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsolePermission(w, r, permissionSessionsManage) {
		return
	}
	id := chi.URLParam(r, "id")
	var harness models.Harness
	orgID := getOrgID(r)
	if err := s.db.Where("organization_id = ? AND (id = ? OR harness_id = ?)", orgID, id, id).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if harness.Status != "quarantined" {
		writeError(w, http.StatusConflict, "only a quarantined harness may be reactivated; revoked harnesses must re-enroll with a new credential")
		return
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.Harness{}).
			Where("id = ? AND organization_id = ? AND status = ?", harness.ID, orgID, "quarantined").
			Updates(map[string]interface{}{"status": "enrolled", "risk_state": "normal"})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrInvalidData
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: harness.OrganizationID,
			EventType:      "cp.harness.reactivated",
			ActorID:        getActorID(r),
			ActorType:      "admin",
			Action:         "reactivate_harness",
			ResourceType:   "harness",
			ResourceID:     harness.ID,
			Details:        "harness reactivated",
			Result:         "success",
			OccurredAt:     time.Now().Format(time.RFC3339),
		}).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrInvalidData) {
			writeError(w, http.StatusConflict, "harness standing changed; refresh before retrying")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "enrolled"})
}

func (s *Server) handleUpdateEndpoint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var ep models.InferenceEndpoint
	if err := s.db.Where("id = ? OR endpoint_id = ?", id, id).First(&ep).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var updates struct {
		AssuranceLevel *string `json:"assurance_level,omitempty"`
		Status         *string `json:"status,omitempty"`
	}
	decodeJSON(r, &updates)
	if updates.AssuranceLevel != nil {
		ep.AssuranceLevel = *updates.AssuranceLevel
	}
	if updates.Status != nil {
		ep.Status = *updates.Status
	}
	s.db.Save(&ep)
	writeJSON(w, http.StatusOK, ep)
}

func (s *Server) handleDrainEndpoint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.db.Model(&models.InferenceEndpoint{}).Where("endpoint_id = ?", id).
		Update("status", "draining")
	writeJSON(w, http.StatusOK, map[string]string{"status": "draining"})
}

func (s *Server) handleGetSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	rules, _ := s.security.EnsureRulesSeeded(orgID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"rules": rules})
}

func (s *Server) handleUpdateSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	var updates struct {
		RuleID   string `json:"rule_id"`
		Enabled  *bool  `json:"enabled"`
		Action   string `json:"action"`
		Severity string `json:"severity"`
		Pattern  string `json:"pattern"`
	}
	if err := decodeJSON(r, &updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if updates.RuleID == "" {
		writeError(w, http.StatusBadRequest, "rule_id is required")
		return
	}
	orgID := getOrgID(r)
	if err := s.security.SetRule(orgID, updates.RuleID, updates.Enabled, updates.Action, updates.Severity, updates.Pattern); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.security.rule_updated",
		ActorType:      "admin",
		Action:         "update_security_rule",
		ResourceType:   "security_rule",
		ResourceID:     updates.RuleID,
		Details:        fmt.Sprintf(`{"rule_id":"%s","enabled":%v,"action":"%s"}`, updates.RuleID, updates.Enabled, updates.Action),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleSecurityRules(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	// Idempotent seed-then-list: a fresh org sees the authoritative
	// catalog instead of an empty list (same pattern as
	// handleGetSecurityPolicy).
	if _, err := s.security.EnsureRulesSeeded(orgID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rules, err := s.security.ListRules(orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

// handleListRuleOverrides returns the scoped DELTA rows for one
// scope target (PAT-1432 admin surface, PAT-1433 UI).
func (s *Server) handleListRuleOverrides(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	level := r.URL.Query().Get("scope_level")
	scopeID := r.URL.Query().Get("scope_id")
	if level == "" || scopeID == "" {
		writeError(w, http.StatusBadRequest, "scope_level and scope_id are required")
		return
	}
	if level == "org" {
		writeError(w, http.StatusBadRequest, "org rules live in the catalog (GET /api/security/rules), not the override table")
		return
	}
	overrides, err := s.security.ListRuleOverrides(orgID, level, scopeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, overrides)
}

// handlePutRuleOverride stores one scoped delta (team/user/harness).
func (s *Server) handlePutRuleOverride(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScopeLevel string `json:"scope_level"`
		ScopeID    string `json:"scope_id"`
		RuleID     string `json:"rule_id"`
		Enabled    *bool  `json:"enabled"`
		Severity   string `json:"severity"`
		Action     string `json:"action"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := getOrgID(r)
	if err := s.security.SetRuleOverride(orgID, req.ScopeLevel, req.ScopeID, req.RuleID, req.Enabled, req.Severity, req.Action); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.security.rule_override_set",
		ActorType:      "admin",
		Action:         "set_security_rule_override",
		ResourceType:   "security_rule_override",
		ResourceID:     req.RuleID,
		Details:        fmt.Sprintf(`{"rule_id":"%s","scope_level":"%s","scope_id":"%s"}`, req.RuleID, req.ScopeLevel, req.ScopeID),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleDeleteRuleOverride reverts a rule to the next-wider scope.
func (s *Server) handleDeleteRuleOverride(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScopeLevel string `json:"scope_level"`
		ScopeID    string `json:"scope_id"`
		RuleID     string `json:"rule_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := getOrgID(r)
	if err := s.security.DeleteRuleOverride(orgID, req.ScopeLevel, req.ScopeID, req.RuleID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.security.rule_override_deleted",
		ActorType:      "admin",
		Action:         "delete_security_rule_override",
		ResourceType:   "security_rule_override",
		ResourceID:     req.RuleID,
		Details:        fmt.Sprintf(`{"rule_id":"%s","scope_level":"%s","scope_id":"%s"}`, req.RuleID, req.ScopeLevel, req.ScopeID),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// splitCSV splits a comma-separated filter value, trimming whitespace and
// dropping empties so "critical,high" / "critical, high" collapse to
// [critical high]. Used by the canonical security scope contract.
func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// securityFindingScope applies the canonical security-findings filter/count
// contract (PAT-1484). Dashboard KPI cards, dashboard drill-down links, and
// the findings list all derive from this same builder so a card's count always
// equals the destination list's matching count for the identical scope — the
// UI never duplicates filtering logic.
//
// severity accepts a comma-separated list (e.g. "critical,high" → IN).
// status accepts a single value or the reserved token "unresolved", which
// means status != 'resolved' (a finding remains on the radar once it is no
// longer open for action), matching the dashboard remediation/risk KPI.
func (s *Server) securityFindingScope(q *gorm.DB, severity, status string) *gorm.DB {
	if sev := splitCSV(severity); len(sev) > 0 {
		q = q.Where("severity IN ?", sev)
	}
	if status == "unresolved" {
		q = q.Where("status != ?", "resolved")
	} else if strings.TrimSpace(status) != "" {
		q = q.Where("status = ?", status)
	}
	return q
}

func (s *Server) handleSecurityFindings(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.SecurityFinding{}).Where("organization_id = ?", orgID)
	// Server-side filters (security UX5/UX12 + PAT-1490): severity
	// (comma-list), status (or "unresolved" token), type, repository,
	// date range — via the shared scope contract so the list reconciles
	// with dashboard KPI counts and relationship drill-downs.
	q = s.securityFindingScope(q, r.URL.Query().Get("severity"), r.URL.Query().Get("status"))
	for _, key := range []string{"finding_type"} {
		if v := r.URL.Query().Get(key); v != "" {
			q = q.Where(key+" = ?", v)
		}
	}
	// PAT-1490: scope findings to a repository by joining through its
	// sessions, so a repo's "보안 발견" count drills to the identical list.
	if repo := r.URL.Query().Get("repository"); repo != "" {
		q = q.Where("session_id IN (?)", models.RepositorySessionIDs(s.db, orgID, repo))
	}
	if v := r.URL.Query().Get("from"); v != "" {
		q = q.Where("occurred_at >= ?", v)
	}
	if v := r.URL.Query().Get("to"); v != "" {
		q = q.Where("occurred_at <= ?", v)
	}
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		page, _ := strconv.Atoi(pageStr)
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		if size == 0 {
			size = 25
		}
		if page == 0 {
			page = 1
		}
		var total int64
		q.Count(&total)
		var findings []models.SecurityFinding
		q.Order("occurred_at DESC").Offset((page - 1) * size).Limit(size).Find(&findings)
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": findings, "total": total, "page": page, "size": size})
		return
	}
	var findings []models.SecurityFinding
	q.Order("occurred_at DESC").Limit(100).Find(&findings)
	writeJSON(w, http.StatusOK, findings)
}

func (s *Server) handleSecurityFindingDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var finding models.SecurityFinding
	if err := s.db.Where("id = ?", id).First(&finding).Error; err != nil {
		writeError(w, http.StatusNotFound, "finding not found")
		return
	}
	result := map[string]interface{}{"finding": finding}
	if finding.SessionID != "" {
		var session models.Session
		if s.db.Where("session_id = ?", finding.SessionID).First(&session).Error == nil {
			result["session"] = session
			if session.UserID != "" {
				var user models.User
				if s.db.Where("id = ?", session.UserID).First(&user).Error == nil {
					result["user"] = user
				}
			}
			if session.HarnessID != "" {
				var harness models.Harness
				if s.db.Where("harness_id = ?", session.HarnessID).First(&harness).Error == nil {
					result["harness"] = harness
				}
			}
		}
	}
	var auditEvents []models.AuditEvent
	s.db.Where("resource_id = ?", id).Order("occurred_at DESC").Limit(20).Find(&auditEvents)
	result["audit_events"] = auditEvents
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleUpdateFinding(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.db.Model(&models.SecurityFinding{}).Where("id = ?", id).Update("status", req.Status)
	orgID := getOrgID(r)
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.security.finding_updated",
		ActorType:      "admin",
		Action:         "update_finding_status",
		ResourceType:   "security_finding",
		ResourceID:     id,
		Details:        "status=" + req.Status,
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	var finding models.SecurityFinding
	s.db.Where("id = ?", id).First(&finding)
	writeJSON(w, http.StatusOK, finding)
}

func (s *Server) handleSecurityLockdown(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsolePermission(w, r, permissionSessionsManage) {
		return
	}
	orgID := getOrgID(r)
	var req struct {
		Scope     string `json:"scope"` // org | project
		ProjectID string `json:"project_id"`
		Reason    string `json:"reason"`
	}
	if r.Body != http.NoBody {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if req.Scope == "" {
		req.Scope = "org"
	}
	if req.Scope != "org" && req.Scope != "project" {
		writeError(w, http.StatusBadRequest, "scope must be org or project")
		return
	}
	if req.Scope == "project" && req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "project scope requires project_id")
		return
	}
	if req.Scope == "project" {
		var count int64
		if err := s.db.Model(&models.Project{}).Where("id = ? AND organization_id = ?", req.ProjectID, orgID).Count(&count).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if count != 1 {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}

	// Resolve the affected harness set only after the same org/project locks
	// used by session creation have been acquired.
	var affectedHarnesses []models.Harness
	actorID := getActorID(r)
	scope := sessionlifecycle.Scope{OrganizationID: orgID, ProjectID: req.ProjectID, ForceTerminal: true, ActorType: "admin"}
	var outcomes []sessionlifecycle.Outcome
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var organization models.Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", orgID).First(&organization).Error; err != nil {
			return err
		}
		if req.Scope == "project" {
			var project models.Project
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND organization_id = ?", req.ProjectID, orgID).First(&project).Error; err != nil {
				return err
			}
		} else {
			scope.ProjectID = ""
		}
		harnessQuery := tx.Where("organization_id = ?", orgID)
		if req.Scope == "project" {
			harnessQuery = harnessQuery.Where("harness_id IN (?)", tx.Model(&models.Session{}).Select("harness_id").
				Where("organization_id = ? AND status IN ? AND project_id = ?", orgID, models.SessionNonTerminalStatuses(), req.ProjectID))
		}
		if err := harnessQuery.Find(&affectedHarnesses).Error; err != nil {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		lockdown := models.SecurityLockdown{OrganizationID: orgID, Scope: req.Scope, ProjectID: scope.ProjectID}
		if err := tx.Where("organization_id = ? AND scope = ? AND project_id = ?", orgID, req.Scope, scope.ProjectID).
			Assign(map[string]interface{}{"status": "active", "reason": req.Reason, "activated_by": actorID, "activated_at": now, "released_by": "", "released_at": nil}).
			FirstOrCreate(&lockdown).Error; err != nil {
			return err
		}
		var transitionErr error
		outcomes, transitionErr = s.sessionLifecycle.TransitionScopeInTransaction(tx, scope, "terminated", "security_lockdown", req.Reason, actorID)
		if transitionErr != nil {
			return transitionErr
		}
		for _, outcome := range outcomes {
			if outcome.Result != sessionlifecycle.ResultUpdated {
				return fmt.Errorf("security lockdown: session %s transition %s", outcome.RequestedID, outcome.Result)
			}
		}
		details, _ := json.Marshal(map[string]string{"scope": req.Scope, "project_id": scope.ProjectID, "reason": req.Reason})
		return tx.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.security.lockdown_activated", ActorID: actorID, ActorType: "admin",
			Action: "activate_lockdown", ResourceType: "security_lockdown", ResourceID: scope.ProjectID,
			Details: string(details), Result: "success", OccurredAt: now,
		}).Error
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	finalized, finalizeErr := s.sessionLifecycle.FinalizeTransitions(orgID, outcomes, "terminated", "security_lockdown", req.Reason, actorID, "admin")
	if finalizeErr != nil {
		outcomes = finalized
	}
	affectedSessions := 0
	transitionFailed := false
	for _, outcome := range outcomes {
		if outcome.Result == sessionlifecycle.ResultUpdated && len(outcome.CleanupFailures) == 0 {
			affectedSessions++
		} else {
			transitionFailed = true
		}
	}

	// Live propagation (security B1): DB termination is enforced by the
	// relay's per-request session-status gate; the directive additionally
	// notifies the relay channel when configured.
	relayCtx, cancelRelay := stdctx.WithTimeout(r.Context(), 30*time.Second)
	defer cancelRelay()
	jobs := make(chan models.Harness)
	var relayDelivered atomic.Int64
	var relayFailed atomic.Int64
	workerCount := len(affectedHarnesses)
	if workerCount > 16 {
		workerCount = 16
	}
	var relayWorkers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		relayWorkers.Add(1)
		go func() {
			defer relayWorkers.Done()
			for h := range jobs {
				if err := s.pushRelayDirectiveContext(relayCtx, "emergency_lockdown", orgID, h.HarnessID, req.Reason, map[string]interface{}{"scope": req.Scope}); err != nil {
					relayFailed.Add(1)
				} else {
					relayDelivered.Add(1)
				}
			}
		}()
	}
	sent := 0
sendRelayJobs:
	for _, h := range affectedHarnesses {
		select {
		case jobs <- h:
			sent++
		case <-relayCtx.Done():
			relayFailed.Add(int64(len(affectedHarnesses) - sent))
			break sendRelayJobs
		}
	}
	close(jobs)
	relayWorkers.Wait()
	relayPropagated := relayFailed.Load() == 0

	auditDetails, _ := json.Marshal(map[string]interface{}{"scope": req.Scope, "project_id": req.ProjectID, "reason": req.Reason, "relay_delivered": relayDelivered.Load(), "relay_failed": relayFailed.Load()})
	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.security.emergency_lockdown",
		ActorID:        actorID,
		ActorType:      "admin",
		Action:         "emergency_lockdown",
		Details:        string(auditDetails),
		Result:         map[bool]string{true: "failure", false: "success"}[transitionFailed || !relayPropagated],
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	auditPersisted := s.db.Create(audit).Error == nil
	statusCode := http.StatusOK
	status := "lockdown_activated"
	if transitionFailed || !relayPropagated || !auditPersisted {
		statusCode = http.StatusConflict
		status = "lockdown_partially_applied"
	}
	writeJSON(w, statusCode, map[string]interface{}{
		"status": status, "scope": req.Scope,
		"affected_harnesses": len(affectedHarnesses), "affected_sessions": affectedSessions, "session_outcomes": outcomes,
		"relay_propagated": relayPropagated, "relay_delivered": relayDelivered.Load(), "relay_failed": relayFailed.Load(), "lockdown_state_persisted": true, "audit_persisted": auditPersisted,
	})
}

func (s *Server) handleSecurityLockdownRelease(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsolePermission(w, r, permissionSessionsManage) {
		return
	}
	orgID := getOrgID(r)
	actorID := getActorID(r)
	var req struct {
		Scope     string `json:"scope"`
		ProjectID string `json:"project_id"`
		Reason    string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Scope != "org" && req.Scope != "project" {
		writeError(w, http.StatusBadRequest, "scope must be org or project")
		return
	}
	if req.Scope == "project" && strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "project scope requires project_id")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var organization models.Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", orgID).First(&organization).Error; err != nil {
			return err
		}
		if req.Scope == "project" {
			var project models.Project
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND organization_id = ?", req.ProjectID, orgID).First(&project).Error; err != nil {
				return err
			}
		} else {
			req.ProjectID = ""
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		updated := tx.Model(&models.SecurityLockdown{}).
			Where("organization_id = ? AND scope = ? AND project_id = ? AND status = ?", orgID, req.Scope, req.ProjectID, "active").
			Updates(map[string]interface{}{"status": "released", "released_by": actorID, "released_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		details, _ := json.Marshal(map[string]string{"scope": req.Scope, "project_id": req.ProjectID, "reason": req.Reason})
		return tx.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.security.lockdown_released", ActorID: actorID, ActorType: "admin",
			Action: "release_lockdown", ResourceType: "security_lockdown", ResourceID: req.ProjectID,
			Details: string(details), Result: "success", OccurredAt: now,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusConflict, "no active lockdown exists for the requested scope")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var harnesses []models.Harness
	harnessQuery := s.db.Where("organization_id = ?", orgID)
	if req.Scope == "project" {
		harnessQuery = harnessQuery.Where("harness_id IN (?)", s.db.Model(&models.Session{}).Select("harness_id").Where("organization_id = ? AND project_id = ?", orgID, req.ProjectID))
	}
	if err := harnessQuery.Find(&harnesses).Error; err != nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"status": "lockdown_released", "relay_propagated": false, "error": "release persisted but affected harnesses could not be resolved"})
		return
	}
	relayCtx, cancelRelay := stdctx.WithTimeout(r.Context(), 30*time.Second)
	defer cancelRelay()
	jobs := make(chan models.Harness)
	var delivered atomic.Int64
	var failed atomic.Int64
	workerCount := len(harnesses)
	if workerCount > 16 {
		workerCount = 16
	}
	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for harness := range jobs {
				if err := s.pushRelayDirectiveContext(relayCtx, "release_lockdown", orgID, harness.HarnessID, req.Reason, map[string]interface{}{"scope": req.Scope, "project_id": req.ProjectID}); err != nil {
					failed.Add(1)
				} else {
					delivered.Add(1)
				}
			}
		}()
	}
	for _, harness := range harnesses {
		select {
		case jobs <- harness:
		case <-relayCtx.Done():
			failed.Add(1)
		}
	}
	close(jobs)
	workers.Wait()
	deliveryDetails, _ := json.Marshal(map[string]interface{}{"scope": req.Scope, "project_id": req.ProjectID, "relay_delivered": delivered.Load(), "relay_failed": failed.Load()})
	deliveryResult := "success"
	if failed.Load() > 0 {
		deliveryResult = "partial"
	}
	auditErr := models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: orgID, EventType: "cp.security.lockdown_release_delivery", ActorID: actorID, ActorType: "admin",
		Action: "release_lockdown_delivery", ResourceType: "security_lockdown", ResourceID: req.ProjectID,
		Details: string(deliveryDetails), Result: deliveryResult, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	statusCode := http.StatusOK
	status := "lockdown_released"
	if failed.Load() > 0 || auditErr != nil {
		statusCode = http.StatusConflict
		status = "lockdown_released_with_propagation_failures"
	}
	writeJSON(w, statusCode, map[string]interface{}{
		"status": status, "scope": req.Scope, "project_id": req.ProjectID,
		"relay_propagated": failed.Load() == 0, "relay_delivered": delivered.Load(), "relay_failed": failed.Load(), "audit_persisted": auditErr == nil,
	})
}

// handleSecurityLockdownImpact previews the lockdown blast radius
// (security UX9).
func (s *Server) handleSecurityLockdownImpact(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsolePermission(w, r, permissionSessionsManage) {
		return
	}
	orgID := getOrgID(r)
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "org"
	}
	projectID := r.URL.Query().Get("project_id")
	if scope != "org" && scope != "project" {
		writeError(w, http.StatusBadRequest, "scope must be org or project")
		return
	}
	if scope == "project" {
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "project scope requires project_id")
			return
		}
		var count int64
		if err := s.db.Model(&models.Project{}).Where("id = ? AND organization_id = ?", projectID, orgID).Count(&count).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if count != 1 {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
	}
	statuses := models.SessionNonTerminalStatuses()
	q := s.db.Model(&models.Session{}).Where("organization_id = ? AND status IN ?", orgID, statuses)
	if scope == "project" {
		q = q.Where("project_id = ?", projectID)
	}
	type statusCount struct {
		Status string
		Count  int64
	}
	var counts []statusCount
	if err := q.Select("status, COUNT(*) AS count").Group("status").Scan(&counts).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	breakdown := make(map[string]int64, len(statuses))
	var inProgressSessions int64
	for _, item := range counts {
		breakdown[item.Status] = item.Count
		inProgressSessions += item.Count
	}
	hq := s.db.Model(&models.Harness{}).Where("organization_id = ?", orgID)
	if scope == "project" {
		hq = hq.Where("harness_id IN (?)", s.db.Model(&models.Session{}).Select("harness_id").
			Where("organization_id = ? AND project_id = ? AND status IN ?", orgID, projectID, statuses))
	}
	var affectedHarnesses int64
	if err := hq.Count(&affectedHarnesses).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"scope": scope, "project_id": projectID, "active_sessions": breakdown["active"],
		"in_progress_sessions": inProgressSessions, "affected_harnesses": affectedHarnesses, "status_breakdown": breakdown,
	})
}

// handleBulkSecurityFindings resolves/suppresses many findings at once
// (security UX7).
func (s *Server) handleBulkSecurityFindings(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		IDs    []string `json:"ids"`
		Status string   `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids are required")
		return
	}
	result := s.db.Model(&models.SecurityFinding{}).
		Where("organization_id = ? AND id IN ?", orgID, req.IDs).
		Update("status", req.Status)
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, result.Error.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.security.findings_bulk_updated",
		ActorType:      "admin",
		Action:         "bulk_update_findings",
		ResourceType:   "security_finding",
		Details:        fmt.Sprintf(`{"count":%d,"status":"%s"}`, result.RowsAffected, req.Status),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "updated", "count": result.RowsAffected})
}

// handleSuppressFinding implements the suppress/accept-risk workflow
// (security C1).
func (s *Server) handleSuppressFinding(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		Reason string `json:"reason"`
		Days   int    `json:"days"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	if err := s.security.SuppressFinding(orgID, id, req.Reason, getActorID(r), req.Days); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.security.finding_suppressed",
		ActorType:      "admin",
		Action:         "suppress_finding",
		ResourceType:   "security_finding",
		ResourceID:     id,
		Details:        fmt.Sprintf(`{"reason":"%s","days":%d}`, req.Reason, req.Days),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "suppressed"})
}

// handleReopenFinding clears a suppression (security C1).
func (s *Server) handleReopenFinding(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if err := s.security.ReopenFinding(orgID, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reopened"})
}

// handleScanSession replays detection over a session's exchanges
// (security UX8).
func (s *Server) handleScanSession(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	result, err := s.security.ScanSession(orgID, req.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// --- Alert endpoints (security C2/C3) ---
//
// PAT-1502 PR 1: response redaction boundary. The GORM model holds
// `Target` as a write-only secret (json:"-"). All read paths go through
// redactAlertEndpoint; the raw URL never reaches the client.
//
// PAT-1502 PR 2 will replace the plaintext column with a keymgmt
// secret reference and add test/rotate endpoints. PR 1 keeps the
// surface minimal so PR 2's durability work is reviewable on its own.

func (s *Server) handleListAlertEndpoints(w http.ResponseWriter, r *http.Request) {
	if !s.requireAlertPermission(w, r, AlertActionRead, "") {
		return
	}
	orgID := getOrgID(r)
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	type alertEndpointListRow struct {
		models.AlertEndpoint
		SecretConfigured bool `gorm:"column:secret_configured"`
	}
	var rows []alertEndpointListRow
	query := s.db.Model(&models.AlertEndpoint{}).
		Select("id, organization_id, name, type, credential_id, rotation_required, last_rotated_at, last_test_at, last_test_status, severities_json, enabled, created_at, updated_at, CASE WHEN target_enc <> '' OR target <> '' THEN true ELSE false END AS secret_configured").
		Where("organization_id = ?", orgID).Order("id ASC").Limit(limit + 1)
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		query = query.Where("id > ?", cursor)
	}
	if err := query.Scan(&rows).Error; err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionRead, "", "failure", map[string]interface{}{"reason_code": "storage_error"}) {
			return
		}
		writeError(w, http.StatusInternalServerError, "could not list alert endpoints")
		return
	}
	if len(rows) > limit {
		w.Header().Set("X-Next-Cursor", rows[limit-1].ID)
		rows = rows[:limit]
	}
	responses := make([]AlertEndpointResponse, 0, len(rows))
	for _, row := range rows {
		response := redactAlertEndpoint(row.AlertEndpoint)
		response.SecretConfigured = row.SecretConfigured
		responses = append(responses, response)
	}
	if err := s.auditAlertAction(r, AlertActionRead, "", "success", map[string]interface{}{"count": len(rows)}); err != nil {
		writeError(w, http.StatusInternalServerError, "could not record alert endpoint access")
		return
	}
	writeJSON(w, http.StatusOK, responses)
}

func (s *Server) handleCreateAlertEndpoint(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if !s.requireAlertPermission(w, r, AlertActionCreate, "") {
		return
	}
	if s.keyProvider == nil {
		// PAT-1502 PR 2: write paths fail closed when no KeyProvider
		// is configured. An unconfigured server cannot accept secret
		// material.
		if !s.recordAlertAuditOrFail(w, r, AlertActionCreate, "", "failure", map[string]interface{}{"reason_code": "provider_not_configured"}) {
			return
		}
		writeError(w, http.StatusServiceUnavailable, "alert endpoint storage is not configured")
		return
	}
	var req AlertEndpointCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionCreate, "", "failure", map[string]interface{}{"reason_code": "invalid_request"}) {
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	target := strings.TrimSpace(req.Target)
	if req.Name == "" || target == "" {
		if !s.recordAlertAuditOrFail(w, r, AlertActionCreate, "", "failure", map[string]interface{}{"reason_code": "missing_required_field"}) {
			return
		}
		writeError(w, http.StatusBadRequest, "name and target are required")
		return
	}
	if len(req.Name) > 255 || len(target) > 1024 {
		if !s.recordAlertAuditOrFail(w, r, AlertActionCreate, "", "failure", map[string]interface{}{"reason_code": "field_too_long"}) {
			return
		}
		writeError(w, http.StatusBadRequest, "name or target exceeds the allowed length")
		return
	}
	if req.Type == "" {
		req.Type = "webhook"
	}
	if req.Type != "slack" && req.Type != "webhook" && req.Type != "siem" {
		if !s.recordAlertAuditOrFail(w, r, AlertActionCreate, "", "failure", map[string]interface{}{"reason_code": "invalid_provider_type"}) {
			return
		}
		writeError(w, http.StatusBadRequest, "unsupported alert endpoint type")
		return
	}
	if !isAcceptableAlertTarget(req.Type, target) {
		if !s.recordAlertAuditOrFail(w, r, AlertActionCreate, "", "failure", map[string]interface{}{"reason_code": "invalid_target"}) {
			return
		}
		writeError(w, http.StatusBadRequest, "target does not match the selected provider format")
		return
	}
	severities, valid := normalizeAlertSeverities(req.Severities)
	if !valid {
		if !s.recordAlertAuditOrFail(w, r, AlertActionCreate, "", "failure", map[string]interface{}{"reason_code": "invalid_severity"}) {
			return
		}
		writeError(w, http.StatusBadRequest, "unsupported alert severity")
		return
	}
	severitiesJSON, _ := json.Marshal(severities)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	epID := models.GenerateID("alert")
	enc, kekID, credentialID, bindingVersion, err := keymgmt.SealAlertSecret(s.keyProvider, target, keymgmt.AlertSecretContext{
		OrganizationID: orgID, EndpointID: epID, ProviderType: req.Type,
	})
	if err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionCreate, "", "failure", map[string]interface{}{"reason_code": "encryption_failed"}) {
			return
		}
		writeError(w, http.StatusInternalServerError, "could not seal target")
		return
	}
	ep := &models.AlertEndpoint{
		Base:           models.Base{ID: epID},
		OrganizationID: orgID, Name: req.Name, Type: req.Type,
		Target: "", TargetEnc: enc, TargetKEKID: kekID,
		TargetBindingVersion: bindingVersion,
		CredentialID:         credentialID,
		SeveritiesJSON:       string(severitiesJSON), Enabled: enabled,
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(ep).Error; err != nil {
			return err
		}
		return s.auditAlertActionDB(tx, r, AlertActionCreate, ep.ID, "success", map[string]interface{}{
			"credential_id": ep.CredentialID, "type": ep.Type, "enabled": ep.Enabled,
		})
	}); err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionCreate, "", "failure", map[string]interface{}{"reason_code": "storage_error"}) {
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create alert endpoint")
		return
	}
	writeJSON(w, http.StatusCreated, redactAlertEndpoint(*ep))
}

func (s *Server) handleDeleteAlertEndpoint(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	if !s.requireAlertPermission(w, r, AlertActionDelete, id) {
		return
	}
	var ep models.AlertEndpoint
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&ep).Error; err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionDelete, id, "failure", map[string]interface{}{"reason_code": "not_found"}) {
			return
		}
		writeError(w, http.StatusNotFound, "alert endpoint not found")
		return
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND organization_id = ?", id, orgID).Delete(&models.AlertEndpoint{}).Error; err != nil {
			return err
		}
		return s.auditAlertActionDB(tx, r, AlertActionDelete, id, "success", map[string]interface{}{
			"credential_id": credentialIDForSecret(ep), "type": ep.Type,
		})
	}); err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionDelete, id, "failure", map[string]interface{}{"reason_code": "storage_error", "credential_id": credentialIDForSecret(ep)}) {
			return
		}
		writeError(w, http.StatusInternalServerError, "could not delete alert endpoint")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Alert endpoint rotation replaces PCCP's stored credential. It cannot revoke
// the previous credential at Slack or another upstream provider, so the
// response and audit record explicitly preserve that operator obligation.
func (s *Server) handleRotateAlertEndpoint(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	if !s.requireAlertPermission(w, r, AlertActionRotate, id) {
		return
	}
	if s.keyProvider == nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionRotate, id, "failure", map[string]interface{}{"reason_code": "provider_not_configured"}) {
			return
		}
		writeError(w, http.StatusServiceUnavailable, "alert endpoint storage is not configured")
		return
	}
	var req struct {
		Target string `json:"target"`
		Enable *bool  `json:"enable,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionRotate, id, "failure", map[string]interface{}{"reason_code": "invalid_request"}) {
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		if !s.recordAlertAuditOrFail(w, r, AlertActionRotate, id, "failure", map[string]interface{}{"reason_code": "missing_target"}) {
			return
		}
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}
	if len(target) > 1024 {
		if !s.recordAlertAuditOrFail(w, r, AlertActionRotate, id, "failure", map[string]interface{}{"reason_code": "field_too_long"}) {
			return
		}
		writeError(w, http.StatusBadRequest, "target exceeds the allowed length")
		return
	}
	var ep models.AlertEndpoint
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&ep).Error; err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionRotate, id, "failure", map[string]interface{}{"reason_code": "not_found"}) {
			return
		}
		writeError(w, http.StatusNotFound, "alert endpoint not found")
		return
	}
	if !isAcceptableAlertTarget(ep.Type, target) {
		if !s.recordAlertAuditOrFail(w, r, AlertActionRotate, id, "failure", map[string]interface{}{"reason_code": "invalid_target", "credential_id": credentialIDForSecret(ep)}) {
			return
		}
		writeError(w, http.StatusBadRequest, "target does not match the selected provider format")
		return
	}
	enc, kekID, newCredID, bindingVersion, err := keymgmt.SealAlertSecret(s.keyProvider, target, keymgmt.AlertSecretContext{
		OrganizationID: orgID, EndpointID: id, ProviderType: ep.Type,
	})
	if err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionRotate, id, "failure", map[string]interface{}{"reason_code": "encryption_failed", "credential_id": credentialIDForSecret(ep)}) {
			return
		}
		writeError(w, http.StatusInternalServerError, "could not seal new target")
		return
	}
	oldCredID := credentialIDForSecret(ep)
	hadPriorCredential := ep.Target != "" || ep.TargetEnc != ""
	now := time.Now().UTC()
	enabled := ep.Enabled
	revocationRequired := hadPriorCredential && (oldCredID == "" || oldCredID != newCredID)
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		var updateErr error
		enabled, updateErr = applyAlertRotation(tx, ep, enc, kekID, newCredID, bindingVersion, now, req.Enable)
		if updateErr != nil {
			return updateErr
		}
		return s.auditAlertActionDB(tx, r, AlertActionRotate, id, "success", map[string]interface{}{
			"old_credential_id": oldCredID, "new_credential_id": newCredID, "type": ep.Type,
			"provider_revocation_required": revocationRequired, "enabled": enabled,
		})
	}); err != nil {
		reasonCode := "storage_error"
		if errors.Is(err, errAlertEndpointChanged) {
			reasonCode = "endpoint_changed"
		}
		if !s.recordAlertAuditOrFail(w, r, AlertActionRotate, id, "failure", map[string]interface{}{"reason_code": reasonCode, "credential_id": oldCredID}) {
			return
		}
		if errors.Is(err, errAlertEndpointChanged) {
			writeError(w, http.StatusConflict, "alert endpoint is being tested or changed; retry rotation")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not rotate alert endpoint")
		return
	}
	updated := ep
	updated.Target = ""
	updated.TargetEnc = enc
	updated.TargetKEKID = kekID
	updated.TargetBindingVersion = bindingVersion
	updated.CredentialID = newCredID
	updated.RotationRequired = false
	updated.LastRotatedAt = &now
	updated.Enabled = enabled
	response := redactAlertEndpoint(updated)
	response.ProviderRevocationRequired = revocationRequired
	writeJSON(w, http.StatusOK, response)
}

// handleDisableAlertEndpoint stops delivery without deleting the encrypted
// configuration or its audit correlation identifier.
func (s *Server) handleDisableAlertEndpoint(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	if !s.requireAlertPermission(w, r, AlertActionDisable, id) {
		return
	}
	var ep models.AlertEndpoint
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&ep).Error; err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionDisable, id, "failure", map[string]interface{}{"reason_code": "not_found"}) {
			return
		}
		writeError(w, http.StatusNotFound, "alert endpoint not found")
		return
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.AlertEndpoint{}).
			Where("id = ? AND organization_id = ?", id, orgID).
			Update("enabled", false).Error; err != nil {
			return err
		}
		return s.auditAlertActionDB(tx, r, AlertActionDisable, id, "success", map[string]interface{}{
			"credential_id": credentialIDForSecret(ep), "type": ep.Type, "enabled": false,
		})
	}); err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionDisable, id, "failure", map[string]interface{}{"reason_code": "storage_error", "credential_id": credentialIDForSecret(ep)}) {
			return
		}
		writeError(w, http.StatusInternalServerError, "could not disable alert endpoint")
		return
	}
	ep.Enabled = false
	writeJSON(w, http.StatusOK, redactAlertEndpoint(ep))
}

// Alert endpoint test — sends a synthetic Slack-style "ping" to the
// resolved target. The endpoint's URL is decrypted on the server
// side and used to dispatch one HTTP POST. The provider response
// body is never returned; only the status class (2xx/non-2xx) and
// a short reason. PAT-1502 PR 2.
func (s *Server) handleTestAlertEndpoint(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	if !s.requireAlertPermission(w, r, AlertActionTest, id) {
		return
	}
	if s.keyProvider == nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionTest, id, "failure", map[string]interface{}{"reason_code": "provider_not_configured"}) {
			return
		}
		writeError(w, http.StatusServiceUnavailable, "alert endpoint storage is not configured")
		return
	}
	var ep models.AlertEndpoint
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&ep).Error; err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionTest, id, "failure", map[string]interface{}{"reason_code": "not_found"}) {
			return
		}
		writeError(w, http.StatusNotFound, "alert endpoint not found")
		return
	}
	if ep.RotationRequired {
		if !s.recordAlertAuditOrFail(w, r, AlertActionTest, id, "failure", map[string]interface{}{"reason_code": "rotation_required", "credential_id": credentialIDForSecret(ep)}) {
			return
		}
		writeError(w, http.StatusConflict, "credential rotation is required before testing")
		return
	}
	now := time.Now()
	if s.alertNow != nil {
		now = s.alertNow()
	}
	if err := s.reserveAlertTest(r, ep, now); err != nil {
		switch {
		case errors.Is(err, errAlertTestRateLimited):
			if auditErr := s.auditAlertAction(r, AlertActionTest, id, "denied", map[string]interface{}{"reason_code": "rate_limited", "credential_id": credentialIDForSecret(ep)}); auditErr != nil {
				writeError(w, http.StatusInternalServerError, "could not record test denial")
				return
			}
			writeError(w, http.StatusTooManyRequests, "rate limited; try again later")
		case errors.Is(err, errAlertEndpointChanged):
			if auditErr := s.auditAlertAction(r, AlertActionTest, id, "denied", map[string]interface{}{"reason_code": "endpoint_changed", "credential_id": credentialIDForSecret(ep)}); auditErr != nil {
				writeError(w, http.StatusInternalServerError, "could not record endpoint conflict")
				return
			}
			writeError(w, http.StatusConflict, "alert endpoint changed; retry the test")
		default:
			if auditErr := s.auditAlertAction(r, AlertActionTest, id, "failure", map[string]interface{}{"reason_code": "storage_error", "credential_id": credentialIDForSecret(ep)}); auditErr != nil {
				writeError(w, http.StatusInternalServerError, "could not record test reservation failure")
				return
			}
			writeError(w, http.StatusInternalServerError, "could not reserve test delivery")
		}
		return
	}
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&ep).Error; err != nil {
		if !s.recordAlertAuditOrFail(w, r, AlertActionTest, id, "failure", map[string]interface{}{"reason_code": "endpoint_changed", "credential_id": credentialIDForSecret(ep)}) {
			return
		}
		writeError(w, http.StatusConflict, "alert endpoint changed; retry the test")
		return
	}
	target, err := keymgmt.OpenAlertSecret(s.keyProvider, ep.TargetEnc, ep.TargetKEKID, ep.Target, ep.TargetBindingVersion, ep.CredentialID, keymgmt.AlertSecretContext{
		OrganizationID: orgID, EndpointID: ep.ID, ProviderType: ep.Type,
	})
	if err != nil {
		if finishErr := s.finishAlertTest(r, ep, now, "decryption_failed", "failure", map[string]interface{}{"reason_code": "decryption_failed", "credential_id": credentialIDForSecret(ep)}); finishErr != nil {
			writeError(w, http.StatusInternalServerError, "could not record target resolution failure")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not resolve target")
		return
	}
	if target == "" {
		if finishErr := s.finishAlertTest(r, ep, now, "credential_not_configured", "failure", map[string]interface{}{"reason_code": "credential_not_configured"}); finishErr != nil {
			writeError(w, http.StatusInternalServerError, "could not record missing credential")
			return
		}
		writeError(w, http.StatusBadRequest, "endpoint has no secret configured")
		return
	}
	if !isAcceptableAlertTarget(ep.Type, target) {
		if finishErr := s.finishAlertTest(r, ep, now, "invalid_target", "failure", map[string]interface{}{"reason_code": "invalid_target", "credential_id": credentialIDForSecret(ep)}); finishErr != nil {
			writeError(w, http.StatusInternalServerError, "could not record invalid target")
			return
		}
		writeError(w, http.StatusBadRequest, "stored target is invalid")
		return
	}
	payload := []byte(`{"text":"[pccp] alert endpoint test"}`)
	ctx, cancel := stdctx.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		if finishErr := s.finishAlertTest(r, ep, now, "request_build_failed", "failure", map[string]interface{}{"reason_code": "request_build_failed", "credential_id": credentialIDForSecret(ep)}); finishErr != nil {
			writeError(w, http.StatusInternalServerError, "could not record request build failure")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not build test request")
		return
	}
	req2.Header.Set("Content-Type", "application/json")
	resp, err := s.alertHTTPClient.Do(req2)
	if err != nil {
		reason := security.AlertDeliveryErrorClass(err)
		if err := s.finishAlertTest(r, ep, now, reason, "failure", map[string]interface{}{"credential_id": credentialIDForSecret(ep), "reason_code": reason}); err != nil {
			writeError(w, http.StatusInternalServerError, "test delivery failed and could not be recorded")
			return
		}
		writeError(w, http.StatusBadGateway, "test delivery failed")
		return
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 64<<10)
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	statusClass := "non_2xx"
	if ok {
		statusClass = "2xx"
	}
	result := "success"
	if !ok {
		result = "failure"
	}
	if err := s.finishAlertTest(r, ep, now, statusClass, result, map[string]interface{}{
		"credential_id": credentialIDForSecret(ep), "status_class": statusClass, "http_status": resp.StatusCode,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "test result could not be recorded")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status_class": statusClass,
		"http_status":  resp.StatusCode,
		"ok":           ok,
	})
}

// --- PII lexicon (security C5) ---

func (s *Server) handleGetLexicon(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	lexicon := s.security.GetLexicon(orgID)
	if lexicon == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"version": "builtin", "patterns": map[string]string{}})
		return
	}
	writeJSON(w, http.StatusOK, lexicon)
}

func (s *Server) handleUpdateLexicon(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		Version  string            `json:"version"`
		Patterns map[string]string `json:"patterns"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	lexicon, err := s.security.SetLexicon(orgID, req.Version, getActorID(r), req.Patterns)
	if err != nil {
		// PAT-1508: validation/safety failures are client errors (400);
		// storage failures stay 500.
		if strings.HasPrefix(err.Error(), "security: lexicon rule") || err.Error() == "security: lexicon cannot be empty" {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.security.lexicon_published",
		ActorType:      "admin",
		Action:         "publish_pii_lexicon",
		ResourceType:   "pii_lexicon",
		ResourceID:     lexicon.ID,
		Details:        fmt.Sprintf(`{"version":"%s","patterns":%d}`, lexicon.Version, len(req.Patterns)),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, lexicon)
}

func (s *Server) handleSecurityCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := getOrgID(r)
	result := s.security.CheckContext(orgID, req.Text)
	writeJSON(w, http.StatusOK, result)
}

// handleGlobalSearch implements the unified search service (00
// cross-cutting A11): one query fans out across users, harnesses,
// projects, repositories, sessions, models, endpoints, business units,
// and findings. Results carry a route path for the command palette and
// a cross-entity action where one exists (e.g. start a 1:1 chat with a
// user, view a harness's sessions).
func (s *Server) handleGlobalSearch(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []map[string]interface{}{})
		return
	}
	like := "%" + q + "%"
	const limit = 5
	results := make([]map[string]interface{}, 0, 30)
	add := func(typ, icon, id, label, sub, path string, action map[string]interface{}) {
		if id == "" {
			return
		}
		row := map[string]interface{}{"type": typ, "type_icon": icon, "id": id, "label": label, "sub": sub, "path": path}
		if action != nil {
			row["action"] = action
		}
		results = append(results, row)
	}
	// scopedWhere builds an org-scoped query. Values are ALWAYS bound,
	// never interpolated into SQL text (injection defense-in-depth even
	// for claim-derived values).
	scopedWhere := func(pattern string, likes ...interface{}) *gorm.DB {
		if orgID != "" {
			pattern += " AND organization_id = ?"
			likes = append(likes, orgID)
		}
		return s.db.Where(pattern, likes...)
	}

	// Users → detail route + cross-actions (1:1 chat, sessions).
	var users []models.User
	scopedWhere("name LIKE ? OR name_ko LIKE ? OR email LIKE ?", like, like, like).
		Order("created_at DESC").Limit(limit).Find(&users)
	for _, u := range users {
		add("user", "◉", u.ID, firstNonEmpty(u.NameKo, u.Name, u.Email), u.Email, "/users/"+u.ID,
			map[string]interface{}{"type": "chat", "href": "/communications?user=" + u.ID})
	}

	var harnesses []models.Harness
	scopedWhere("harness_id LIKE ? OR binary_version LIKE ?", like, like).
		Order("created_at DESC").Limit(limit).Find(&harnesses)
	for _, h := range harnesses {
		add("harness", "⬡", h.ID, h.HarnessID, h.Status+" · v"+h.BinaryVersion, "/harnesses/"+h.ID, nil)
	}

	var projects []models.Project
	scopedWhere("name LIKE ? OR name_ko LIKE ? OR slug LIKE ?", like, like, like).
		Order("created_at DESC").Limit(limit).Find(&projects)
	for _, p := range projects {
		add("project", "▣", p.ID, firstNonEmpty(p.NameKo, p.Name), p.Slug, "/projects/"+p.ID, nil)
	}

	var repos []models.Repository
	scopedWhere("name LIKE ? OR clone_url LIKE ? OR scm_provider LIKE ?", like, like, like).
		Order("created_at DESC").Limit(limit).Find(&repos)
	for _, rp := range repos {
		add("repository", "▤", rp.ID, rp.Name, rp.CloneURL, "/repositories/"+rp.ID, nil)
	}

	var sessions []models.Session
	scopedWhere("title LIKE ? OR session_id LIKE ?", like, like).
		Order("created_at DESC").Limit(limit).Find(&sessions)
	for _, sess := range sessions {
		add("session", "◐", sess.ID, firstNonEmpty(sess.Title, sess.SessionID), sess.Status, "/sessions/"+sess.SessionID, nil)
	}

	var pkgs []models.ModelPackage
	scopedWhere("name LIKE ? OR model_id LIKE ?", like, like).
		Order("created_at DESC").Limit(limit).Find(&pkgs)
	for _, m := range pkgs {
		add("model", "◆", m.ID, firstNonEmpty(m.Name, m.ModelID), m.Family, "/models/"+m.ID, nil)
	}

	var eps []models.InferenceEndpoint
	scopedWhere("endpoint_id LIKE ? OR model_id LIKE ?", like, like).
		Order("created_at DESC").Limit(limit).Find(&eps)
	for _, e := range eps {
		add("endpoint", "◇", e.ID, e.EndpointID, e.Status, "/endpoints/"+e.ID, nil)
	}

	var bus []models.BusinessUnit
	scopedWhere("name LIKE ? OR name_ko LIKE ?", like, like).
		Order("created_at DESC").Limit(limit).Find(&bus)
	for _, b := range bus {
		add("business_unit", "▦", b.ID, firstNonEmpty(b.NameKo, b.Name), b.Type, "/users", nil)
	}

	var findings []models.SecurityFinding
	scopedWhere("title LIKE ? OR title_ko LIKE ? OR finding_type LIKE ?", like, like, like).
		Order("occurred_at DESC").Limit(limit).Find(&findings)
	for _, f := range findings {
		add("finding", "🛡", f.ID, firstNonEmpty(f.TitleKo, f.Title), f.FindingType+" · "+f.Severity, "/findings/"+f.ID, nil)
	}

	writeJSON(w, http.StatusOK, results)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	dash := map[string]interface{}{}

	var userCount, harnessCount, sessionCount, endpointCount int64
	q := s.db.Model(&models.User{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Count(&userCount)

	q = s.db.Model(&models.Harness{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Count(&harnessCount)

	q = s.db.Model(&models.Session{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Count(&sessionCount)

	q = s.db.Model(&models.InferenceEndpoint{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Count(&endpointCount)

	dash["users"] = userCount
	dash["harnesses"] = harnessCount
	dash["sessions"] = sessionCount
	dash["endpoints"] = endpointCount

	// Active sessions
	var activeSessions []models.Session
	q = s.db.Model(&models.Session{}).Where("status = 'active'")
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Order("opened_at DESC").Limit(10).Find(&activeSessions)
	dash["active_sessions"] = activeSessions
	var activeSessionCount int64
	s.db.Model(&models.Session{}).Where("organization_id = ? AND status = ?", orgID, "active").Count(&activeSessionCount)
	dash["active_session_count"] = activeSessionCount

	// Recent audit events
	var recentEvents []models.AuditEvent
	q = s.db.Model(&models.AuditEvent{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Order("occurred_at DESC").Limit(20).Find(&recentEvents)
	dash["recent_events"] = recentEvents
	dash["recent_activity"] = recentEvents

	// Active incidents (A5): security findings with severity critical/
	// high + open remediation tasks are surfaced for the ops view.
	// PAT-1484: the KPI count resolves through the SAME canonical scope
	// contract as the destination list (/security?tab=findings&
	// severity=critical,high&status=unresolved) so card ↔ list reconcile.
	var openFindings int64
	s.securityFindingScope(s.db.Model(&models.SecurityFinding{}).Where("organization_id = ?", orgID), "critical,high", "unresolved").
		Count(&openFindings)
	dash["open_critical_findings"] = openFindings
	// PAT-1487: canonical metric dictionary. Every repeated count resolves
	// through the shared scope builders so the card, the side panel, and the
	// destination list always agree, and intentionally-different scopes are
	// labelled distinctly by the UI:
	//   - open_critical_findings : severity IN (critical,high) AND status != resolved
	//   - unresolved_findings    : ANY severity AND status != resolved
	//   - total_findings         : every finding (any severity/status)
	var unresolvedFindings, totalFindings int64
	s.securityFindingScope(s.db.Model(&models.SecurityFinding{}).Where("organization_id = ?", orgID), "", "unresolved").
		Count(&unresolvedFindings)
	s.db.Model(&models.SecurityFinding{}).Where("organization_id = ?", orgID).Count(&totalFindings)
	dash["unresolved_findings"] = unresolvedFindings
	dash["total_findings"] = totalFindings
	// Dashboard-level freshness + stale/partial state (PAT-1487): consumers
	// show the last-updated time and mark the whole panel as stale instead of
	// rendering contradictory numbers. A per-card failure flag is retained by
	// the frontend (loading/error), never faked stale here.
	dash["dashboard_last_updated"] = time.Now().Format(time.RFC3339)
	dash["dashboard_stale"] = false
	var openRemediations int64
	s.db.Model(&models.ComplianceRemediation{}).
		Where("organization_id = ? AND status != 'done'", orgID).Count(&openRemediations)
	dash["open_remediations"] = openRemediations
	// PAT-1488: admin action-center metrics. Every group the action center
	// renders must be backed by a real server-side count so the card never
	// manufactures alerts the data model cannot support, and each count uses
	// the same scope contract as its destination list (fleet approval queue,
	// harness quarantine list), so card ↔ list reconcile.
	var quarantinedHarnesses, pendingApprovals int64
	s.db.Model(&models.Harness{}).
		Where("organization_id = ? AND status = 'quarantined'", orgID).Count(&quarantinedHarnesses)
	s.db.Model(&models.Approval{}).
		Where("organization_id = ? AND decision = 'pending'", orgID).Count(&pendingApprovals)
	dash["quarantined_harnesses"] = quarantinedHarnesses
	dash["pending_approvals"] = pendingApprovals

	// Recents (A7): recently updated entities for the object hub.
	var recentUsers []models.User
	s.db.Model(&models.User{}).Where("organization_id = ?", orgID).
		Order("updated_at DESC").Limit(5).Find(&recentUsers)
	dash["recent_users"] = recentUsers
	var recentProjects []models.Project
	s.db.Model(&models.Project{}).Where("organization_id = ?", orgID).
		Order("updated_at DESC").Limit(5).Find(&recentProjects)
	recentProjectRows, err := s.decorateProjects(recentProjects, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dash["recent_projects"] = recentProjectRows
	dash["recent_events"] = recentEvents

	writeJSON(w, http.StatusOK, dash)
}

func (s *Server) handleListBroadcasts(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var broadcasts []models.Broadcast
	s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Limit(50).Find(&broadcasts)
	writeJSON(w, http.StatusOK, broadcasts)
}

func (s *Server) handleCreateFileTransfer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SenderID       string `json:"sender_id"`
		RecipientID    string `json:"recipient_id"`
		FileName       string `json:"file_name"`
		FileSize       int64  `json:"file_size"`
		FileType       string `json:"file_type"`
		Classification string `json:"classification"`
		ExpiresAt      string `json:"expires_at"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := getOrgID(r)
	ft, err := s.comms.CreateFileTransfer(orgID, req.SenderID, req.RecipientID, req.FileName, req.FileSize, req.FileType, req.Classification, req.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ft)
}

func (s *Server) handleListFileTransfers(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var transfers []models.FileTransfer
	s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Limit(50).Find(&transfers)
	writeJSON(w, http.StatusOK, transfers)
}

func (s *Server) handleGetRepoProvenance(w http.ResponseWriter, r *http.Request) {
	repoId := chi.URLParam(r, "repoId")
	orgID := getOrgID(r)

	// Get changesets for this repo
	var changeSets []models.ChangeSet
	s.db.Where("organization_id = ? AND repository_id = ?", orgID, repoId).
		Order("created_at DESC").Limit(50).Find(&changeSets)

	// Get provenance spans for this repo
	var spans []models.ProvenanceSpan
	s.db.Where("organization_id = ? AND repository_id = ?", orgID, repoId).
		Order("created_at DESC").Limit(100).Find(&spans)

	// Get sessions that touched this repo
	sessionIds := make(map[string]bool)
	for _, cs := range changeSets {
		if cs.SessionID != "" {
			sessionIds[cs.SessionID] = true
		}
	}
	var sessions []models.Session
	for sid := range sessionIds {
		var sess models.Session
		if s.db.Where("session_id = ?", sid).First(&sess).Error == nil {
			sessions = append(sessions, sess)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"change_sets": changeSets,
		"spans":       spans,
		"sessions":    sessions,
	})
}

func (s *Server) handleGetRepoChangeSets(w http.ResponseWriter, r *http.Request) {
	repoId := chi.URLParam(r, "repoId")
	orgID := getOrgID(r)
	var changeSets []models.ChangeSet
	s.db.Where("organization_id = ? AND repository_id = ?", orgID, repoId).
		Order("created_at DESC").Limit(50).Find(&changeSets)
	writeJSON(w, http.StatusOK, changeSets)
}

func (s *Server) handleGetRepoSpans(w http.ResponseWriter, r *http.Request) {
	repoId := chi.URLParam(r, "repoId")
	orgID := getOrgID(r)
	var spans []models.ProvenanceSpan
	s.db.Where("organization_id = ? AND repository_id = ?", orgID, repoId).
		Order("created_at DESC").Limit(100).Find(&spans)
	writeJSON(w, http.StatusOK, spans)
}

func (s *Server) handleGetRepoProvenanceStats(w http.ResponseWriter, r *http.Request) {
	repoId := chi.URLParam(r, "repoId")
	orgID := getOrgID(r)

	var totalChangeSets int64
	s.db.Model(&models.ChangeSet{}).Where("organization_id = ? AND repository_id = ?", orgID, repoId).Count(&totalChangeSets)

	var aiGenerated int64
	s.db.Model(&models.ChangeSet{}).Where("organization_id = ? AND repository_id = ? AND attribution_state = ?", orgID, repoId, "AI_GENERATED").Count(&aiGenerated)

	var humanEdited int64
	s.db.Model(&models.ChangeSet{}).Where("organization_id = ? AND repository_id = ? AND attribution_state = ?", orgID, repoId, "AI_THEN_HUMAN_EDITED").Count(&humanEdited)

	var humanWritten int64
	s.db.Model(&models.ChangeSet{}).Where("organization_id = ? AND repository_id = ? AND attribution_state = ?", orgID, repoId, "HUMAN_WRITTEN").Count(&humanWritten)

	var totalSpans int64
	s.db.Model(&models.ProvenanceSpan{}).Where("organization_id = ? AND repository_id = ?", orgID, repoId).Count(&totalSpans)

	var linesAdded int64
	var linesRemoved int64
	type lineResult struct{ Sum int64 }
	var lr lineResult
	s.db.Model(&models.ChangeSet{}).Where("organization_id = ? AND repository_id = ?", orgID, repoId).Select("COALESCE(SUM(lines_added), 0) as sum").Scan(&lr)
	linesAdded = lr.Sum
	s.db.Model(&models.ChangeSet{}).Where("organization_id = ? AND repository_id = ?", orgID, repoId).Select("COALESCE(SUM(lines_removed), 0) as sum").Scan(&lr)
	linesRemoved = lr.Sum

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_changesets": totalChangeSets,
		"ai_generated":     aiGenerated,
		"ai_then_human":    humanEdited,
		"human_written":    humanWritten,
		"total_spans":      totalSpans,
		"lines_added":      linesAdded,
		"lines_removed":    linesRemoved,
		"ai_percentage": fmt.Sprintf("%.0f%%", func() float64 {
			if totalChangeSets > 0 {
				return float64(aiGenerated) / float64(totalChangeSets) * 100
			} else {
				return 0
			}
		}()),
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- Enterprise Harness Feature Handlers ---

func (s *Server) handleShadowAI(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	findings, err := s.korean.DetectShadowAI(orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, findings)
}

func (s *Server) handleCodeSpanLookup(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "repoId")
	orgID := getOrgID(r)
	filePath := r.URL.Query().Get("file")
	startLine, _ := strconv.Atoi(r.URL.Query().Get("start"))
	endLine, _ := strconv.Atoi(r.URL.Query().Get("end"))
	if endLine == 0 {
		endLine = startLine
	}
	result, err := s.provenance.LookupCodeSpan(orgID, repoID, filePath, startLine, endLine)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handlePostChangeSet receives a ChangeSet from a harness with file-level provenance.
// The harness submits this after applying code mutations (Plan B — provenance evidence).
func (s *Server) handlePostChangeSet(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		SessionID        string   `json:"session_id"`
		ExchangeID       string   `json:"exchange_id"`
		RepositoryID     string   `json:"repository_id"`
		Branch           string   `json:"branch"`
		FilesChanged     []string `json:"files_changed"`
		DiffSummary      string   `json:"diff_summary"`
		LinesAdded       int      `json:"lines_added"`
		LinesRemoved     int      `json:"lines_removed"`
		AttributionState string   `json:"attribution_state"`
		Confidence       float64  `json:"confidence"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cs, err := s.provenance.CreateChangeSet(provenance.CreateChangeSetRequest{
		OrganizationID:   orgID,
		SessionID:        req.SessionID,
		ExchangeID:       req.ExchangeID,
		RepositoryID:     req.RepositoryID,
		Branch:           req.Branch,
		FilesChanged:     req.FilesChanged,
		DiffSummary:      req.DiffSummary,
		LinesAdded:       req.LinesAdded,
		LinesRemoved:     req.LinesRemoved,
		AttributionState: req.AttributionState,
		Confidence:       req.Confidence,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// AI change-control (projects B7, §33.4): score the change; changes
	// requiring approval route to the project's queue instead of
	// flowing straight through.
	if cr, ok := s.queueHighRiskChange(orgID, cs, req.FilesChanged); ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{"change_set": cs, "change_request": cr, "queued": true})
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

// queueHighRiskChange scores a changeset with the impact engine and
// creates a change-control queue item when approval is required.
func (s *Server) queueHighRiskChange(orgID string, cs *models.ChangeSet, files []string) (*models.ChangeRequest, bool) {
	if len(files) == 0 {
		return nil, false
	}
	var req impact.AnalyzeRequest
	req.OrganizationID = orgID
	req.RepositoryID = cs.RepositoryID
	if len(files) > 0 {
		req.FilePath = files[0]
	}
	req.SymbolsChanged = files
	for _, f := range files {
		ps := impact.DetectPathSensitivity(f)
		if ps.IsAuth || strings.Contains(f, "auth") || strings.Contains(f, "credential") || strings.Contains(f, "token") {
			req.IsAuth = true
		}
		if ps.IsCrypto || strings.Contains(f, "crypto") || strings.Contains(f, "key") || strings.Contains(f, "sign") {
			req.IsCrypto = true
		}
		if ps.IsDB || strings.Contains(f, "migration") || strings.Contains(f, "schema") {
			req.IsDBMigration = true
		}
		if ps.IsConfig || strings.Contains(f, "config") || strings.Contains(f, "prod") {
			req.IsConfig = true
		}
	}
	_, score, err := s.impact.AnalyzeChange(req)
	if err != nil || score == nil || !score.RequiresApproval {
		return nil, false
	}
	var repo models.Repository
	s.db.First(&repo, "id = ?", cs.RepositoryID)
	projectID := repo.ProjectID
	cr := &models.ChangeRequest{
		OrganizationID: orgID,
		ProjectID:      projectID,
		RepositoryID:   cs.RepositoryID,
		ChangeSetID:    cs.ID,
		SessionID:      cs.SessionID,
		Title:          fmt.Sprintf("%d개 파일 변경 · AI-generated change", len(files)),
		Kind:           "ai_code_change",
		RiskLevel:      score.Level,
		RiskScore:      score.Score,
		Status:         "pending",
		RequestedBy:    cs.UserID,
	}
	if err := s.db.Create(cr).Error; err != nil {
		return nil, false
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.project.change_request_created",
		ActorType:      "system",
		Action:         "queue_change_request",
		ResourceType:   "change_request",
		ResourceID:     cr.ID,
		Details:        fmt.Sprintf(`{"changeset":"%s","risk":"%s","score":%.1f}`, cs.ID, score.Level, score.Score),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	return cr, true
}

// handlePostProvenanceSpan receives a ProvenanceSpan from a harness, mapping a
// code region to its origin session/user/model (Plan B — provenance evidence).
func (s *Server) handlePostProvenanceSpan(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		RepositoryID     string  `json:"repository_id"`
		ChangeSetID      string  `json:"change_set_id"`
		FilePath         string  `json:"file_path"`
		CommitSHA        string  `json:"commit_sha"`
		SymbolLang       string  `json:"symbol_lang"`
		SymbolName       string  `json:"symbol_name"`
		StartLine        int     `json:"start_line"`
		EndLine          int     `json:"end_line"`
		AttributionState string  `json:"attribution_state"`
		Confidence       float64 `json:"confidence"`
		SessionID        string  `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	span, err := s.provenance.CreateProvenanceSpan(provenance.CreateSpanRequest{
		OrganizationID:   orgID,
		RepositoryID:     req.RepositoryID,
		ChangeSetID:      req.ChangeSetID,
		FilePath:         req.FilePath,
		CommitSHA:        req.CommitSHA,
		SymbolLang:       req.SymbolLang,
		SymbolName:       req.SymbolName,
		StartLine:        req.StartLine,
		EndLine:          req.EndLine,
		AttributionState: req.AttributionState,
		Confidence:       req.Confidence,
		SessionID:        req.SessionID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, span)
}

func (s *Server) handleForcedVersion(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		MinVersion  string `json:"min_version"`
		ReleaseRing string `json:"release_ring"`
		Deadline    string `json:"deadline"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.MinVersion == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "min_version required"})
		return
	}
	if err := s.korean.SetForcedHarnessVersion(orgID, req.MinVersion, req.ReleaseRing, req.Deadline, req.Reason); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "forced version set"})
}

func (s *Server) handleListEnterpriseFeatures(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var features []models.EnterpriseHarnessFeature
	s.db.Where("organization_id = ?", orgID).Order("category, feature_key").Find(&features)
	writeJSON(w, http.StatusOK, features)
}

// pattyMandatoryEnterpriseFeatures mirrors the `mandatory` flags in the
// client catalog (web/src/enterpriseFeatures.ts FEATURE_CATALOG). Weakening
// one of these (enabled/enforced true→false) requires a privileged role.
var pattyMandatoryEnterpriseFeatures = map[string]bool{
	"code_review": true, "code_signing": true, "audit_export": true,
	"sso_binding": true, "device_attestation": true, "data_classification": true,
	"supply_chain": true, "network_egress": true, "secret_broker": true,
	"mandatory_ack": true, "ai_attribution": true, "command_auth": true,
	"mcp_allowlist": true, "model_recall": true,
}

// enterpriseRolePrivileged mirrors PATTY_ROLES in web/src/enterpriseFeatures.ts.
func enterpriseRolePrivileged(role string) bool {
	return role == "super_admin" || role == "security_admin"
}

// enterpriseRoleAdmin mirrors ADMIN_ROLES in web/src/enterpriseFeatures.ts.
func enterpriseRoleAdmin(role string) bool {
	return enterpriseRolePrivileged(role) || role == "admin" || role == "owner"
}

// enterpriseConfigHeadEpoch reads the head (max) rollout epoch from the
// feature's governance config JSON; 0 for empty or invalid configs.
func enterpriseConfigHeadEpoch(config string) int {
	var g struct {
		Rollouts []struct {
			Epoch int `json:"epoch"`
		} `json:"rollouts"`
	}
	if err := json.Unmarshal([]byte(config), &g); err != nil {
		return 0
	}
	head := 0
	for _, r := range g.Rollouts {
		if r.Epoch > head {
			head = r.Epoch
		}
	}
	return head
}

func (s *Server) handleUpdateEnterpriseFeature(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	// Feature mutations are admin-only for every feature, not just
	// patty-mandatory ones.
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "admin_role_required")
		return
	}
	var req struct {
		Enabled       bool   `json:"enabled"`
		Enforced      bool   `json:"enforced"`
		Config        string `json:"config"`
		Reason        string `json:"reason"`
		ExpectedEpoch *int   `json:"expected_epoch"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// The UI promises every change is audit-logged with a reason, so the
	// server enforces it rather than trusting the client.
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "reason_required")
		return
	}
	// Org-scoped lookup: another tenant's feature id is indistinguishable
	// from a missing one.
	var feature models.EnterpriseHarnessFeature
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&feature).Error; err != nil {
		writeError(w, http.StatusNotFound, "feature not found")
		return
	}
	// Patty-mandatory (패티 필수): tenant admins may not weaken these.
	weakening := (feature.Enabled && !req.Enabled) || (feature.Enforced && !req.Enforced)
	if weakening && pattyMandatoryEnterpriseFeatures[feature.FeatureKey] && !enterpriseRolePrivileged(getRole(r)) {
		writeError(w, http.StatusForbidden, "patty_mandatory_weakening_forbidden")
		return
	}
	// Optimistic concurrency: the client sends the head epoch its change was
	// based on; if the stored head moved, reject so rollouts never clobber.
	if req.ExpectedEpoch != nil && enterpriseConfigHeadEpoch(feature.Config) != *req.ExpectedEpoch {
		writeError(w, http.StatusConflict, "epoch_conflict")
		return
	}
	// True CAS: the state the decision was based on is folded into the
	// UPDATE's WHERE clause, so a concurrent write between our read and this
	// update matches zero rows instead of clobbering.
	query := s.db.Model(&models.EnterpriseHarnessFeature{}).Where("id = ? AND organization_id = ?", id, orgID)
	if req.ExpectedEpoch != nil {
		query = query.Where("enabled = ? AND enforced = ? AND config = ?", feature.Enabled, feature.Enforced, feature.Config)
	}
	res := query.Updates(map[string]interface{}{"enabled": req.Enabled, "enforced": req.Enforced, "config": req.Config})
	if res.Error != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if res.RowsAffected == 0 {
		writeError(w, http.StatusConflict, "epoch_conflict")
		return
	}
	details, _ := json.Marshal(map[string]interface{}{
		"feature_key": feature.FeatureKey,
		"enabled":     req.Enabled,
		"enforced":    req.Enforced,
		"reason":      strings.TrimSpace(req.Reason),
		"head_epoch":  enterpriseConfigHeadEpoch(req.Config),
	})
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		ActorID:        getActorID(r),
		ActorType:      "user",
		EventType:      "enterprise.feature_updated",
		Action:         "update_enterprise_feature",
		ResourceType:   "enterprise_feature",
		ResourceID:     id,
		Result:         "success",
		Details:        string(details),
		OccurredAt:     time.Now().UTC().Format(time.RFC3339Nano),
	})
	s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&feature)
	writeJSON(w, http.StatusOK, feature)
}

func (s *Server) handleListEnterpriseViolations(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "엔터프라이즈 위반 조회 권한이 없습니다")
		return
	}
	// PAT-1516: counts per feature for dashboard reconciliation.
	if r.URL.Query().Get("counts") == "true" {
		type row struct {
		FeatureKey string `json:"feature_key"`
		Open       int    `json:"open"`
		Resolved   int    `json:"resolved"`
	}
		var rows []row
		if err := s.db.Model(&models.EnterpriseFeatureViolation{}).
			Select("feature_key, SUM(CASE WHEN resolved = false THEN 1 ELSE 0 END) as open, SUM(CASE WHEN resolved = true THEN 1 ELSE 0 END) as resolved").
			Where("organization_id = ?", orgID).
			Group("feature_key").Scan(&rows).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "위반 집계 조회 실패")
			return
		}
		writeJSON(w, http.StatusOK, rows)
		return
	}
	var violations []models.EnterpriseFeatureViolation
	if err := s.db.Where("organization_id = ? AND resolved = false", orgID).Order("occurred_at DESC").Limit(100).Find(&violations).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "위반 목록 조회 실패")
		return
	}
	writeJSON(w, http.StatusOK, violations)
}

// handleGetEnterpriseViolation returns the detail record with all
// linked entities (PAT-1516).
func (s *Server) handleGetEnterpriseViolation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "엔터프라이즈 위반 조회 권한이 없습니다")
		return
	}
	var v models.EnterpriseFeatureViolation
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&v).Error; err != nil {
		writeError(w, http.StatusNotFound, "위반 사항을 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// handleResolveViolation requires a disposition + reason + RBAC; for
// risk_accepted the caller must supply an expiry so accepted-risk
// windows do not stay open forever (PAT-1516).
func (s *Server) handleResolveViolation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, http.StatusForbidden, "엔터프라이즈 위반 해결 권한이 없습니다")
		return
	}
	var req struct {
		Disposition        string              `json:"disposition"`        // fixed | false_positive | risk_accepted | duplicate | suppressed
		DispositionReason  string              `json:"disposition_reason"` // required
		Evidence           []map[string]string `json:"evidence"`
		OwnerID            string              `json:"owner_id"`
		ExpiresAt          string              `json:"expires_at"` // required when disposition=risk_accepted
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Disposition {
	case "fixed", "false_positive", "risk_accepted", "duplicate", "suppressed":
	default:
		writeError(w, http.StatusBadRequest, "disposition must be fixed|false_positive|risk_accepted|duplicate|suppressed")
		return
	}
	if req.DispositionReason == "" {
		writeError(w, http.StatusBadRequest, "disposition_reason required")
		return
	}
	if req.Disposition == "risk_accepted" {
		if req.ExpiresAt == "" || req.ExpiresAt == "0001-01-01T00:00:00Z" {
			writeError(w, http.StatusBadRequest, "risk_accepted requires expires_at (accepted-risk window)")
			return
		}
		if t, err := time.Parse(time.RFC3339, req.ExpiresAt); err != nil || t.Before(time.Now()) {
			writeError(w, http.StatusBadRequest, "expires_at must be a future RFC3339 timestamp")
			return
		}
	}
	actor := getActorID(r)
	if actor == "" {
		actor = getOperatorEmail(r)
	}
	evJSON, _ := json.Marshal(req.Evidence)
	expires := req.ExpiresAt
	if expires == "0001-01-01T00:00:00Z" {
		expires = ""
	}
	updates := map[string]interface{}{
		"resolved":           true,
		"disposition":        req.Disposition,
		"disposition_reason": req.DispositionReason,
		"evidence_json":      string(evJSON),
		"owner_id":           req.OwnerID,
		"resolved_by":        actor,
		"resolved_at":        time.Now().UTC().Format(time.RFC3339),
		"expires_at":         expires,
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.EnterpriseFeatureViolation{}).
			Where("id = ? AND organization_id = ? AND resolved = false", id, orgID).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var existing models.EnterpriseFeatureViolation
			if err := tx.Where("id = ? AND organization_id = ?", id, orgID).First(&existing).Error; err == nil && existing.Resolved {
				return gorm.ErrDuplicatedKey
			}
			return gorm.ErrRecordNotFound
		}
		return tx.Create(&models.AuditEvent{
			OrganizationID: orgID, EventType: "cp.enterprise.violation_resolved",
			ActorID: actor, ActorType: "user",
			ResourceType: "enterprise_violation", ResourceID: id,
			Action: "resolve", Result: "success",
			Details: string(evJSON), OccurredAt: updates["resolved_at"].(string),
		}).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			writeError(w, http.StatusConflict, "이미 해결된 위반 사항입니다")
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "위반 사항을 찾을 수 없습니다")
			return
		}
		writeError(w, http.StatusInternalServerError, "위반 해결 처리 실패")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "resolved", "disposition": req.Disposition})
}

func (s *Server) handleSeedEnterpriseFeatures(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	defaults := []struct {
		Key, Name, NameKo, Category, PRD string
		Enforced                         bool
	}{
		{"code_review", "Policy-Enforced Code Review", "정책 기반 코드 리뷰", "governance", "§33.4", true},
		{"code_signing", "Mandatory Code Signing", "의무 코드 서명", "security", "§18.6", true},
		{"coding_standards", "Compliance-Aware Coding Standards", "컴플라이언스 코딩 표준", "compliance", "§33.11", false},
		{"audit_export", "Audit Trail Export", "감사 증거 내보내기", "audit", "§40.3", true},
		{"sso_binding", "SSO/SCIM Identity Binding", "SSO/SCIM 신원 연결", "identity", "§32.1", true},
		{"device_attestation", "Device Posture Attestation", "기기 보안 상태 증명", "security", "§14.1", true},
		{"sandbox_execution", "Mandatory Sandbox Execution", "의무 샌드박스 실행", "security", "§31.2", false},
		{"data_classification", "Data Classification Tagging", "데이터 분류 태깅", "governance", "§16", true},
		{"supply_chain", "Supply Chain Validation", "공급망 검증", "security", "§15.3", true},
		{"network_egress", "Network Egress Control", "네트워크 송신 제어", "security", "§17.4", true},
		{"secret_broker", "Key/Secret Brokering", "키/비밀정보 브로커링", "security", "§17.5", true},
		{"forensic_snapshot", "Forensic Snapshot", "포렌식 스냅샷", "audit", "§14.2", false},
		{"exception_workflow", "Policy Exception Workflow", "정책 예외 워크플로", "governance", "§33.8", false},
		{"mandatory_ack", "Mandatory Acknowledgement", "의무 승인 확인", "governance", "§33.6", true},
		{"change_freeze", "Change-Freeze Mode", "변경 동결 모드", "governance", "§33.13", false},
		{"ai_attribution", "AI Code Attribution", "AI 코드 기여 추적", "audit", "§19", true},
		{"command_auth", "Command Authorization", "명령어 인가", "security", "§17.3", true},
		{"mcp_allowlist", "MCP Server Allowlist", "MCP 서버 허용 목록", "security", "§17.2", true},
		{"model_recall", "Emergency Model Recall", "긴급 모델 리콜", "governance", "§33.9", true},
		{"project_offboard", "Project Offboarding", "프로젝트 오프보딩", "audit", "§33.14", false},
	}

	// liveFeatures are the capabilities whose enforcement loop is
	// actually wired today (validated end-to-end by the DARI live
	// suites: governed tool gate, workflow gates, sandbox policy,
	// network grants, provenance, evidence receipts). Everything else
	// seeds as planned — the tracker reports reality, not aspiration.
	liveFeatures := map[string]bool{
		"change_freeze":       true, // workflow gate (governed)
		"model_recall":        true, // workflow gate (governed)
		"command_auth":        true, // tool registry gate (governed C3)
		"network_egress":      true, // network grants (governed C4)
		"mcp_allowlist":       true, // tool registry covers MCP (C3)
		"ai_attribution":      true, // provenance emission + ingestion (B1/B2)
		"audit_export":        true, // evidence receipts + ack loop (B3)
		"data_classification": true, // DLP finding classification on live path
		// NOT live yet — flips true when P3a lands (separate work):
		// "sandbox_execution": the governance push carries no sandbox
		// rows, so nothing enforces mandatory sandboxing today.
		"sandbox_execution": false,
		// NOT live yet — flips true when P3a lands (separate work):
		// "mandatory_ack": broadcast acks are recorded but never
		// checked/enforced by any gate.
		"mandatory_ack": false,
	}

	inserted := 0
	for _, d := range defaults {
		// Idempotent seed: re-running never duplicates rows.
		var existing models.EnterpriseHarnessFeature
		if err := s.db.Where("organization_id = ? AND feature_key = ?", orgID, d.Key).First(&existing).Error; err == nil {
			continue
		}
		live := liveFeatures[d.Key]
		feature := &models.EnterpriseHarnessFeature{
			Base:           models.Base{},
			OrganizationID: orgID,
			FeatureKey:     d.Key,
			FeatureName:    d.Name,
			FeatureNameKo:  d.NameKo,
			Category:       d.Category,
			PRDRef:         d.PRD,
			Enabled:        live,
			Enforced:       live && d.Enforced,
			Status: func() string {
				if live {
					return "active"
				}
				return "planned"
			}(),
		}
		if err := s.db.Create(feature).Error; err != nil {
			log.Printf("enterprise seed: failed to create %s: %v", d.Key, err)
			continue
		}
		inserted++
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "seeded", "count": inserted, "note": "features seed by actual enforcement state; unwired capabilities are 'planned'"})
}

// handleListChangeSubmissions returns pending change-control
// submissions surfaced by governed harnesses (ActionEnvelope rows of
// type changeboard.submit, newest first).
func (s *Server) handleListChangeSubmissions(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var envs []models.ActionEnvelope
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	q := s.db.Where("organization_id = ? AND action_type = ?", orgID, "changeboard.submit").Order("occurred_at DESC").Limit(100)
	q.Find(&envs)
	transcriptVisible := hasConsolePermission(r, permissionLiveTranscript)
	out := make([]map[string]any, 0, len(envs))
	for _, e := range envs {
		row := map[string]any{
			"envelope_id": e.ID, "harness_id": e.HarnessID, "session_id": e.SessionID,
			"occurred_at": e.OccurredAt,
		}
		if transcriptVisible {
			var payload map[string]any
			_ = json.Unmarshal([]byte(e.ActionPayload), &payload)
			row["payload"] = payload
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleReviewChangeSubmission approves/rejects a pending submission by
// delivering a SIGNED admin directive through the relay admin channel;
// the connector's dispatcher verifies + executes against its durable
// board. The envelope_id is echoed as the directive's correlation id.
func (s *Server) handleReviewChangeSubmission(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	decision := "changeboard.approve"
	if strings.HasSuffix(r.URL.Path, "/reject") {
		decision = "changeboard.reject"
	}
	var env models.ActionEnvelope
	orgID := getOrgID(r)
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&env).Error; err != nil {
		writeError(w, http.StatusNotFound, "submission not found")
		return
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(env.ActionPayload), &payload)
	subID, _ := payload["submission_id"].(string)
	if err := s.pushRelayDirectiveContext(r.Context(), decision, orgID, env.HarnessID, "reviewed via console", map[string]interface{}{"submission_id": subID}); err != nil {
		writeError(w, http.StatusBadGateway, "relay directive delivery failed: "+err.Error())
		return
	}
	if err := s.db.Create(&models.AuditEvent{
		OrganizationID: orgID, EventType: "cp.governance.submission_reviewed",
		ActorType: "admin", Action: decision, ResourceType: "change_submission",
		ResourceID: subID, Details: "harness=" + env.HarnessID,
		Result: "delivered", OccurredAt: time.Now().Format(time.RFC3339),
	}).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "directive delivered but audit persistence failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "directive delivered", "submission": subID})
}

// handleSREProbes performs live reachability probes for the SRE
// console's component cards: the relay's DARI/admin listener and the
// PIA serving endpoint. Addresses come from env; an unset address is
// reported as "unconfigured" — never a fake green dot.
func (s *Server) handleSREProbes(w http.ResponseWriter, r *http.Request) {
	probe := func(addr string) map[string]any {
		if addr == "" {
			return map[string]any{"status": "unconfigured", "addr": ""}
		}
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			return map[string]any{"status": "down", "addr": addr, "error": err.Error()}
		}
		conn.Close()
		return map[string]any{"status": "up", "addr": addr}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"relay": probe(config.RelayProbeAddr()),
		"pia":   probe(config.PIAProbeAddr()),
		"cp":    map[string]any{"status": "up"}, // this handler answering IS the probe
	})
}

// handleSubmitEndpointAttestation records a PIA's measurement
// envelope (the registry's lease gate requires a fresh attestation).
func (s *Server) handleSubmitEndpointAttestation(w http.ResponseWriter, r *http.Request) {
	var att models.EndpointAttestation
	if err := decodeJSON(r, &att); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if id := chi.URLParam(r, "id"); id != "" {
		att.EndpointID = id
	}
	if att.OrganizationID == "" {
		att.OrganizationID = getOrgID(r)
	}
	if err := s.registry.RecordAttestation(&att); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, att)
}
