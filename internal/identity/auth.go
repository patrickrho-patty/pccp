package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/patrickrho-patty/pccp/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CredentialRevocations is the control plane's monotonic in-process view of
// revoked peer-credential serials. Callers receive copies of its maps.
type CredentialRevocations struct {
	mu      sync.RWMutex
	epoch   uint64
	serials map[string]uint64
}

// newCredentialRevocations builds the in-process view; with a DB it
// REBUILDS the epoch + serial set from the durable ledger so
// revocation survives restarts (DARI §F.7 — a restarted relay must
// never accept a credential revoked before it went down).
func newCredentialRevocations(db *gorm.DB) *CredentialRevocations {
	r := &CredentialRevocations{serials: make(map[string]uint64)}
	if db == nil {
		return r
	}
	var rows []models.CredentialRevocationRecord
	if err := db.Find(&rows).Error; err != nil {
		return r // fail-safe: in-memory view only; connect-time DB status still enforced
	}
	for _, row := range rows {
		r.serials[row.Serial] = row.RevokedEpoch
		if row.RevokedEpoch > r.epoch {
			r.epoch = row.RevokedEpoch
		}
	}
	return r
}

func (r *CredentialRevocations) reserveEpoch() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.epoch++
	return r.epoch
}

func (r *CredentialRevocations) applyCommitted(serial string, epoch uint64) {
	if serial == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if epoch > r.epoch {
		r.epoch = epoch
	}
	r.serials[serial] = epoch
}

func (r *CredentialRevocations) snapshot() (uint64, map[string]uint64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	serials := make(map[string]uint64, len(r.serials))
	for serial, epoch := range r.serials {
		serials[serial] = epoch
	}
	return r.epoch, serials
}

// AdminCredentials stores admin login credentials (for local/dev auth).
type AdminCredentials struct {
	Email    string `gorm:"type:varchar(255);uniqueIndex;not null" json:"email"`
	Password string `gorm:"type:varchar(255);not null" json:"-"`
	models.Base
	OrganizationID string `gorm:"type:varchar(64);index" json:"organization_id"`
	UserID         string `gorm:"type:varchar(64);index" json:"user_id,omitempty"`
	Name           string `gorm:"type:varchar(255)" json:"name"`
	Role           string `gorm:"type:varchar(64);default:'admin'" json:"role"`
	// PermissionsJSON carries action-scoped console grants for operators whose
	// role alone should not grant every security action.
	PermissionsJSON string `gorm:"type:text" json:"-"`
	MFASecret       string `gorm:"type:varchar(64)" json:"-"`
	MFAEnrolled     bool   `gorm:"default:false" json:"mfa_enrolled"`
}

func (a *AdminCredentials) BeforeSave(_ *gorm.DB) error {
	a.Email = NormalizeEmail(a.Email)
	return nil
}

// TableName overrides the table name for AdminCredentials.
func (AdminCredentials) TableName() string {
	return "admin_credentials"
}

// AuthService handles JWT-based authentication for the admin API.
type AuthService struct {
	db     *gorm.DB
	secret []byte
}

var ErrAlreadyBootstrapped = errors.New("auth: system is already bootstrapped")

const (
	consoleTokenIssuer   = "pccp"
	consoleTokenAudience = "pccp-console"
	consoleTokenPurpose  = "console"
)

// NewAuthService creates a new auth service.
func NewAuthService(db *gorm.DB, jwtSecret string) *AuthService {
	return &AuthService{
		db:     db,
		secret: []byte(jwtSecret),
	}
}

// Claims are the JWT claims for an admin session.
type Claims struct {
	Email              string   `json:"email"`
	OrganizationID     string   `json:"org_id"`
	Role               string   `json:"role"`
	Permissions        []string `json:"permissions,omitempty"`
	Purpose            string   `json:"purpose"`
	UserLifecycleEpoch uint64   `json:"user_lifecycle_epoch,omitempty"`
	jwt.RegisteredClaims
}

// BootstrapAdmin creates the initial admin credentials if none exist. It never
// doubles as password recovery; existing credentials are immutable through this
// unauthenticated first-run path.
func (a *AuthService) BootstrapAdmin(email, password, orgID string) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		return a.BootstrapAdminWithDB(tx, email, password, orgID)
	})
}

func (a *AuthService) BootstrapAdminWithDB(db *gorm.DB, email, password, orgID string) error {
	email = NormalizeEmail(email)
	var count int64
	if err := db.Model(&AdminCredentials{}).Count(&count).Error; err != nil {
		return fmt.Errorf("auth: inspect bootstrap state: %w", err)
	}
	if count > 0 {
		return ErrAlreadyBootstrapped
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("auth: hash password: %w", err)
	}

	admin := &AdminCredentials{
		Email:          email,
		Password:       string(hash),
		OrganizationID: orgID,
		Name:           "관리자",
		Role:           "super_admin",
	}
	if err := db.Create(admin).Error; err != nil {
		return fmt.Errorf("auth: create admin: %w", err)
	}
	return nil
}

// Login authenticates an admin and returns a JWT token.
func (a *AuthService) Login(email, password string) (string, error) {
	email = NormalizeEmail(email)
	var admin AdminCredentials
	if err := a.db.Where("email = ?", email).First(&admin).Error; err != nil {
		return "", fmt.Errorf("auth: admin not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(password)); err != nil {
		return "", fmt.Errorf("auth: invalid credentials")
	}

	return a.IssueToken(admin.Email, admin.OrganizationID, admin.Role)
}

// IssueToken signs a console-session JWT for the supplied identity.
// Login uses it after password verification; the SSO callbacks use it
// after identity-provider verification — both produce the SAME claim
// shape so the auth middleware accepts either path.
func (a *AuthService) IssueToken(email, orgID, role string) (string, error) {
	var token string
	err := a.db.Transaction(func(tx *gorm.DB) error {
		var admin AdminCredentials
		permissions := []string(nil)
		if err := tx.Where("LOWER(email) = LOWER(?) AND organization_id = ?", email, orgID).First(&admin).Error; err == nil {
			if admin.Role != "" {
				role = admin.Role
			}
			if admin.PermissionsJSON != "" {
				if err := json.Unmarshal([]byte(admin.PermissionsJSON), &permissions); err != nil {
					return fmt.Errorf("auth: invalid permission grants")
				}
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("auth: resolve operator grants: %w", err)
		}
		subject, lifecycleEpoch, err := activeUserSubject(tx, email, orgID, admin.UserID)
		if err != nil {
			return err
		}
		token, err = a.signToken(email, orgID, role, permissions, subject, lifecycleEpoch)
		return err
	})
	return token, err
}

// IssueTokenForUser signs an SSO console token for the exact managed user that
// the verified external identity resolved. Administrator grants are inherited
// only from credentials already linked to that immutable user ID; an unrelated
// legacy credential sharing the same email is never consulted.
func (a *AuthService) IssueTokenForUser(userID, orgID, role string) (string, error) {
	var token string
	err := a.db.Transaction(func(tx *gorm.DB) error {
		var err error
		token, err = a.IssueTokenForUserWithDB(tx, userID, orgID, role)
		return err
	})
	return token, err
}

func (a *AuthService) IssueTokenForUserWithDB(db *gorm.DB, userID, orgID, role string) (string, error) {
	var user models.User
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND organization_id = ?", userID, orgID).First(&user).Error; err != nil {
		return "", fmt.Errorf("auth: resolve managed SSO user: %w", err)
	}
	if user.Status != models.UserStatusActive {
		return "", fmt.Errorf("auth: account is %s", user.Status)
	}
	if NormalizeEmail(user.Email) == "" {
		return "", errors.New("auth: managed SSO user has no email")
	}
	permissions := []string(nil)
	var linked []AdminCredentials
	if err := db.Where("organization_id = ? AND user_id = ?", orgID, user.ID).Limit(2).Find(&linked).Error; err != nil {
		return "", fmt.Errorf("auth: resolve linked operator grants: %w", err)
	}
	if len(linked) > 1 {
		return "", errors.New("auth: ambiguous linked operator grants")
	}
	if len(linked) == 1 {
		if linked[0].Role != "" {
			role = linked[0].Role
		}
		if linked[0].PermissionsJSON != "" {
			if err := json.Unmarshal([]byte(linked[0].PermissionsJSON), &permissions); err != nil {
				return "", errors.New("auth: invalid permission grants")
			}
		}
	}
	return a.signToken(user.Email, orgID, role, permissions, user.ID, user.LifecycleEpoch)
}

// SetPermissions updates the durable grants consumed by password and SSO
// token issuance. Existing sessions retain their signed grants until expiry.
func (a *AuthService) SetPermissions(orgID, email, role string, permissions []string) error {
	return a.SetPermissionsWithDB(a.db, orgID, email, role, permissions)
}

func (a *AuthService) SetPermissionsWithDB(db *gorm.DB, orgID, email, role string, permissions []string) error {
	email = NormalizeEmail(email)
	encoded, err := json.Marshal(permissions)
	if err != nil {
		return err
	}
	updates := map[string]interface{}{"permissions_json": string(encoded)}
	var managed models.User
	if err := db.Select("id").Where("organization_id = ? AND email = ?", orgID, email).First(&managed).Error; err == nil {
		updates["user_id"] = managed.ID
	}
	result := db.Model(&AdminCredentials{}).
		Where("organization_id = ? AND email = ?", orgID, email).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		admin := AdminCredentials{
			Email: email, Password: "sso-only", OrganizationID: orgID,
			UserID: managed.ID, Role: role, PermissionsJSON: string(encoded), Name: email,
		}
		if admin.Role == "" {
			admin.Role = "security_operator"
		}
		return db.Create(&admin).Error
	}
	return nil
}

// IssueTokenWithPermissions signs an action-scoped console session. SSO and
// delegated-console integrations use this when an operator should receive a
// narrow grant instead of a broad administrator role.
func (a *AuthService) IssueTokenWithPermissions(email, orgID, role string, permissions []string) (string, error) {
	if a.db == nil {
		return a.signToken(email, orgID, role, permissions, "", 0)
	}
	var token string
	err := a.db.Transaction(func(tx *gorm.DB) error {
		subject, lifecycleEpoch, err := activeUserSubject(tx, email, orgID, "")
		if err != nil {
			return err
		}
		token, err = a.signToken(email, orgID, role, permissions, subject, lifecycleEpoch)
		return err
	})
	return token, err
}

func activeUserSubject(tx *gorm.DB, email, orgID, userID string) (string, uint64, error) {
	var user models.User
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ?", orgID)
	if userID != "" {
		query = query.Where("id = ?", userID)
	} else {
		query = query.Where("email = ?", NormalizeEmail(email))
	}
	err := query.First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("auth: resolve managed user: %w", err)
	}
	if user.Status != models.UserStatusActive {
		return "", 0, fmt.Errorf("auth: account is %s", user.Status)
	}
	return user.ID, user.LifecycleEpoch, nil
}

func (a *AuthService) signToken(email, orgID, role string, permissions []string, subject string, lifecycleEpoch uint64) (string, error) {
	if role == "" {
		role = "member"
	}
	claims := &Claims{
		Email:              email,
		OrganizationID:     orgID,
		Role:               role,
		Permissions:        append([]string(nil), permissions...),
		Purpose:            consoleTokenPurpose,
		UserLifecycleEpoch: lifecycleEpoch,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    consoleTokenIssuer,
			Audience:  jwt.ClaimStrings{consoleTokenAudience},
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.secret)
}

// ValidateClaimsLifecycle performs current-state introspection for every
// console request. A signed token is necessary but not sufficient after its
// managed user has been suspended or offboarded.
func (a *AuthService) ValidateClaimsLifecycle(claims *Claims) error {
	if a.db == nil || claims == nil || claims.OrganizationID == "" {
		return errors.New("auth: lifecycle subject is unavailable")
	}
	var organization models.Organization
	if err := a.db.Select("id").Where("id = ? AND status = ?", claims.OrganizationID, "active").First(&organization).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("auth: organization is inactive or unavailable")
		}
		return fmt.Errorf("auth: organization lookup failed: %w", err)
	}
	var user models.User
	q := a.db.Where("organization_id = ?", claims.OrganizationID)
	if claims.Subject != "" {
		q = q.Where("id = ?", claims.Subject)
	} else if claims.Email != "" {
		q = q.Where("LOWER(email) = LOWER(?)", claims.Email)
	} else {
		return errors.New("auth: lifecycle subject is unavailable")
	}
	err := q.First(&user).Error
	if err == nil {
		if user.Status != models.UserStatusActive {
			return fmt.Errorf("auth: account is %s", user.Status)
		}
		if claims.UserLifecycleEpoch != user.LifecycleEpoch {
			return errors.New("auth: token predates the current account lifecycle epoch")
		}
		// Upgrade a legacy email-only token in request context; all new tokens
		// carry the immutable subject at mint time.
		claims.Subject = user.ID
		return a.validateCurrentGrants(claims, &user)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("auth: lifecycle lookup failed: %w", err)
	}
	if claims.Subject != "" {
		return errors.New("auth: managed user no longer exists")
	}
	return a.validateCurrentGrants(claims, nil)
}

func (a *AuthService) validateCurrentGrants(claims *Claims, user *models.User) error {
	query := a.db.Where("organization_id = ?", claims.OrganizationID)
	if user != nil {
		var linked []AdminCredentials
		if err := query.Where("user_id = ?", user.ID).Limit(2).Find(&linked).Error; err != nil {
			return fmt.Errorf("auth: operator grant lookup failed: %w", err)
		}
		if len(linked) > 1 {
			return errors.New("auth: ambiguous operator grant")
		}
		if len(linked) == 1 {
			return compareCurrentGrants(claims, linked[0])
		}
		if claims.Role == "member" && len(claims.Permissions) == 0 {
			return nil
		}
		// Local credentials created before immutable user linkage may still back a
		// password session. They are accepted only by an exact tenant/email lookup;
		// SSO member tokens never inherit their authority.
		query = a.db.Where("organization_id = ? AND user_id = '' AND LOWER(email) = LOWER(?)", claims.OrganizationID, user.Email)
	} else {
		query = query.Where("LOWER(email) = LOWER(?)", claims.Email)
	}
	var credentials []AdminCredentials
	if err := query.Limit(2).Find(&credentials).Error; err != nil {
		return fmt.Errorf("auth: operator lookup failed: %w", err)
	}
	if len(credentials) != 1 {
		return errors.New("auth: operator no longer exists")
	}
	return compareCurrentGrants(claims, credentials[0])
}

func compareCurrentGrants(claims *Claims, credentials AdminCredentials) error {
	role := credentials.Role
	if role == "" {
		role = "member"
	}
	permissions := []string(nil)
	if credentials.PermissionsJSON != "" {
		if err := json.Unmarshal([]byte(credentials.PermissionsJSON), &permissions); err != nil {
			return errors.New("auth: invalid current permission grants")
		}
	}
	if claims.Role != role || !sameStringSet(claims.Permissions, permissions) {
		return errors.New("auth: token predates the current authorization grants")
	}
	return nil
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

// VerifyToken validates a JWT token and returns the claims.
func (a *AuthService) VerifyToken(tokenStr string) (*Claims, error) {
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return a.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(consoleTokenIssuer),
		jwt.WithAudience(consoleTokenAudience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil {
		return nil, fmt.Errorf("auth: parse token: %w", err)
	}
	if !token.Valid {
		return nil, errors.New("auth: invalid token")
	}
	if claims.Purpose != consoleTokenPurpose {
		return nil, errors.New("auth: token purpose is not console access")
	}
	return claims, nil
}

// AuthMiddleware extracts and validates the JWT token from the Authorization header.
// Returns the claims or an error.
func (a *AuthService) AuthMiddleware(authHeader string) (*Claims, error) {
	if authHeader == "" {
		return nil, errors.New("auth: missing Authorization header")
	}
	return a.VerifyToken(authHeader)
}
