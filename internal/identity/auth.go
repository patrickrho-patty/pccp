package identity

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/patrickrho-patty/pccp/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// CredentialRevocations is the control plane's monotonic in-process view of
// revoked peer-credential serials. Callers receive copies of its maps.
type CredentialRevocations struct {
	mu      sync.RWMutex
	epoch   uint64
	serials map[string]uint64
}

func newCredentialRevocations() *CredentialRevocations {
	return &CredentialRevocations{serials: make(map[string]uint64)}
}

func (r *CredentialRevocations) revoke(serial string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.epoch++
	if serial != "" {
		r.serials[serial] = r.epoch
	}
	return r.epoch
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
	Name           string `gorm:"type:varchar(255)" json:"name"`
	Role           string `gorm:"type:varchar(64);default:'admin'" json:"role"`
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

// NewAuthService creates a new auth service.
func NewAuthService(db *gorm.DB, jwtSecret string) *AuthService {
	return &AuthService{
		db:     db,
		secret: []byte(jwtSecret),
	}
}

// Claims are the JWT claims for an admin session.
type Claims struct {
	Email          string `json:"email"`
	OrganizationID string `json:"org_id"`
	Role           string `json:"role"`
	jwt.RegisteredClaims
}

// BootstrapAdmin creates the initial admin credentials if none exist.
func (a *AuthService) BootstrapAdmin(email, password, orgID string) error {
	var count int64
	a.db.Model(&AdminCredentials{}).Count(&count)
	if count > 0 {
		return nil // already bootstrapped
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
	if err := a.db.Create(admin).Error; err != nil {
		return fmt.Errorf("auth: create admin: %w", err)
	}
	return nil
}

// Login authenticates an admin and returns a JWT token.
func (a *AuthService) Login(email, password string) (string, error) {
	var admin AdminCredentials
	if err := a.db.Where("email = ?", email).First(&admin).Error; err != nil {
		return "", fmt.Errorf("auth: admin not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(password)); err != nil {
		return "", fmt.Errorf("auth: invalid credentials")
	}

	claims := &Claims{
		Email:          admin.Email,
		OrganizationID: admin.OrganizationID,
		Role:           admin.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "pccp",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(a.secret)
	if err != nil {
		return "", fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, nil
}

// VerifyToken validates a JWT token and returns the claims.
func (a *AuthService) VerifyToken(tokenStr string) (*Claims, error) {
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth: parse token: %w", err)
	}
	if !token.Valid {
		return nil, errors.New("auth: invalid token")
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
