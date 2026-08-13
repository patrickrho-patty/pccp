package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/communications"
	"github.com/patrickrho-patty/pccp/internal/context"
	"github.com/patrickrho-patty/pccp/internal/events"
	"github.com/patrickrho-patty/pccp/internal/fleet"
	"github.com/patrickrho-patty/pccp/internal/gitscm"
	"github.com/patrickrho-patty/pccp/internal/impact"
	"github.com/patrickrho-patty/pccp/internal/sandbox"
	"github.com/patrickrho-patty/pccp/internal/security"
	"github.com/patrickrho-patty/pccp/internal/workintel"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/policy"
	"github.com/patrickrho-patty/pccp/internal/provenance"
	"github.com/patrickrho-patty/pccp/internal/registry"
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
	router     *chi.Mux
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
	}
	s.setupRouter()
	return s, nil
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

	// OpenAI-compatible inference adapter (§38.5)
	r.Post("/v1/chat/completions", s.handleCompatChatCompletions)

	// Auth routes (no auth required)
	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/login", s.handleLogin)
		r.Post("/bootstrap", s.handleBootstrap)
	})

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
			r.Get("/{id}", s.handleGetUser)
			r.Put("/{id}", s.handleUpdateUser)
			r.Delete("/{id}", s.handleDeleteUser)
		})

		// Harnesses
		r.Route("/harnesses", func(r chi.Router) {
			r.Get("/", s.handleListHarnesses)
			r.Post("/enroll", s.handleEnrollHarness)
			r.Get("/{id}", s.handleGetHarness)
			r.Post("/{id}/revoke", s.handleRevokeHarness)
			r.Post("/{id}/quarantine", s.handleQuarantineHarness)
			r.Post("/{id}/reactivate", s.handleReactivateHarness)
		})

		// Projects
		r.Route("/projects", func(r chi.Router) {
			r.Get("/", s.handleListProjects)
			r.Post("/", s.handleCreateProject)
			r.Get("/{id}", s.handleGetProject)
			r.Put("/{id}", s.handleUpdateProject)
			r.Delete("/{id}", s.handleDeleteProject)
		})

		// Repositories
		r.Route("/repositories", func(r chi.Router) {
			r.Get("/", s.handleListRepositories)
			r.Post("/", s.handleRegisterRepository)
			r.Get("/{id}", s.handleGetRepository)
			r.Put("/{id}", s.handleUpdateRepository)
			r.Delete("/{id}", s.handleDeleteRepository)
			r.Post("/{id}/baselines", s.handleCreateBaseline)
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
			r.Get("/{id}/usage", s.handleGetSessionUsage)
			r.Get("/{id}/timeline", s.handleGetSessionTimeline)
			r.Get("/{id}/exchanges", s.handleGetSessionExchanges)
		})

		// Model registry
		r.Route("/models", func(r chi.Router) {
			r.Get("/", s.handleListModelPackages)
			r.Post("/", s.handleRegisterModelPackage)
			r.Get("/{id}", s.handleGetModelPackage)
			r.Post("/{id}/publish", s.handlePublishModel)
			r.Post("/{id}/recall", s.handleRecallModel)
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
		})

		// Policy
		r.Route("/policy", func(r chi.Router) {
			r.Get("/epochs", s.handleListEpochs)
			r.Post("/epochs", s.handleCreateEpoch)
			r.Get("/leases", s.handleListLeases)
			r.Post("/leases", s.handleIssueLease)
		})

		// Communications
		r.Route("/communications", func(r chi.Router) {
			r.Get("/conversations", s.handleListConversations)
			r.Post("/conversations", s.handleCreateConversation)
			r.Get("/conversations/{id}/messages", s.handleListMessages)
			r.Post("/conversations/{id}/messages", s.handleSendMessage)
			r.Get("/presence", s.handleGetPresence)
			r.Post("/presence", s.handleUpdatePresence)
			r.Post("/broadcasts", s.handleSendBroadcast)
			r.Get("/broadcasts", s.handleListBroadcasts)
			r.Post("/file-transfers", s.handleCreateFileTransfer)
			r.Get("/file-transfers", s.handleListFileTransfers)
			r.Get("/presence", s.handleGetPresence)
			r.Post("/presence", s.handleUpdatePresence)
		})

		// Work Intelligence
		r.Route("/analytics", func(r chi.Router) {
			r.Get("/usage", s.handleGetUsageSummary)
			r.Get("/engineering", s.handleGetEngineeringMetrics)
			r.Get("/security", s.handleGetSecurityMetrics)
			r.Get("/scorecard", s.handleGetScorecard)
			r.Get("/export", s.handleExportMetrics)
		})

		// Security
		r.Post("/security/check", s.handleSecurityCheck)
		r.Get("/security/policy", s.handleGetSecurityPolicy)
		r.Put("/security/policy", s.handleUpdateSecurityPolicy)
		r.Get("/security/findings", s.handleSecurityFindings)
			r.Get("/security/findings/{id}", s.handleSecurityFindingDetail)
			r.Put("/security/findings/{id}", s.handleUpdateFinding)
		r.Post("/security/lockdown", s.handleSecurityLockdown)

		// Fleet Operations
		r.Route("/fleet", func(r chi.Router) {
			r.Get("/inventory", s.handleFleetInventory)
			r.Get("/sessions/{id}/inspect", s.handleInspectSession)
			r.Post("/actions", s.handleFleetAction)
		})

		// Git/SCM
		r.Route("/scm", func(r chi.Router) {
			r.Get("/heatmaps", s.handleRepositoryHeatmap)
			r.Post("/baselines", s.handleCreateBaselineSCM)
			r.Post("/branch-protection", s.handleSetBranchProtection)
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
		r.Post("/demo-seed", s.handleSeedDemoData)
		})

		// Audit
		r.Route("/audit", func(r chi.Router) {
			r.Get("/", s.handleListAuditEvents)
		})

		// Additional service routes
		s.setupAdditionalRoutes(r, s.ext())

		// Dashboard
		r.Get("/dashboard", s.handleDashboard)
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
func (s *Server) ListenAndServe(addr string) error {
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
			// For dev convenience, allow unauthenticated access when no admin exists
			var count int64
			s.db.Raw("SELECT count(*) FROM admin_credentials").Scan(&count)
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
		"status":     "ok",
		"timestamp":  time.Now().Format(time.RFC3339),
		"service":    "pccp-control-plane",
		"version":    "0.1.0",
		"ca_pubkey":  s.identity.CAPublicKeyHex(),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token, err := s.auth.Login(req.Email, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "로그인 실패 / invalid credentials")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"token": token,
	})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		OrgName  string `json:"org_name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Create default org
	org, err := s.identity.CreateOrganization(req.OrgName, req.OrgName, "default", "enterprise")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Bootstrap admin
	if err := s.auth.BootstrapAdmin(req.Email, req.Password, org.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"organization_id": org.ID,
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
			"organization_id":   org.ID,
			"plan_tier":         org.PlanTier,
			"user_seats": map[string]interface{}{
				"used":     userCount,
				"max":      org.MaxUserSeats,
				"available": org.MaxUserSeats - int(userCount),
				"utilization": fmt.Sprintf("%.0f%%", float64(userCount)/float64(org.MaxUserSeats)*100),
			},
			"harness_seats": map[string]interface{}{
				"used":     harnessCount,
				"max":      org.MaxHarnessSeats,
				"available": org.MaxHarnessSeats - int(harnessCount),
				"utilization": fmt.Sprintf("%.0f%%", float64(harnessCount)/float64(org.MaxHarnessSeats)*100),
			},
			"active_sessions": activeSessions,
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
	var users []models.User
	q := s.db.Model(&models.User{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Find(&users)
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID string `json:"organization_id"`
		Email          string `json:"email"`
		Name           string `json:"name"`
		NameKo         string `json:"name_ko"`
		AuthMethod     string `json:"auth_method"`
		Title          string `json:"title"`
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
	user, err := s.identity.CreateUser(orgID, req.Email, req.Name, req.NameKo, req.AuthMethod, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Title != "" {
		user.Title = req.Title
		s.db.Save(user)
	}
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
	var harnesses []models.Harness
	q := s.db.Model(&models.Harness{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Order("created_at DESC").Find(&harnesses)
	writeJSON(w, http.StatusOK, harnesses)
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
	harness, cred, err := s.identity.EnrollHarness(req)
	if err != nil {
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var projects []models.Project
	q := s.db.Model(&models.Project{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Find(&projects)
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID string   `json:"organization_id"`
		Name           string   `json:"name"`
		NameKo         string   `json:"name_ko"`
		Slug           string   `json:"slug"`
		AllowedModels  []string `json:"allowed_models"`
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
	writeJSON(w, http.StatusCreated, proj)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var proj models.Project
	if err := s.db.First(&proj, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "프로젝트를 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, proj)
}

func (s *Server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var repos []models.Repository
	q := s.db.Model(&models.Repository{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
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
	repo, err := s.identity.RegisterRepository(orgID, req.ProjectID, req.Name, req.FullName, req.DefaultBranch, req.Sensitivity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
		Branch       string `json:"branch"`
		CommitSHA    string `json:"commit_sha"`
		CommitMessage string `json:"commit_message"`
		AuthorName   string `json:"author_name"`
		AuthorEmail  string `json:"author_email"`
		CommittedAt  string `json:"committed_at"`
		TreeDigest   string `json:"tree_digest"`
		SessionID    string `json:"session_id"`
		OrgID        string `json:"org_id"`
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

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var sessions []models.Session
	q := s.db.Model(&models.Session{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
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
	sess, err := s.identity.OpenSession(orgID, req.HarnessID, req.UserID, req.ProjectID,
		req.RepositoryID, req.Branch, req.BaselineID, req.Title, req.TaskPurpose, req.ModelClass)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.identity.CloseSession(sess.SessionID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

func (s *Server) handleCompatChatCompletions(w http.ResponseWriter, r *http.Request) {
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
			"session":        sess,
			"actions":        actions,
			"change_sets":    changeSets,
			"findings":       findings,
			"approvals":      approvals,
			"usage_records":  usageRecords,
		})
	}

func (s *Server) handleGetSessionUsage(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var sess models.Session
		if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		var records []models.UsageRecord
		s.db.Where("session_id = ?", sess.SessionID).Find(&records)

		summary := map[string]interface{}{
			"total_records":    len(records),
			"input_tokens":     0,
			"output_tokens":    0,
			"total_tokens":     0,
			"by_metric":        map[string]int64{},
			"by_model":         map[string]int64{},
		}
		totalIn, totalOut := 0, 0
		for _, rec := range records {
			if rec.MetricType == "tokens_in" {
				totalIn += int(rec.Quantity)
			}
			if rec.MetricType == "tokens_out" {
				totalOut += int(rec.Quantity)
			}
			summary["by_metric"].(map[string]int64)[rec.MetricType] += rec.Quantity
			summary["by_model"].(map[string]int64)[rec.ModelPackageID] += rec.Quantity
		}
		summary["input_tokens"] = totalIn
		summary["output_tokens"] = totalOut
		summary["total_tokens"] = totalIn + totalOut

		writeJSON(w, http.StatusOK, summary)
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
	if v, ok := raw["package_id"]; ok { json.Unmarshal(v, &pkg.PackageID) }
	if v, ok := raw["model_id"]; ok { json.Unmarshal(v, &pkg.ModelID) }
	if v, ok := raw["name"]; ok { json.Unmarshal(v, &pkg.Name) }
	if v, ok := raw["name_ko"]; ok { json.Unmarshal(v, &pkg.NameKo) }
	if v, ok := raw["family"]; ok { json.Unmarshal(v, &pkg.Family) }
	if v, ok := raw["version"]; ok { json.Unmarshal(v, &pkg.Version) }
	if v, ok := raw["release"]; ok { json.Unmarshal(v, &pkg.Release) }
	if v, ok := raw["weights_merkle_root"]; ok { json.Unmarshal(v, &pkg.WeightsMerkleRoot) }
	if v, ok := raw["tokenizer_digest"]; ok { json.Unmarshal(v, &pkg.TokenizerDigest) }
	if v, ok := raw["config_digest"]; ok { json.Unmarshal(v, &pkg.ConfigDigest) }
	if v, ok := raw["entitlement_class"]; ok { json.Unmarshal(v, &pkg.EntitlementClass) }
	if v, ok := raw["minimum_endpoint_assurance"]; ok { json.Unmarshal(v, &pkg.MinAssuranceLevel) }
	if v, ok := raw["state"]; ok { json.Unmarshal(v, &pkg.State) }
	if v, ok := raw["context_window"]; ok { json.Unmarshal(v, &pkg.ContextWindow) }
	// Array fields → store as JSON string
	if v, ok := raw["capabilities"]; ok { pkg.CapabilitiesJSON = string(v) }
	if v, ok := raw["weights_shards"]; ok { pkg.WeightsShardsJSON = string(v) }
	if v, ok := raw["adapters"]; ok { pkg.AdaptersJSON = string(v) }
	if v, ok := raw["serving_engines"]; ok { pkg.ServingEnginesJSON = string(v) }
	if v, ok := raw["allowed_data_classes"]; ok { pkg.AllowedDataClasses = string(v) }

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
	writeJSON(w, http.StatusOK, map[string]string{"status": "recalled"})
}

func (s *Server) handleListEndpoints(w http.ResponseWriter, r *http.Request) {
	var endpoints []models.InferenceEndpoint
	s.db.Order("created_at DESC").Find(&endpoints)
	writeJSON(w, http.StatusOK, endpoints)
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
	writeJSON(w, http.StatusCreated, endpoint)
}

func (s *Server) handleGetEndpoint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var ep models.InferenceEndpoint
	if err := s.db.Where("id = ? OR endpoint_id = ?", id, id).First(&ep).Error; err != nil {
		writeError(w, http.StatusNotFound, "엔드포인트를 찾을 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, ep)
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

func (s *Server) handleCreateEpoch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID string   `json:"organization_id"`
		AllowedModels  []string `json:"allowed_models"`
		TransitionMode string   `json:"transition_mode"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = getOrgID(r)
	}
	if req.TransitionMode == "" {
		req.TransitionMode = "immediate"
	}
	epoch, err := s.policy.CreatePolicyEpoch(orgID, req.AllowedModels, req.TransitionMode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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

func (s *Server) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var events []models.AuditEvent
	q := s.db.Model(&models.AuditEvent{})
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	q.Order("occurred_at DESC").Limit(200).Find(&events)
	writeJSON(w, http.StatusOK, events)
}

// --- Communications Handlers ---

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	// For now, return all org conversations
	var convs []models.Conversation
	s.db.Where("organization_id = ?", orgID).Order("last_message_at DESC").Find(&convs)
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
	writeJSON(w, http.StatusCreated, conv)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	convID := chi.URLParam(r, "id")
	messages, err := s.comms.ListMessages(convID, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
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
	msg, err := s.comms.SendMessage(convID, req.SenderID, req.SenderType, "text", req.Content, req.ParentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
		UserID   string `json:"user_id"`
		Status   string `json:"status"`
		Activity string `json:"activity"`
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
	bc, err := s.comms.SendBroadcast(orgID, req.Severity, req.Title, req.TitleKo, req.Body, req.BodyKo, req.TargetType, req.TargetID, req.RequiresAck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
	sb, err := s.sandbox.CreateSandbox(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sb)
}

func (s *Server) handleDestroySandbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sb, err := s.sandbox.DestroySandbox(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
	if err := s.db.First(&user, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var updates struct {
		Name       *string `json:"name,omitempty"`
		NameKo     *string `json:"name_ko,omitempty"`
		Email      *string `json:"email,omitempty"`
		Title      *string `json:"title,omitempty"`
		Status     *string `json:"status,omitempty"`
		AuthMethod *string `json:"auth_method,omitempty"`
		Locale     *string `json:"locale,omitempty"`
		Timezone   *string `json:"timezone,omitempty"`
	}
	if err := decodeJSON(r, &updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if updates.Name != nil { user.Name = *updates.Name }
	if updates.NameKo != nil { user.NameKo = *updates.NameKo }
	if updates.Email != nil { user.Email = *updates.Email }
	if updates.Title != nil { user.Title = *updates.Title }
	if updates.Status != nil { user.Status = *updates.Status }
	if updates.AuthMethod != nil { user.AuthMethod = *updates.AuthMethod }
	if updates.Locale != nil { user.Locale = *updates.Locale }
	if updates.Timezone != nil { user.Timezone = *updates.Timezone }
	s.db.Save(&user)
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.db.Model(&models.User{}).Where("id = ?", id).Update("status", "offboarded")
	writeJSON(w, http.StatusOK, map[string]string{"status": "offboarded"})
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var proj models.Project
	if err := s.db.First(&proj, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var updates struct {
		Name           *string `json:"name,omitempty"`
		NameKo         *string `json:"name_ko,omitempty"`
		Description    *string `json:"description,omitempty"`
		Status         *string `json:"status,omitempty"`
	}
	decodeJSON(r, &updates)
	if updates.Name != nil { proj.Name = *updates.Name }
	if updates.NameKo != nil { proj.NameKo = *updates.NameKo }
	if updates.Description != nil { proj.Description = *updates.Description }
	if updates.Status != nil { proj.Status = *updates.Status }
	s.db.Save(&proj)
	writeJSON(w, http.StatusOK, proj)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.db.Model(&models.Project{}).Where("id = ?", id).Update("status", "archived")
	writeJSON(w, http.StatusOK, map[string]string{"status": "archived"})
}

func (s *Server) handleUpdateRepository(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var repo models.Repository
	if err := s.db.First(&repo, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var updates struct {
		Sensitivity *string `json:"sensitivity,omitempty"`
		Status      *string `json:"status,omitempty"`
	}
	decodeJSON(r, &updates)
	if updates.Sensitivity != nil { repo.Sensitivity = *updates.Sensitivity }
	if updates.Status != nil { repo.Status = *updates.Status }
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
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.db.Model(&sess).Update("status", "paused")
	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (s *Server) handleResumeSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var sess models.Session
	if err := s.db.Where("id = ? OR session_id = ?", id, id).First(&sess).Error; err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.db.Model(&sess).Update("status", "active")
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
		Name        *string `json:"name,omitempty"`
		NameKo      *string `json:"name_ko,omitempty"`
		Description *string `json:"description,omitempty"`
	}
	decodeJSON(r, &updates)
	if updates.Name != nil { pkg.Name = *updates.Name }
	if updates.NameKo != nil { pkg.NameKo = *updates.NameKo }
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "quarantined"})
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
	if updates.AssuranceLevel != nil { ep.AssuranceLevel = *updates.AssuranceLevel }
	if updates.Status != nil { ep.Status = *updates.Status }
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
	// Return current DLP rule configuration
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rules": []map[string]interface{}{
			{"rule_id": "pii-kr-rrn", "name": "Korean RRN", "name_ko": "주민등록번호", "type": "korean_pii", "severity": "critical", "enabled": true, "action": "block"},
			{"rule_id": "pii-kr-business", "name": "Business Registration", "name_ko": "사업자등록번호", "type": "korean_pii", "severity": "high", "enabled": true, "action": "mask"},
			{"rule_id": "pii-kr-phone", "name": "Korean Phone", "name_ko": "전화번호", "type": "korean_pii", "severity": "medium", "enabled": true, "action": "mask"},
			{"rule_id": "pii-kr-account", "name": "Bank Account", "name_ko": "계좌번호", "type": "korean_pii", "severity": "high", "enabled": true, "action": "block"},
			{"rule_id": "secret-aws", "name": "AWS Access Key", "name_ko": "AWS 접근키", "type": "secret", "severity": "critical", "enabled": true, "action": "block"},
			{"rule_id": "secret-jwt", "name": "JWT Token", "name_ko": "JWT 토큰", "type": "secret", "severity": "high", "enabled": true, "action": "block"},
			{"rule_id": "secret-private-key", "name": "Private Key", "name_ko": "개인키", "type": "secret", "severity": "critical", "enabled": true, "action": "block"},
			{"rule_id": "secret-github", "name": "GitHub PAT", "name_ko": "GitHub 토큰", "type": "secret", "severity": "high", "enabled": true, "action": "block"},
			{"rule_id": "injection-ignore", "name": "Instruction Override", "name_ko": "명령어 재정의", "type": "prompt_injection", "severity": "high", "enabled": true, "action": "block"},
			{"rule_id": "injection-jailbreak", "name": "Jailbreak Attempt", "name_ko": "탈옥 시도", "type": "prompt_injection", "severity": "high", "enabled": true, "action": "block"},
		},
	})
}

func (s *Server) handleUpdateSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	var updates struct {
		RuleID  string `json:"rule_id"`
		Enabled *bool  `json:"enabled"`
	}
	if err := decodeJSON(r, &updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// In production, this would persist to a PolicyPack record
	// For now, record in audit
	orgID := getOrgID(r)
	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.security.rule_updated",
		ActorType:      "admin",
		Action:         "update_security_rule",
		ResourceType:   "security_rule",
		ResourceID:     updates.RuleID,
		Details:        fmt.Sprintf(`{"rule_id":"%s","enabled":%v}`, updates.RuleID, updates.Enabled),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(audit)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleSecurityFindings(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var findings []models.SecurityFinding
	s.db.Where("organization_id = ?", orgID).Order("occurred_at DESC").Limit(100).Find(&findings)
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
	// Terminate all active sessions
	s.db.Model(&models.Session{}).
		Where("organization_id = ? AND status = 'active'", orgID).
		Update("status", "terminated")
	s.db.Model(&models.Harness{}).
		Where("organization_id = ?", orgID).
		Update("risk_state", "high")

	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.security.emergency_lockdown",
		ActorType:      "admin",
		Action:         "emergency_lockdown",
		Details:        "Emergency lockdown activated via Security console",
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(audit)
	writeJSON(w, http.StatusOK, map[string]string{"status": "lockdown_activated"})
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
			SenderID     string `json:"sender_id"`
			RecipientID  string `json:"recipient_id"`
			FileName     string `json:"file_name"`
			FileSize     int64  `json:"file_size"`
			FileType     string `json:"file_type"`
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


func (s *Server) handleSeedDemoData(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	results := map[string]int{}

	demoUsers := []struct{ Email, Name, NameKo, Title, Dept string }{
		{"kim@patty.dev", "Kim Gaebal", "김개발", "시니어 개발자", "dev"},
		{"lee@patty.dev", "Lee Tester", "이테스트", "QA 엔지니어", "qa"},
		{"park@patty.dev", "Park Secur", "박보안", "보안 엔지니어", "security"},
		{"choi@patty.dev", "Choi Lead", "최리드", "테크 리드", "dev"},
	}
	userIDs := map[string]string{}
	for _, u := range demoUsers {
		existing := &models.User{}
		if s.db.Where("email = ?", u.Email).First(existing).Error == nil {
			userIDs[u.Email] = existing.ID
			continue
		}
		usr, err := s.identity.CreateUser(orgID, u.Email, u.Name, u.NameKo, "local", "")
		if err != nil { continue }
		usr.Title = u.Title
		usr.BusinessUnitID = u.Dept
		s.db.Save(usr)
		userIDs[u.Email] = usr.ID
		results["users"]++
	}

	for _, p := range []struct{ Name, NameKo, Slug string }{
		{"backend-api", "백엔드 API", "backend-api"},
		{"frontend-app", "프론트엔드 앱", "frontend-app"},
		{"infra", "인프라", "infra"},
	} {
		existing := &models.Project{}
		if s.db.Where("slug = ?", p.Slug).First(existing).Error == nil { continue }
		s.db.Create(&models.Project{
			AuditBase: models.AuditBase{OrganizationID: orgID, Classification: "internal"},
			Name: p.Name, NameKo: p.NameKo, Slug: p.Slug, Status: "active",
		})
		results["projects"]++
	}

	for _, repo := range []struct{ Name, Provider, URL string }{
		{"backend-api", "github", "https://github.com/patty/backend-api.git"},
		{"frontend-app", "github", "https://github.com/patty/frontend-app.git"},
	} {
		existing := &models.Repository{}
		if s.db.Where("name = ?", repo.Name).First(existing).Error == nil { continue }
		var proj models.Project
		s.db.Where("slug = ?", repo.Name).First(&proj)
		s.db.Create(&models.Repository{
			AuditBase: models.AuditBase{OrganizationID: orgID, Classification: "internal"},
			Name: repo.Name, FullName: repo.Name, ProjectID: proj.ID,
			SCMProvider: repo.Provider, CloneURL: repo.URL, DefaultBranch: "main",
		})
		results["repos"]++
	}

	for email, hid := range map[string]string{"kim@patty.dev":"hrn_kim_001","lee@patty.dev":"hrn_lee_002","park@patty.dev":"hrn_park_003","choi@patty.dev":"hrn_choi_004"} {
		if _, ok := userIDs[email]; !ok { continue }
		existing := &models.Harness{}
		if s.db.Where("harness_id = ?", hid).First(existing).Error == nil { continue }
		s.db.Create(&models.Harness{
			Base: models.Base{}, OrganizationID: orgID,
			HarnessID: hid, Status: "active", BinaryVersion: "1.2.0",
			BuildChannel: "stable", PublicKey: "demo-" + hid,
		})
		results["harnesses"]++
	}

	var projAPI models.Project
	s.db.Where("slug = ?", "backend-api").First(&projAPI)
	var projFE models.Project
	s.db.Where("slug = ?", "frontend-app").First(&projFE)
	var projInfra models.Project
	s.db.Where("slug = ?", "infra").First(&projInfra)

	demoSess := []struct{ Title, UserID, HID, PID, Status string }{
		{"환불 로직 구현", userIDs["kim@patty.dev"], "hrn_kim_001", projAPI.ID, "active"},
		{"테스트 코드 작성", userIDs["lee@patty.dev"], "hrn_lee_002", projAPI.ID, "active"},
		{"보안 취약점 분석", userIDs["park@patty.dev"], "hrn_park_003", projAPI.ID, "closed"},
		{"UI 리팩토링", userIDs["choi@patty.dev"], "hrn_choi_004", projFE.ID, "paused"},
		{"인프라 설정", userIDs["kim@patty.dev"], "hrn_kim_001", projInfra.ID, "completed"},
		{"API 문서화", userIDs["lee@patty.dev"], "hrn_lee_002", projAPI.ID, "terminated"},
	}
	for i, ds := range demoSess {
		sm := &models.Session{
			AuditBase: models.AuditBase{OrganizationID: orgID, Classification: "internal"},
			SessionID: fmt.Sprintf("sess_demo_%03d", i+1),
			UserID: ds.UserID, HarnessID: ds.HID, ProjectID: ds.PID,
			Title: ds.Title, Status: ds.Status, ModelClass: "patty-code-standard",
			OpenedAt: time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
		}
		if ds.Status == "closed" || ds.Status == "completed" || ds.Status == "terminated" {
			sm.ClosedAt = time.Now().Add(-time.Duration(i-2) * time.Hour).Format(time.RFC3339)
		}
		s.db.Create(sm)
		results["sessions"]++
	}

	for _, f := range []struct{ Type, Sev, Title, TitleKo string }{
		{"pii_leak", "high", "Korean RRN detected", "주민번호 감지"},
		{"secret_exposure", "critical", "AWS key in context", "AWS 키 노출"},
		{"prompt_injection", "medium", "Indirect injection", "간접 인젝션"},
	} {
		s.db.Create(&models.SecurityFinding{
			Base: models.Base{}, OrganizationID: orgID,
			FindingType: f.Type, Severity: f.Sev, Title: f.Title, TitleKo: f.TitleKo,
			Status: "open", OccurredAt: time.Now().Add(-time.Hour).Format(time.RFC3339),
		})
		results["findings"]++
	}

	conv, _ := s.comms.CreateConversation(orgID, "channel", "개발팀 채널", []string{})
	if conv != nil {
		s.comms.SendMessage(conv.ID, "admin", "user", "text", "팀 미팅이 3시에 있습니다", "")
		results["conversations"]++
	}
	s.comms.SendBroadcast(orgID, "info", "Scheduled Maintenance", "예정된 유지보수", "Saturday 2-4 AM", "토요일 새벽 2-4시", "all", "", false)
	results["broadcasts"]++

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "seeded", "results": results})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- Enterprise Harness Feature Handlers ---

func (s *Server) handleListEnterpriseFeatures(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var features []models.EnterpriseHarnessFeature
	s.db.Where("organization_id = ?", orgID).Order("category, feature_key").Find(&features)
	writeJSON(w, http.StatusOK, features)
}

func (s *Server) handleUpdateEnterpriseFeature(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Enabled  bool `json:"enabled"`
		Enforced bool `json:"enforced"`
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
		Enforced bool
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

	inserted := 0
	for _, d := range defaults {
		feature := &models.EnterpriseHarnessFeature{
			Base: models.Base{},
			OrganizationID: orgID,
			FeatureKey:     d.Key,
			FeatureName:    d.Name,
			FeatureNameKo:  d.NameKo,
			Category:       d.Category,
			PRDRef:         d.PRD,
			Enabled:        true,
			Enforced:       d.Enforced,
			Status:         "active",
		}
		if err := s.db.Create(feature).Error; err != nil {
			log.Printf("enterprise seed: failed to create %s: %v", d.Key, err)
			continue
		}
		inserted++
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "seeded", "count": inserted})
}