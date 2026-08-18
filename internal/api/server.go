package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/patrickrho-patty/pccp/internal/config"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
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
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/policy"
	"github.com/patrickrho-patty/pccp/internal/provenance"
	"github.com/patrickrho-patty/pccp/internal/registry"
	"github.com/patrickrho-patty/pccp/internal/sandbox"
	"github.com/patrickrho-patty/pccp/internal/security"
	"github.com/patrickrho-patty/pccp/internal/workintel"
	"gorm.io/gorm"
)

// Server is the Control Plane HTTP API server.
type Server struct {
	db         *gorm.DB
	identity   *identity.Service
	auth       *identity.AuthService
	registry   *registry.Service
	policy     *policy.Service
	provenance *provenance.Service
	security   *security.Service
	comms      *communications.Service
	workintel  *workintel.Service
	events     *events.Service
	gitscm     *gitscm.Service
	impact     *impact.Service
	fleet      *fleet.Service
	context    *context.Service
	sandbox    *sandbox.Service
	korean     *korean.Service
	jwtSecret  string
	router     *chi.Mux
	// keyProvider seals/opens alert-endpoint secret references. nil
	// by default so test fixtures without an HSM/KMS still build.
	// Production callers must inject via SetKeyProvider; write paths
	// fail closed when it is nil. PAT-1502 PR 2.
	keyProvider keymgmt.KeyProvider
	testAlert   *testAlertState
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
	evtSvc, _ := events.New(db)
	gitSvc := gitscm.New(db)
	impactSvc := impact.New(db)
	fleetSvc := fleet.New(db)
	ctxSvc := context.New(db, secSvc)
	sandboxSvc := sandbox.New(db)
	koreanSvc := korean.New(db)
	if err != nil {
		return nil, fmt.Errorf("api: init provenance: %w", err)
	}

	s := &Server{
		db:         db,
		identity:   idSvc,
		auth:       authSvc,
		registry:   regSvc,
		policy:     polSvc,
		provenance: provSvc,
		security:   secSvc,
		comms:      commsSvc,
		workintel:  wiSvc,
		events:     evtSvc,
		gitscm:     gitSvc,
		impact:     impactSvc,
		fleet:      fleetSvc,
		context:    ctxSvc,
		sandbox:    sandboxSvc,
		korean:     koreanSvc,
		jwtSecret:  jwtSecret,
	}
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
		ExposedHeaders:   []string{"Link"},
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
			r.Get("/oidc/auth-url", s.wrapSSOOIDCAuthURL(ext))
			r.Post("/oidc/callback", s.wrapSSOOIDCCallback(ext))
		})
	}

	// Authenticated API routes
	r.Route("/api", func(r chi.Router) {
		r.Use(s.authMiddleware)

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
			r.Post("/exceptions", s.handleCreatePolicyException)
			r.Post("/exceptions/{id}/decide", s.handleDecidePolicyException)
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
		r.Get("/security/lockdown-impact", s.handleSecurityLockdownImpact)
		r.Post("/security/scan-session", s.handleScanSession)
		r.Get("/security/alerts", s.handleListAlertEndpoints)
		r.Post("/security/alerts", s.handleCreateAlertEndpoint)
		r.Delete("/security/alerts/{id}", s.handleDeleteAlertEndpoint)
		r.Post("/security/alerts/{id}/test", s.handleTestAlertEndpoint)
		r.Post("/security/alerts/{id}/rotate", s.handleRotateAlertEndpoint)
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
			r.Get("/image-allowlist", s.handleSandboxImageAllowlist)
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
			r.Put("/violations/{id}", s.handleResolveViolation)
			r.Post("/features/seed", s.handleSeedEnterpriseFeatures)
			// D2 change-control review queue: pending connector
			// submissions (governed action envelopes) + approve/reject
			// via signed relay directives.
			r.Get("/submissions", s.handleListChangeSubmissions)
			r.Post("/submissions/{id}/approve", s.handleReviewChangeSubmission)
			r.Post("/submissions/{id}/reject", s.handleReviewChangeSubmission)
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

// sweepSessions transitions sessions past their idle window to idle
// and auto-closes sessions past their TTL (web/02 A4 — the status
// machine's idle state was previously unreachable).
// bridgeSpineToRealtime polls the durable event spine for governed
// exchange + security events and forwards them to the realtime hub.
// 2s cadence: live enough for an activity feed, bounded DB load.
func (s *Server) bridgeSpineToRealtime() {
	last := time.Now().Add(-time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		var events []models.AuditEvent
		if err := s.db.Where(
			"(event_type LIKE ? OR event_type LIKE ?) AND occurred_at > ?",
			"cp.exchange.%", "cp.security%", last.Format(time.RFC3339Nano),
		).Order("occurred_at ASC").Limit(200).Find(&events).Error; err != nil {
			continue
		}
		for _, e := range events {
			switch {
			case strings.HasPrefix(e.EventType, "cp.exchange."):
				s.ext().Realtime.NotifyExchangeEvent(e.OrganizationID, e.ResourceType, e.ResourceID, strings.TrimPrefix(e.EventType, "cp.exchange."), 0)
			default:
				s.ext().Realtime.NotifySecurityFinding(e.OrganizationID, "info", e.EventType)
			}
			if t, err := time.Parse(time.RFC3339Nano, e.OccurredAt); err == nil {
				last = t
			}
		}
	}
}

func (s *Server) sweepSessions() {
	now := time.Now()
	// active → idle: last activity older than IdleTTL.
	s.db.Exec(`UPDATE sessions SET status = 'idle' WHERE status = 'active' AND idle_ttl > 0 AND last_activity_at != '' AND last_activity_at != ?`,
		now.Format(time.RFC3339))
	var idleCandidates []models.Session
	s.db.Where("status IN ('active','idle')").Find(&idleCandidates)
	for _, sess := range idleCandidates {
		opened, err := time.Parse(time.RFC3339, sess.OpenedAt)
		if err != nil {
			continue
		}
		if sess.SessionTTL > 0 && now.Sub(opened) > time.Duration(sess.SessionTTL)*time.Second {
			s.db.Model(&sess).Update("status", "closed")
		} else if sess.Status == "active" {
			last := opened
			if sess.LastActivityAt != "" {
				if t, err := time.Parse(time.RFC3339, sess.LastActivityAt); err == nil {
					last = t
				}
			}
			if sess.IdleTTL > 0 && now.Sub(last) > time.Duration(sess.IdleTTL)*time.Second {
				s.db.Model(&sess).Update("status", "idle")
			}
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

		authHeader := r.Header.Get("Authorization")
		claims, err := s.auth.AuthMiddleware(authHeader)
		if err != nil {
			// Dev convenience ONLY when the bootstrap-empty state is
			// CONFIRMED. The check ensures the table exists (fresh
			// test/dev DBs) and then reads the count with error
			// propagation — a failed query fails CLOSED: a DB error
			// must never wave unauthenticated requests through.
			var count int64
			dbErr := s.db.Exec("CREATE TABLE IF NOT EXISTS admin_credentials (id varchar(64) PRIMARY KEY)").Error
			if dbErr == nil {
				dbErr = s.db.Raw("SELECT count(*) FROM admin_credentials").Scan(&count).Error
			}
			if dbErr != nil {
				writeError(w, http.StatusUnauthorized, "auth: cannot verify bootstrap state")
				return
			}
			if count == 0 {
				next.ServeHTTP(w, r)
				return
			}
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

	// Idempotent org: reuse an existing org by exact name instead of
	// minting a new one on every re-bootstrap (the audit's data-integrity
	// finding — repeat bootstraps used to spam orgs).
	var org models.Organization
	if err := s.db.Where("name = ?", req.OrgName).First(&org).Error; err == nil {
		// Existing org: honor a profile change only when the org has no
		// sessions yet (an in-use org's profile is an operator decision,
		// not a bootstrap re-run).
		var sessions int64
		s.db.Model(&models.Session{}).Where("organization_id = ?", org.ID).Count(&sessions)
		if org.Profile != req.Profile && sessions == 0 {
			s.db.Model(&org).Update("profile", req.Profile)
			org.Profile = req.Profile
		}
	} else {
		created, cerr := s.identity.CreateOrganization(req.OrgName, req.OrgName, "default", req.Profile)
		if cerr != nil {
			writeError(w, http.StatusInternalServerError, cerr.Error())
			return
		}
		org = *created
	}

	// Bootstrap admin (idempotent per identity.BootstrapAdmin)
	if err := s.auth.BootstrapAdmin(req.Email, req.Password, org.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Profile/pack choice is recorded honestly (web/26 A/B).
	s.db.Where("organization_id = ? AND key = ?", org.ID, "bootstrap.profile").
		Delete(&models.OrgSetting{})
	s.db.Create(&models.OrgSetting{
		Base:           models.Base{ID: models.GenerateID("os")},
		OrganizationID: org.ID, Key: "bootstrap.profile", Value: org.Profile,
	})
	if req.PolicyPack != "" {
		s.db.Where("organization_id = ? AND key = ?", org.ID, "bootstrap.policy_pack").
			Delete(&models.OrgSetting{})
		s.db.Create(&models.OrgSetting{
			Base:           models.Base{ID: models.GenerateID("os")},
			OrganizationID: org.ID, Key: "bootstrap.policy_pack", Value: req.PolicyPack,
		})
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
	result := s.db.Find(&orgs)
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
		q.Count(&total)
		var users []models.User
		q.Offset((page - 1) * size).Limit(size).Find(&users)
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": s.decorateUsers(users), "total": total, "page": page, "size": size})
		return
	}

	// Full list (backward compatible — used by cross-page lookups)
	var users []models.User
	q.Find(&users)
	writeJSON(w, http.StatusOK, s.decorateUsers(users))
}

// decorateUsers keeps list-level relationship summaries consistent with the
// user detail view. Harness membership is stored as a JSON array, so matching
// is done after decoding rather than with an unsafe substring query.
func (s *Server) decorateUsers(users []models.User) []map[string]interface{} {
	harnessesByOrg := map[string][]models.Harness{}
	for _, user := range users {
		if _, loaded := harnessesByOrg[user.OrganizationID]; loaded {
			continue
		}
		var harnesses []models.Harness
		s.db.Where("organization_id = ? AND status != ?", user.OrganizationID, "revoked").Find(&harnesses)
		harnessesByOrg[user.OrganizationID] = harnesses
	}

	out := make([]map[string]interface{}, 0, len(users))
	for _, user := range users {
		row := map[string]interface{}{}
		b, _ := json.Marshal(user)
		_ = json.Unmarshal(b, &row)
		var roleIDs []string
		s.db.Model(&models.UserRole{}).Where("organization_id = ? AND user_id = ?", user.OrganizationID, user.ID).Pluck("role_id", &roleIDs)
		var harnessCount int64
		for _, harness := range harnessesByOrg[user.OrganizationID] {
			var allowedUsers []string
			if json.Unmarshal([]byte(harness.AllowedUsers), &allowedUsers) == nil && containsStr(allowedUsers, user.ID) {
				harnessCount++
			}
		}
		row["role_ids"] = roleIDs
		row["harness_count"] = harnessCount
		out = append(out, row)
	}
	return out
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
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
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = getOrgID(r)
	}
	// Dedup: check if email already exists in this org
	var existing models.User
	if err := s.db.Where("email = ? AND organization_id = ?", req.Email, orgID).First(&existing).Error; err == nil {
		writeError(w, http.StatusConflict, "이미 등록된 이메일입니다 · Email already exists: "+req.Email)
		return
	}
	// Enforce user seat limit (enterprise licensing, PRD §29.10).
	var org models.Organization
	if s.db.First(&org, "id = ?", orgID).Error == nil && org.MaxUserSeats > 0 {
		var userCount int64
		s.db.Model(&models.User{}).Where("organization_id = ? AND status != 'offboarded'", orgID).Count(&userCount)
		if userCount >= int64(org.MaxUserSeats) {
			writeError(w, http.StatusPaymentRequired, fmt.Sprintf("사용자 좌석 한도 초과 · User seat limit reached (%d/%d)", userCount, org.MaxUserSeats))
			return
		}
	}
	user, err := s.identity.CreateUser(orgID, req.Email, req.Name, req.NameKo, req.AuthMethod, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Title != "" || req.BusinessUnitID != "" {
		user.Title = req.Title
		user.BusinessUnitID = req.BusinessUnitID
		s.db.Save(user)
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.user.created",
		ActorType:      "admin",
		Action:         "create_user",
		ResourceType:   "user",
		ResourceID:     user.ID,
		Details:        fmt.Sprintf(`{"email":"%s","name":"%s"}`, req.Email, req.Name),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var user models.User
	if err := s.db.First(&user, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "사용자를 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, user)
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
	if req.OrganizationID == "" {
		req.OrganizationID = getOrgID(r)
	}
	if req.OrganizationID == "" {
		writeError(w, http.StatusBadRequest, "organization_id is required — enroll from an organization-scoped operator session")
		return
	}
	// One-time enrollment code flow (harnesses B3): when the harness
	// self-enrolls with a code, validate + burn it instead of trusting
	// a raw admin paste.
	if code := strings.TrimSpace(req.EnrollmentCode); code != "" {
		if err := s.consumeEnrollmentCode(req.OrganizationID, code, req.UserID, req.HarnessID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
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
	// Enforce harness seat limit (enterprise licensing, PRD §29.10).
	var hOrg models.Organization
	if s.db.First(&hOrg, "id = ?", req.OrganizationID).Error == nil && hOrg.MaxHarnessSeats > 0 {
		var harnessCount int64
		s.db.Model(&models.Harness{}).Where("organization_id = ? AND status NOT IN ('revoked')", req.OrganizationID).Count(&harnessCount)
		if harnessCount >= int64(hOrg.MaxHarnessSeats) {
			writeError(w, http.StatusPaymentRequired, fmt.Sprintf("하네스 좌석 한도 초과 · Harness seat limit reached (%d/%d)", harnessCount, hOrg.MaxHarnessSeats))
			return
		}
	}
	harness, cred, err := s.identity.EnrollHarness(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: req.OrganizationID,
		EventType:      "cp.harness.enrolled",
		ActorType:      "admin",
		Action:         "enroll_harness",
		ResourceType:   "harness",
		ResourceID:     harness.ID,
		Details:        fmt.Sprintf("harness_id: %s, user_id: %s", req.HarnessID, req.UserID),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"harness":    harness,
		"credential": cred,
	})
}

func (s *Server) handleGetHarness(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var harness models.Harness
	if err := s.db.Where("id = ? OR harness_id = ?", id, id).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "하네스를 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, harness)
}

func (s *Server) handleRevokeHarness(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var harness models.Harness
	if err := s.db.Where("id = ? OR harness_id = ?", id, id).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if r.Body != http.NoBody {
		decodeJSON(r, &req)
	}
	if err := s.identity.RevokeHarness(harness.OrganizationID, harness.HarnessID, req.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Live propagation (harnesses C3): the credential is in the CA
	// revocation list (relay rebuilds at boot) AND we push the
	// revocation to the live relay channel when configured.
	relayPropagated := true
	if err := s.pushRelayDirective("revoke_harness_certificate", harness.OrganizationID, harness.HarnessID, req.Reason, nil); err != nil {
		relayPropagated = false
		log.Printf("api: revoke %s: relay propagation skipped: %v", harness.HarnessID, err)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "revoked", "relay_propagated": relayPropagated})
}

// harnessStaleAfter is the heartbeat window after which an
// enrolled/active harness is flagged stale (harnesses B2).
const harnessStaleAfter = 10 * time.Minute

// consumeEnrollmentCode validates and burns a one-time enrollment code
// (harnesses B3). Unused + unexpired codes are consumed by the
// enrolling harness ID; anything else fails closed.
func (s *Server) consumeEnrollmentCode(orgID, code, userID, harnessID string) error {
	if userID == "" {
		return fmt.Errorf("enrollment code flow requires user_id")
	}
	if harnessID == "" {
		return fmt.Errorf("enrollment code flow requires harness_id")
	}
	var ec models.EnrollmentCode
	if err := s.db.Where("code = ? AND organization_id = ?", code, orgID).First(&ec).Error; err != nil {
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
	res := s.db.Model(&models.EnrollmentCode{}).
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
	var harness models.Harness
	if err := s.db.Where("id = ? OR harness_id = ?", id, id).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "하네스를 찾을 수 없습니다")
		return
	}
	result := map[string]interface{}{"harness": harness}

	if harness.DeviceID != "" {
		var device models.Device
		if s.db.First(&device, "id = ?", harness.DeviceID).Error == nil {
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
		s.db.Where("id IN ?", allowedUserIDs).Find(&users)
		result["allowed_users"] = users
	}

	var sessions []models.Session
	s.db.Where("harness_id = ?", harness.HarnessID).Order("created_at DESC").Find(&sessions)
	result["sessions"] = sessions

	// Attestation history: device posture + attestation audit events.
	var attEvents []models.AuditEvent
	s.db.Where("resource_type = ? AND (resource_id = ? OR resource_id = ?)", "harness", harness.ID, harness.HarnessID).
		Where("action LIKE ?", "%attestation%").
		Order("occurred_at DESC").Limit(20).Find(&attEvents)
	result["attestation_events"] = attEvents

	var auditEvents []models.AuditEvent
	s.db.Where("resource_id IN ?", []string{harness.ID, harness.HarnessID}).
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
	base := strings.TrimSuffix(os.Getenv("PCCP_RELAY_ADMIN_URL"), "/")
	if base == "" {
		return fmt.Errorf("PCCP_RELAY_ADMIN_URL not configured")
	}
	body, _ := json.Marshal(map[string]interface{}{
		"org_id":       orgID,
		"target":       harnessID,
		"command_type": commandType,
		"reason":       reason,
		"issued_by":    "control-plane",
		"payload_b64":  base64.StdEncoding.EncodeToString(func() []byte { b, _ := json.Marshal(payload); return b }()),
	})
	resp, err := http.Post(base+"/v1/admin/directives", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("relay rejected directive: %s", resp.Status)
	}
	return nil
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.Project{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
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
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": s.decorateProjects(projects), "total": total, "page": page, "size": size,
		})
		return
	}
	var projects []models.Project
	q.Find(&projects)
	writeJSON(w, http.StatusOK, s.decorateProjects(projects))
}

// decorateProjects attaches per-project aggregates (repos, sessions,
// real membership count) so list cards render true numbers without
// client-side cross-page joins (projects B1/UX4).
func (s *Server) decorateProjects(projects []models.Project) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(projects))
	for _, proj := range projects {
		row := projectViewRow(proj)
		var memberCount, repoCount, sessionCount, activeCount int64
		s.db.Model(&models.ProjectMember{}).Where("project_id = ?", proj.ID).Count(&memberCount)
		s.db.Model(&models.Repository{}).Where("project_id = ?", proj.ID).Count(&repoCount)
		s.db.Model(&models.Session{}).Where("project_id = ?", proj.ID).Count(&sessionCount)
		s.db.Model(&models.Session{}).Where("project_id = ? AND status = 'active'", proj.ID).Count(&activeCount)
		row["member_count"] = memberCount
		row["repository_count"] = repoCount
		row["session_count"] = sessionCount
		row["active_session_count"] = activeCount
		out = append(out, row)
	}
	return out
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID string   `json:"organization_id"`
		Name           string   `json:"name"`
		NameKo         string   `json:"name_ko"`
		Slug           string   `json:"slug"`
		AllowedModels  []string `json:"allowed_models"`
		Description    string   `json:"description"`
		ProjectCode    string   `json:"project_code"`
		GroupAffiliate string   `json:"group_affiliate"`
		PolicyPackID   string   `json:"policy_pack_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = getOrgID(r)
	}
	proj, err := s.identity.CreateProject(orgID, req.Name, req.NameKo, req.Slug, req.AllowedModels)
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
	writeJSON(w, http.StatusCreated, projectViewRow(*proj))
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var proj models.Project
	if err := s.db.First(&proj, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, projectViewRow(proj))
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
	var repo models.Repository
	if err := s.db.First(&repo, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "저장소를 찾을 수 없습니다")
		return
	}
	if repo.CloneURL == "" {
		writeError(w, http.StatusBadRequest, "clone_url이 없습니다 · Repository has no clone URL")
		return
	}
	head, err := s.gitscm.SyncRepository(r.Context(), &repo)
	if err != nil {
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
		if v := r.URL.Query().Get("project"); v != "" {
			q = q.Where("project_id = ?", v)
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
		q.Count(&total)
		var sessions []models.Session
		q.Offset((page - 1) * size).Limit(size).Find(&sessions)
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": sessions, "total": total, "page": page, "size": size})
		return
	}
	var sessions []models.Session
	q.Order("created_at DESC").Find(&sessions)
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
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = getOrgID(r)
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
		if err := s.db.Where("id = ? AND repository_id = ?", req.BaselineID, req.RepositoryID).First(&baseline).Error; err != nil {
			writeError(w, http.StatusBadRequest, "베이스라인을 찾을 수 없습니다 · baseline not found for repository")
			return
		}
	}
	// Archive freeze (projects B4, §33.13-style): an archived project
	// rejects new sessions until restored.
	if req.ProjectID != "" {
		var proj models.Project
		if s.db.First(&proj, "id = ?", req.ProjectID).Error == nil && proj.Status == "archived" {
			writeError(w, http.StatusForbidden, "보관된 프로젝트입니다 · Project is archived — restore it to open new sessions")
			return
		}
	}
	sess, err := s.identity.OpenSession(orgID, req.HarnessID, req.UserID, req.ProjectID,
		req.RepositoryID, req.Branch, req.BaselineID, req.Title, req.TaskPurpose, req.ModelClass)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Bind the active policy epoch so the session carries governance context (PRD §13.1).
	if epoch, eerr := s.policy.GetActiveEpoch(orgID); eerr == nil && epoch != nil {
		// Acknowledgement campaign gate (policy C2, §33.6): when the
		// active epoch requires ack, unacked users cannot open new
		// sessions.
		if epoch.RequiresAck && req.UserID != "" && !s.policy.HasAcked(orgID, epoch.EpochID, req.UserID) {
			writeError(w, http.StatusForbidden, "정책 확인 필요 · Policy epoch requires acknowledgement before new sessions")
			return
		}
		sess.PolicyEpochID = epoch.EpochID
		s.db.Save(sess)
		// A1: bind a capability lease — the session is "governed" only
		// when a live lease over the active epoch exists (refuse open
		// when none can be issued).
		var allowedModels []string
		_ = json.Unmarshal([]byte(epoch.AllowedModelsJSON), &allowedModels)
		lease, lerr := s.policy.IssueCapabilityLease(policy.IssueLeaseRequest{
			OrganizationID: orgID,
			SubjectPeerID:  req.HarnessID,
			UserID:         req.UserID,
			SessionID:      sess.SessionID,
			PolicyEpochID:  epoch.EpochID,
			AllowedModels:  allowedModels,
			Validity:       time.Duration(sess.SessionTTL) * time.Second,
		})
		if lerr != nil {
			writeError(w, http.StatusForbidden, "활성 정책에 대한 임대 발급 실패 · cannot issue capability lease for the active epoch")
			return
		}
		sess.LeaseID = lease.LeaseID
		s.db.Save(sess)
	} else {
		// Fail closed: no active epoch = no governed session.
		writeError(w, http.StatusForbidden, "활성 정책 epoch 없음 · no active policy epoch — session refused")
		return
	}
	s.ext().Realtime.NotifySessionUpdate(orgID, sess.SessionID, "active")
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.session.opened",
		ActorType:      "admin",
		Action:         "open_session",
		ResourceType:   "session",
		ResourceID:     sess.ID,
		Details:        fmt.Sprintf(`{"title":"%s","harness_id":"%s"}`, req.Title, req.HarnessID),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "세션을 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.identity.CloseSession(sess.SessionID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Auto-teardown (web/15 feature 10): sandboxes bound to the session
	// are destroyed with it (best-effort — statuses recorded honestly).
	var bound []models.SandboxRecord
	s.db.Where("organization_id = ? AND session_id = ?", orgID, sess.SessionID).Find(&bound)
	for _, sb := range bound {
		if sb.Status != "destroyed" {
			_, _ = s.sandbox.DestroySandbox(sb.ID)
		}
	}
	s.ext().Realtime.NotifySessionUpdate(orgID, sess.SessionID, "closed")
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.session.closed",
		ActorType:      "admin",
		Action:         "close_session",
		ResourceType:   "session",
		ResourceID:     sess.ID,
		Details:        "session closed",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
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
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
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
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	// Load actions (tool calls, commands, model requests)
	var actions []models.ActionEnvelope
	s.db.Where("session_id = ?", sess.SessionID).Order("occurred_at DESC").Limit(100).Find(&actions)

	// Load change sets (code changes)
	var changeSets []models.ChangeSet
	s.db.Where("session_id = ?", sess.SessionID).Order("created_at DESC").Find(&changeSets)

	// Load security findings
	var findings []models.SecurityFinding
	s.db.Where("session_id = ?", sess.SessionID).Order("occurred_at DESC").Find(&findings)

	// Load approvals
	var approvals []models.Approval
	s.db.Where("session_id = ?", sess.SessionID).Order("created_at DESC").Find(&approvals)

	// Load usage records
	var usageRecords []models.UsageRecord
	s.db.Where("session_id = ?", sess.SessionID).Order("occurred_at DESC").Limit(50).Find(&usageRecords)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session":       sess,
		"actions":       actions,
		"change_sets":   changeSets,
		"findings":      findings,
		"approvals":     approvals,
		"usage_records": usageRecords,
	})
}

func (s *Server) handleGetSessionUsage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.Where("organization_id = ? AND (id = ? OR session_id = ?)", orgID, id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	days, since, until := usageWindowFromRequest(r, time.Now())
	report, err := s.buildUsageReport(orgID, usageFilter{SessionID: sess.SessionID}, fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleGetProvenance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
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
	var req policy.IssueLeaseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Validity == 0 {
		req.Validity = 1 * time.Hour
	}
	lease, err := s.policy.IssueCapabilityLease(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
	if base == "" {
		return 0
	}
	payload, _ := json.Marshal(map[string]string{
		"org_id":   orgID,
		"severity": severity,
		"body":     body,
	})
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(base+"/v1/broadcasts", "application/json", bytes.NewReader(payload))
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
	days := 30
	summary, err := s.workintel.GetUsageSummary(orgID, days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
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
	data, err := s.workintel.ExportMetricsJSON(orgID, 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// modelCostRow is one model's server-computed cost line. PAT-1501:
// the row is a by-unit breakdown — no cross-unit aggregation.
type modelCostRow struct {
	ModelPackageID     string  `json:"model_package_id"`
	ModelName          string  `json:"model_name,omitempty"`
	TokensIn           int64   `json:"tokens_in"`
	TokensOut          int64   `json:"tokens_out"`
	TokensUnit         string  `json:"tokens_unit"` // always "tokens"
	CostKRW            float64 `json:"cost_krw"`
	CostUnit           string  `json:"cost_unit"` // always "krw"
	RecordedCostMicros int64   `json:"recorded_cost_micros"`
	RecordedCurrency   string  `json:"recorded_currency,omitempty"`
	DifferenceMicros   int64   `json:"difference_micros"`
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
	days, since, until := usageWindowFromRequest(r, time.Now())
	usageReport, err := s.buildUsageReport(orgID, usageFilter{}, fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var sums []struct {
		ModelPackageID string
		MetricType     string
		Total          int64
	}
	if err := s.db.Model(&models.UsageRecord{}).
		Select("model_package_id, metric_type, SUM(quantity) as total").
		Where("organization_id = ? AND occurred_at >= ? AND metric_type IN ('tokens_in','tokens_out') AND model_package_id != ''", orgID, since).
		Group("model_package_id, metric_type").Scan(&sums).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var pkgs []models.ModelPackage
	s.db.Find(&pkgs)
	pkgBy := make(map[string]models.ModelPackage, len(pkgs))
	for _, p := range pkgs {
		pkgBy[p.PackageID] = p
	}

	byPkg := make(map[string]*modelCostRow)
	order := []string{}
	for _, sm := range sums {
		mc, ok := byPkg[sm.ModelPackageID]
		if !ok {
			mc = &modelCostRow{
				ModelPackageID: sm.ModelPackageID,
				TokensUnit:     UnitTokens,
				CostUnit:       UnitKRW,
			}
			byPkg[sm.ModelPackageID] = mc
			order = append(order, sm.ModelPackageID)
		}
		if sm.MetricType == "tokens_in" {
			mc.TokensIn = sm.Total
		} else {
			mc.TokensOut = sm.Total
		}
	}

	rowsOut := make([]modelCostRow, 0, len(order))
	totalKRW := 0.0
	anyPriced := false
	allReconciled := true
	type recordedModelCost struct {
		amount   int64
		currency string
		present  bool
		mixed    bool
	}
	recordedByModel := map[string]recordedModelCost{}
	for _, row := range usageReport.Drilldown {
		if row.ModelPackageID == "" {
			continue
		}
		recorded := recordedByModel[row.ModelPackageID]
		recorded.present = true
		if recorded.currency != "" && row.Currency != "" && recorded.currency != row.Currency {
			recorded.mixed = true
		}
		if recorded.currency == "" {
			recorded.currency = row.Currency
		}
		recorded.amount += row.AmountMicros
		recordedByModel[row.ModelPackageID] = recorded
	}
	for _, pid := range order {
		mc := byPkg[pid]
		if p, ok := pkgBy[pid]; ok {
			mc.ModelName = p.Name
			if p.PriceInputPer1K > 0 || p.PriceOutputPer1K > 0 {
				mc.Priced = true
				mc.CostKRW = float64(mc.TokensIn)/1000*p.PriceInputPer1K + float64(mc.TokensOut)/1000*p.PriceOutputPer1K
			}
		}
		recorded := recordedByModel[pid]
		mc.RecordedCostMicros = recorded.amount
		mc.RecordedCurrency = recorded.currency
		expectedMicros := int64(math.Round(mc.CostKRW * 1_000_000))
		mc.DifferenceMicros = recorded.amount - expectedMicros
		mc.Reconciled = mc.Priced && recorded.present && !recorded.mixed && strings.EqualFold(recorded.currency, "KRW") && mc.DifferenceMicros == 0
		if !mc.Reconciled {
			allReconciled = false
		}
		if mc.Priced {
			anyPriced = true
			totalKRW += mc.CostKRW
		}
		rowsOut = append(rowsOut, *mc)
	}

	// PAT-1501: total in KRW is its own typed row, not a number
	// sitting next to tokens.
	totalUsage := Usage{Quantity: int64(math.Round(totalKRW)), Unit: UnitKRW, Currency: UnitKRW, WindowStart: since, WindowEnd: until, Reconciled: anyPriced && allReconciled}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"days":             days,
		"window_start":     since,
		"window_end":       until,
		"models":           rowsOut,
		"total":            totalUsage,
		"any_priced":       anyPriced,
		"display_currency": UnitKRW,
		"usage_report":     usageReport,
	})
}

// handleGetUsageBreakdown returns the org's metered consumption
// bucketed by unit. Cross-unit aggregation is impossible at the
// response type level — the UI receives one Usage row per unit.
// PAT-1501.
func (s *Server) handleGetUsageBreakdown(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	days, since, until := usageWindowFromRequest(r, time.Now())
	report, err := s.buildUsageReport(orgID, usageFilter{}, fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && d > 0 {
			days = d
		}
	}
	since = now.AddDate(0, 0, -days).Format(time.RFC3339)
	until = now.Format(time.RFC3339)
	return days, since, until
}

// unitForMetric maps a UsageRecord metric_type to its unit. Unknown
// metrics return "" so the caller surfaces them rather than silently
// including them in a cross-unit sum. PAT-1501.
func unitForMetric(metric string) string {
	switch metric {
	case "tokens_in", "tokens_out", "cache_write", "cache_read":
		return UnitTokens
	case "gpu_seconds":
		return UnitSeconds
	case "storage_bytes":
		return UnitBytes
	case "tool_call", "reservation":
		return UnitCount
	case "usd_micro", "usd":
		return UnitUSDMicro
	}
	return ""
}

// --- Fleet Handlers ---

func (s *Server) handleFleetInventory(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	inventory, err := s.fleet.GetFleetInventory(orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, inventory)
}

func (s *Server) handleInspectSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	inspector, err := s.fleet.InspectSession(orgID, sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, inspector)
}

func (s *Server) handleFleetAction(w http.ResponseWriter, r *http.Request) {
	var req fleet.ActionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.fleet.PerformAction(req); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: req.OrganizationID,
		EventType:      "cp.fleet.action",
		ActorType:      "admin",
		Action:         string(req.Action),
		ResourceType:   "harness",
		ResourceID:     req.HarnessID,
		Details:        req.Reason,
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "executed"})
}

// --- Git/SCM Handlers ---

func (s *Server) handleRepositoryHeatmap(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	heatmap, err := s.gitscm.GetRepositoryHeatmap(orgID)
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
	sb, err := s.sandbox.DestroySandbox(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
	writeJSON(w, http.StatusOK, sb)
}

func (s *Server) handleForensicSnapshot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	snapshotID, err := s.sandbox.ForensicSnapshot(id)
	if err != nil {
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
	var user models.User
	if err := s.db.First(&user, "id = ? AND organization_id = ?", id, getOrgID(r)).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
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
		cols["email"] = *updates.Email
	}
	if updates.Title != nil {
		cols["title"] = *updates.Title
	}
	if updates.AuthMethod != nil {
		cols["auth_method"] = *updates.AuthMethod
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
	if len(cols) > 0 {
		s.db.Model(&user).Updates(cols)
	}
	updateDetails, _ := json.Marshal(map[string]string{"reason": reason})
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r),
		EventType:      "cp.user.updated",
		ActorType:      "admin",
		Action:         "update_user",
		ResourceType:   "user",
		ResourceID:     id,
		Details:        string(updateDetails),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, user)
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
	code, err := s.identity.GenerateEnrollmentCode(orgID, userID, 24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.user.enrollment_code",
		ActorType:      "admin",
		Action:         "issue_enrollment_code",
		ResourceType:   "user",
		ResourceID:     userID,
		Details:        "enrollment code issued (24h validity)",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
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
	var proj models.Project
	if err := s.db.First(&proj, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var updates struct {
		Name           *string  `json:"name,omitempty"`
		NameKo         *string  `json:"name_ko,omitempty"`
		Description    *string  `json:"description,omitempty"`
		Status         *string  `json:"status,omitempty"`
		AllowedModels  []string `json:"allowed_models,omitempty"`
		ProjectCode    *string  `json:"project_code,omitempty"`
		GroupAffiliate *string  `json:"group_affiliate,omitempty"`
		PolicyPackID   *string  `json:"policy_pack_id,omitempty"`
	}
	decodeJSON(r, &updates)
	if updates.Name != nil {
		proj.Name = *updates.Name
	}
	if updates.NameKo != nil {
		proj.NameKo = *updates.NameKo
	}
	if updates.Description != nil {
		proj.Description = *updates.Description
	}
	if updates.Status != nil {
		proj.Status = *updates.Status
	}
	// Explicit empty array clears the allowance (제한 없음); absent key
	// leaves it unchanged (PAT-1491).
	if updates.AllowedModels != nil {
		b, _ := json.Marshal(updates.AllowedModels)
		proj.AllowedModelClasses = string(b)
	}
	if updates.ProjectCode != nil {
		proj.ProjectCode = *updates.ProjectCode
	}
	if updates.GroupAffiliate != nil {
		proj.GroupAffiliate = *updates.GroupAffiliate
	}
	if updates.PolicyPackID != nil {
		proj.PolicyPackID = *updates.PolicyPackID
	}
	s.db.Save(&proj)
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
	writeJSON(w, http.StatusOK, projectViewRow(proj))
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	s.db.Model(&models.Project{}).Where("id = ?", id).Update("status", "archived")
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.project.archived",
		ActorType:      "admin",
		Action:         "archive_project",
		ResourceType:   "project",
		ResourceID:     id,
		Details:        "project archived",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	impact := s.projectArchiveImpact(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "archived", "impact": impact})
}

// projectArchiveImpact counts what an archive affects (projects UX14):
// active sessions that will be frozen, attached repositories, members.
func (s *Server) projectArchiveImpact(projectID string) map[string]interface{} {
	var activeSessions, repos, members int64
	s.db.Model(&models.Session{}).Where("project_id = ? AND status = 'active'", projectID).Count(&activeSessions)
	s.db.Model(&models.Repository{}).Where("project_id = ? AND status = 'active'", projectID).Count(&repos)
	s.db.Model(&models.ProjectMember{}).Where("project_id = ?", projectID).Count(&members)
	return map[string]interface{}{
		"active_sessions": activeSessions,
		"repositories":    repos,
		"members":         members,
	}
}

// handleProjectArchiveImpact previews the archive blast radius before
// the operator confirms (projects UX14).
func (s *Server) handleProjectArchiveImpact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	writeJSON(w, http.StatusOK, s.projectArchiveImpact(id))
}

// handleRestoreProject un-archives a project (projects B4).
func (s *Server) handleRestoreProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	s.db.Model(&models.Project{}).Where("id = ?", id).Update("status", "active")
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.project.restored",
		ActorType:      "admin",
		Action:         "restore_project",
		ResourceType:   "project",
		ResourceID:     id,
		Details:        "project restored",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// handleProjectUsage rolls up the project cost center (projects B6,
// §29.12): sessions, token usage, and recorded cost across all of the
// project's sessions.
func (s *Server) handleProjectUsage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var proj models.Project
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&proj).Error; err != nil {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	var sessionIDs []string
	if err := s.db.Model(&models.Session{}).Where("organization_id = ? AND project_id = ?", orgID, id).Pluck("session_id", &sessionIDs).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	days, since, until := usageWindowFromRequest(r, time.Now())
	report, err := s.buildUsageReport(orgID, usageFilter{SessionIDs: sessionIDs}, fmt.Sprintf("%dd", days), since, until)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	report.SessionCount = len(sessionIDs)
	writeJSON(w, http.StatusOK, report)
}

// handleListProjectMembers returns the real roster with user info
// (projects B1) — no more session-derived guessing.
func (s *Server) handleListProjectMembers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var members []models.ProjectMember
	s.db.Where("project_id = ?", id).Order("created_at DESC").Find(&members)
	type memberRow struct {
		models.ProjectMember
		User *models.User `json:"user,omitempty"`
	}
	out := make([]memberRow, 0, len(members))
	for _, m := range members {
		row := memberRow{ProjectMember: m}
		var user models.User
		if s.db.First(&user, "id = ?", m.UserID).Error == nil {
			row.User = &user
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAddProjectMember assigns a role to a user on the project
// (projects B1).
func (s *Server) handleAddProjectMember(w http.ResponseWriter, r *http.Request) {
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
	var proj models.Project
	if s.db.First(&proj, "id = ?", id).Error != nil {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	member := models.ProjectMember{
		OrganizationID: orgID,
		ProjectID:      id,
		UserID:         req.UserID,
		Role:           req.Role,
		GrantedBy:      "console",
	}
	if err := s.db.Where("project_id = ? AND user_id = ?", id, req.UserID).
		Assign(models.ProjectMember{Role: req.Role}).
		FirstOrCreate(&member).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.project.member_added",
		ActorType:      "admin",
		Action:         "add_project_member",
		ResourceType:   "project",
		ResourceID:     id,
		Details:        fmt.Sprintf(`{"user_id":"%s","role":"%s"}`, req.UserID, req.Role),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, member)
}

// handleRemoveProjectMember removes a roster entry (projects B1).
func (s *Server) handleRemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := chi.URLParam(r, "userId")
	orgID := getOrgID(r)
	s.db.Where("project_id = ? AND user_id = ?", id, userID).Delete(&models.ProjectMember{})
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.project.member_removed",
		ActorType:      "admin",
		Action:         "remove_project_member",
		ResourceType:   "project",
		ResourceID:     id,
		Details:        fmt.Sprintf(`{"user_id":"%s"}`, userID),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// handleBindProjectPolicyPack binds a versioned policy pack to the
// project (projects B2) — surfaced as the project's effective policy.
func (s *Server) handleBindProjectPolicyPack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		PolicyPackID string `json:"policy_pack_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.PolicyPackID != "" {
		var pack models.PolicyPack
		if s.db.First(&pack, "id = ?", req.PolicyPackID).Error != nil {
			writeError(w, http.StatusNotFound, "policy pack not found")
			return
		}
	}
	s.db.Model(&models.Project{}).Where("id = ?", id).Update("policy_pack_id", req.PolicyPackID)
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.project.policy_pack_bound",
		ActorType:      "admin",
		Action:         "bind_project_policy_pack",
		ResourceType:   "project",
		ResourceID:     id,
		Details:        fmt.Sprintf(`{"policy_pack_id":"%s"}`, req.PolicyPackID),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "bound", "policy_pack_id": req.PolicyPackID})
}

// handleGetProjectDetail assembles the project detail page (projects
// B3): repos, real membership roster, sessions, policy binding, usage,
// change-control queue, and audit.
func (s *Server) handleGetProjectDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var proj models.Project
	if err := s.db.First(&proj, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	result := map[string]interface{}{"project": projectViewRow(proj)}

	var repos []models.Repository
	s.db.Where("project_id = ?", id).Find(&repos)
	result["repositories"] = repos

	var members []models.ProjectMember
	s.db.Where("project_id = ?", id).Find(&members)
	type memberRow struct {
		models.ProjectMember
		User *models.User `json:"user,omitempty"`
	}
	rows := make([]memberRow, 0, len(members))
	for _, m := range members {
		row := memberRow{ProjectMember: m}
		var user models.User
		if s.db.First(&user, "id = ?", m.UserID).Error == nil {
			row.User = &user
		}
		rows = append(rows, row)
	}
	result["members"] = rows

	var sessions []models.Session
	s.db.Where("project_id = ?", id).Order("created_at DESC").Limit(50).Find(&sessions)
	result["sessions"] = sessions

	if proj.PolicyPackID != "" {
		var pack models.PolicyPack
		if s.db.First(&pack, "id = ?", proj.PolicyPackID).Error == nil {
			result["policy_pack"] = pack
		}
	}

	var changes []models.ChangeRequest
	s.db.Where("project_id = ?", id).Order("created_at DESC").Find(&changes)
	result["change_requests"] = changes

	var auditEvents []models.AuditEvent
	s.db.Where("resource_id = ?", id).Order("occurred_at DESC").Limit(50).Find(&auditEvents)
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

func (s *Server) handlePauseSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.db.Model(&sess).Update("status", "paused")
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.session.paused",
		ActorType:      "admin",
		Action:         "pause_session",
		ResourceType:   "session",
		ResourceID:     sess.ID,
		Details:        "session paused",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (s *Server) handleResumeSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.db.Model(&sess).Update("status", "active")
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.session.resumed",
		ActorType:      "admin",
		Action:         "resume_session",
		ResourceType:   "session",
		ResourceID:     sess.ID,
		Details:        "session resumed",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

func (s *Server) handleUpdateModel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var pkg models.ModelPackage
	if err := s.db.Where("id = ? OR package_id = ?", id, id).First(&pkg).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var updates struct {
		Name             *string  `json:"name,omitempty"`
		NameKo           *string  `json:"name_ko,omitempty"`
		Description      *string  `json:"description,omitempty"`
		PriceInputPer1K  *float64 `json:"price_input_per_1k,omitempty"`
		PriceOutputPer1K *float64 `json:"price_output_per_1k,omitempty"`
	}
	decodeJSON(r, &updates)
	if updates.Name != nil {
		pkg.Name = *updates.Name
	}
	if updates.NameKo != nil {
		pkg.NameKo = *updates.NameKo
	}
	if updates.PriceInputPer1K != nil {
		pkg.PriceInputPer1K = *updates.PriceInputPer1K
	}
	if updates.PriceOutputPer1K != nil {
		pkg.PriceOutputPer1K = *updates.PriceOutputPer1K
	}
	s.db.Save(&pkg)
	writeJSON(w, http.StatusOK, pkg)
}

func (s *Server) handleQuarantineHarness(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var harness models.Harness
	if err := s.db.Where("id = ? OR harness_id = ?", id, id).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.db.Model(&harness).Updates(map[string]interface{}{
		"status":     "quarantined",
		"risk_state": "high",
	})
	// Terminate active sessions
	s.db.Model(&models.Session{}).Where("harness_id = ? AND status = 'active'", harness.HarnessID).
		Update("status", "terminated")
	s.db.Create(&models.AuditEvent{
		OrganizationID: harness.OrganizationID,
		EventType:      "cp.harness.quarantined",
		ActorType:      "admin",
		Action:         "quarantine_harness",
		ResourceType:   "harness",
		ResourceID:     harness.ID,
		Details:        "harness quarantined; active sessions terminated",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
	// Live propagation (harnesses C3): the connect-time gate reads DB
	// status on the next connection; the directive additionally tells
	// the relay to kill the current transport when configured.
	relayPropagated := true
	if err := s.pushRelayDirective("quarantine_device", harness.OrganizationID, harness.HarnessID, "quarantined via control plane", map[string]interface{}{"terminate_sessions": true}); err != nil {
		relayPropagated = false
		log.Printf("api: quarantine %s: relay propagation skipped: %v", harness.HarnessID, err)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "quarantined", "relay_propagated": relayPropagated})
}

func (s *Server) handleReactivateHarness(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var harness models.Harness
	if err := s.db.Where("id = ? OR harness_id = ?", id, id).First(&harness).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.db.Model(&harness).Updates(map[string]interface{}{
		"status":     "enrolled",
		"risk_state": "normal",
	})
	s.db.Create(&models.AuditEvent{
		OrganizationID: harness.OrganizationID,
		EventType:      "cp.harness.reactivated",
		ActorType:      "admin",
		Action:         "reactivate_harness",
		ResourceType:   "harness",
		ResourceID:     harness.ID,
		Details:        "harness reactivated",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	})
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

func (s *Server) handleSecurityFindings(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	q := s.db.Model(&models.SecurityFinding{}).Where("organization_id = ?", orgID)
	// Server-side filters (security UX5/UX12): severity, status, type,
	// date range.
	for _, key := range []string{"severity", "status", "finding_type"} {
		if v := r.URL.Query().Get(key); v != "" {
			q = q.Where(key+" = ?", v)
		}
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
	orgID := getOrgID(r)
	var req struct {
		Scope     string `json:"scope"` // org | project
		ProjectID string `json:"project_id"`
		Reason    string `json:"reason"`
	}
	if r.Body != http.NoBody {
		decodeJSON(r, &req)
	}
	if req.Scope == "" {
		req.Scope = "org"
	}
	if req.Scope == "project" && req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "project scope requires project_id")
		return
	}

	// Terminate active sessions (org-wide or project-scoped) and raise
	// harness risk state.
	sessionQ := s.db.Model(&models.Session{}).Where("organization_id = ? AND status = 'active'", orgID)
	harnessQ := s.db.Model(&models.Harness{}).Where("organization_id = ?", orgID)
	if req.Scope == "project" {
		sessionQ = sessionQ.Where("project_id = ?", req.ProjectID)
	}
	var affectedHarnesses []models.Harness
	if req.Scope == "project" {
		// Harnesses with active sessions in the project.
		s.db.Model(&models.Harness{}).
			Where("organization_id = ? AND harness_id IN (?)", orgID,
				s.db.Model(&models.Session{}).Select("harness_id").
					Where("organization_id = ? AND status = 'active' AND project_id = ?", orgID, req.ProjectID)).
			Find(&affectedHarnesses)
	} else {
		s.db.Where("organization_id = ?", orgID).Find(&affectedHarnesses)
	}
	sessionQ.Update("status", "terminated")
	harnessQ.Update("risk_state", "high")

	// Live propagation (security B1): DB termination is enforced by the
	// relay's per-request session-status gate; the directive additionally
	// notifies the relay channel when configured.
	relayPropagated := true
	for _, h := range affectedHarnesses {
		if err := s.pushRelayDirective("emergency_lockdown", orgID, h.HarnessID, req.Reason, map[string]interface{}{"scope": req.Scope}); err != nil {
			relayPropagated = false
		}
	}

	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.security.emergency_lockdown",
		ActorType:      "admin",
		Action:         "emergency_lockdown",
		Details:        fmt.Sprintf(`{"scope":"%s","project_id":"%s","reason":"%s"}`, req.Scope, req.ProjectID, req.Reason),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(audit)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "lockdown_activated", "scope": req.Scope,
		"affected_harnesses": len(affectedHarnesses), "relay_propagated": relayPropagated,
	})
}

// handleSecurityLockdownImpact previews the lockdown blast radius
// (security UX9).
func (s *Server) handleSecurityLockdownImpact(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	scope := r.URL.Query().Get("scope")
	projectID := r.URL.Query().Get("project_id")
	q := s.db.Model(&models.Session{}).Where("organization_id = ? AND status = 'active'", orgID)
	hq := s.db.Model(&models.Harness{}).Where("organization_id = ?", orgID)
	if scope == "project" && projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	var activeSessions, activeHarnesses int64
	q.Count(&activeSessions)
	hq.Where("status IN ?", []string{"enrolled", "active"}).Count(&activeHarnesses)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"scope": scope, "active_sessions": activeSessions, "active_harnesses": activeHarnesses,
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
	orgID := getOrgID(r)
	var endpoints []models.AlertEndpoint
	s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Find(&endpoints)
	writeJSON(w, http.StatusOK, redactAlertEndpoints(endpoints))
}

func (s *Server) handleCreateAlertEndpoint(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	role := getRole(r)
	if role != "admin" && role != "owner" && role != "super_admin" {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	if s.keyProvider == nil {
		// PAT-1502 PR 2: write paths fail closed when no KeyProvider
		// is configured. An unconfigured server cannot accept secret
		// material.
		writeError(w, http.StatusServiceUnavailable, "alert endpoint storage is not configured")
		return
	}
	var req AlertEndpointCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	target := strings.TrimSpace(req.Target)
	if req.Name == "" || target == "" {
		writeError(w, http.StatusBadRequest, "name and target are required")
		return
	}
	if !isAcceptableAlertTarget(req.Type, target) {
		writeError(w, http.StatusBadRequest, "target must be an http(s) URL on a public host")
		return
	}
	if req.Type == "" {
		req.Type = "webhook"
	}
	severitiesJSON, _ := json.Marshal(req.Severities)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	enc, kekID, err := PersistTarget(s.keyProvider, target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not seal target")
		return
	}
	ep := &models.AlertEndpoint{
		OrganizationID: orgID, Name: req.Name, Type: req.Type,
		Target: "", TargetEnc: enc, TargetKEKID: kekID,
		SeveritiesJSON: string(severitiesJSON), Enabled: enabled,
	}
	if err := s.db.Create(ep).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	credID := credentialIDForTarget(target)
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		ActorID:        getActorID(r),
		ActorType:      "user",
		EventType:      "security.alert_endpoint.create",
		Action:         "create",
		ResourceType:   "alert_endpoint",
		ResourceID:     ep.ID,
		Result:         "success",
		Details:        fmt.Sprintf(`{"credential_id":%q,"type":%q,"name":%q}`, credID, ep.Type, ep.Name),
	})
	writeJSON(w, http.StatusCreated, redactAlertEndpoint(*ep))
}

func (s *Server) handleDeleteAlertEndpoint(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	role := getRole(r)
	if role != "admin" && role != "owner" && role != "super_admin" {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	id := chi.URLParam(r, "id")
	var ep models.AlertEndpoint
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&ep).Error; err != nil {
		writeError(w, http.StatusNotFound, "alert endpoint not found")
		return
	}
	// We resolve the target (encrypted preferred, legacy fallback)
	// solely to compute a stable credential_id for the audit row.
	// The plaintext URL never leaves this handler.
	target, _ := ResolveTarget(s.keyProvider, ep.TargetEnc, ep.TargetKEKID, ep.Target)
	credID := credentialIDForTarget(target)
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).Delete(&models.AlertEndpoint{}).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		ActorID:        getActorID(r),
		ActorType:      "user",
		EventType:      "security.alert_endpoint.delete",
		Action:         "delete",
		ResourceType:   "alert_endpoint",
		ResourceID:     id,
		Result:         "success",
		Details:        fmt.Sprintf(`{"credential_id":%q,"type":%q,"name":%q}`, credID, ep.Type, ep.Name),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Alert endpoint rotation — replaces the stored credential with a
// new one. The previous credential is destroyed at the provider
// level (the DEK is overwritten; the old DEK ciphertext remains on
// disk but cannot be decrypted). Provider-side revocation (e.g.
// disabling a Slack webhook) is a separate operation against the
// upstream service — this endpoint only updates PCCP's record.
// PAT-1502 PR 2.
func (s *Server) handleRotateAlertEndpoint(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	role := getRole(r)
	if role != "admin" && role != "owner" && role != "super_admin" {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	if s.keyProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "alert endpoint storage is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Target string `json:"target"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}
	var ep models.AlertEndpoint
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&ep).Error; err != nil {
		writeError(w, http.StatusNotFound, "alert endpoint not found")
		return
	}
	if !isAcceptableAlertTarget(ep.Type, target) {
		writeError(w, http.StatusBadRequest, "target must be an http(s) URL on a public host")
		return
	}
	enc, kekID, err := PersistTarget(s.keyProvider, target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not seal new target")
		return
	}
	oldCredID := credentialIDForTarget(mustResolveTarget(s.keyProvider, ep.TargetEnc, ep.TargetKEKID, ep.Target))
	if err := s.db.Model(&models.AlertEndpoint{}).
		Where("id = ? AND organization_id = ?", id, orgID).
		Updates(map[string]interface{}{
			"target":        "",
			"target_enc":    enc,
			"target_kek_id": kekID,
		}).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	newCredID := credentialIDForTarget(target)
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		ActorID:        getActorID(r),
		ActorType:      "user",
		EventType:      "security.alert_endpoint.rotate",
		Action:         "rotate",
		ResourceType:   "alert_endpoint",
		ResourceID:     id,
		Result:         "success",
		Details:        fmt.Sprintf(`{"old_credential_id":%q,"new_credential_id":%q,"type":%q}`, oldCredID, newCredID, ep.Type),
	})
	updated := ep
	updated.Target = ""
	updated.TargetEnc = enc
	updated.TargetKEKID = kekID
	writeJSON(w, http.StatusOK, redactAlertEndpoint(updated))
}

// Alert endpoint test — sends a synthetic Slack-style "ping" to the
// resolved target. The endpoint's URL is decrypted on the server
// side and used to dispatch one HTTP POST. The provider response
// body is never returned; only the status class (2xx/non-2xx) and
// a short reason. PAT-1502 PR 2.
func (s *Server) handleTestAlertEndpoint(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	role := getRole(r)
	if role != "admin" && role != "owner" && role != "super_admin" {
		writeError(w, http.StatusForbidden, "organization administrator role required")
		return
	}
	if s.keyProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "alert endpoint storage is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	var ep models.AlertEndpoint
	if err := s.db.Where("id = ? AND organization_id = ?", id, orgID).First(&ep).Error; err != nil {
		writeError(w, http.StatusNotFound, "alert endpoint not found")
		return
	}
	target, err := ResolveTarget(s.keyProvider, ep.TargetEnc, ep.TargetKEKID, ep.Target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not resolve target")
		return
	}
	if target == "" {
		writeError(w, http.StatusBadRequest, "endpoint has no secret configured")
		return
	}
	// Per-endpoint rate limit (PAT-1502 PR 2): one test per minute.
	now := time.Now()
	if s.testAlert != nil && s.testAlert.now != nil {
		now = s.testAlert.now()
	}
	if !s.testAlertRateLimit(id, now) {
		writeError(w, http.StatusTooManyRequests, "rate limited; try again later")
		return
	}
	// SSRF guard: reject private/loopback/link-local hosts.
	if err := assertPublicHost(target); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload := []byte(`{"text":"[pccp] alert endpoint test"}`)
	ctx, cancel := ctxWithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build test request")
		return
	}
	req2.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req2)
	if err != nil {
		s.db.Create(&models.AuditEvent{
			OrganizationID: orgID,
			ActorID:        getActorID(r),
			ActorType:      "user",
			EventType:      "security.alert_endpoint.test",
			Action:         "test",
			ResourceType:   "alert_endpoint",
			ResourceID:     id,
			Result:         "failure",
			Details:        fmt.Sprintf(`{"credential_id":%q,"reason":%q}`, credentialIDForTarget(target), err.Error()),
		})
		writeError(w, http.StatusBadGateway, "test delivery failed")
		return
	}
	defer resp.Body.Close()
	ok := resp.StatusCode < 300
	statusClass := "non_2xx"
	if ok {
		statusClass = "2xx"
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: orgID,
		ActorID:        getActorID(r),
		ActorType:      "user",
		EventType:      "security.alert_endpoint.test",
		Action:         "test",
		ResourceType:   "alert_endpoint",
		ResourceID:     id,
		Result:         "success",
		Details:        fmt.Sprintf(`{"credential_id":%q,"status_class":%q,"http_status":%d}`, credentialIDForTarget(target), statusClass, resp.StatusCode),
	})
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
	var openFindings int64
	s.db.Model(&models.SecurityFinding{}).
		Where("organization_id = ? AND severity IN ('critical','high') AND status != 'resolved'", orgID).
		Count(&openFindings)
	dash["open_critical_findings"] = openFindings
	var openRemediations int64
	s.db.Model(&models.ComplianceRemediation{}).
		Where("organization_id = ? AND status != 'done'", orgID).Count(&openRemediations)
	dash["open_remediations"] = openRemediations

	// Recents (A7): recently updated entities for the object hub.
	var recentUsers []models.User
	s.db.Model(&models.User{}).Where("organization_id = ?", orgID).
		Order("updated_at DESC").Limit(5).Find(&recentUsers)
	dash["recent_users"] = recentUsers
	var recentProjects []models.Project
	s.db.Model(&models.Project{}).Where("organization_id = ?", orgID).
		Order("updated_at DESC").Limit(5).Find(&recentProjects)
	dash["recent_projects"] = recentProjects
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
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := getOrgID(r)
	ft, err := s.comms.CreateFileTransfer(orgID, req.SenderID, req.RecipientID, req.FileName, req.FileSize, req.FileType, req.Classification)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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

func (s *Server) handleUpdateEnterpriseFeature(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Enabled  bool   `json:"enabled"`
		Enforced bool   `json:"enforced"`
		Config   string `json:"config"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.db.Model(&models.EnterpriseHarnessFeature{}).Where("id = ?", id).
		Updates(map[string]interface{}{"enabled": req.Enabled, "enforced": req.Enforced, "config": req.Config})
	var feature models.EnterpriseHarnessFeature
	s.db.Where("id = ?", id).First(&feature)
	writeJSON(w, http.StatusOK, feature)
}

func (s *Server) handleListEnterpriseViolations(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var violations []models.EnterpriseFeatureViolation
	s.db.Where("organization_id = ? AND resolved = false", orgID).Order("occurred_at DESC").Limit(100).Find(&violations)
	writeJSON(w, http.StatusOK, violations)
}

func (s *Server) handleResolveViolation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.db.Model(&models.EnterpriseFeatureViolation{}).Where("id = ?", id).Update("resolved", true)
	writeJSON(w, http.StatusOK, map[string]string{"status": "resolved"})
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
	q := s.db.Where("action_type = ?", "changeboard.submit").Order("occurred_at DESC").Limit(100)
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Find(&envs)
	out := make([]map[string]any, 0, len(envs))
	for _, e := range envs {
		var payload map[string]any
		_ = json.Unmarshal([]byte(e.ActionPayload), &payload)
		out = append(out, map[string]any{
			"envelope_id": e.ID, "harness_id": e.HarnessID, "session_id": e.SessionID,
			"occurred_at": e.OccurredAt, "payload": payload,
		})
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
	if err := s.db.Where("id = ?", id).First(&env).Error; err != nil {
		writeError(w, http.StatusNotFound, "submission not found")
		return
	}
	base := strings.TrimSuffix(config.RelayAdminURL(), "/")
	if base == "" {
		writeError(w, http.StatusPreconditionFailed, "live review requires the relay admin channel (config relay_admin_url)")
		return
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(env.ActionPayload), &payload)
	subID, _ := payload["submission_id"].(string)
	directivePayload, _ := json.Marshal(map[string]string{"submission_id": subID})
	body, _ := json.Marshal(map[string]any{
		"org_id":       env.OrganizationID,
		"target":       env.HarnessID,
		"command_type": decision,
		"reason":       "reviewed via console",
		"issued_by":    "console:" + getOrgID(r),
		"payload_b64":  base64.StdEncoding.EncodeToString(directivePayload),
	})
	resp, err := http.Post(base+"/v1/admin/directives", "application/json", bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusBadGateway, "relay directive delivery failed: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		writeError(w, http.StatusBadGateway, "relay rejected directive: "+resp.Status)
		return
	}
	s.db.Create(&models.AuditEvent{
		OrganizationID: getOrgID(r), EventType: "cp.governance.submission_reviewed",
		ActorType: "admin", Action: decision, ResourceType: "change_submission",
		ResourceID: subID, Details: "harness=" + env.HarnessID,
		Result: "delivered", OccurredAt: time.Now().Format(time.RFC3339),
	})
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
